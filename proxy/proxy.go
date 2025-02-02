package proxy

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"sync/atomic"
	"time"

	"github.com/armon/go-socks5"
	"golang.org/x/crypto/ssh"
)

type ProxyServer struct {
	sshClient         *ssh.Client
	socksServer       *socks5.Server
	listener          net.Listener
	config            *ProxyConfig
	activeConnections int32
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
	sshAddress := p.config.SSHHost + ":" + p.config.SSHPort
	client, err := ssh.Dial("tcp", sshAddress, sshConfig)
	if err != nil {
		return err
	}
	p.sshClient = client

	dialer := func(ctx context.Context, network, addr string) (net.Conn, error) {
		atomic.AddInt32(&p.activeConnections, 1)
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

	// Здесь удаляем опции UDP, т.к. они больше не поддерживаются.
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

	// Запускаем SOCKS5-сервер (обрабатывает TCP-соединения)
	go func() {
		if err := p.socksServer.Serve(listener); err != nil {
			log.Printf("SOCKS5 server error: %v", err)
		}
	}()

	log.Printf("SOCKS5 proxy listening on %s", listenAddr)
	return nil
}

func (s *ProxyServer) Stop() error {
	if s.listener != nil {
		s.listener.Close()
	}
	if s.sshClient != nil {
		s.sshClient.Close()
	}
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
	// Если получен UDP Associate, возвращаем dummy-ответ
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
		// Оставляем соединение открытым
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
	remote, err := s.sshClient.Dial("tcp", target)
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
