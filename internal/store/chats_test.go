package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestChats(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 24, 6, 0, 0, 0, time.UTC)
	s.SetClock(func() time.Time { return now })

	first, err := s.CreateChat(ctx, Chat{SessionID: "ses_a", Title: "Первый"})
	if err != nil {
		t.Fatal(err)
	}
	if first.Kind != ChatKindUser || first.ID == "" {
		t.Fatalf("чат: %+v", first)
	}
	now = now.Add(time.Minute)
	second, _ := s.CreateChat(ctx, Chat{SessionID: "ses_b", Title: "Второй", Kind: ChatKindScheduled})

	if _, err := s.CreateChat(ctx, Chat{SessionID: "ses_a"}); err == nil {
		t.Fatal("одна сессия OpenCode — один чат")
	}
	if _, err := s.CreateChat(ctx, Chat{}); err == nil {
		t.Fatal("ждали ошибку без сессии")
	}

	list, _ := s.ListChats(ctx, 0)
	if len(list) != 2 || list[0].ID != second.ID {
		t.Fatalf("свежий чат должен быть первым: %+v", list)
	}
	// Сообщение в первом чате поднимает его наверх.
	now = now.Add(time.Minute)
	if err := s.TouchChat(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	list, _ = s.ListChats(ctx, 0)
	if list[0].ID != first.ID {
		t.Fatalf("после сообщения первый чат наверху: %+v", list)
	}

	if err := s.SetChatTitle(ctx, first.ID, "Переименован"); err != nil {
		t.Fatal(err)
	}
	got, err := s.ChatBySession(ctx, "ses_a")
	if err != nil || got.Title != "Переименован" {
		t.Fatalf("по сессии: %+v, %v", got, err)
	}

	if err := s.DeleteChat(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetChat(ctx, first.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("удалённый чат читается: %v", err)
	}
	list, _ = s.ListChats(ctx, 0)
	if len(list) != 1 {
		t.Fatalf("после удаления: %+v", list)
	}
	var n int
	s.DB().QueryRow(`SELECT COUNT(*) FROM chats WHERE deleted_at IS NOT NULL`).Scan(&n)
	if n != 1 {
		t.Fatal("удаление чата должно быть мягким")
	}
}
