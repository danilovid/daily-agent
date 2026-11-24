// Команда agentcheck проверяет связь с агентом: поднимает OpenCode с профилем
// приложения во временной папке, отправляет тестовый запрос и печатает только
// цифры — время запуска, время ответа, итог и длину ответа. Текст ответа не
// выводится и никуда не передаётся.
//
//	go run ./cmd/agentcheck -stub        с заглушкой модели, без сети
//	go run ./cmd/agentcheck -n 10        с моделью для рабочих задач (запускает автор)
//	go run ./cmd/agentcheck -personal    с моделью для личных задач
//
// Настоящие модели берутся из настроек приложения («Настройки» → «Агент»)
// так же, как в приложении. Ключи — из Связки ключей (служба «Daily Agent»).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"daily-agent/internal/agent"
	"daily-agent/internal/agent/stubmodel"
	"daily-agent/internal/app"
	"daily-agent/internal/store"
)

const prompt = "Ответь одним словом: готов."

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "ошибка:", err)
		os.Exit(1)
	}
}

func run() error {
	useStub := flag.Bool("stub", false, "заглушка модели вместо настоящей")
	n := flag.Int("n", 1, "сколько запросов отправить")
	timeout := flag.Duration("timeout", 2*time.Minute, "предельное время одного ответа")
	personal := flag.Bool("personal", false, "проверить модель для личных задач, а не для рабочих")
	flag.Parse()
	if *n < 1 || *n > 100 {
		return errors.New("-n должно быть от 1 до 100")
	}

	ctx := context.Background()
	var setup *agent.Setup
	if *useStub {
		stub, err := stubmodel.Start()
		if err != nil {
			return err
		}
		defer stub.Close()
		setup = agent.Model{BaseURL: stub.URL, ModelID: stubmodel.ModelID}.Setup()
	} else {
		s, err := savedSetup(ctx)
		if err != nil {
			return err
		}
		setup = s
	}

	// Отдельная временная папка: проверка не мешает запущенному приложению.
	root, err := os.MkdirTemp("", "da-check-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	profile := agent.Profile{Root: root, Path: agent.LoginPath(), Inherit: agent.ShellEnv()}

	began := time.Now()
	sup := agent.NewManager(agent.Config{Profile: profile}, setup)
	model := sup.ModelFor(*personal)
	if model == nil {
		return errors.New("модель не выбрана: задайте её в приложении («Настройки» → «Агент») или запустите с -stub")
	}
	sup.Start(ctx)
	defer sup.Stop()
	readyCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	if err := sup.WaitReady(readyCtx); err != nil {
		return err
	}
	fmt.Printf("OpenCode %s, запуск: %.1f с\n", agent.PinnedVersion, time.Since(began).Seconds())

	client, err := sup.Client()
	if err != nil {
		return err
	}
	ok := 0
	var total time.Duration
	for i := 1; i <= *n; i++ {
		askCtx, cancel := context.WithTimeout(ctx, *timeout)
		reply, err := client.Ask(askCtx, agent.SessionOptions{
			Title:     "Проверка связи",
			Directory: profile.Workspace(),
			Model:     model,
		}, prompt)
		cancel()
		passed := err == nil && reply.Text != ""
		if passed {
			ok++
			total += reply.Duration
		}
		fmt.Printf("запрос %d: %.2f с, итог: %s, символов в ответе: %d\n",
			i, reply.Duration.Seconds(), verdict(passed), len([]rune(reply.Text)))
	}
	avg := 0.0
	if ok > 0 {
		avg = total.Seconds() / float64(ok)
	}
	fmt.Printf("итого: %d из %d, среднее время ответа: %.2f с\n", ok, *n, avg)
	if ok != *n {
		return fmt.Errorf("ответили %d из %d", ok, *n)
	}
	return nil
}

// savedSetup берёт модели так же, как приложение: из его настроек, провайдеров
// из настроек OpenCode пользователя и Связки ключей. Из базы читаются только
// настройки моделей.
func savedSetup(ctx context.Context) (*agent.Setup, error) {
	path, err := store.DefaultPath()
	if err != nil {
		return nil, err
	}
	db, err := store.Open(ctx, path)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	user, err := agent.LoadUserConfig()
	if err != nil {
		return nil, err
	}
	return app.LoadSetup(ctx, db, agent.Keychain{Path: os.Getenv(agent.EnvKeychainPath)}, user)
}

func verdict(ok bool) string {
	if ok {
		return "ок"
	}
	return "ошибка"
}
