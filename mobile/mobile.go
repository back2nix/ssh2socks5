package mobile

import (
	"sync"

	"ssh2socks5/proxy"
)

var (
	currentProxy *proxy.ProxyServer
	proxyLock    sync.Mutex
)

func StartProxy(sshHost, sshPort, sshUser, sshPassword, keyPath, localPort, proxyType string) error {
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
		ProxyType:   proxyType,
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
