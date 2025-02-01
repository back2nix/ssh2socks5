package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/net/proxy"
)

const (
	proxyPort = "1081"
	sshHost   = "35.193.63.104"
	sshUser   = "bg"
	keyPath   = "/home/bg/Documents/code/backup/.ssh/google-france-key"
)

func setupSSHClient(t *testing.T) *ssh.Client {
	// Read private key
	key, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("Failed to read private key: %v", err)
	}

	// Parse private key
	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		t.Fatalf("Failed to parse private key: %v", err)
	}

	config := &ssh.ClientConfig{
		User: sshUser,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         30 * time.Second,
	}

	// Connect to SSH server
	client, err := ssh.Dial("tcp", fmt.Sprintf("%s:22", sshHost), config)
	if err != nil {
		t.Fatalf("Failed to connect to SSH server: %v", err)
	}

	return client
}

func TestSocks5Proxy(t *testing.T) {
	proxyAddr := "127.0.0.1:" + proxyPort
	testURL := "http://example.com"

	// Test basic connectivity to proxy
	t.Run("Test Proxy Connectivity", func(t *testing.T) {
		conn, err := net.DialTimeout("tcp", proxyAddr, 5*time.Second)
		if err != nil {
			t.Fatalf("Cannot connect to proxy: %v", err)
		}
		defer conn.Close()
	})

	// Test HTTP request through proxy
	t.Run("Test HTTP Request", func(t *testing.T) {
		dialer, err := proxy.SOCKS5("tcp", proxyAddr, nil, proxy.Direct)
		if err != nil {
			t.Fatalf("Failed to create SOCKS5 dialer: %v", err)
		}

		httpClient := &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
					return dialer.Dial(network, addr)
				},
			},
			Timeout: 30 * time.Second,
		}

		resp, err := httpClient.Get(testURL)
		if err != nil {
			t.Fatalf("Failed to connect through proxy: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}
	})

	// Test SSH connection with key authentication
	t.Run("Test SSH Connection", func(t *testing.T) {
		client := setupSSHClient(t)
		defer client.Close()

		session, err := client.NewSession()
		if err != nil {
			t.Fatalf("Failed to create SSH session: %v", err)
		}
		defer session.Close()

		output, err := session.Output("echo 'Test connection successful'")
		if err != nil {
			t.Fatalf("Failed to execute command: %v", err)
		}

		t.Logf("SSH command output: %s", string(output))
	})
}
