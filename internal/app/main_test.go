package app

import (
	"context"
	"os"
	"testing"

	"daily-agent/internal/store"
)

// internalMCPEnv — тестовый бинарник с этой переменной работает как свой
// MCP-сервер приложения (как «daily-agent mcp»). Значение — путь к базе.
const internalMCPEnv = "DA_DAILY_MCP_DB"

func TestMain(m *testing.M) {
	if path := os.Getenv(internalMCPEnv); path != "" {
		db, err := store.Open(context.Background(), path)
		if err != nil {
			os.Exit(1)
		}
		NewMCPServer(db, SystemClock()).ServeStdio(context.Background(), os.Stdin, os.Stdout)
		db.Close()
		return
	}
	os.Exit(m.Run())
}
