package devdata

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"daily-agent/internal/app"
	"daily-agent/internal/store"
)

func TestSeedThenReset(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(ctx, filepath.Join(t.TempDir(), store.FileName))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	msk := time.FixedZone("MSK", 3*3600)
	now := time.Date(2026, 11, 24, 9, 18, 0, 0, msk)
	clock := app.Clock{Now: func() time.Time { return now }, Location: msk}

	counts, err := Seed(ctx, s, now)
	if err != nil {
		t.Fatal(err)
	}
	if counts["tasks"] != 7 || counts["meetings"] != 4 || counts["tags"] != 3 || counts["summaries"] != 1 {
		t.Fatalf("после наполнения: %v", counts)
	}

	view, err := app.NewTodayService(s, clock).Today(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Meetings) != 2 || len(view.Inbox) != 3 || len(view.Backlog) != 3 || view.Summary == nil {
		t.Fatalf("экран «Сегодня»: встреч %d, входящих %d, бэклог %d, сводка %v",
			len(view.Meetings), len(view.Inbox), len(view.Backlog), view.Summary != nil)
	}
	if view.Backlog[0].DueDate != "2026-11-24" {
		t.Errorf("первая задача бэклога должна быть со сроком сегодня: %+v", view.Backlog[0])
	}

	side, err := app.NewSidebarService(s, clock).Sidebar(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if want := []int{24, 26, 30}; len(side.MeetingDays) != 3 ||
		side.MeetingDays[0] != want[0] || side.MeetingDays[1] != want[1] || side.MeetingDays[2] != want[2] {
		t.Errorf("дни со встречами = %v, ждали %v", side.MeetingDays, want)
	}
	if len(side.Tags) != 3 || side.Tags[0].Name != "Работа" || side.Tags[1].Name != "Личное" || side.Tags[2].Name != "Релиз" {
		t.Errorf("теги = %+v", side.Tags)
	}

	if _, err := Seed(ctx, s, now); !errors.Is(err, ErrNotEmpty) {
		t.Fatalf("повторное наполнение: ждали ErrNotEmpty, получили %v", err)
	}

	removed, err := Reset(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	if removed.Total() != counts.Total() {
		t.Errorf("удалено %d, было %d", removed.Total(), counts.Total())
	}
	after, err := Count(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	if after.Total() != 0 {
		t.Fatalf("после очистки осталось: %v", after)
	}

	// После очистки базу можно наполнить снова.
	if _, err := Seed(ctx, s, now); err != nil {
		t.Fatalf("наполнение после очистки: %v", err)
	}
}
