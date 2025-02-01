package main

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	socks5 "golang.org/x/net/proxy"
)

func TestProxyServer(t *testing.T) {
	config := &ProxyConfig{
		SSHHost:   "35.193.63.104",
		SSHPort:   "22",
		SSHUser:   "bg",
		KeyPath:   "/home/bg/Documents/code/backup/.ssh/google-france-key",
		LocalPort: "1080",
		LogPath:   "test.log",
	}

	proxyServer, err := NewProxyServer(config)
	if err != nil {
		t.Fatalf("Failed to create proxy server: %v", err)
	}
	defer proxyServer.Stop()

	if err := proxyServer.Start(); err != nil {
		t.Fatalf("Failed to start proxy server: %v", err)
	}

	// Give the proxy server time to start
	time.Sleep(time.Second)

	proxyAddr := "127.0.0.1:" + config.LocalPort
	testURL := "http://example.com"

	t.Run("Test Proxy Connectivity", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		var d net.Dialer
		conn, err := d.DialContext(ctx, "tcp", proxyAddr)
		if err != nil {
			t.Fatalf("Cannot connect to proxy: %v", err)
		}
		conn.Close()
	})

	t.Run("Test HTTP Request", func(t *testing.T) {
		// Create a SOCKS5 dialer
		socksDialer, err := socks5.SOCKS5("tcp", proxyAddr, nil, &net.Dialer{
			Timeout: 2 * time.Second,
		})
		if err != nil {
			t.Fatalf("Failed to create SOCKS5 dialer: %v", err)
		}

		httpClient := &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
					return socksDialer.Dial(network, addr)
				},
			},
			Timeout: 2 * time.Second,
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
}
