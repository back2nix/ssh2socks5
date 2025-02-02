package mobile

import (
	"sync"

	"ssh2socks5/proxy"
)

var (
	currentProxy *proxy.ProxyServer
	proxyLock    sync.Mutex
)

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

	err = p.Start()
	if err != nil {
		return err
	}

	currentProxy = p
	return nil
}

func StopProxy() error {
	proxyLock.Lock()
	defer proxyLock.Unlock()

	if currentProxy != nil {
		err := currentProxy.Stop()
		currentProxy = nil
		return err
	}
	return nil
}
