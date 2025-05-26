package proxy

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/armon/go-socks5"
	"golang.org/x/crypto/ssh"
)

type ProxyServer struct {
	sshClient         *ssh.Client
	sshConfig         *ssh.ClientConfig
	socksServer       *socks5.Server
	httpServer        *http.Server
	listener          net.Listener
	httpListener      net.Listener
	config            *ProxyConfig
	activeConnections int32
	ctx               context.Context
	cancel            context.CancelFunc
	clientLock        sync.Mutex
	proxyType         string
	wg                sync.WaitGroup
	shutdownComplete  chan struct{}
	logChan           chan string
	logListener       net.Listener
	logServer         *http.Server
	connectionPool    chan *ssh.Client
	maxConnections    int32
}

type ProxyConfig struct {
	SSHHost     string
	SSHPort     string
	SSHUser     string
	SSHPassword string
	KeyPath     string
	LocalPort   string
	LogPath     string
	ProxyType   string
}

type trackedConn struct {
	net.Conn
	onClose func()
}

func (c *trackedConn) Close() error {
	err := c.Conn.Close()
	if c.onClose != nil {
		c.onClose()
	}
	return err
}

type timeoutConn struct {
	net.Conn
	readTimeout  time.Duration
	writeTimeout time.Duration
}

func (c *timeoutConn) Read(b []byte) (n int, err error) {
	if c.readTimeout > 0 {
		c.Conn.SetReadDeadline(time.Now().Add(c.readTimeout))
	}
	return c.Conn.Read(b)
}

func (c *timeoutConn) Write(b []byte) (n int, err error) {
	if c.writeTimeout > 0 {
		c.Conn.SetWriteDeadline(time.Now().Add(c.writeTimeout))
	}
	return c.Conn.Write(b)
}

// Фильтрованный логгер для socks5
type filteredLogger struct {
	proxy *ProxyServer
}

func (l *filteredLogger) Printf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	// Не логируем обычные сетевые ошибки
	if !isNormalNetworkError(msg) {
		l.proxy.logMessage("SOCKS5: " + msg)
	}
}

func NewProxyServer(config *ProxyConfig) (*ProxyServer, error) {
	return &ProxyServer{
		config:           config,
		proxyType:        config.ProxyType,
		shutdownComplete: make(chan struct{}),
		connectionPool:   make(chan *ssh.Client, 5),
		maxConnections:   100,
	}, nil
}

func (p *ProxyServer) Start() error {
	if err := p.setupLogging("0.0.0.0:1792"); err != nil {
		return err
	}

	var authMethods []ssh.AuthMethod
	if p.config.SSHPassword != "" {
		authMethods = append(authMethods, ssh.Password(p.config.SSHPassword))
	}
	if p.config.KeyPath != "" {
		key, err := os.ReadFile(p.config.KeyPath)
		if err != nil {
			return err
		}
		signer, err := ssh.ParsePrivateKey(key)
		if err != nil {
			return err
		}
		authMethods = append(authMethods, ssh.PublicKeys(signer))
	}

	sshConfig := &ssh.ClientConfig{
		User:            p.config.SSHUser,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         30 * time.Second,
	}

	p.sshConfig = sshConfig
	sshAddress := p.config.SSHHost + ":" + p.config.SSHPort

	client, err := ssh.Dial("tcp", sshAddress, sshConfig)
	if err != nil {
		return err
	}

	p.clientLock.Lock()
	p.sshClient = client
	p.clientLock.Unlock()

	p.ctx, p.cancel = context.WithCancel(context.Background())

	go p.maintainConnectionPool()

	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		p.monitorSSHConnection()
	}()

	listenAddr := "0.0.0.0:" + p.config.LocalPort
	if p.proxyType == "http" {
		return p.startHTTPProxy(listenAddr)
	}
	return p.startSocksProxy(listenAddr)
}

func (p *ProxyServer) maintainConnectionPool() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			for len(p.connectionPool) < 2 {
				client, err := p.createNewSSHClient()
				if err != nil {
					p.logMessage(fmt.Sprintf("Failed to maintain connection pool: %v", err))
					break
				}
				select {
				case p.connectionPool <- client:
				case <-p.ctx.Done():
					client.Close()
					return
				}
			}
		}
	}
}

func (p *ProxyServer) createNewSSHClient() (*ssh.Client, error) {
	sshAddress := p.config.SSHHost + ":" + p.config.SSHPort
	return ssh.Dial("tcp", sshAddress, p.sshConfig)
}

