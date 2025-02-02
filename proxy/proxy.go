package proxy

import (
	"context"
	"log"
	"net"
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
	listener          net.Listener
	config            *ProxyConfig
	activeConnections int32
	ctx               context.Context
	cancel            context.CancelFunc
	clientLock        sync.Mutex
}

type ProxyConfig struct {
	SSHHost     string
	SSHPort     string
	SSHUser     string
	SSHPassword string
	KeyPath     string
	LocalPort   string
	LogPath     string
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
		config: config,
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
	// set the initial SSH connection
	p.clientLock.Lock()
	p.sshClient = client
	p.clientLock.Unlock()

	// Set up context for graceful shutdown and reconnection loop.
	p.ctx, p.cancel = context.WithCancel(context.Background())
	go p.monitorSSHConnection()

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
	listenAddr := "0.0.0.0:" + p.config.LocalPort
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return err
	}
	p.listener = listener
	go func() {
		if err := p.socksServer.Serve(listener); err != nil {
			log.Printf("SOCKS5 server error: %v", err)
		}
	}()
	log.Printf("SOCKS5 proxy listening on %s", listenAddr)
	return nil
}

// getConnectedSSHClient ensures that the SSH client is alive and reconnects if needed.
func (p *ProxyServer) getConnectedSSHClient(ctx context.Context) (*ssh.Client, error) {
	p.clientLock.Lock()
	defer p.clientLock.Unlock()
	if p.sshClient == nil {
		return p.reconnectSSH(ctx)
	}
	// Use a keepalive request to check the connection.
	_, _, err := p.sshClient.SendRequest("keepalive@openssh.com", true, nil)
	if err != nil {
		log.Printf("SSH connection appears dead: %v. Reconnecting...", err)
		p.sshClient.Close()
		p.sshClient = nil
		return p.reconnectSSH(ctx)
	}
	return p.sshClient, nil
}

// reconnectSSH attempts to reconnect using the stored SSH configuration.
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

// monitorSSHConnection periodically sends keepalive requests and reconnects if necessary.
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
					// Reconnect outside the lock.
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

func (s *ProxyServer) Stop() error {
	if s.cancel != nil {
		s.cancel()
	}
	if s.listener != nil {
		s.listener.Close()
	}
	s.clientLock.Lock()
	if s.sshClient != nil {
		s.sshClient.Close()
	}
	s.clientLock.Unlock()
	return nil
}
