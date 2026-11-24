package app

import (
	"context"
	"errors"
	"strings"

	"daily-agent/internal/recur"
	"daily-agent/internal/store"
)

// SearchLimit — сколько записей возвращает поиск.
const SearchLimit = 50

// SearchService — поиск из боковой панели.
type SearchService struct {
	store *store.Store
	clock Clock
}

// NewSearchService создаёт сервис поиска.
func NewSearchService(s *store.Store, clock Clock) *SearchService {
	return &SearchService{store: s, clock: clock}
}

// Search ищет задачи и встречи по началам слов в названиях и заметках.
// Регистр не важен, «ё» и «е» не различаются. У повторяющейся встречи
// показывается ближайшая будущая встреча серии.
func (s *SearchService) Search(ctx context.Context, query string) (SearchView, error) {
	query = strings.TrimSpace(query)
	view := SearchView{Query: query, Tasks: []TaskView{}, Meetings: []MeetingView{}}
	hits, err := s.store.Search(ctx, query, SearchLimit)
	if err != nil {
		return SearchView{}, err
	}
	var tasks []store.Task
	var occs []occurrence
	now := s.clock.now()
	for _, h := range hits {
		switch h.Kind {
		case store.SearchKindTask:
			t, err := s.store.GetTask(ctx, h.ID)
			if errors.Is(err, store.ErrNotFound) {
				continue
			}
			if err != nil {
				return SearchView{}, err
			}
			tasks = append(tasks, t)
		case store.SearchKindMeeting:
			m, err := s.store.GetMeeting(ctx, h.ID)
			if errors.Is(err, store.ErrNotFound) {
				continue
			}
			if err != nil {
				return SearchView{}, err
			}
			start := m.StartsAt
			if rule, err := recur.Parse(m.RepeatRule); err == nil && !rule.IsZero() {
				if next := recur.Occurrences(m.StartsAt, rule, s.clock.loc(), now, now.AddDate(1, 0, 0)); len(next) > 0 {
					start = next[0]
				}
			}
			occs = append(occs, occurrence{meeting: m, start: start})
		}
	}
	if view.Tasks, err = taskViews(ctx, s.store, s.clock, tasks); err != nil {
		return SearchView{}, err
	}
	if view.Meetings, err = meetingViews(ctx, s.store, s.clock, occs); err != nil {
		return SearchView{}, err
	}
	return view, nil
}
