package proxy

import (
	"bufio"
	"context"
	"io"
	"log"
	"net"
	"net/http"
	"os"
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
	proxyType         string // "socks5" or "http"
	wg                sync.WaitGroup
	shutdownComplete  chan struct{}
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

func NewProxyServer(config *ProxyConfig) (*ProxyServer, error) {
	return &ProxyServer{
		config:           config,
		proxyType:        config.ProxyType,
		shutdownComplete: make(chan struct{}),
	}, nil
}

func (p *ProxyServer) Start() error {
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

func (p *ProxyServer) startSocksProxy(listenAddr string) error {
	dialer := func(ctx context.Context, network, addr string) (net.Conn, error) {
		atomic.AddInt32(&p.activeConnections, 1)
		client, err := p.getConnectedSSHClient(ctx)
		if err != nil {
			atomic.AddInt32(&p.activeConnections, -1)
			return nil, err
		}
		conn, err := client.Dial(network, addr)
		if err != nil {
			atomic.AddInt32(&p.activeConnections, -1)
			return nil, err
		}
		return &trackedConn{
			Conn: conn,
			onClose: func() {
				atomic.AddInt32(&p.activeConnections, -1)
			},
		}, nil
	}

	socksConfig := &socks5.Config{
		Dial: dialer,
	}

	socksServer, err := socks5.New(socksConfig)
	if err != nil {
		return err
	}

	p.socksServer = socksServer
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return err
	}
	p.listener = listener

	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		if err := p.socksServer.Serve(listener); err != nil && !isClosedError(err) {
			log.Printf("SOCKS5 server error: %v", err)
		}
	}()

	log.Printf("SOCKS5 proxy listening on %s", listenAddr)
	return nil
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
	}
	p.httpServer = server

	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		if err := server.Serve(listener); err != nil && !isClosedError(err) {
			log.Printf("HTTP proxy server error: %v", err)
		}
	}()

	log.Printf("HTTP proxy listening on %s", listenAddr)
	return nil
}

func (p *ProxyServer) handleHTTPConnection(w http.ResponseWriter, r *http.Request) {
	client, err := p.getConnectedSSHClient(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	targetHost := r.Host
	if r.URL.Port() == "" {
		targetHost = targetHost + ":80"
	}

	conn, err := client.Dial("tcp", targetHost)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	defer conn.Close()

	r.RequestURI = ""
	if err := r.Write(conn); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp, err := http.ReadResponse(bufio.NewReader(conn), r)
	if err != nil {
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
	io.Copy(w, resp.Body)
}

func (p *ProxyServer) handleHTTPSConnection(w http.ResponseWriter, r *http.Request) {
	client, err := p.getConnectedSSHClient(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	targetHost := r.Host
	if r.URL.Port() == "" {
		targetHost = targetHost + ":443"
	}

	targetConn, err := client.Dial("tcp", targetHost)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "Hijacking not supported", http.StatusInternalServerError)
		return
	}

	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))

	p.wg.Add(2)
	go func() {
		defer p.wg.Done()
		defer clientConn.Close()
		defer targetConn.Close()
		io.Copy(targetConn, clientConn)
	}()
	go func() {
		defer p.wg.Done()
		defer clientConn.Close()
		defer targetConn.Close()
		io.Copy(clientConn, targetConn)
	}()
}

func (p *ProxyServer) getConnectedSSHClient(ctx context.Context) (*ssh.Client, error) {
	p.clientLock.Lock()
	defer p.clientLock.Unlock()

	if p.sshClient == nil {
		return p.reconnectSSH(ctx)
	}

	_, _, err := p.sshClient.SendRequest("keepalive@openssh.com", true, nil)
	if err != nil {
		log.Printf("SSH connection appears dead: %v. Reconnecting...", err)
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

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		newClient, err = ssh.Dial("tcp", sshAddress, p.sshConfig)
		if err == nil {
			log.Printf("Successfully reconnected to SSH at %s", sshAddress)
			p.sshClient = newClient
			return p.sshClient, nil
		}

		log.Printf("Failed to reconnect SSH: %v. Retrying in 5 seconds...", err)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
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
					log.Printf("SSH keepalive failed: %v. Attempting to reconnect...", err)
					p.sshClient.Close()
					p.sshClient = nil
					p.clientLock.Unlock()
					_, err := p.reconnectSSH(p.ctx)
					if err != nil {
						log.Printf("Reconnection attempt failed: %v", err)
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

	if p.listener != nil {
		p.listener.Close()
	}

	if p.httpListener != nil {
		p.httpListener.Close()
	}

	if p.httpServer != nil {
		p.httpServer.Shutdown(context.Background())
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

func isClosedError(err error) bool {
	return err != nil && (err.Error() == "http: Server closed" || err.Error() == "use of closed network connection")
}