func (p *ProxyServer) startSocksProxy(listenAddr string) error {
	dialer := func(ctx context.Context, network, addr string) (net.Conn, error) {
		if atomic.LoadInt32(&p.activeConnections) >= p.maxConnections {
			return nil, fmt.Errorf("connection limit reached")
		}

		p.logMessage(fmt.Sprintf("SOCKS5: New connection request to %s:%s", network, addr))

		atomic.AddInt32(&p.activeConnections, 1)
		client, err := p.getConnectedSSHClient(ctx)
		if err != nil {
			atomic.AddInt32(&p.activeConnections, -1)
			if !isNetworkError(err) {
				p.logMessage(fmt.Sprintf("SOCKS5: Failed to get SSH client: %v", err))
			}
			return nil, err
		}

		dialCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()

		type dialResult struct {
			conn net.Conn
			err  error
		}
		dialChan := make(chan dialResult, 1)

		go func() {
			conn, err := client.Dial(network, addr)
			dialChan <- dialResult{conn, err}
		}()

		select {
		case <-dialCtx.Done():
			atomic.AddInt32(&p.activeConnections, -1)
			return nil, fmt.Errorf("dial timeout to %s:%s", network, addr)
		case result := <-dialChan:
			if result.err != nil {
				atomic.AddInt32(&p.activeConnections, -1)
				if !isNetworkError(result.err) {
					p.logMessage(fmt.Sprintf("SOCKS5: Failed to dial target %s:%s: %v", network, addr, result.err))
				}
				return nil, result.err
			}

			p.logMessage(fmt.Sprintf("SOCKS5: Successfully established connection to %s:%s", network, addr))

			wrappedConn := &timeoutConn{
				Conn:         result.conn,
				readTimeout:  60 * time.Second,
				writeTimeout: 30 * time.Second,
			}

			return &trackedConn{
				Conn: wrappedConn,
				onClose: func() {
					atomic.AddInt32(&p.activeConnections, -1)
					p.logMessage(fmt.Sprintf("SOCKS5: Closed connection to %s:%s", network, addr))
				},
			}, nil
		}
	}

	// Создаём фильтрованный логгер
	filteredLog := log.New(&filteredLogWriter{p}, "", log.LstdFlags)

	socksConfig := &socks5.Config{
		Dial:   dialer,
		Logger: filteredLog, // Используем стандартный log.Logger
	}

	socksServer, err := socks5.New(socksConfig)
	if err != nil {
		p.logMessage(fmt.Sprintf("Failed to create SOCKS5 server: %v", err))
		return err
	}

	p.socksServer = socksServer
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		p.logMessage(fmt.Sprintf("Failed to start listener on %s: %v", listenAddr, err))
		return err
	}

	p.listener = listener
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		if err := p.socksServer.Serve(listener); err != nil && !isClosedError(err) {
			p.logMessage(fmt.Sprintf("SOCKS5 server error: %v", err))
		}
	}()

	p.logMessage(fmt.Sprintf("SOCKS5 proxy listening on %s", listenAddr))
	return nil
}

// Обёртка для io.Writer чтобы фильтровать логи
type filteredLogWriter struct {
	proxy *ProxyServer
}

func (w *filteredLogWriter) Write(p []byte) (n int, err error) {
	msg := strings.TrimSpace(string(p))
	if !isNormalNetworkError(msg) {
		w.proxy.logMessage("SOCKS5: " + msg)
	}
	return len(p), nil
}

func (p *ProxyServer) startHTTPProxy(listenAddr string) error {
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return err
	}
	p.httpListener = listener

	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodConnect {
				p.handleHTTPSConnection(w, r)
			} else {
				p.handleHTTPConnection(w, r)
			}
		}),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	p.httpServer = server

	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		if err := server.Serve(listener); err != nil && !isClosedError(err) {
			p.logMessage(fmt.Sprintf("HTTP proxy server error: %v", err))
		}
	}()

	p.logMessage(fmt.Sprintf("HTTP proxy listening on %s", listenAddr))
	return nil
}

