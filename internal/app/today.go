package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"daily-agent/internal/store"
)

// Clock — источник текущего времени и часового пояса.
// В приложении это системные часы, в тестах — подставные.
type Clock struct {
	Now      func() time.Time
	Location *time.Location
}

// SystemClock — системные часы и местный часовой пояс.
func SystemClock() Clock { return Clock{Now: time.Now, Location: time.Local} }

func (c Clock) now() time.Time { return c.Now().In(c.loc()) }

func (c Clock) loc() *time.Location {
	if c.Location == nil {
		return time.Local
	}
	return c.Location
}

// dayBounds возвращает начало дня и начало следующего дня в часовом поясе часов.
func (c Clock) dayBounds(day string) (time.Time, time.Time, error) {
	d, err := time.ParseInLocation(store.DateLayout, day, c.loc())
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("день %q: ждали формат ГГГГ-ММ-ДД", day)
	}
	start := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, c.loc())
	end := time.Date(d.Year(), d.Month(), d.Day()+1, 0, 0, 0, 0, c.loc())
	return start, end, nil
}

// TodayService отдаёт данные экрана «Сегодня».
type TodayService struct {
	store *store.Store
	clock Clock
}

// NewTodayService создаёт сервис экрана «Сегодня».
func NewTodayService(s *store.Store, clock Clock) *TodayService {
	return &TodayService{store: s, clock: clock}
}

// Today возвращает повестку на день day в формате ГГГГ-ММ-ДД.
// Пустой day означает сегодняшний день.
//
// Встречи — те, что начинаются в этот день по местному времени.
// Сводка — последняя сводка за этот день.
// «Входящие» и «Бэклог» — неудалённые задачи с этими статусами.
func (s *TodayService) Today(ctx context.Context, day string) (TodayView, error) {
	day = strings.TrimSpace(day)
	if day == "" {
		day = s.clock.now().Format(store.DateLayout)
	}
	start, end, err := s.clock.dayBounds(day)
	if err != nil {
		return TodayView{}, err
	}

	view := TodayView{Day: day, Meetings: []MeetingView{}, Inbox: []TaskView{}, Backlog: []TaskView{}}

	summary, err := s.store.SummaryForDay(ctx, day)
	if err != nil {
		return TodayView{}, err
	}
	if summary != nil {
		view.Summary = &SummaryView{Text: summary.Text, CreatedAt: s.formatTime(summary.CreatedAt)}
	}

	meetings, err := s.store.MeetingsBetween(ctx, start, end)
	if err != nil {
		return TodayView{}, err
	}
	ids := make([]string, len(meetings))
	for i, m := range meetings {
		ids[i] = m.ID
	}
	links, err := s.store.MeetingLinks(ctx, ids)
	if err != nil {
		return TodayView{}, err
	}
	for _, m := range meetings {
		mv := MeetingView{
			ID:          m.ID,
			Title:       m.Title,
			StartsAt:    s.formatTime(m.StartsAt),
			DurationMin: m.DurationMin,
			Links:       []LinkView{},
		}
		for _, l := range links[m.ID] {
			mv.Links = append(mv.Links, LinkView{Title: l.Title, URL: l.URL})
		}
		view.Meetings = append(view.Meetings, mv)
	}

	if view.Inbox, err = s.tasks(ctx, store.StatusInbox); err != nil {
		return TodayView{}, err
	}
	if view.Backlog, err = s.tasks(ctx, store.StatusBacklog); err != nil {
		return TodayView{}, err
	}
	return view, nil
}

func (s *TodayService) tasks(ctx context.Context, status string) ([]TaskView, error) {
	tasks, err := s.store.TasksByStatus(ctx, status)
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(tasks))
	for i, t := range tasks {
		ids[i] = t.ID
	}
	tags, err := s.store.TaskTags(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]TaskView, 0, len(tasks))
	for _, t := range tasks {
		tv := TaskView{
			ID:        t.ID,
			Kind:      t.Kind,
			Source:    t.Source,
			Title:     t.Title,
			Status:    t.Status,
			DueDate:   t.DueDate,
			URL:       t.URL,
			CreatedAt: s.formatTime(t.CreatedAt),
			Tags:      tagViews(tags[t.ID]),
		}
		out = append(out, tv)
	}
	return out, nil
}

func (s *TodayService) formatTime(t time.Time) string {
	return t.In(s.clock.loc()).Format(time.RFC3339)
}

func tagViews(tags []store.Tag) []TagView {
	out := make([]TagView, 0, len(tags))
	for _, t := range tags {
		out = append(out, TagView{ID: t.ID, Name: t.Name, Color: t.Color})
	}
	return out
}
