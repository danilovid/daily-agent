package app

import (
	"context"
	"strings"

	"daily-agent/internal/store"
)

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
// Встречи — те, что начинаются в этот день по местному времени, включая
// встречи повторяющихся серий. Сводка — последняя сводка за этот день.
// «Входящие» и «Бэклог» — неудалённые задачи с этими статусами.
func (s *TodayService) Today(ctx context.Context, day string) (TodayView, error) {
	day = strings.TrimSpace(day)
	if day == "" {
		day = s.clock.today()
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
		view.Summary = &SummaryView{Text: summary.Text, CreatedAt: s.clock.format(summary.CreatedAt)}
	}

	occs, err := loadOccurrences(ctx, s.store, s.clock.loc(), start, end)
	if err != nil {
		return TodayView{}, err
	}
	if view.Meetings, err = meetingViews(ctx, s.store, s.clock, occs); err != nil {
		return TodayView{}, err
	}

	for _, part := range []struct {
		status string
		dst    *[]TaskView
	}{{store.StatusInbox, &view.Inbox}, {store.StatusBacklog, &view.Backlog}} {
		tasks, err := s.store.TasksByStatus(ctx, part.status)
		if err != nil {
			return TodayView{}, err
		}
		if *part.dst, err = taskViews(ctx, s.store, s.clock, tasks); err != nil {
			return TodayView{}, err
		}
	}
	return view, nil
}
