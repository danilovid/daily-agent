// Package devdata наполняет базу вымышленными тестовыми данными и очищает её.
//
// Данные похожи на макет «Сегодня», но даты считаются от текущего дня,
// чтобы экран «Сегодня» не пустел на следующий день. Все имена, задачи
// и адреса вымышлены. Адреса — в зоне .test, которая не существует в сети.
package devdata

import (
	"context"
	"errors"
	"fmt"
	"time"

	"daily-agent/internal/store"
)

// ErrNotEmpty — в базе уже есть записи, наполнять её нельзя.
var ErrNotEmpty = errors.New("база не пуста: сначала очистите её командой reset")

// Tables — таблицы с данными в порядке, безопасном для удаления.
var Tables = []string{
	"task_tags", "task_links", "meeting_links",
	"tasks", "meetings", "tags", "summaries", "chats",
	"sync_state", "agent_runs", "settings",
}

// Counts — число записей по таблицам.
type Counts map[string]int

// Total возвращает общее число записей.
func (c Counts) Total() int {
	n := 0
	for _, v := range c {
		n += v
	}
	return n
}

// Count считает записи во всех таблицах, включая мягко удалённые.
func Count(ctx context.Context, s *store.Store) (Counts, error) {
	out := Counts{}
	for _, t := range Tables {
		var n int
		if err := s.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM "+t).Scan(&n); err != nil {
			return nil, fmt.Errorf("подсчёт %s: %w", t, err)
		}
		out[t] = n
	}
	return out, nil
}

// Reset удаляет все записи из всех таблиц. Схема остаётся.
// Возвращает, сколько записей было удалено из каждой таблицы.
func Reset(ctx context.Context, s *store.Store) (Counts, error) {
	tx, err := s.DB().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	out := Counts{}
	for _, t := range Tables {
		res, err := tx.ExecContext(ctx, "DELETE FROM "+t)
		if err != nil {
			return nil, fmt.Errorf("очистка %s: %w", t, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return nil, err
		}
		out[t] = int(n)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

// Seed наполняет пустую базу тестовыми данными на день, в который попадает now.
// Если в базе уже есть записи, возвращает ErrNotEmpty и ничего не меняет.
func Seed(ctx context.Context, s *store.Store, now time.Time) (Counts, error) {
	before, err := Count(ctx, s)
	if err != nil {
		return nil, err
	}
	if before.Total() > 0 {
		return nil, ErrNotEmpty
	}

	loc := now.Location()
	day := func(offset, hour, min int) time.Time {
		return time.Date(now.Year(), now.Month(), now.Day()+offset, hour, min, 0, 0, loc)
	}
	today := day(0, 0, 0).Format(store.DateLayout)

	tags := map[string]store.Tag{}
	for _, t := range []store.Tag{
		{Name: "Работа", Color: "#6FB1FF"},
		{Name: "Личное", Color: "#FFB36B"},
		{Name: "Релиз", Color: "#8FE0C0"},
	} {
		created, err := s.CreateTag(ctx, t)
		if err != nil {
			return nil, err
		}
		tags[t.Name] = created
	}

	meetings := []struct {
		m    store.Meeting
		link *store.Link
	}{
		{store.Meeting{Title: "Планёрка команды", StartsAt: day(0, 10, 0), DurationMin: 30, Note: "Статус релиза и блокеры"},
			&store.Link{Title: "Созвон", URL: "https://meet.example.test/standup"}},
		{store.Meeting{Title: "Ревью архитектуры", StartsAt: day(0, 14, 30), DurationMin: 60}, nil},
		{store.Meeting{Title: "Демо спринта", StartsAt: day(2, 11, 0), DurationMin: 45},
			&store.Link{Title: "Созвон", URL: "https://meet.example.test/demo"}},
		// Повторяющаяся встреча без ссылки: в списке встреч у неё кнопка «Ссылка на созвон».
		{store.Meeting{Title: "Встреча один на один", StartsAt: day(6, 15, 0), DurationMin: 30, RepeatRule: "FREQ=WEEKLY"}, nil},
	}
	for _, item := range meetings {
		m, err := s.CreateMeeting(ctx, item.m)
		if err != nil {
			return nil, err
		}
		if item.link != nil {
			l := *item.link
			l.OwnerID = m.ID
			if _, err := s.AddMeetingLink(ctx, l); err != nil {
				return nil, err
			}
		}
	}

	tasks := []struct {
		t    store.Task
		tags []string
		link *store.Link
	}{
		// Входящие: новые внешние записи.
		{store.Task{Title: "Прислать оценку сроков по интеграции", Source: store.SourceTracker, ExternalID: "T-101",
			Status: store.StatusInbox, URL: "https://tracker.example.test/T-101", CreatedAt: day(-1, 18, 40)}, []string{"Работа"}, nil},
		{store.Task{Title: "Проверить конфигурацию стенда", Source: store.SourceTracker, ExternalID: "T-102",
			Status: store.StatusInbox, URL: "https://tracker.example.test/T-102", CreatedAt: day(0, 8, 15)}, []string{"Работа"}, nil},
		{store.Task{Title: "Посмотреть черновик документа", Kind: store.KindMR, Source: store.SourceRepo, ExternalID: "17",
			Status: store.StatusInbox, URL: "https://gitlab.example.test/team/docs/-/merge_requests/17", CreatedAt: day(0, 8, 52)}, []string{"Работа"}, nil},
		// Бэклог.
		{store.Task{Title: "Ответить Марине про сроки релиза", Source: store.SourceRepo, ExternalID: "issue-7",
			Status: store.StatusBacklog, DueDate: today, URL: "https://gitlab.example.test/team/app/-/issues/7", CreatedAt: day(-3, 11, 0)}, []string{"Работа", "Релиз"}, nil},
		{store.Task{Title: "Подготовить заметки к ревью архитектуры", Status: store.StatusBacklog, CreatedAt: day(-2, 17, 5),
			Note: "Вопросы по схеме данных и поиску"}, []string{"Работа"},
			&store.Link{Title: "Документ", URL: "https://docs.example.test/arch-review"}},
		{store.Task{Title: "Продлить страховку на машину", Status: store.StatusBacklog, CreatedAt: day(-1, 21, 30)}, []string{"Личное"}, nil},
		// Выполненная задача на экран «Сегодня» не попадает.
		{store.Task{Title: "Обновить зависимости проекта", Status: store.StatusDone, CreatedAt: day(-4, 10, 0)}, []string{"Работа"}, nil},
	}
	for _, item := range tasks {
		t, err := s.CreateTask(ctx, item.t)
		if err != nil {
			return nil, err
		}
		for _, name := range item.tags {
			if err := s.AddTaskTag(ctx, t.ID, tags[name].ID); err != nil {
				return nil, err
			}
		}
		if item.link != nil {
			l := *item.link
			l.OwnerID = t.ID
			if _, err := s.AddTaskLink(ctx, l); err != nil {
				return nil, err
			}
		}
	}

	if _, err := s.CreateSummary(ctx, store.Summary{
		Day:       today,
		Text:      "Сегодня две встречи и три задачи. Начни с ответа Марине: срок сегодня. Во «Входящих» ждут разбора три новые записи из трекера и репозитория.",
		CreatedAt: day(0, 9, 2),
	}); err != nil {
		return nil, err
	}

	return Count(ctx, s)
}