func (p *ProxyServer) handleHTTPConnection(w http.ResponseWriter, r *http.Request) {
	if atomic.LoadInt32(&p.activeConnections) >= p.maxConnections {
		http.Error(w, "Connection limit reached", http.StatusServiceUnavailable)
		return
	}

	p.logMessage(fmt.Sprintf("Handling HTTP connection to: %s", r.Host))

	client, err := p.getConnectedSSHClient(r.Context())
	if err != nil {
		if !isNetworkError(err) {
			p.logMessage(fmt.Sprintf("Failed to get SSH client for HTTP connection: %v", err))
		}
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	targetHost := r.Host
	if r.URL.Port() == "" {
		targetHost = targetHost + ":80"
	}

	atomic.AddInt32(&p.activeConnections, 1)
	defer atomic.AddInt32(&p.activeConnections, -1)

	conn, err := client.Dial("tcp", targetHost)
	if err != nil {
		if !isNetworkError(err) {
			p.logMessage(fmt.Sprintf("Failed to dial target host %s: %v", targetHost, err))
		}
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	defer conn.Close()

	// Добавляем таймауты
	timeoutConn := &timeoutConn{
		Conn:         conn,
		readTimeout:  30 * time.Second,
		writeTimeout: 30 * time.Second,
	}

	r.RequestURI = ""
	if err := r.Write(timeoutConn); err != nil {
		if !isNetworkError(err) {
			p.logMessage(fmt.Sprintf("Failed to write request to target: %v", err))
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp, err := http.ReadResponse(bufio.NewReader(timeoutConn), r)
	if err != nil {
		if !isNetworkError(err) {
			p.logMessage(fmt.Sprintf("Failed to read response from target: %v", err))
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(resp.StatusCode)

	copied, err := io.Copy(w, resp.Body)
	if err != nil && !isNetworkError(err) {
		p.logMessage(fmt.Sprintf("Error copying response body: %v", err))
	} else {
		p.logMessage(fmt.Sprintf("Successfully proxied HTTP connection to %s, copied %d bytes", targetHost, copied))
	}
}

func (p *ProxyServer) handleHTTPSConnection(w http.ResponseWriter, r *http.Request) {
	if atomic.LoadInt32(&p.activeConnections) >= p.maxConnections {
		http.Error(w, "Connection limit reached", http.StatusServiceUnavailable)
		return
	}

	p.logMessage(fmt.Sprintf("Handling HTTPS connection to: %s", r.Host))

	client, err := p.getConnectedSSHClient(r.Context())
	if err != nil {
		if !isNetworkError(err) {
			p.logMessage(fmt.Sprintf("Failed to get SSH client for HTTPS connection: %v", err))
		}
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	targetHost := r.Host
	if r.URL.Port() == "" {
		targetHost = targetHost + ":443"
	}

	atomic.AddInt32(&p.activeConnections, 1)

	targetConn, err := client.Dial("tcp", targetHost)
	if err != nil {
		atomic.AddInt32(&p.activeConnections, -1)
		if !isNetworkError(err) {
			p.logMessage(fmt.Sprintf("Failed to dial target host %s: %v", targetHost, err))
		}
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		atomic.AddInt32(&p.activeConnections, -1)
		targetConn.Close()
		p.logMessage("Failed to hijack connection: hijacking not supported")
		http.Error(w, "Hijacking not supported", http.StatusInternalServerError)
		return
	}

	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		atomic.AddInt32(&p.activeConnections, -1)
		targetConn.Close()
		if !isNetworkError(err) {
			p.logMessage(fmt.Sprintf("Failed to hijack connection: %v", err))
		}
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
	p.logMessage(fmt.Sprintf("HTTPS tunnel established to %s", targetHost))

	// Добавляем таймауты для туннеля
	clientTimeout := &timeoutConn{
		Conn:         clientConn,
		readTimeout:  60 * time.Second,
		writeTimeout: 30 * time.Second,
	}
	targetTimeout := &timeoutConn{
		Conn:         targetConn,
		readTimeout:  60 * time.Second,
		writeTimeout: 30 * time.Second,
	}

	p.wg.Add(2)
	go func() {
		defer p.wg.Done()
		defer atomic.AddInt32(&p.activeConnections, -1)
		defer clientTimeout.Close()
		defer targetTimeout.Close()
		copied, err := io.Copy(targetTimeout, clientTimeout)
		if err != nil && !isNetworkError(err) {
			p.logMessage(fmt.Sprintf("Error in client->target tunnel: %v", err))
		} else {
			p.logMessage(fmt.Sprintf("Client->target tunnel closed, copied %d bytes", copied))
		}
	}()
	go func() {
		defer p.wg.Done()
		defer clientTimeout.Close()
		defer targetTimeout.Close()
		copied, err := io.Copy(clientTimeout, targetTimeout)
		if err != nil && !isNetworkError(err) {
			p.logMessage(fmt.Sprintf("Error in target->client tunnel: %v", err))
		} else {
			p.logMessage(fmt.Sprintf("Target->client tunnel closed, copied %d bytes", copied))
		}
	}()
}

func (p *ProxyServer) getConnectedSSHClient(ctx context.Context) (*ssh.Client, error) {
	select {
	case client := <-p.connectionPool:
		_, _, err := client.SendRequest("keepalive@openssh.com", true, nil)
		if err == nil {
			return client, nil
		}
		client.Close()
	default:
	}

	p.clientLock.Lock()
	defer p.clientLock.Unlock()

	if p.sshClient == nil {
		return p.reconnectSSH(ctx)
	}

	_, _, err := p.sshClient.SendRequest("keepalive@openssh.com", true, nil)
	if err != nil {
		if !isNetworkError(err) {
			p.logMessage(fmt.Sprintf("SSH connection appears dead: %v. Reconnecting...", err))
		}
		p.sshClient.Close()
		p.sshClient = nil
		return p.reconnectSSH(ctx)
	}

	return p.sshClient, nil
}

func (p *ProxyServer) reconnectSSH(ctx context.Context) (*ssh.Client, error) {
	sshAddress := p.config.SSHHost + ":" + p.config.SSHPort
	var newClient *ssh.Client
	var err error

	maxRetries := 3
	for retry := 0; retry < maxRetries; retry++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		if retry > 0 {
			backoff := time.Duration(1<<uint(retry-1)) * time.Second
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}

		newClient, err = ssh.Dial("tcp", sshAddress, p.sshConfig)
		if err == nil {
			p.logMessage(fmt.Sprintf("Successfully reconnected to SSH at %s", sshAddress))
			p.sshClient = newClient
			return p.sshClient, nil
		}

		if !isNetworkError(err) {
			p.logMessage(fmt.Sprintf("Failed to reconnect SSH (attempt %d/%d): %v", retry+1, maxRetries, err))
		}
	}

	return nil, fmt.Errorf("failed to reconnect after %d attempts: %v", maxRetries, err)
}

func (p *ProxyServer) monitorSSHConnection() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			p.clientLock.Lock()
			if p.sshClient != nil {
				_, _, err := p.sshClient.SendRequest("keepalive@openssh.com", true, nil)
				if err != nil {
					if !isNetworkError(err) {
						p.logMessage(fmt.Sprintf("SSH keepalive failed: %v. Attempting to reconnect...", err))
					}
					p.sshClient.Close()
					p.sshClient = nil
					p.clientLock.Unlock()
					_, err := p.reconnectSSH(p.ctx)
					if err != nil && !isNetworkError(err) {
						p.logMessage(fmt.Sprintf("Reconnection attempt failed: %v", err))
					}
					continue
				}
			}
			p.clientLock.Unlock()
		}
	}
}

func (p *ProxyServer) Stop() error {
	if p.cancel != nil {
		p.cancel()
	}

	close(p.connectionPool)
	for client := range p.connectionPool {
		client.Close()
	}

	if p.listener != nil {
		p.listener.Close()
	}

	if p.httpListener != nil {
		p.httpListener.Close()
	}

	if p.logListener != nil {
		p.logListener.Close()
	}

	if p.httpServer != nil {
		p.httpServer.Shutdown(context.Background())
	}

	if p.logServer != nil {
		p.logServer.Shutdown(context.Background())
	}

	p.clientLock.Lock()
	if p.sshClient != nil {
		p.sshClient.Close()
	}
	p.clientLock.Unlock()

	p.wg.Wait()
	close(p.shutdownComplete)

	return nil
}

func (p *ProxyServer) setupLogging(listenAddr string) error {
	logListener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return err
	}
	p.logListener = logListener
	p.logChan = make(chan string, 100)

	mux := http.NewServeMux()
	mux.HandleFunc("/logs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
			return
		}

		for {
			select {
			case <-r.Context().Done():
				return
			case msg := <-p.logChan:
				_, err := fmt.Fprintf(w, "data: %s\n\n", msg)
				if err != nil {
					return
				}
				flusher.Flush()
			}
		}
	})

	p.logServer = &http.Server{
		Handler: mux,
	}

	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		if err := p.logServer.Serve(logListener); err != nil && !isClosedError(err) {
			p.logMessage(fmt.Sprintf("Log server error: %v", err))
		}
	}()

	return nil
}

func (p *ProxyServer) logMessage(msg string) {
	select {
	case p.logChan <- msg:
	default:
		// Channel is full, drop the message
	}
}

// Вспомогательные функции
func isNetworkError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "broken pipe") ||
		strings.Contains(errStr, "connection reset") ||
		strings.Contains(errStr, "connection refused") ||
		strings.Contains(errStr, "network is unreachable") ||
		strings.Contains(errStr, "no route to host") ||
		strings.Contains(errStr, "EOF") ||
		strings.Contains(errStr, "timeout")
}

func isNormalNetworkError(msg string) bool {
	return strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "EOF") ||
		strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "use of closed network connection")
}

func isClosedError(err error) bool {
	return err != nil && (err.Error() == "http: Server closed" ||
		strings.Contains(err.Error(), "use of closed network connection"))
}
