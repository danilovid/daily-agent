package store

import (
	"context"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

// Таблицы из раздела 5 docs/architecture.md.
var wantTables = []string{
	"agent_runs", "chats", "meeting_links", "meetings", "settings", "summaries",
	"sync_state", "tags", "task_links", "task_tags", "tasks",
}

func openTemp(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "nested", FileName)
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s, path
}

func tableNames(t *testing.T, s *Store) []string {
	t.Helper()
	rows, err := s.DB().Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		names = append(names, n)
	}
	return names
}

func TestMigrationsOnEmptyDatabase(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()

	got := tableNames(t, s)
	want := append([]string(nil), wantTables...)
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("таблицы: получили %v, ждали %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("таблицы: получили %v, ждали %v", got, want)
		}
	}

	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.SchemaVersion(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if v != len(migrations) {
		t.Fatalf("версия схемы %d, ждали %d", v, len(migrations))
	}

	var fk int
	if err := s.DB().QueryRow(`PRAGMA foreign_keys`).Scan(&fk); err != nil || fk != 1 {
		t.Fatalf("foreign_keys = %d, err %v", fk, err)
	}
	var mode string
	if err := s.DB().QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("journal_mode = %q, err %v", mode, err)
	}
}

func TestMigrationsAreIdempotentAndDataSurvivesReopen(t *testing.T) {
	s, path := openTemp(t)
	ctx := context.Background()
	if _, err := s.CreateTask(ctx, Task{Title: "Проверить повторное открытие"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s2, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("повторное открытие: %v", err)
	}
	defer s2.Close()
	inbox, err := s2.TasksByStatus(ctx, StatusInbox)
	if err != nil {
		t.Fatal(err)
	}
	if len(inbox) != 1 || inbox[0].Title != "Проверить повторное открытие" {
		t.Fatalf("после повторного открытия: %+v", inbox)
	}
}

func TestNewerSchemaIsRejected(t *testing.T) {
	s, path := openTemp(t)
	if _, err := s.DB().Exec(`PRAGMA user_version = 999`); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if _, err := Open(context.Background(), path); err == nil {
		t.Fatal("ждали ошибку для схемы новее приложения")
	}
}

func TestSourceExternalIDIsUnique(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	ext := Task{Title: "Задача спринта", Source: SourceTracker, ExternalID: "T-1"}
	if _, err := s.CreateTask(ctx, ext); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTask(ctx, ext); err == nil {
		t.Fatal("ждали ошибку на повторную пару source и external_id")
	}
	// Тот же внешний идентификатор из другого источника допустим.
	if _, err := s.CreateTask(ctx, Task{Title: "MR", Kind: KindMR, Source: SourceRepo, ExternalID: "T-1"}); err != nil {
		t.Fatal(err)
	}
	// Свои задачи без external_id не мешают друг другу.
	for i := 0; i < 2; i++ {
		if _, err := s.CreateTask(ctx, Task{Title: "Своя"}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCheckConstraints(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	if _, err := s.CreateTask(ctx, Task{Title: "x", Status: "unknown"}); err == nil {
		t.Fatal("ждали ошибку на неизвестный статус")
	}
	if _, err := s.CreateMeeting(ctx, Meeting{Title: "x", Source: "calendar", StartsAt: time.Now()}); err == nil {
		t.Fatal("ждали ошибку на неизвестный источник встречи")
	}
	if err := s.AddTaskTag(ctx, "нет-такой-задачи", "нет-такого-тега"); err == nil {
		t.Fatal("ждали ошибку внешнего ключа")
	}
}

func TestTimeFormatSortsAsText(t *testing.T) {
	a := time.Date(2026, 11, 24, 7, 0, 0, 0, time.UTC)
	b := a.Add(500 * time.Millisecond)
	if !(FormatTime(a) < FormatTime(b)) {
		t.Fatalf("%s должно быть раньше %s", FormatTime(a), FormatTime(b))
	}
	msk := time.FixedZone("MSK", 3*3600)
	if got := FormatTime(time.Date(2026, 11, 24, 10, 0, 0, 0, msk)); got != "2026-11-24T07:00:00.000Z" {
		t.Fatalf("FormatTime = %s", got)
	}
	back, err := ParseTime(FormatTime(b))
	if err != nil || !back.Equal(b) {
		t.Fatalf("ParseTime = %v, %v", back, err)
	}
}
