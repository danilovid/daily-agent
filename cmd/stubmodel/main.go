// Команда stubmodel запускает заглушку модели отдельно от приложения, на
// 127.0.0.1. Её адрес можно ввести на экране «Настройки» → «Агент» и
// проверить смену модели без настоящей модели и без сети.
//
//	go run ./cmd/stubmodel -port 8765
package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"daily-agent/internal/agent/stubmodel"
)

func main() {
	port := flag.Int("port", 8765, "порт на 127.0.0.1")
	flag.Parse()
	srv, err := stubmodel.StartAt("127.0.0.1:" + strconv.Itoa(*port))
	if err != nil {
		fmt.Fprintln(os.Stderr, "ошибка:", err)
		os.Exit(1)
	}
	defer srv.Close()
	fmt.Printf("Заглушка модели: адрес %s, модель — любая, например %s. Остановить — Ctrl+C.\n", srv.URL, stubmodel.ModelID)
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	fmt.Printf("Запросов к заглушке: %d\n", len(srv.Requests()))
}
