package main

import (
	"context"
	"embed"
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"

	"daily-agent/internal/app"
	"daily-agent/internal/store"
)

// Собранный интерфейс из frontend/dist встраивается в программу.
//
//go:embed all:frontend/dist
var assets embed.FS

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
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

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
		db.Close()
		log.Fatal(err)
	}
}
