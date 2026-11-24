package app

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"daily-agent/internal/store"
)

var msk = time.FixedZone("MSK", 3*3600)

// fixedNow — вторник, 24 ноября 2026, 09:18 по Москве.
var fixedNow = time.Date(2026, 11, 24, 9, 18, 0, 0, msk)

func testClock() Clock {
	return Clock{Now: func() time.Time { return fixedNow }, Location: msk}
}

func openStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(context.Background(), filepath.Join(t.TempDir(), store.FileName))
	if err != nil {
		t.Fatal(err)
	}
	s.SetClock(func() time.Time { return fixedNow })
	t.Cleanup(func() { s.Close() })
	return s
}

func at(day, hour, min int) time.Time {
	return time.Date(2026, 11, day, hour, min, 0, 0, msk)
}

func mustMeeting(t *testing.T, s *store.Store, title string, startsAt time.Time) store.Meeting {
	t.Helper()
	m, err := s.CreateMeeting(context.Background(), store.Meeting{Title: title, StartsAt: startsAt, DurationMin: 30})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func mustTask(t *testing.T, s *store.Store, task store.Task) store.Task {
	t.Helper()
	out, err := s.CreateTask(context.Background(), task)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func softDelete(t *testing.T, s *store.Store, table, id string) {
	t.Helper()
	if _, err := s.DB().Exec(`UPDATE `+table+` SET deleted_at = ? WHERE id = ?`, store.FormatTime(fixedNow), id); err != nil {
		t.Fatal(err)
	}
}

func titles[T any](items []T, title func(T) string) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = title(it)
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestTodayReturnsDataForRequestedDay(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()

	// Встречи вокруг 24 ноября по Москве, вставлены не по порядку.
	mustMeeting(t, s, "Вечер накануне", at(23, 23, 59))
	late := mustMeeting(t, s, "Поздняя", at(24, 23, 59))
	mustMeeting(t, s, "Полночь", at(24, 0, 0))
	standup := mustMeeting(t, s, "Планёрка", at(24, 10, 0))
	mustMeeting(t, s, "Следующий день", at(25, 0, 0))
	removed := mustMeeting(t, s, "Удалённая", at(24, 12, 0))
	softDelete(t, s, "meetings", removed.ID)
	_ = late

	if _, err := s.AddMeetingLink(ctx, store.Link{OwnerID: standup.ID, Title: "Созвон", URL: "https://meet.example.test/standup"}); err != nil {
		t.Fatal(err)
	}

	// Задачи разных статусов.
	work, err := s.CreateTag(ctx, store.Tag{Name: "Работа", Color: "#6FB1FF"})
	if err != nil {
		t.Fatal(err)
	}
	inbox := mustTask(t, s, store.Task{Title: "Новая из трекера", Source: store.SourceTracker, ExternalID: "T-1", Status: store.StatusInbox})
	if err := s.AddTaskTag(ctx, inbox.ID, work.ID); err != nil {
		t.Fatal(err)
	}
	mustTask(t, s, store.Task{Title: "Без срока", Status: store.StatusBacklog})
	mustTask(t, s, store.Task{Title: "Срок сегодня", Status: store.StatusBacklog, DueDate: "2026-11-24"})
	mustTask(t, s, store.Task{Title: "Готовая", Status: store.StatusDone})
	mustTask(t, s, store.Task{Title: "Отклонённая", Status: store.StatusRejected, Source: store.SourceRepo, ExternalID: "42", Kind: store.KindMR})
	gone := mustTask(t, s, store.Task{Title: "Удалённая задача", Status: store.StatusBacklog})
	softDelete(t, s, "tasks", gone.ID)

	// Сводки за соседние дни.
	if _, err := s.CreateSummary(ctx, store.Summary{Day: "2026-11-23", Text: "Вчерашняя сводка"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateSummary(ctx, store.Summary{Day: "2026-11-24", Text: "Сводка на сегодня"}); err != nil {
		t.Fatal(err)
	}

	svc := NewTodayService(s, testClock())
	view, err := svc.Today(ctx, "2026-11-24")
	if err != nil {
		t.Fatal(err)
	}

	if view.Day != "2026-11-24" {
		t.Errorf("day = %q", view.Day)
	}
	gotMeetings := titles(view.Meetings, func(m MeetingView) string { return m.Title })
	if want := []string{"Полночь", "Планёрка", "Поздняя"}; !equal(gotMeetings, want) {
		t.Errorf("встречи = %v, ждали %v", gotMeetings, want)
	}
	if view.Meetings[1].StartsAt != "2026-11-24T10:00:00+03:00" {
		t.Errorf("время планёрки = %q", view.Meetings[1].StartsAt)
	}
	if len(view.Meetings[1].Links) != 1 || view.Meetings[1].Links[0].URL != "https://meet.example.test/standup" {
		t.Errorf("ссылки планёрки = %+v", view.Meetings[1].Links)
	}
	if view.Summary == nil || view.Summary.Text != "Сводка на сегодня" {
		t.Errorf("сводка = %+v", view.Summary)
	}

	gotInbox := titles(view.Inbox, func(t TaskView) string { return t.Title })
	if want := []string{"Новая из трекера"}; !equal(gotInbox, want) {
		t.Errorf("входящие = %v, ждали %v", gotInbox, want)
	}
	if len(view.Inbox[0].Tags) != 1 || view.Inbox[0].Tags[0].Name != "Работа" {
		t.Errorf("теги входящей = %+v", view.Inbox[0].Tags)
	}
	gotBacklog := titles(view.Backlog, func(t TaskView) string { return t.Title })
	if want := []string{"Срок сегодня", "Без срока"}; !equal(gotBacklog, want) {
		t.Errorf("бэклог = %v, ждали %v", gotBacklog, want)
	}

	// Соседний день: свои встречи и своя сводка.
	prev, err := svc.Today(ctx, "2026-11-23")
	if err != nil {
		t.Fatal(err)
	}
	if got := titles(prev.Meetings, func(m MeetingView) string { return m.Title }); !equal(got, []string{"Вечер накануне"}) {
		t.Errorf("встречи 23 ноября = %v", got)
	}
	if prev.Summary == nil || prev.Summary.Text != "Вчерашняя сводка" {
		t.Errorf("сводка 23 ноября = %+v", prev.Summary)
	}

	// День без данных: пустые списки, а не nil, и нет сводки.
	empty, err := svc.Today(ctx, "2026-12-01")
	if err != nil {
		t.Fatal(err)
	}
	if empty.Meetings == nil || len(empty.Meetings) != 0 || empty.Summary != nil {
		t.Errorf("пустой день = %+v", empty)
	}
}

func TestTodayDefaultsToCurrentDay(t *testing.T) {
	s := openStore(t)
	mustMeeting(t, s, "Планёрка", at(24, 10, 0))
	view, err := NewTodayService(s, testClock()).Today(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if view.Day != "2026-11-24" || len(view.Meetings) != 1 {
		t.Fatalf("view = %+v", view)
	}
}

func TestTodayRejectsBadDay(t *testing.T) {
	s := openStore(t)
	if _, err := NewTodayService(s, testClock()).Today(context.Background(), "24.11.2026"); err == nil {
		t.Fatal("ждали ошибку формата дня")
	}
}

func TestSidebar(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	mustMeeting(t, s, "Планёрка", at(24, 10, 0))
	mustMeeting(t, s, "Ревью", at(24, 14, 30))
	mustMeeting(t, s, "Демо", at(26, 11, 0))
	// 1 декабря 00:30 по Москве — в UTC это ещё 30 ноября. В ноябре точки быть не должно.
	mustMeeting(t, s, "Декабрь", time.Date(2026, 12, 1, 0, 30, 0, 0, msk))
	mustTask(t, s, store.Task{Title: "a", Status: store.StatusInbox})
	mustTask(t, s, store.Task{Title: "b", Status: store.StatusBacklog})
	mustTask(t, s, store.Task{Title: "c", Status: store.StatusBacklog})
	if _, err := s.CreateTag(ctx, store.Tag{Name: "Работа", Color: "#6FB1FF"}); err != nil {
		t.Fatal(err)
	}

	view, err := NewSidebarService(s, testClock()).Sidebar(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if view.Today != "2026-11-24" || view.Month != "2026-11" {
		t.Errorf("today = %q, month = %q", view.Today, view.Month)
	}
	if view.Counts != (SidebarCounts{Inbox: 1, Backlog: 2, MeetingsToday: 2, Chats: 0}) {
		t.Errorf("counts = %+v", view.Counts)
	}
	if len(view.Tags) != 1 || view.Tags[0].Name != "Работа" {
		t.Errorf("tags = %+v", view.Tags)
	}
	wantDays := []int{24, 26}
	if len(view.MeetingDays) != len(wantDays) {
		t.Fatalf("meetingDays = %v, ждали %v", view.MeetingDays, wantDays)
	}
	for i := range wantDays {
		if view.MeetingDays[i] != wantDays[i] {
			t.Fatalf("meetingDays = %v, ждали %v", view.MeetingDays, wantDays)
		}
	}

	dec, err := NewSidebarService(s, testClock()).Sidebar(ctx, "2026-12")
	if err != nil {
		t.Fatal(err)
	}
	if len(dec.MeetingDays) != 1 || dec.MeetingDays[0] != 1 {
		t.Errorf("декабрь: meetingDays = %v", dec.MeetingDays)
	}
	if _, err := NewSidebarService(s, testClock()).Sidebar(ctx, "ноябрь"); err == nil {
		t.Error("ждали ошибку формата месяца")
	}
}
