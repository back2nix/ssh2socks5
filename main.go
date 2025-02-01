package main

import (
	"context"
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/armon/go-socks5"
	"golang.org/x/crypto/ssh"
)

// setupLogger configures logging to both file and stdout
func setupLogger(logPath string) *os.File {
	logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		log.Fatal("Failed to open log file:", err)
	}
	log.SetOutput(logFile)
	return logFile
}

func main() {
	// Setup logging
	logPath := filepath.Join("logs", "proxy.log")
	os.MkdirAll("logs", 0o755)
	logFile := setupLogger(logPath)
	defer logFile.Close()

	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds | log.Lshortfile)

	sshHost := flag.String("host", "", "SSH server address (required)")
	sshPort := flag.String("port", "22", "SSH server port (default 22)")
	sshUser := flag.String("user", "", "SSH username (required)")
	sshPassword := flag.String("password", "", "SSH password (used if key not provided)")
	keyPath := flag.String("key", "", "Path to SSH private key")
	localPort := flag.String("lport", "1080", "Local SOCKS5 proxy port (default 1080)")
	flag.Parse()

	log.Printf("Starting proxy with configuration - Host: %s, Port: %s, User: %s, LocalPort: %s\n",
		*sshHost, *sshPort, *sshUser, *localPort)

	if *sshHost == "" || *sshUser == "" || (*sshPassword == "" && *keyPath == "") {
		log.Fatal("Must specify host, user, and either password or key.")
	}

	var authMethods []ssh.AuthMethod
	if *sshPassword != "" {
		log.Println("Using password authentication")
		authMethods = append(authMethods, ssh.Password(*sshPassword))
	}

	if *keyPath != "" {
		log.Printf("Attempting to read private key from: %s\n", *keyPath)
		key, err := os.ReadFile(*keyPath)
		if err != nil {
			log.Fatalf("Failed to read private key: %v", err)
		}
		signer, err := ssh.ParsePrivateKey(key)
		if err != nil {
			log.Fatalf("Failed to parse private key: %v", err)
		}
		log.Println("Successfully loaded private key")
		authMethods = append(authMethods, ssh.PublicKeys(signer))
	}

	sshConfig := &ssh.ClientConfig{
		User:            *sshUser,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         30 * time.Second,
	}

	sshAddress := *sshHost + ":" + *sshPort
	log.Printf("Attempting SSH connection to %s\n", sshAddress)

	client, err := ssh.Dial("tcp", sshAddress, sshConfig)
	if err != nil {
		log.Fatalf("SSH connection error: %v", err)
	}
	defer client.Close()
	log.Printf("Successfully connected to SSH server %s\n", sshAddress)

	dialer := func(ctx context.Context, network, addr string) (net.Conn, error) {
		log.Printf("Dialing %s connection to %s\n", network, addr)
		conn, err := client.Dial(network, addr)
		if err != nil {
			log.Printf("Dial error: %v\n", err)
			return nil, err
		}
		log.Printf("Successfully established connection to %s\n", addr)
		return conn, nil
	}

	socksConfig := &socks5.Config{
		Dial:   dialer,
		Logger: log.New(logFile, "[SOCKS5] ", log.Ldate|log.Ltime|log.Lshortfile),
	}

	socksServer, err := socks5.New(socksConfig)
	if err != nil {
		log.Fatalf("Failed to create SOCKS5 server: %v", err)
	}

	listenAddr := "127.0.0.1:" + *localPort
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		log.Fatalf("Failed to start listening on %s: %v", listenAddr, err)
	}
	log.Printf("SOCKS5 proxy started listening on %s\n", listenAddr)

	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
		<-sigChan
		log.Println("Received termination signal. Stopping SOCKS5 server.")
		listener.Close()
	}()

	if err := socksServer.Serve(listener); err != nil {
		log.Printf("SOCKS5 server terminated: %v\n", err)
	}
}
