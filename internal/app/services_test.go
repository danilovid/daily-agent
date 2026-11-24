package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"daily-agent/internal/store"
)

func TestTodayIncludesRecurringMeetings(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	meetings := NewMeetingsService(s, testClock())
	// Планёрка по будням с понедельника 16 ноября; 24 ноября — вторник.
	if _, err := meetings.Create(ctx, MeetingInput{Title: "Планёрка", Date: "2026-11-16", Start: "10:00", DurationMin: 15,
		Repeat: "FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR"}); err != nil {
		t.Fatal(err)
	}
	view, err := NewTodayService(s, testClock()).Today(ctx, "2026-11-24")
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Meetings) != 1 || view.Meetings[0].StartsAt != "2026-11-24T10:00:00+03:00" {
		t.Fatalf("встречи 24 ноября: %+v", view.Meetings)
	}
	saturday, _ := NewTodayService(s, testClock()).Today(ctx, "2026-11-28")
	if len(saturday.Meetings) != 0 {
		t.Fatalf("в субботу планёрки нет: %+v", saturday.Meetings)
	}
	side, _ := NewSidebarService(s, testClock()).Sidebar(ctx, "2026-11")
	// Будни с 16 по 30 ноября: 16–20, 23–27, 30.
	if len(side.MeetingDays) != 11 || side.Counts.MeetingsToday != 1 {
		t.Fatalf("боковая панель: дни %v, сегодня %d", side.MeetingDays, side.Counts.MeetingsToday)
	}
}

