package app

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"daily-agent/internal/store"
)

func TestWatchDataSeesOtherProcess(t *testing.T) {
	path := filepath.Join(t.TempDir(), store.FileName)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	app, err := store.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	other, err := store.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()

	old := WatchInterval
	WatchInterval = 20 * time.Millisecond
	defer func() { WatchInterval = old }()
	var changes atomic.Int32
	go WatchData(ctx, app, func() { changes.Add(1) })
	time.Sleep(60 * time.Millisecond)

	app.CreateTask(ctx, store.Task{Title: "своя запись"})
	time.Sleep(80 * time.Millisecond)
	if changes.Load() != 0 {
		t.Fatal("свои записи окно и так видит")
	}
	// Другой процесс, например MCP-сервер, записал план.
	task, _ := other.CreateTask(ctx, store.Task{Title: "задача", Status: store.StatusBacklog})
	other.SaveDayPlan(ctx, store.DayPlan{Day: "2026-11-24", Source: store.PlanSourceAgent, Items: []store.DayPlanItem{{TaskID: task.ID, EstimateMin: 30}}})
	deadline := time.Now().Add(time.Second)
	for changes.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if changes.Load() == 0 {
		t.Fatal("запись другого процесса не замечена")
	}
}
