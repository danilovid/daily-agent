package store

import (
	"context"
	"testing"
	"time"
)

func TestSettings(t *testing.T) {
	s, path := openTemp(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 24, 6, 0, 0, 0, time.UTC)
	s.SetClock(func() time.Time { return now })

	if _, ok, err := s.Setting(ctx, SettingModelURL); err != nil || ok {
		t.Fatalf("пустая база: ok=%v err=%v", ok, err)
	}
	if err := s.SetSettings(ctx, map[string]string{
		SettingModelURL:  "http://127.0.0.1:9/v1",
		SettingModelID:   "test-model",
		"general.digest": "09:00",
	}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Settings(ctx, "agent.")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[SettingModelURL] != "http://127.0.0.1:9/v1" || got[SettingModelID] != "test-model" {
		t.Fatalf("настройки агента: %v", got)
	}
	all, _ := s.Settings(ctx, "")
	if len(all) != 3 {
		t.Fatalf("все настройки: %v", all)
	}

	// Время изменения меняется только у тех, что изменились.
	now = now.Add(time.Hour)
	if err := s.SetSettings(ctx, map[string]string{SettingModelURL: "http://127.0.0.1:9/v1", SettingModelID: ""}); err != nil {
		t.Fatal(err)
	}
	stamps := map[string]string{}
	rows, err := s.DB().QueryContext(ctx, `SELECT key, updated_at FROM settings`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var k, v string
		rows.Scan(&k, &v)
		stamps[k] = v
	}
	rows.Close()
	if stamps[SettingModelURL] != "2026-11-24T06:00:00.000Z" || stamps[SettingModelID] != "2026-11-24T07:00:00.000Z" {
		t.Fatalf("время изменения: %v", stamps)
	}
	if v, ok, _ := s.Setting(ctx, SettingModelID); !ok || v != "" {
		t.Fatalf("пустое значение: %q %v", v, ok)
	}

	if err := s.SetSettings(ctx, map[string]string{" ": "x"}); err == nil {
		t.Fatal("ждали ошибку на пустое имя")
	}

	// Настройки переживают перезапуск.
	s.Close()
	s2, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if v, _, _ := s2.Setting(ctx, SettingModelURL); v != "http://127.0.0.1:9/v1" {
		t.Fatalf("после перезапуска: %q", v)
	}
}
