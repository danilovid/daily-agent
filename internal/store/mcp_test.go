package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMCPServers(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 25, 6, 0, 0, 0, time.UTC)
	s.SetClock(func() time.Time { return now })

	tracker, err := s.SaveMCPServer(ctx, MCPServer{Name: "tracker", Title: "Трекер", Source: MCPSourceApp, URL: "https://tracker.example.test/mcp", Auth: MCPAuthOAuth, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if tracker.ID == "" || tracker.Type != "remote" || tracker.Tools != nil {
		t.Fatalf("новый сервер: %+v", tracker)
	}
	if _, err := s.SaveMCPServer(ctx, MCPServer{Name: "tracker", Source: MCPSourceApp}); err == nil {
		t.Fatal("ждали ошибку для повторного имени")
	}
	if _, err := s.SaveMCPServer(ctx, MCPServer{Name: "x", Source: "other"}); err == nil {
		t.Fatal("ждали ошибку для неизвестного источника")
	}
	repo, _ := s.SaveMCPServer(ctx, MCPServer{Name: "repo", Source: MCPSourceOpenCode, Type: "local", Command: []string{"repo-mcp", "--stdio"}})

	now = now.Add(time.Hour)
	tracker.Enabled = false
	tracker.Tools = []string{"tracker_get_issue"}
	tracker, err = s.SaveMCPServer(ctx, tracker)
	if err != nil {
		t.Fatal(err)
	}
	if tracker.Enabled || len(tracker.Tools) != 1 || !tracker.UpdatedAt.Equal(now) || !tracker.CreatedAt.Before(now) {
		t.Fatalf("после изменения: %+v", tracker)
	}
	// Пустой список — «ничего не разрешено», а не «по умолчанию».
	tracker.Tools = []string{}
	tracker, _ = s.SaveMCPServer(ctx, tracker)
	if tracker.Tools == nil || len(tracker.Tools) != 0 {
		t.Fatalf("пустой список инструментов: %#v", tracker.Tools)
	}

	list, _ := s.MCPServers(ctx)
	if len(list) != 2 || list[0].Name != "repo" || len(list[0].Command) != 2 || list[0].Enabled {
		t.Fatalf("список: %+v", list)
	}
	if err := s.DeleteMCPServer(ctx, repo.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MCPServerByID(ctx, repo.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("удалённый сервер читается: %v", err)
	}
	// Имя удалённого сервера можно занять снова; сама запись остаётся.
	if _, err := s.SaveMCPServer(ctx, MCPServer{Name: "repo", Source: MCPSourceApp}); err != nil {
		t.Fatal(err)
	}
	var n int
	s.DB().QueryRow(`SELECT COUNT(*) FROM mcp_servers WHERE deleted_at IS NOT NULL`).Scan(&n)
	if n != 1 {
		t.Fatal("удаление должно быть мягким")
	}
}
