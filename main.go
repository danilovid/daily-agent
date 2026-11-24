package main

import (
	"context"
	"embed"
	"errors"
	"log"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v3/pkg/application"

	agentprofile "daily-agent/agent"
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
	supervisor, stopModel := newSupervisor(ctx, filepath.Join(filepath.Dir(dbPath), "agent"))
	defer stopModel()

	wails := application.New(application.Options{
		Name:        "Daily Agent",
		Description: "Личный рабочий помощник",
		Services: []application.Service{
			application.NewService(app.NewTodayService(db, clock)),
			application.NewService(app.NewSidebarService(db, clock)),
			application.NewService(app.NewMeetingsService(db, clock)),
			application.NewService(app.NewTasksService(db, clock)),
			application.NewService(app.NewTagsService(db)),
			application.NewService(app.NewSearchService(db, clock)),
			application.NewService(app.NewLinksService(db, openURL)),
			application.NewService(app.NewAgentService(supervisor.Status, clock)),
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
			supervisor.Stop()
			stopModel()
		},
	})

	supervisor.OnChange(func(st agent.Status) {
		wails.Event.Emit(app.AgentStatusEvent, app.AgentStatusFrom(st, clock))
	})
	// Приложение работает и без агента: свои задачи и встречи не ждут OpenCode.
	supervisor.Start(ctx)
	defer supervisor.Stop()

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
		supervisor.Stop()
		db.Close()
		log.Fatal(err)
	}
}

// newSupervisor готовит надзор за OpenCode. Модель: заглушка при
// DAILY_AGENT_STUB_MODEL=1, иначе адрес и модель из окружения и ключ из
// Связки ключей. Без модели OpenCode запускается, но агент не ответит.
func newSupervisor(ctx context.Context, root string) (*agent.Supervisor, func()) {
	cfg := agent.Config{Profile: agent.Profile{Root: root}, Logf: log.Printf}
	stop := func() {}
	if os.Getenv(EnvStubModel) == "1" {
		stub, err := stubmodel.Start()
		if err != nil {
			log.Printf("заглушка модели: %v", err)
		} else {
			model := agent.Model{BaseURL: stub.URL, ModelID: stubmodel.ModelID}
			cfg.Options = agentprofile.Options{Provider: model.Provider()}
			stop = func() { stub.Close() }
			log.Printf("агент работает с заглушкой модели")
		}
	} else if model, err := agent.ModelFromEnv(ctx); err == nil {
		cfg.Options = agentprofile.Options{Provider: model.Provider()}
		cfg.ExtraEnv = model.Env()
	} else if !errors.Is(err, agent.ErrNoModel) {
		log.Printf("модель агента: %v", err)
	}
	return agent.NewSupervisor(cfg), stop
}
