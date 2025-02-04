//go:build !android
// +build !android

package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"ssh2socks5/proxy"
)

func main() {
	sshHost := flag.String("host", "", "SSH server address (required)")
	sshPort := flag.String("port", "22", "SSH server port (default 22)")
	sshUser := flag.String("user", "", "SSH username (required)")
	sshPassword := flag.String("password", "", "SSH password (used if key not provided)")
	keyPath := flag.String("key", "", "Path to SSH private key")
	localPort := flag.String("lport", "1080", "Local SOCKS5 proxy port (default 1080)")
	proxyType := flag.String("proxyType", "socks5", "Local SOCKS5 proxy port (default 1080)")
	flag.Parse()

	config := &proxy.ProxyConfig{
		SSHHost:     *sshHost,
		SSHPort:     *sshPort,
		SSHUser:     *sshUser,
		SSHPassword: *sshPassword,
		KeyPath:     *keyPath,
		LocalPort:   *localPort,
		LogPath:     filepath.Join("logs", "proxy.log"),
		ProxyType:   *proxyType,
	}

	if config.SSHHost == "" || config.SSHUser == "" || (config.SSHPassword == "" && config.KeyPath == "") {
		log.Fatal("Must specify host, user, and either password or key.")
	}

	proxyServer, err := proxy.NewProxyServer(config)
	if err != nil {
		log.Fatal(err)
	}

	if err := proxyServer.Start(); err != nil {
		log.Printf("SSH connection error: %v", err)
		return
	}

	log.Printf("Proxy listening on :%s", config.LocalPort)
	log.Printf("SSH connection established to %s:%s", config.SSHHost, config.SSHPort)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	if err := proxyServer.Stop(); err != nil {
		log.Printf("Error stopping proxy: %v", err)
	}
}
