//go:build android
// +build android

package main

import (
	"log"
	"ssh2socks5/mobile"
)

// Экспортируемые функции должны начинаться с заглавной буквы
func StartProxyWithKey(key string) error {
	log.Println("Starting proxy with user-provided key...")
	return mobile.StartProxy(
		"35.193.63.104", // host
		"22",            // port
		"bg",            // user
		"",              // password
		key,             // key from text field
		"1081",          // local port
	)
}

// Экспортируйте функцию остановки
func StopProxyService() {
	mobile.StopProxy()
}
