// Команда devdata наполняет базу приложения вымышленными тестовыми данными
// или очищает её. Печатает только числа: сколько записей в каких таблицах.
//
//	go run ./cmd/devdata seed  [-db путь]
//	go run ./cmd/devdata reset -yes [-db путь]
//
// Без -db команда работает с базой приложения: значение DAILY_AGENT_DB или
// ~/Library/Application Support/Daily Agent/daily-agent.db.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"time"

	"daily-agent/internal/devdata"
	"daily-agent/internal/store"
)

const usage = `Использование:
  go run ./cmd/devdata seed  [-db путь]       наполнить пустую базу тестовыми данными
  go run ./cmd/devdata reset -yes [-db путь]  удалить все записи (схема остаётся)
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "ошибка:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return errors.New("не указана команда")
	}
	cmd, rest := args[0], args[1:]

	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	dbPath := fs.String("db", "", "путь к файлу базы")
	yes := fs.Bool("yes", false, "подтвердить очистку")
	if err := fs.Parse(rest); err != nil {
		return err
	}

	path := *dbPath
	if path == "" {
		p, err := store.DefaultPath()
		if err != nil {
			return err
		}
		path = p
	}

	ctx := context.Background()
	switch cmd {
	case "seed":
		s, err := store.Open(ctx, path)
		if err != nil {
			return err
		}
		defer s.Close()
		counts, err := devdata.Seed(ctx, s, time.Now())
		if err != nil {
			return err
		}
		printCounts("добавлено записей", counts)
	case "reset":
		if !*yes {
			return errors.New("очистка удаляет все записи; добавьте -yes, чтобы подтвердить")
		}
		s, err := store.Open(ctx, path)
		if err != nil {
			return err
		}
		defer s.Close()
		counts, err := devdata.Reset(ctx, s)
		if err != nil {
			return err
		}
		printCounts("удалено записей", counts)
	default:
		fmt.Fprint(os.Stderr, usage)
		return fmt.Errorf("неизвестная команда %q", cmd)
	}
	return nil
}

func printCounts(title string, c devdata.Counts) {
	names := make([]string, 0, len(c))
	for name := range c {
		names = append(names, name)
	}
	sort.Strings(names)
	fmt.Printf("%s: %d\n", title, c.Total())
	for _, name := range names {
		if c[name] > 0 {
			fmt.Printf("  %-14s %d\n", name, c[name])
		}
	}
}
