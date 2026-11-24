package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"daily-agent/internal/recur"
	"daily-agent/internal/store"
)

// Периоды экрана «Встречи».
const (
	PeriodUpcoming = "upcoming"
	PeriodPast     = "past"
)

// UpcomingDays — на сколько дней вперёд показываются предстоящие встречи.
const UpcomingDays = 14

// PastDays — на сколько дней назад показываются прошедшие встречи.
const PastDays = 30

// MaxDurationMin — самая длинная встреча.
const MaxDurationMin = 24 * 60

// RepeatOptions — варианты повтора в карточке встречи.
var RepeatOptions = []RepeatOption{
	{Value: "", Label: "Не повторять"},
	{Value: "FREQ=DAILY", Label: "Каждый день"},
	{Value: "FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR", Label: "Каждый будний день"},
	{Value: "FREQ=WEEKLY", Label: "Каждую неделю"},
	{Value: "FREQ=WEEKLY;INTERVAL=2", Label: "Каждые 2 недели"},
	{Value: "FREQ=MONTHLY", Label: "Каждый месяц"},
}

// MeetingsService — экран и карточка встреч.
type MeetingsService struct {
	store *store.Store
	clock Clock
}

// NewMeetingsService создаёт сервис встреч.
func NewMeetingsService(s *store.Store, clock Clock) *MeetingsService {
	return &MeetingsService{store: s, clock: clock}
}

// RepeatOptions возвращает варианты повтора для карточки встречи.
func (s *MeetingsService) RepeatOptions() []RepeatOption { return RepeatOptions }

// List возвращает встречи по дням. period: upcoming — от текущего момента
// на UpcomingDays дней вперёд, past — за PastDays дней назад, дни от новых к старым.
func (s *MeetingsService) List(ctx context.Context, period string) (MeetingsView, error) {
	now := s.clock.now()
	loc := s.clock.loc()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)

	upcoming, err := loadOccurrences(ctx, s.store, loc, todayStart, todayStart.AddDate(0, 0, UpcomingDays+1))
	if err != nil {
		return MeetingsView{}, err
	}
	var notEnded []occurrence
	for _, o := range upcoming {
		if o.end().After(now) {
			notEnded = append(notEnded, o)
		}
	}
	view := MeetingsView{Period: period, UpcomingCount: len(notEnded), Days: []MeetingDayView{}}

	var selected []occurrence
	switch period {
	case PeriodUpcoming, "":
		view.Period = PeriodUpcoming
		selected = notEnded
	case PeriodPast:
		past, err := loadOccurrences(ctx, s.store, loc, todayStart.AddDate(0, 0, -PastDays), now)
		if err != nil {
			return MeetingsView{}, err
		}
		for _, o := range past {
			if !o.end().After(now) {
				selected = append(selected, o)
			}
		}
	default:
		return MeetingsView{}, fmt.Errorf("неизвестный период %q", period)
	}

	views, err := meetingViews(ctx, s.store, s.clock, selected)
	if err != nil {
		return MeetingsView{}, err
	}
	for i, o := range selected {
		day := o.start.In(loc).Format(store.DateLayout)
		if n := len(view.Days); n == 0 || view.Days[n-1].Day != day {
			view.Days = append(view.Days, MeetingDayView{Day: day, Meetings: []MeetingView{}})
		}
		last := &view.Days[len(view.Days)-1]
		last.Meetings = append(last.Meetings, views[i])
	}
	if view.Period == PeriodPast {
		for i, j := 0, len(view.Days)-1; i < j; i, j = i+1, j-1 {
			view.Days[i], view.Days[j] = view.Days[j], view.Days[i]
		}
	}
	return view, nil
}

// Get возвращает встречу для карточки.
func (s *MeetingsService) Get(ctx context.Context, id string) (MeetingDetail, error) {
	m, err := s.store.GetMeeting(ctx, id)
	if err != nil {
		return MeetingDetail{}, userError(err)
	}
	return s.detail(ctx, m)
}

func (s *MeetingsService) detail(ctx context.Context, m store.Meeting) (MeetingDetail, error) {
	links, err := s.store.MeetingLinks(ctx, []string{m.ID})
	if err != nil {
		return MeetingDetail{}, err
	}
	start := m.StartsAt.In(s.clock.loc())
	return MeetingDetail{
		ID:          m.ID,
		Source:      m.Source,
		Title:       m.Title,
		Date:        start.Format(store.DateLayout),
		Start:       start.Format("15:04"),
		DurationMin: m.DurationMin,
		Repeat:      m.RepeatRule,
		Note:        m.Note,
		Links:       linkViews(links[m.ID]),
		Editable:    m.Source == store.MeetingSourceManual,
	}, nil
}

// fromInput проверяет поля карточки и переносит их во встречу.
func (s *MeetingsService) fromInput(m store.Meeting, in MeetingInput) (store.Meeting, []store.Link, error) {
	links, err := cleanLinks(in.Links, "Созвон")
	if err != nil {
		return store.Meeting{}, nil, err
	}
	m.Note = in.Note
	if m.Source != "" && m.Source != store.MeetingSourceManual {
		// Встреча из сервиса видеовстреч: название и время меняет только источник.
		return m, links, nil
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return store.Meeting{}, nil, errors.New("введите название встречи")
	}
	startsAt, err := time.ParseInLocation(store.DateLayout+" 15:04", in.Date+" "+in.Start, s.clock.loc())
	if err != nil {
		return store.Meeting{}, nil, errors.New("укажите дату и время начала встречи")
	}
	if in.DurationMin <= 0 || in.DurationMin > MaxDurationMin {
		return store.Meeting{}, nil, fmt.Errorf("длительность должна быть от 1 до %d минут", MaxDurationMin)
	}
	repeat, err := recur.Canonical(in.Repeat)
	if err != nil {
		return store.Meeting{}, nil, errors.New("неизвестный вариант повтора")
	}
	m.Title = title
	m.StartsAt = startsAt
	m.DurationMin = in.DurationMin
	m.RepeatRule = repeat
	return m, links, nil
}

// Create добавляет встречу вручную.
func (s *MeetingsService) Create(ctx context.Context, in MeetingInput) (MeetingDetail, error) {
	m, links, err := s.fromInput(store.Meeting{Source: store.MeetingSourceManual}, in)
	if err != nil {
		return MeetingDetail{}, err
	}
	saved, err := s.store.SaveMeeting(ctx, m, links)
	if err != nil {
		return MeetingDetail{}, userError(err)
	}
	return s.detail(ctx, saved)
}

// Update меняет встречу. У повторяющейся встречи меняется вся серия.
func (s *MeetingsService) Update(ctx context.Context, id string, in MeetingInput) (MeetingDetail, error) {
	current, err := s.store.GetMeeting(ctx, id)
	if err != nil {
		return MeetingDetail{}, userError(err)
	}
	m, links, err := s.fromInput(current, in)
	if err != nil {
		return MeetingDetail{}, err
	}
	saved, err := s.store.SaveMeeting(ctx, m, links)
	if err != nil {
		return MeetingDetail{}, userError(err)
	}
	return s.detail(ctx, saved)
}

// Delete удаляет встречу. У повторяющейся встречи удаляется вся серия.
func (s *MeetingsService) Delete(ctx context.Context, id string) error {
	return userError(s.store.DeleteMeeting(ctx, id))
}
