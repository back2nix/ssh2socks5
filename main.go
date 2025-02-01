package main

import (
	"context"
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/armon/go-socks5"
	"golang.org/x/crypto/ssh"
)

// ProxyServer encapsulates the SOCKS5 proxy functionality
type ProxyServer struct {
	sshClient         *ssh.Client
	socksServer       *socks5.Server
	listener          net.Listener
	logger            *log.Logger
	logFile           *os.File
	config            *ProxyConfig
	activeConnections int32
}

// ProxyConfig holds all configuration parameters
type ProxyConfig struct {
	SSHHost     string
	SSHPort     string
	SSHUser     string
	SSHPassword string
	KeyPath     string
	LocalPort   string
	LogPath     string
}

// trackedConn wraps a net.Conn to track connection closure
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
	if err := os.MkdirAll("logs", 0o755); err != nil {
		return nil, err
	}

	logFile, err := os.OpenFile(config.LogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}

	logger := log.New(logFile, "", log.Ldate|log.Ltime|log.Lmicroseconds|log.Lshortfile)
	logger.Printf("Creating new proxy server with config: %+v", *config)

	return &ProxyServer{
		logger:  logger,
		logFile: logFile,
		config:  config,
	}, nil
}

func (p *ProxyServer) Start() error {
	p.logger.Printf("Starting proxy server")

	var authMethods []ssh.AuthMethod
	if p.config.SSHPassword != "" {
		p.logger.Printf("Using password authentication")
		authMethods = append(authMethods, ssh.Password(p.config.SSHPassword))
	}
	if p.config.KeyPath != "" {
		p.logger.Printf("Reading SSH key from: %s", p.config.KeyPath)
		key, err := os.ReadFile(p.config.KeyPath)
		if err != nil {
			p.logger.Printf("Failed to read SSH key: %v", err)
			return err
		}
		signer, err := ssh.ParsePrivateKey(key)
		if err != nil {
			p.logger.Printf("Failed to parse SSH key: %v", err)
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

	p.logger.Printf("Attempting SSH connection to %s:%s as user %s",
		p.config.SSHHost, p.config.SSHPort, p.config.SSHUser)

	sshAddress := p.config.SSHHost + ":" + p.config.SSHPort
	client, err := ssh.Dial("tcp", sshAddress, sshConfig)
	if err != nil {
		p.logger.Printf("SSH connection failed: %v", err)
		return err
	}
	p.logger.Printf("SSH connection established successfully")
	p.sshClient = client

	dialer := func(ctx context.Context, network, addr string) (net.Conn, error) {
		atomic.AddInt32(&p.activeConnections, 1)
		p.logger.Printf("SOCKS5 dialing %s %s (Active connections: %d)",
			network, addr, atomic.LoadInt32(&p.activeConnections))

		conn, err := client.Dial(network, addr)
		if err != nil {
			atomic.AddInt32(&p.activeConnections, -1)
			p.logger.Printf("SOCKS5 dial error: %v", err)
			return nil, err
		}
		p.logger.Printf("SOCKS5 connection established to %s", addr)

		return &trackedConn{
			Conn: conn,
			onClose: func() {
				atomic.AddInt32(&p.activeConnections, -1)
				p.logger.Printf("Connection closed. Active connections: %d",
					atomic.LoadInt32(&p.activeConnections))
			},
		}, nil
	}

	socksConfig := &socks5.Config{
		Dial:   dialer,
		Logger: log.New(p.logFile, "[SOCKS5] ", log.Ldate|log.Ltime|log.Lshortfile),
	}

	socksServer, err := socks5.New(socksConfig)
	if err != nil {
		p.logger.Printf("Failed to create SOCKS5 server: %v", err)
		return err
	}
	p.logger.Printf("SOCKS5 server created successfully")
	p.socksServer = socksServer

	listenAddr := "127.0.0.1:" + p.config.LocalPort
	p.logger.Printf("Starting listener on %s", listenAddr)
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		p.logger.Printf("Failed to start listener: %v", err)
		return err
	}
	p.logger.Printf("Listener started successfully")
	p.listener = listener

	// Start periodic status logging
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		for {
			select {
			case <-ticker.C:
				p.logger.Printf("Status: Active connections: %d, SSH connected: %v",
					atomic.LoadInt32(&p.activeConnections),
					p.sshClient != nil)
			}
		}
	}()

	go func() {
		p.logger.Printf("Starting SOCKS5 server")
		if err := p.socksServer.Serve(listener); err != nil {
			p.logger.Printf("SOCKS5 server error: %v", err)
		}
	}()

	return nil
}

func (p *ProxyServer) Stop() error {
	p.logger.Printf("Stopping proxy server")

	if p.listener != nil {
		p.logger.Printf("Closing listener")
		p.listener.Close()
	}
	if p.sshClient != nil {
		p.logger.Printf("Closing SSH client")
		p.sshClient.Close()
	}
	if p.logFile != nil {
		p.logger.Printf("Closing log file")
		p.logFile.Close()
	}
	return nil
}

func main() {
	sshHost := flag.String("host", "", "SSH server address (required)")
	sshPort := flag.String("port", "22", "SSH server port (default 22)")
	sshUser := flag.String("user", "", "SSH username (required)")
	sshPassword := flag.String("password", "", "SSH password (used if key not provided)")
	keyPath := flag.String("key", "", "Path to SSH private key")
	localPort := flag.String("lport", "1080", "Local SOCKS5 proxy port (default 1080)")

	flag.Parse()

	config := &ProxyConfig{
		SSHHost:     *sshHost,
		SSHPort:     *sshPort,
		SSHUser:     *sshUser,
		SSHPassword: *sshPassword,
		KeyPath:     *keyPath,
		LocalPort:   *localPort,
		LogPath:     filepath.Join("logs", "proxy.log"),
	}

	if config.SSHHost == "" || config.SSHUser == "" || (config.SSHPassword == "" && config.KeyPath == "") {
		log.Fatal("Must specify host, user, and either password or key.")
	}

	proxy, err := NewProxyServer(config)
	if err != nil {
		log.Fatal(err)
	}

	if err := proxy.Start(); err != nil {
		log.Fatal(err)
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	if err := proxy.Stop(); err != nil {
		log.Printf("Error stopping proxy: %v", err)
	}
}

