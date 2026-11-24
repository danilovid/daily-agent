// Команда mcpstub запускает заглушку MCP-сервера — вымышленный трекер с
// инструментами get_issue, list_issues и create_issue. Нужна, чтобы проверить
// раздел «MCP-серверы» без настоящих рабочих систем и без сети.
//
//	go run ./cmd/mcpstub -port 8770          удалённый сервер на 127.0.0.1
//	go run ./cmd/mcpstub -port 8771 -oauth   он же со входом через браузер
//	go run ./cmd/mcpstub -stdio              локальный сервер (stdin и stdout)
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"daily-agent/internal/agent/mcpstub"
)

func main() {
	port := flag.Int("port", 8770, "порт на 127.0.0.1")
	oauth := flag.Bool("oauth", false, "требовать вход через браузер")
	stdio := flag.Bool("stdio", false, "локальный сервер через stdin и stdout")
	flag.Parse()
	if *stdio {
		if err := mcpstub.ServeStdio(os.Stdin, os.Stdout, io.Discard, nil); err != nil {
			os.Exit(1)
		}
		return
	}
	srv, err := mcpstub.StartHTTPAt("127.0.0.1:"+strconv.Itoa(*port), *oauth)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ошибка:", err)
		os.Exit(1)
	}
	defer srv.Close()
	fmt.Printf("Заглушка MCP: адрес %s, вход через браузер: %v. Остановить — Ctrl+C.\n", srv.URL, *oauth)
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	fmt.Printf("Вызовов инструментов: %d\n", len(srv.Calls()))
}
