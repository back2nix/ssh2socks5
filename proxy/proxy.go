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
	socksConfig := &socks5.Config{
		Dial: dialer,
	}
	socksServer, err := socks5.New(socksConfig)
	if err != nil {
		return err
	}
	p.socksServer = socksServer
	listenAddr := "127.0.0.1:" + p.config.LocalPort
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return err
	}
	p.listener = listener
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			conn, err := listener.Accept()
			if err != nil {
				log.Printf("Failed to accept connection: %v", err)
				continue
			}
			go p.handleClient(conn) // Добавьте эту строку
		}
	}()
	go func() {
		if err := p.socksServer.Serve(listener); err != nil {
		}
	}()
	return nil
}

func (p *ProxyServer) Stop() error {
	if p.listener != nil {
		p.listener.Close()
	}
	if p.sshClient != nil {
		p.sshClient.Close()
	}
	return nil
}

func (s *ProxyServer) handleClient(conn net.Conn) {
	defer conn.Close()

	log.Printf("New connection from: %s", conn.RemoteAddr())

	// Установим таймаут на чтение
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))

	// 1. Чтение версии протокола
	version := make([]byte, 1)
	if _, err := io.ReadFull(conn, version); err != nil {
		log.Printf("Version read error: %v", err)
		return
	}
	log.Printf("SOCKS version received: %d", version[0])

	// 2. Проверка версии
	if version[0] != 5 {
		log.Printf("Unsupported SOCKS version: %d", version[0])
		return
	}

	// 3. Чтение методов аутентификации
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

	// 4. Отправка ответа о методе аутентификации (без аутентификации)
	if _, err := conn.Write([]byte{0x05, 0x00}); err != nil {
		log.Printf("Failed to send auth response: %v", err)
		return
	}

	// 5. Чтение запроса
	// - версия протокола (1 байт)
	// - команда (1 байт)
	// - зарезервированный байт (1 байт)
	// - тип адреса (1 байт)
	header := make([]byte, 4)
	if _, err := io.ReadFull(conn, header); err != nil {
		log.Printf("Failed to read request header: %v", err)
		return
	}

	// Проверка версии в запросе
	if header[0] != 5 {
		log.Printf("Invalid SOCKS version in request: %d", header[0])
		return
	}

	// 6. Чтение адреса
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

	// 7. Чтение порта
	portBytes := make([]byte, 2)
	if _, err := io.ReadFull(conn, portBytes); err != nil {
		log.Printf("Failed to read port: %v", err)
		return
	}
	port := binary.BigEndian.Uint16(portBytes)

	log.Printf("Received request for: %s:%d", addr, port)

	// 8. Установка соединения через SSH туннель
	target := fmt.Sprintf("%s:%d", addr, port)
	remote, err := s.sshClient.Dial("tcp", target)
	if err != nil {
		log.Printf("Failed to connect to target: %v", err)
		// Отправка ответа об ошибке
		conn.Write([]byte{0x05, 0x04, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})
		return
	}
	defer remote.Close()

	// 9. Отправка успешного ответа
	conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})

	// 10. Проксирование данных
	go func() {
		io.Copy(remote, conn)
	}()
	io.Copy(conn, remote)
}