func TestMeetingsCreateUpdateDelete(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	svc := NewMeetingsService(s, testClock())

	bad := []MeetingInput{
		{Title: " ", Date: "2026-11-25", Start: "10:00", DurationMin: 30},
		{Title: "Без даты", Start: "10:00", DurationMin: 30},
		{Title: "Без времени", Date: "2026-11-25", DurationMin: 30},
		{Title: "Ноль минут", Date: "2026-11-25", Start: "10:00"},
		{Title: "Сутки с лишним", Date: "2026-11-25", Start: "10:00", DurationMin: 24*60 + 1},
		{Title: "Странный повтор", Date: "2026-11-25", Start: "10:00", DurationMin: 30, Repeat: "FREQ=YEARLY"},
		{Title: "Плохая ссылка", Date: "2026-11-25", Start: "10:00", DurationMin: 30, Links: []LinkInput{{URL: "javascript:alert(1)"}}},
		{Title: "Ссылка без схемы", Date: "2026-11-25", Start: "10:00", DurationMin: 30, Links: []LinkInput{{URL: "meet.example.test/x"}}},
	}
	for _, in := range bad {
		if _, err := svc.Create(ctx, in); err == nil {
			t.Errorf("ждали ошибку для %+v", in)
		}
	}

	created, err := svc.Create(ctx, MeetingInput{Title: "  Демо спринта ", Date: "2026-11-26", Start: "11:00", DurationMin: 45,
		Repeat: "freq=weekly", Note: "Показать поиск",
		Links: []LinkInput{{Title: "", URL: " https://meet.example.test/demo "}}})
	if err != nil {
		t.Fatal(err)
	}
	if created.Title != "Демо спринта" || created.Date != "2026-11-26" || created.Start != "11:00" ||
		created.Repeat != "FREQ=WEEKLY" || !created.Editable {
		t.Fatalf("созданная встреча: %+v", created)
	}
	if len(created.Links) != 1 || created.Links[0].Title != "Созвон" || created.Links[0].URL != "https://meet.example.test/demo" {
		t.Fatalf("ссылка: %+v", created.Links)
	}

	updated, err := svc.Update(ctx, created.ID, MeetingInput{Title: "Демо", Date: "2026-11-27", Start: "12:30", DurationMin: 60,
		Links: []LinkInput{{ID: created.Links[0].ID, Title: "Zoom", URL: "https://meet.example.test/demo2"}}})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Title != "Демо" || updated.Date != "2026-11-27" || updated.Start != "12:30" || updated.Repeat != "" ||
		len(updated.Links) != 1 || updated.Links[0].ID != created.Links[0].ID || updated.Links[0].Title != "Zoom" {
		t.Fatalf("изменённая встреча: %+v", updated)
	}

	if err := svc.Delete(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Get(ctx, created.ID); err == nil || !strings.Contains(err.Error(), "не найдена") {
		t.Fatalf("удалённая встреча: %v", err)
	}
	if err := svc.Delete(ctx, created.ID); err == nil {
		t.Fatal("повторное удаление должно вернуть ошибку")
	}
}

func TestMeetingsListUpcomingAndPast(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	svc := NewMeetingsService(s, testClock())
	// Сейчас 24 ноября, 09:18.
	create := func(title, date, start string, dur int, repeat string) {
		t.Helper()
		if _, err := svc.Create(ctx, MeetingInput{Title: title, Date: date, Start: start, DurationMin: dur, Repeat: repeat}); err != nil {
			t.Fatal(err)
		}
	}
	create("Утренняя, уже прошла", "2026-11-24", "08:00", 30, "")
	create("Идёт сейчас", "2026-11-24", "09:00", 60, "")
	create("Планёрка", "2026-11-24", "10:00", 30, "")
	create("Демо", "2026-11-26", "11:00", 45, "")
	create("Один на один", "2026-11-30", "15:00", 30, "FREQ=WEEKLY")
	create("Далеко", "2026-12-20", "10:00", 30, "")
	create("Прошлая неделя", "2026-11-17", "12:00", 30, "")
	create("Давно", "2026-09-01", "12:00", 30, "")

	up, err := svc.List(ctx, PeriodUpcoming)
	if err != nil {
		t.Fatal(err)
	}
	var days []string
	var titles []string
	for _, d := range up.Days {
		days = append(days, d.Day)
		for _, m := range d.Meetings {
			titles = append(titles, m.Title)
		}
	}
	// Окно — 14 дней вперёд, до 8 декабря включительно: «Один на один» 30 ноября и 7 декабря.
	if want := "2026-11-24,2026-11-26,2026-11-30,2026-12-07"; strings.Join(days, ",") != want {
		t.Errorf("дни предстоящих = %v, ждали %s", days, want)
	}
	if want := "Идёт сейчас,Планёрка,Демо,Один на один,Один на один"; strings.Join(titles, ",") != want {
		t.Errorf("предстоящие = %v", titles)
	}
	if up.UpcomingCount != 5 {
		t.Errorf("число предстоящих = %d", up.UpcomingCount)
	}

	past, err := svc.List(ctx, PeriodPast)
	if err != nil {
		t.Fatal(err)
	}
	days, titles = nil, nil
	for _, d := range past.Days {
		days = append(days, d.Day)
		for _, m := range d.Meetings {
			titles = append(titles, m.Title)
		}
	}
	if want := "2026-11-24,2026-11-17"; strings.Join(days, ",") != want {
		t.Errorf("дни прошедших = %v, ждали %s", days, want)
	}
	if want := "Утренняя, уже прошла,Прошлая неделя"; strings.Join(titles, ",") != want {
		t.Errorf("прошедшие = %v", titles)
	}
	if past.UpcomingCount != 5 {
		t.Errorf("число предстоящих на вкладке прошедших = %d", past.UpcomingCount)
	}
	if _, err := svc.List(ctx, "someday"); err == nil {
		t.Error("ждали ошибку на неизвестный период")
	}
}

func TestExternalMeetingKeepsSourceFields(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	m, _ := s.CreateMeeting(ctx, store.Meeting{Title: "Из сервиса", Source: store.MeetingSourceMeetings, ExternalID: "m-1",
		StartsAt: at(25, 10, 0), DurationMin: 30})
	svc := NewMeetingsService(s, testClock())
	got, err := svc.Update(ctx, m.ID, MeetingInput{Title: "Переименовал", Date: "2026-11-30", Start: "18:00", DurationMin: 5, Note: "Моя заметка"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Из сервиса" || got.Date != "2026-11-25" || got.Start != "10:00" || got.Note != "Моя заметка" || got.Editable {
		t.Fatalf("внешняя встреча: %+v", got)
	}
}

func TestTasksLifecycle(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	svc := NewTasksService(s, testClock())
	work, _ := NewTagsService(s).Create(ctx, "Работа", "#6fb1ff")

	if _, err := svc.Create(ctx, TaskInput{Title: "  "}); err == nil {
		t.Fatal("ждали ошибку на пустое название")
	}
	if _, err := svc.Create(ctx, TaskInput{Title: "x", DueDate: "завтра"}); err == nil {
		t.Fatal("ждали ошибку на неверный срок")
	}

	own, err := svc.Create(ctx, TaskInput{Title: "Своя задача", DueDate: "2026-11-25", TagIDs: []string{work.ID},
		Links: []LinkInput{{URL: "https://docs.example.test/a"}}})
	if err != nil {
		t.Fatal(err)
	}
	if own.Status != store.StatusBacklog || own.Source != store.SourceOwn || !own.TitleEditable ||
		len(own.Tags) != 1 || len(own.Links) != 1 || own.Links[0].Title != "Ссылка" {
		t.Fatalf("своя задача: %+v", own)
	}

	incoming, _ := s.CreateTask(ctx, store.Task{Title: "Из трекера", Source: store.SourceTracker, ExternalID: "T-1",
		Status: store.StatusInbox, URL: "https://tracker.example.test/T-1"})

	inbox, _ := svc.List(ctx, store.StatusInbox, "")
	if len(inbox) != 1 || inbox[0].ID != incoming.ID {
		t.Fatalf("входящие: %+v", inbox)
	}
	// Принять, затем закрыть и вернуть.
	if err := svc.SetStatus(ctx, incoming.ID, store.StatusBacklog); err != nil {
		t.Fatal(err)
	}
	backlog, _ := svc.List(ctx, store.StatusBacklog, "")
	if len(backlog) != 2 {
		t.Fatalf("принятые и свои задачи в одном списке: %+v", backlog)
	}
	if err := svc.SetStatus(ctx, own.ID, store.StatusDone); err != nil {
		t.Fatal(err)
	}
	done, _ := svc.List(ctx, store.StatusDone, "")
	if len(done) != 1 || done[0].ID != own.ID {
		t.Fatalf("выполненные: %+v", done)
	}
	if err := svc.SetStatus(ctx, own.ID, store.StatusBacklog); err != nil {
		t.Fatal(err)
	}
	// Запрещённые переходы.
	if err := svc.SetStatus(ctx, own.ID, store.StatusInbox); err == nil {
		t.Error("из бэклога во входящие нельзя")
	}
	if err := svc.SetStatus(ctx, own.ID, store.StatusRejected); err == nil {
		t.Error("отклонить можно только входящую")
	}
	if _, err := svc.List(ctx, store.StatusRejected, ""); err == nil {
		t.Error("списка отклонённых нет")
	}

	// Фильтр по тегу.
	tagged, _ := svc.List(ctx, store.StatusBacklog, work.ID)
	if len(tagged) != 1 || tagged[0].ID != own.ID {
		t.Fatalf("по тегу: %+v", tagged)
	}

	// У внешней записи название не меняется, остальное — меняется.
	ext, err := svc.Update(ctx, incoming.ID, TaskInput{Title: "Переименовал", Note: "Заметка", TagIDs: []string{work.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if ext.Title != "Из трекера" || ext.Note != "Заметка" || ext.TitleEditable || len(ext.Tags) != 1 ||
		ext.URL != "https://tracker.example.test/T-1" || ext.ExternalID != "T-1" {
		t.Fatalf("внешняя запись после изменения: %+v", ext)
	}

	if err := svc.Delete(ctx, own.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Get(ctx, own.ID); err == nil {
		t.Fatal("удалённая задача читается")
	}
}

func TestRejectInbox(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	svc := NewTasksService(s, testClock())
	in, _ := s.CreateTask(ctx, store.Task{Title: "Лишнее", Source: store.SourceRepo, Kind: store.KindMR, ExternalID: "7", Status: store.StatusInbox})
	if err := svc.SetStatus(ctx, in.ID, store.StatusRejected); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetTask(ctx, in.ID)
	if got.Status != store.StatusRejected {
		t.Fatalf("статус = %s", got.Status)
	}
	if err := svc.SetStatus(ctx, in.ID, store.StatusBacklog); err == nil {
		t.Fatal("отклонённую нельзя принять обратно")
	}
}

func TestTagsService(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	svc := NewTagsService(s)
	tag, err := svc.Create(ctx, " Релиз ", "#8fe0c0")
	if err != nil || tag.Name != "Релиз" || tag.Color != "#8FE0C0" {
		t.Fatalf("тег: %+v, %v", tag, err)
	}
	for _, c := range []struct{ name, color string }{
		{"релиз", "#8FE0C0"}, {"", "#8FE0C0"}, {"Цвет", "red"}, {strings.Repeat("я", 33), "#8FE0C0"},
	} {
		if _, err := svc.Create(ctx, c.name, c.color); err == nil {
			t.Errorf("ждали ошибку для %q %q", c.name, c.color)
		}
	}
	list, _ := svc.List(ctx)
	if len(list) != 1 || len(svc.Palette()) == 0 {
		t.Fatalf("теги: %+v", list)
	}
}

func TestSearchService(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	tasks := NewTasksService(s, testClock())
	meetings := NewMeetingsService(s, testClock())
	tasks.Create(ctx, TaskInput{Title: "Подготовить заметки к ревью архитектуры"})
	meetings.Create(ctx, MeetingInput{Title: "Ревью архитектуры", Date: "2026-11-10", Start: "14:30", DurationMin: 60, Repeat: "FREQ=WEEKLY"})

	view, err := NewSearchService(s, testClock()).Search(ctx, "  ревью  ")
	if err != nil {
		t.Fatal(err)
	}
	if view.Query != "ревью" || len(view.Tasks) != 1 || len(view.Meetings) != 1 {
		t.Fatalf("результаты: %+v", view)
	}
	// У серии показывается ближайшая будущая встреча: вторник, 24 ноября, 14:30.
	if view.Meetings[0].StartsAt != "2026-11-24T14:30:00+03:00" {
		t.Errorf("ближайшая встреча серии: %s", view.Meetings[0].StartsAt)
	}
	empty, _ := NewSearchService(s, testClock()).Search(ctx, "")
	if len(empty.Tasks) != 0 || len(empty.Meetings) != 0 {
		t.Fatalf("пустой запрос: %+v", empty)
	}
}

func TestLinksServiceOpensOnlyStoredHTTPLinks(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	var opened []string
	svc := NewLinksService(s, func(u string) error { opened = append(opened, u); return nil })

	m, _ := NewMeetingsService(s, testClock()).Create(ctx, MeetingInput{Title: "Созвон", Date: "2026-11-24", Start: "10:00", DurationMin: 30,
		Links: []LinkInput{{URL: "https://meet.example.test/standup"}}})
	if err := svc.OpenMeetingLink(ctx, m.Links[0].ID); err != nil {
		t.Fatal(err)
	}
	task, _ := NewTasksService(s, testClock()).Create(ctx, TaskInput{Title: "Документ", Links: []LinkInput{{URL: "https://docs.example.test/a"}}})
	if err := svc.OpenTaskLink(ctx, task.Links[0].ID); err != nil {
		t.Fatal(err)
	}
	ext, _ := s.CreateTask(ctx, store.Task{Title: "Из трекера", Source: store.SourceTracker, ExternalID: "T-5", URL: "https://tracker.example.test/T-5"})
	if err := svc.OpenTaskSource(ctx, ext.ID); err != nil {
		t.Fatal(err)
	}
	if want := "https://meet.example.test/standup,https://docs.example.test/a,https://tracker.example.test/T-5"; strings.Join(opened, ",") != want {
		t.Fatalf("открыты %v", opened)
	}

	// Адрес не http(s), даже если попал в базу в обход карточки, не открывается.
	bad, _ := s.CreateTask(ctx, store.Task{Title: "Плохой адрес", Source: store.SourceTracker, ExternalID: "T-6", URL: "file:///etc/hosts"})
	if err := svc.OpenTaskSource(ctx, bad.ID); err == nil {
		t.Fatal("ждали отказ для file://")
	}
	if err := svc.OpenMeetingLink(ctx, "нет-такой"); err == nil {
		t.Fatal("ждали ошибку для неизвестной ссылки")
	}
	failing := NewLinksService(s, func(string) error { return errors.New("браузер недоступен") })
	if err := failing.OpenMeetingLink(ctx, m.Links[0].ID); err == nil {
		t.Fatal("ошибка открытия должна дойти до окна")
	}
	if len(opened) != 3 {
		t.Fatalf("лишние открытия: %v", opened)
	}
}
