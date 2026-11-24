package main

import (
	"context"
	"embed"
	"log"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v3/pkg/application"

	"daily-agent/internal/agent"
	"daily-agent/internal/agent/stubmodel"
	"daily-agent/internal/app"
	"daily-agent/internal/store"
)

// Собранный интерфейс из frontend/dist встраивается в программу.
//
//go:embed all:frontend/dist
var assets embed.FS

// EnvStubModel — запуск с заглушкой модели вместо настоящей. Для разработки.
const EnvStubModel = "DAILY_AGENT_STUB_MODEL"

func init() {
	application.RegisterEvent[app.AgentStatusView](app.AgentStatusEvent)
	application.RegisterEvent[app.ChatEvent](app.ChatEventName)
}

func main() {
	ctx := context.Background()

	dbPath, err := store.DefaultPath()
	if err != nil {
		log.Fatal(err)
	}
	db, err := store.Open(ctx, dbPath)
	if err != nil {
		log.Fatalf("база: %v", err)
	}
	defer db.Close()

	clock := app.SystemClock()
	// Ссылки задач и встреч открывает системный браузер. Само приложение по ним не ходит.
	openURL := func(u string) error { return application.Get().Browser.OpenURL(u) }

	// OpenCode агента живёт рядом с базой приложения, в своих папках.
	keychain := agent.Keychain{Path: os.Getenv(agent.EnvKeychainPath)}
	stub := os.Getenv(EnvStubModel) == "1"
	backend, stopModel := newAgent(ctx, filepath.Join(filepath.Dir(dbPath), "agent"), db, keychain, stub)
	defer stopModel()

	// Окно ещё не создано, поэтому события чата уходят через переменную.
	var emitChat func(app.ChatEvent)
	today := app.NewTodayService(db, clock)
	chats := app.NewChatsService(db, clock, backend, today, func(e app.ChatEvent) {
		if emitChat != nil {
			emitChat(e)
		}
	})
	eventsCtx, stopEvents := context.WithCancel(ctx)

	wails := application.New(application.Options{
		Name:        "Daily Agent",
		Description: "Личный рабочий помощник",
		Services: []application.Service{
			application.NewService(today),
			application.NewService(app.NewSidebarService(db, clock)),
			application.NewService(app.NewMeetingsService(db, clock)),
			application.NewService(app.NewTasksService(db, clock)),
			application.NewService(app.NewTagsService(db)),
			application.NewService(app.NewSearchService(db, clock)),
			application.NewService(app.NewLinksService(db, openURL)),
			application.NewService(app.NewAgentService(backend.Status, clock)),
			application.NewService(chats),
			application.NewService(app.NewSettingsService(db, keychain, backend, clock, stub)),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
		// На macOS Run() не возвращается после выхода, отложенные вызовы не
		// выполняются. OpenCode останавливается здесь: выход ждёт этой функции.
		OnShutdown: func() {
			stopEvents()
			backend.Stop()
			stopModel()
		},
	})

	backend.OnChange(func(st agent.Status) {
		wails.Event.Emit(app.AgentStatusEvent, app.AgentStatusFrom(st, clock))
	})
	// При перезапуске OpenCode, например после смены модели, ответы обрываются.
	backend.OnChange(app.ChatStatusHandler(chats))
	emitChat = func(e app.ChatEvent) { wails.Event.Emit(app.ChatEventName, e) }
	// Приложение работает и без агента: свои задачи и встречи не ждут OpenCode.
	backend.Start(ctx)
	defer backend.Stop()
	// Ответы агента приходят потоком событий OpenCode.
	go agent.RunEvents(eventsCtx, backend.Client, app.ChatEventHandler(chats))
	defer stopEvents()

	// Окно как в макетах: 1100 × 860, тёмное, без видимого заголовка.
	// Кнопки окна стоят по центру верхней полосы высотой 52 pt над боковой панелью.
	wails.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "main",
		Title:            "Daily Agent",
		Width:            1100,
		Height:           860,
		MinWidth:         1000,
		MinHeight:        720,
		BackgroundColour: application.NewRGB(0x1E, 0x1E, 0x20),
		URL:              "/",
		Mac: application.MacWindow{
			TitleBar:   application.MacTitleBarHiddenInsetUnified,
			Appearance: application.NSAppearanceNameDarkAqua,
			Backdrop:   application.MacBackdropNormal,
		},
	})

	if err := wails.Run(); err != nil {
		backend.Stop()
		db.Close()
		log.Fatal(err)
	}
}

// newAgent готовит агента. Модель: заглушка при DAILY_AGENT_STUB_MODEL=1,
// иначе настройки приложения, а без них — переменные окружения. Ключ — из
// Связки ключей. Без модели OpenCode запускается, но агент не ответит.
func newAgent(ctx context.Context, root string, db *store.Store, keychain agent.Keychain, stub bool) (*agent.Manager, func()) {
	cfg := agent.Config{Profile: agent.Profile{Root: root}, Logf: log.Printf}
	var model *agent.Model
	stop := func() {}
	if stub {
		srv, err := stubmodel.Start()
		if err != nil {
			log.Printf("заглушка модели: %v", err)
		} else {
			model = &agent.Model{BaseURL: srv.URL, ModelID: stubmodel.ModelID}
			stop = func() { srv.Close() }
			log.Printf("агент работает с заглушкой модели")
		}
	} else if m, _, err := app.LoadModel(ctx, db, keychain); err != nil {
		log.Printf("модель агента: %v", err)
	} else {
		model = m
	}
	return agent.NewManager(cfg, model), stop
}
