package proxy

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
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

func (s *ProxyServer) handleClient(conn net.Conn) {
	defer conn.Close()
	log.Printf("New connection from: %s", conn.RemoteAddr())
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	version := make([]byte, 1)
	if _, err := io.ReadFull(conn, version); err != nil {
		log.Printf("Version read error: %v", err)
		return
	}
	if version[0] != 5 {
		log.Printf("Unsupported SOCKS version: %d", version[0])
		return
	}
	nmethods := make([]byte, 1)
	if _, err := io.ReadFull(conn, nmethods); err != nil {
		log.Printf("Failed to read nmethods: %v", err)
		return
	}
	methods := make([]byte, nmethods[0])
	if _, err := io.ReadFull(conn, methods); err != nil {
		log.Printf("Failed to read methods: %v", err)
		return
	}
	if _, err := conn.Write([]byte{0x05, 0x00}); err != nil {
		log.Printf("Failed to send auth response: %v", err)
		return
	}
	header := make([]byte, 4)
	if _, err := io.ReadFull(conn, header); err != nil {
		log.Printf("Failed to read request header: %v", err)
		return
	}
	if header[0] != 5 {
		log.Printf("Invalid SOCKS version in request: %d", header[0])
		return
	}
	if header[1] == 0x03 {
		dummyIP := net.ParseIP("0.0.0.0").To4()
		dummyPort := uint16(0)
		response := make([]byte, 10)
		response[0] = 0x05
		response[1] = 0x00
		response[2] = 0x00
		response[3] = 0x01
		copy(response[4:8], dummyIP)
		binary.BigEndian.PutUint16(response[8:10], dummyPort)
		if _, err := conn.Write(response); err != nil {
			log.Printf("Failed to send UDP associate response: %v", err)
			return
		}
		log.Printf("UDP associate requested but UDP forwarding is not implemented.")
		buf := make([]byte, 1)
		for {
			if _, err := conn.Read(buf); err != nil {
				return
			}
		}
	}
	var addr string
	switch header[3] {
	case 1: // IPv4
		ipv4 := make([]byte, 4)
		if _, err := io.ReadFull(conn, ipv4); err != nil {
			log.Printf("Failed to read IPv4: %v", err)
			return
		}
		addr = net.IP(ipv4).String()
	case 3: // Domain name
		lenDomain := make([]byte, 1)
		if _, err := io.ReadFull(conn, lenDomain); err != nil {
			log.Printf("Failed to read domain length: %v", err)
			return
		}
		domain := make([]byte, lenDomain[0])
		if _, err := io.ReadFull(conn, domain); err != nil {
			log.Printf("Failed to read domain: %v", err)
			return
		}
		addr = string(domain)
	case 4: // IPv6
		ipv6 := make([]byte, 16)
		if _, err := io.ReadFull(conn, ipv6); err != nil {
			log.Printf("Failed to read IPv6: %v", err)
			return
		}
		addr = net.IP(ipv6).String()
	default:
		log.Printf("Unsupported address type: %d", header[3])
		return
	}
	portBytes := make([]byte, 2)
	if _, err := io.ReadFull(conn, portBytes); err != nil {
		log.Printf("Failed to read port: %v", err)
		return
	}
	port := binary.BigEndian.Uint16(portBytes)
	target := fmt.Sprintf("%s:%d", addr, port)
	client, err := s.getConnectedSSHClient(s.ctx)
	if err != nil {
		log.Printf("Failed to get SSH client: %v", err)
		conn.Write([]byte{0x05, 0x04, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})
		return
	}
	remote, err := client.Dial("tcp", target)
	if err != nil {
		log.Printf("Failed to connect to target: %v", err)
		conn.Write([]byte{0x05, 0x04, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})
		return
	}
	defer remote.Close()
	conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})
	go func() {
		io.Copy(remote, conn)
	}()
	io.Copy(conn, remote)
}
