package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"daily-agent/internal/store"
)

// MonthLayout — формат месяца календаря.
const MonthLayout = "2006-01"

// SidebarService отдаёт данные боковой панели: числа у разделов, теги
// и дни месяца, в которые есть встречи.
type SidebarService struct {
	store *store.Store
	clock Clock
}

// NewSidebarService создаёт сервис боковой панели.
func NewSidebarService(s *store.Store, clock Clock) *SidebarService {
	return &SidebarService{store: s, clock: clock}
}

// Sidebar возвращает данные боковой панели для месяца month в формате ГГГГ-ММ.
// Пустой month означает текущий месяц.
func (s *SidebarService) Sidebar(ctx context.Context, month string) (SidebarView, error) {
	now := s.clock.now()
	today := now.Format(store.DateLayout)
	month = strings.TrimSpace(month)
	if month == "" {
		month = now.Format(MonthLayout)
	}
	m, err := time.ParseInLocation(MonthLayout, month, s.clock.loc())
	if err != nil {
		return SidebarView{}, fmt.Errorf("месяц %q: ждали формат ГГГГ-ММ", month)
	}

	view := SidebarView{Today: today, Month: month, Tags: []TagView{}, MeetingDays: []int{}}

	counts, err := s.store.CountTasks(ctx)
	if err != nil {
		return SidebarView{}, err
	}
	view.Counts.Inbox = counts[store.StatusInbox]
	view.Counts.Backlog = counts[store.StatusBacklog]

	dayStart, dayEnd, err := s.clock.dayBounds(today)
	if err != nil {
		return SidebarView{}, err
	}
	todayMeetings, err := s.store.MeetingsBetween(ctx, dayStart, dayEnd)
	if err != nil {
		return SidebarView{}, err
	}
	view.Counts.MeetingsToday = len(todayMeetings)

	if view.Counts.Chats, err = s.store.CountChats(ctx); err != nil {
		return SidebarView{}, err
	}

	tags, err := s.store.Tags(ctx)
	if err != nil {
		return SidebarView{}, err
	}
	view.Tags = tagViews(tags)

	monthStart := time.Date(m.Year(), m.Month(), 1, 0, 0, 0, 0, s.clock.loc())
	monthEnd := time.Date(m.Year(), m.Month()+1, 1, 0, 0, 0, 0, s.clock.loc())
	meetings, err := s.store.MeetingsBetween(ctx, monthStart, monthEnd)
	if err != nil {
		return SidebarView{}, err
	}
	seen := map[int]bool{}
	for _, mt := range meetings {
		d := mt.StartsAt.In(s.clock.loc()).Day()
		if !seen[d] {
			seen[d] = true
			view.MeetingDays = append(view.MeetingDays, d)
		}
	}
	return view, nil
}
