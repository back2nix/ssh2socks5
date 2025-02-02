package mobile

import (
	"sync"

	"ssh2socks5/proxy"
)

var (
	currentProxy *proxy.ProxyServer
	proxyLock    sync.Mutex
)

// StartProxy starts the SOCKS5 proxy server
func StartProxy(sshHost, sshPort, sshUser, sshPassword, keyPath, localPort string) error {
	proxyLock.Lock()
	defer proxyLock.Unlock()

	config := &proxy.ProxyConfig{
		SSHHost:     sshHost,
		SSHPort:     sshPort,
		SSHUser:     sshUser,
		SSHPassword: sshPassword,
		KeyPath:     keyPath,
		LocalPort:   localPort,
		LogPath:     "logs/proxy.log",
	}

	p, err := proxy.NewProxyServer(config)
	if err != nil {
		return err
	}

	if err := p.Start(); err != nil {
		return err
	}

	currentProxy = p
	return nil
}

// StopProxy stops the SOCKS5 proxy server
func StopProxy() error {
	proxyLock.Lock()
	defer proxyLock.Unlock()

	if currentProxy != nil {
		if err := currentProxy.Stop(); err != nil {
			return err
		}
		currentProxy = nil
	}
	return nil
}
