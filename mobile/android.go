package mobile

// import (
// 	"log"
// )

// // StartProxyWithKey запускает SSH‑прокси, используя указанный путь к приватному ключу.
// func StartProxyWithKey(key string) error {
// 	log.Println("Starting proxy with user-provided key...")
// 	// Вызываем внутреннюю функцию StartProxy, устанавливая необходимые параметры
// 	err := StartProxy(
// 		"35.193.63.104", // SSH‑хост
// 		"22",            // SSH‑порт
// 		"bg",            // SSH‑пользователь
// 		"",              // пароль (оставляем пустым, если используется ключ)
// 		key,             // путь к приватному ключу, переданный из поля ввода
// 		"1081",          // локальный порт для работы прокси
// 	)
// 	if err != nil {
// 		log.Println("Failed to start proxy:", err)
// 	}
// 	return err
// }

// // StopProxyService останавливает запущенный SSH‑прокси.
// func StopProxyService() {
// 	log.Println("Stopping proxy service...")
// 	err := StopProxy()
// 	if err != nil {
// 		log.Println("Failed to stop proxy:", err)
// 	}
// }
