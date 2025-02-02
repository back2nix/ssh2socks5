package mobile

import (
	"ssh2socks5/proxy"
	"sync"
)

var (
	currentProxy *proxy.ProxyServer
	proxyLock    sync.Mutex
)

// StartProxy запускает SSH SOCKS5 прокси с заданной конфигурацией.
// Параметры:
//
//	sshHost - адрес SSH-сервера,
//	sshPort - порт SSH-сервера,
//	sshUser - имя пользователя,
//	sshPassword - пароль (если ключ не указан),
//	keyPath - путь к SSH приватному ключу,
//	localPort - локальный порт для SOCKS5 прокси.
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

// StopProxy останавливает запущенный SSH SOCKS5 прокси.
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
