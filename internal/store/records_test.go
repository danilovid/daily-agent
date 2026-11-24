package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func titlesOf(tasks []Task) []string {
	out := make([]string, len(tasks))
	for i, t := range tasks {
		out[i] = t.Title
	}
	return out
}

func sameStrings(a, b []string) bool {
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

func TestSaveTaskCreatesAndUpdatesWithTagsAndLinks(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	work, _ := s.CreateTag(ctx, Tag{Name: "Работа", Color: "#6FB1FF"})
	home, _ := s.CreateTag(ctx, Tag{Name: "Личное", Color: "#FFB36B"})

	created, err := s.SaveTask(ctx, Task{Title: "  Подготовить отчёт  ", Status: StatusBacklog, DueDate: "2026-11-25"},
		[]string{work.ID}, []Link{{Title: "Документ", URL: "https://docs.example.test/1"}})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.Title != "Подготовить отчёт" || created.Source != SourceOwn || created.Status != StatusBacklog {
		t.Fatalf("созданная задача: %+v", created)
	}
	links, _ := s.TaskLinks(ctx, []string{created.ID})
	if len(links[created.ID]) != 1 {
		t.Fatalf("ссылки: %+v", links)
	}
	first := links[created.ID][0]

	// Меняем поля, теги и ссылки: первую ссылку правим, вторую добавляем перед ней.
	created.Title = "Подготовить отчёт к пятнице"
	created.Note = "Цифры за квартал"
	created.DueDate = ""
	updated, err := s.SaveTask(ctx, created, []string{home.ID}, []Link{
		{Title: "Таблица", URL: "https://sheets.example.test/2"},
		{ID: first.ID, Title: "Черновик", URL: "https://docs.example.test/1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Title != "Подготовить отчёт к пятнице" || updated.Note != "Цифры за квартал" || updated.DueDate != "" {
		t.Fatalf("обновлённая задача: %+v", updated)
	}
	tags, _ := s.TaskTags(ctx, []string{created.ID})
	if len(tags[created.ID]) != 1 || tags[created.ID][0].ID != home.ID {
		t.Fatalf("теги после замены: %+v", tags[created.ID])
	}
	links, _ = s.TaskLinks(ctx, []string{created.ID})
	got := links[created.ID]
	if len(got) != 2 || got[0].Title != "Таблица" || got[1].ID != first.ID || got[1].Title != "Черновик" {
		t.Fatalf("ссылки после замены: %+v", got)
	}

	// Пустые списки снимают все теги и ссылки; тег можно вернуть.
	if _, err := s.SaveTask(ctx, updated, nil, nil); err != nil {
		t.Fatal(err)
	}
	links, _ = s.TaskLinks(ctx, []string{created.ID})
	tags, _ = s.TaskTags(ctx, []string{created.ID})
	if len(links[created.ID]) != 0 || len(tags[created.ID]) != 0 {
		t.Fatalf("после очистки: ссылки %v, теги %v", links, tags)
	}
	if _, err := s.SaveTask(ctx, updated, []string{home.ID}, nil); err != nil {
		t.Fatal(err)
	}
	tags, _ = s.TaskTags(ctx, []string{created.ID})
	if len(tags[created.ID]) != 1 {
		t.Fatalf("тег не вернулся: %+v", tags)
	}

	if _, err := s.SaveTask(ctx, Task{ID: "нет-такой", Title: "x"}, nil, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("обновление несуществующей задачи: %v", err)
	}
	if _, err := s.SaveTask(ctx, Task{Title: "   "}, nil, nil); err == nil {
		t.Fatal("ждали ошибку на пустое название")
	}
	if _, err := s.SaveTask(ctx, Task{Title: "x", DueDate: "завтра"}, nil, nil); err == nil {
		t.Fatal("ждали ошибку на неверный срок")
	}
}

func TestSaveTaskRollsBackOnLinkError(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	if _, err := s.SaveTask(ctx, Task{Title: "Без адреса"}, nil, []Link{{Title: "x", URL: " "}}); err == nil {
		t.Fatal("ждали ошибку на ссылку без адреса")
	}
	all, _ := s.ListTasks(ctx, TaskFilter{})
	if len(all) != 0 {
		t.Fatalf("транзакция не откатилась: %v", titlesOf(all))
	}
}

func TestTaskStatusDeleteAndFilters(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	work, _ := s.CreateTag(ctx, Tag{Name: "Работа", Color: "#6FB1FF"})
	a, _ := s.CreateTask(ctx, Task{Title: "Входящая", Status: StatusInbox})
	b, _ := s.CreateTask(ctx, Task{Title: "Своя", Status: StatusBacklog})
	c, _ := s.CreateTask(ctx, Task{Title: "Со сроком", Status: StatusBacklog, DueDate: "2026-11-24"})
	if err := s.AddTaskTag(ctx, b.ID, work.ID); err != nil {
		t.Fatal(err)
	}

	if err := s.SetTaskStatus(ctx, a.ID, StatusBacklog); err != nil {
		t.Fatal(err)
	}
	backlog, _ := s.ListTasks(ctx, TaskFilter{Statuses: []string{StatusBacklog}})
	if want := []string{"Со сроком", "Входящая", "Своя"}; !sameStrings(titlesOf(backlog), want) {
		t.Fatalf("бэклог = %v, ждали %v", titlesOf(backlog), want)
	}
	tagged, _ := s.ListTasks(ctx, TaskFilter{Statuses: []string{StatusBacklog}, TagID: work.ID})
	if !sameStrings(titlesOf(tagged), []string{"Своя"}) {
		t.Fatalf("по тегу = %v", titlesOf(tagged))
	}
	if err := s.SetTaskStatus(ctx, a.ID, "unknown"); err == nil {
		t.Fatal("ждали ошибку на неизвестный статус")
	}

	if err := s.DeleteTask(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetTask(ctx, c.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("удалённая задача читается: %v", err)
	}
	if err := s.DeleteTask(ctx, c.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("повторное удаление: %v", err)
	}
	if err := s.SetTaskStatus(ctx, c.ID, StatusDone); !errors.Is(err, ErrNotFound) {
		t.Fatalf("смена статуса удалённой: %v", err)
	}
	// Мягкое удаление: строка осталась в базе.
	var n int
	s.DB().QueryRow(`SELECT COUNT(*) FROM tasks WHERE id = ? AND deleted_at IS NOT NULL`, c.ID).Scan(&n)
	if n != 1 {
		t.Fatal("удаление должно быть мягким")
	}
}

func TestSaveMeetingAndWindow(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	day := time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC)

	once, err := s.SaveMeeting(ctx, Meeting{Title: "Разовая", StartsAt: day.Add(10 * time.Hour), DurationMin: 30},
		[]Link{{Title: "Созвон", URL: "https://meet.example.test/a"}})
	if err != nil {
		t.Fatal(err)
	}
	series, _ := s.SaveMeeting(ctx, Meeting{Title: "Серия", StartsAt: day.AddDate(0, 0, -7), DurationMin: 30, RepeatRule: "FREQ=DAILY"}, nil)
	s.SaveMeeting(ctx, Meeting{Title: "Будущая серия", StartsAt: day.AddDate(0, 0, 3), DurationMin: 30, RepeatRule: "FREQ=DAILY"}, nil)
	s.SaveMeeting(ctx, Meeting{Title: "Вчерашняя", StartsAt: day.Add(-time.Hour), DurationMin: 30}, nil)

	got, err := s.MeetingsForWindow(ctx, day, day.AddDate(0, 0, 1))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, m := range got {
		names = append(names, m.Title)
	}
	if want := []string{"Серия", "Разовая"}; !sameStrings(names, want) {
		t.Fatalf("встречи окна = %v, ждали %v", names, want)
	}

	once.Title = "Разовая, перенесена"
	once.StartsAt = once.StartsAt.Add(time.Hour)
	once.DurationMin = 45
	once.Note = "Повестка"
	updated, err := s.SaveMeeting(ctx, once, nil)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Title != "Разовая, перенесена" || updated.DurationMin != 45 || !updated.StartsAt.Equal(day.Add(11*time.Hour)) {
		t.Fatalf("обновлённая встреча: %+v", updated)
	}
	links, _ := s.MeetingLinks(ctx, []string{once.ID})
	if len(links[once.ID]) != 0 {
		t.Fatalf("ссылки должны были удалиться: %+v", links)
	}
	if _, err := s.SaveMeeting(ctx, Meeting{ID: once.ID, Title: "x", StartsAt: day, DurationMin: 0}, nil); err == nil {
		t.Fatal("ждали ошибку на нулевую длительность")
	}

	if err := s.DeleteMeeting(ctx, series.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = s.MeetingsForWindow(ctx, day, day.AddDate(0, 0, 1))
	if len(got) != 1 {
		t.Fatalf("после удаления серии: %+v", got)
	}
}

func TestLinkLookupHidesDeletedOwners(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	m, _ := s.SaveMeeting(ctx, Meeting{Title: "Созвон", StartsAt: time.Now(), DurationMin: 30},
		[]Link{{Title: "Созвон", URL: "https://meet.example.test/x"}})
	links, _ := s.MeetingLinks(ctx, []string{m.ID})
	id := links[m.ID][0].ID
	if l, err := s.MeetingLink(ctx, id); err != nil || l.URL != "https://meet.example.test/x" {
		t.Fatalf("ссылка: %+v, %v", l, err)
	}
	s.DeleteMeeting(ctx, m.ID)
	if _, err := s.MeetingLink(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ссылка удалённой встречи: %v", err)
	}
}

func TestCreateTagIsCaseInsensitiveUnique(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	if _, err := s.CreateTag(ctx, Tag{Name: "Работа", Color: "#6FB1FF"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTag(ctx, Tag{Name: " работа ", Color: "#FFB36B"}); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("ждали ErrDuplicate, получили %v", err)
	}
	if _, err := s.CreateTag(ctx, Tag{Name: "  "}); err == nil {
		t.Fatal("ждали ошибку на пустое имя")
	}
}

func TestSearch(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	answer, _ := s.CreateTask(ctx, Task{Title: "Ответить Марине про сроки релиза", Status: StatusBacklog})
	insurance, _ := s.CreateTask(ctx, Task{Title: "Продлить страховку", Note: "ОСАГО и КАСКО", Status: StatusBacklog})
	rejected, _ := s.CreateTask(ctx, Task{Title: "Релизный чек-лист", Status: StatusRejected, Source: SourceTracker, ExternalID: "T-9"})
	gone, _ := s.CreateTask(ctx, Task{Title: "Удалённый релиз", Status: StatusBacklog})
	s.DeleteTask(ctx, gone.ID)
	standup, _ := s.CreateMeeting(ctx, Meeting{Title: "Планёрка команды", Note: "Статус релиза", StartsAt: time.Now(), DurationMin: 30})

	cases := []struct {
		query string
		want  []string
	}{
		{"планерка", []string{standup.ID}},         // «е» находит «ё»
		{"ПЛАНЁРКА", []string{standup.ID}},         // регистр и «ё» в запросе
		{"осаго", []string{insurance.ID}},          // заметка задачи
		{"марин срок", []string{answer.ID}},        // начала слов, все слова
		{"релиз", []string{answer.ID, standup.ID}}, // без отклонённых и удалённых
		{"марин \"(* ^:", []string{answer.ID}},     // служебные символы FTS5 из ввода отбрасываются
		{"марин OR NEAR", nil},                     // OR и NEAR — обычные слова, а не операторы
		{"   ", nil},
	}
	for _, c := range cases {
		hits, err := s.Search(ctx, c.query, 10)
		if err != nil {
			t.Fatalf("%q: %v", c.query, err)
		}
		got := map[string]bool{}
		for _, h := range hits {
			got[h.ID] = true
		}
		if len(got) != len(c.want) {
			t.Errorf("%q: нашли %v, ждали %v", c.query, hits, c.want)
			continue
		}
		for _, id := range c.want {
			if !got[id] {
				t.Errorf("%q: нет %s в %v", c.query, id, hits)
			}
		}
	}
	_ = rejected

	// После переименования индекс обновился.
	answer.Title = "Написать Олегу"
	if _, err := s.SaveTask(ctx, answer, nil, nil); err != nil {
		t.Fatal(err)
	}
	if hits, _ := s.Search(ctx, "марин", 10); len(hits) != 0 {
		t.Errorf("старое название всё ещё находится: %v", hits)
	}
	if hits, _ := s.Search(ctx, "олег", 10); len(hits) != 1 {
		t.Errorf("новое название не находится: %v", hits)
	}
}
