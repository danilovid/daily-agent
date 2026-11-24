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
	application.RegisterEvent[app.DataChanged](app.DataChangedEvent)
}

func main() {
	// «daily-agent mcp» — свой MCP-сервер приложения через stdin и stdout,
	// без окна. Его запускает OpenCode агента или внешний агент.
	if len(os.Args) > 1 && os.Args[1] == "mcp" {
		os.Exit(runMCP())
	}
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
	backend, stopModel := newAgent(ctx, filepath.Join(filepath.Dir(dbPath), "agent"), db, dbPath, keychain, stub)
	// Провайдеры моделей и MCP-серверы из настроек OpenCode пользователя:
	// только описания, без ключей, токенов, плагинов и прав.
	userConfig := agent.LoadUserConfig
	defer stopModel()

	// Окно ещё не создано, поэтому события чата уходят через переменную.
	var emitChat func(app.ChatEvent)
	today := app.NewTodayService(db, clock)
	chats := app.NewChatsService(db, clock, backend, today, func(e app.ChatEvent) {
		if emitChat != nil {
			emitChat(e)
		}
	})
	settings := app.NewSettingsService(db, keychain, backend, clock, userConfig, stub)
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
			application.NewService(settings),
			// Вход на MCP-сервер открывается в системном браузере.
			application.NewService(app.NewMCPService(settings, backend, openURL, internalCommand())),
			application.NewService(app.NewDayPlanService(db, clock)),
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
	// План и задачи могут записать другие процессы: свой MCP-сервер, внешний
	// агент. Окно узнаёт об этом по признаку изменений базы.
	go app.WatchData(eventsCtx, db, func() { wails.Event.Emit(app.DataChangedEvent, app.DataChanged{}) })
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

// newAgent готовит агента. Модели: заглушка при DAILY_AGENT_STUB_MODEL=1,
// иначе настройки приложения: своя модель по адресу, провайдеры из настроек
// OpenCode пользователя и облачные провайдеры каталога. Ключи — из Связки
// ключей. Без модели OpenCode запускается, но агент не ответит.
func newAgent(ctx context.Context, root string, db *store.Store, dbPath string, keychain agent.Keychain, stub bool) (*agent.Manager, func()) {
	// PATH и прокси — из оболочки входа: приложение из Finder их не получает,
	// а локальные MCP-серверы вроде «npx -y …» без них не запускаются.
	profile := agent.Profile{Root: root, Path: agent.LoginPath(), Inherit: agent.ShellEnv()}
	cfg := agent.Config{Profile: profile, Logf: log.Printf}
	// Свой MCP-сервер: та же программа с подкомандой mcp и та же база.
	if command := internalCommand(); command != nil {
		cfg.Internal = &agent.InternalMCP{
			Name: app.InternalMCPServerName,
			Definition: map[string]any{"type": "local", "command": command,
				"environment": map[string]any{store.EnvPath: dbPath}},
			Tools: app.InternalMCPTools,
		}
	}
	var setup *agent.Setup
	stop := func() {}
	if stub {
		srv, err := stubmodel.Start()
		if err != nil {
			log.Printf("заглушка модели: %v", err)
		} else {
			setup = agent.Model{BaseURL: srv.URL, ModelID: stubmodel.ModelID}.Setup()
			stop = func() { srv.Close() }
			log.Printf("агент работает с заглушкой модели")
		}
	} else {
		user, err := agent.LoadUserConfig()
		if err != nil {
			log.Printf("настройки OpenCode: %v", err)
		}
		if setup, err = app.LoadSetup(ctx, db, keychain, user); err != nil {
			log.Printf("модели агента: %v", err)
		}
	}
	return agent.NewManager(cfg, setup), stop
}

// internalCommand — команда своего MCP-сервера: эта программа и «mcp».
func internalCommand() []string {
	exe, err := os.Executable()
	if err != nil {
		return nil
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	return []string{exe, "mcp"}
}

// runMCP — свой MCP-сервер через stdin и stdout. Журнал — только в stderr:
// stdout занят протоколом.
func runMCP() int {
	log.SetOutput(os.Stderr)
	ctx := context.Background()
	path, err := store.DefaultPath()
	if err != nil {
		log.Print(err)
		return 1
	}
	db, err := store.Open(ctx, path)
	if err != nil {
		log.Printf("база: %v", err)
		return 1
	}
	defer db.Close()
	if err := app.NewMCPServer(db, app.SystemClock()).ServeStdio(ctx, os.Stdin, os.Stdout); err != nil {
		log.Print(err)
		return 1
	}
	return 0
}
