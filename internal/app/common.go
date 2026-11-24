package app

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"daily-agent/internal/recur"
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

func (c Clock) today() string { return c.now().Format(store.DateLayout) }

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

func (c Clock) format(t time.Time) string { return t.In(c.loc()).Format(time.RFC3339) }

// userError переводит ошибки хранилища в понятный окну текст.
func userError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, store.ErrNotFound):
		return errors.New("запись не найдена: возможно, её уже удалили")
	default:
		return err
	}
}

// occurrence — одна встреча: разовая или встреча серии.
type occurrence struct {
	meeting store.Meeting
	start   time.Time
}

func (o occurrence) end() time.Time {
	return o.start.Add(time.Duration(o.meeting.DurationMin) * time.Minute)
}

// loadOccurrences возвращает встречи с началом в [from, to), развернув серии,
// по порядку начала.
func loadOccurrences(ctx context.Context, st *store.Store, loc *time.Location, from, to time.Time) ([]occurrence, error) {
	meetings, err := st.MeetingsForWindow(ctx, from, to)
	if err != nil {
		return nil, err
	}
	var out []occurrence
	for _, m := range meetings {
		rule, err := recur.Parse(m.RepeatRule)
		if err != nil {
			// Непонятное правило показываем как разовую встречу, а не теряем её.
			rule = recur.Rule{}
		}
		for _, start := range recur.Occurrences(m.StartsAt, rule, loc, from, to) {
			out = append(out, occurrence{meeting: m, start: start})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].start.Equal(out[j].start) {
			return out[i].start.Before(out[j].start)
		}
		return out[i].meeting.Title < out[j].meeting.Title
	})
	return out, nil
}

// meetingViews собирает строки встреч со ссылками.
func meetingViews(ctx context.Context, st *store.Store, clock Clock, occs []occurrence) ([]MeetingView, error) {
	ids := make([]string, 0, len(occs))
	seen := map[string]bool{}
	for _, o := range occs {
		if !seen[o.meeting.ID] {
			seen[o.meeting.ID] = true
			ids = append(ids, o.meeting.ID)
		}
	}
	links, err := st.MeetingLinks(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]MeetingView, 0, len(occs))
	for _, o := range occs {
		out = append(out, MeetingView{
			ID:          o.meeting.ID,
			Title:       o.meeting.Title,
			StartsAt:    clock.format(o.start),
			DurationMin: o.meeting.DurationMin,
			Repeat:      o.meeting.RepeatRule,
			Links:       linkViews(links[o.meeting.ID]),
		})
	}
	return out, nil
}

// taskViews собирает строки задач с тегами и ссылками.
func taskViews(ctx context.Context, st *store.Store, clock Clock, tasks []store.Task) ([]TaskView, error) {
	ids := make([]string, len(tasks))
	for i, t := range tasks {
		ids[i] = t.ID
	}
	tags, err := st.TaskTags(ctx, ids)
	if err != nil {
		return nil, err
	}
	links, err := st.TaskLinks(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]TaskView, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, taskView(clock, t, tags[t.ID], links[t.ID]))
	}
	return out, nil
}

func taskView(clock Clock, t store.Task, tags []store.Tag, links []store.Link) TaskView {
	return TaskView{
		ID:        t.ID,
		Kind:      t.Kind,
		Source:    t.Source,
		Title:     t.Title,
		Status:    t.Status,
		DueDate:   t.DueDate,
		URL:       t.URL,
		CreatedAt: clock.format(t.CreatedAt),
		Tags:      tagViews(tags),
		Links:     linkViews(links),
	}
}

func tagViews(tags []store.Tag) []TagView {
	out := make([]TagView, 0, len(tags))
	for _, t := range tags {
		out = append(out, TagView{ID: t.ID, Name: t.Name, Color: t.Color})
	}
	return out
}

func linkViews(links []store.Link) []LinkView {
	out := make([]LinkView, 0, len(links))
	for _, l := range links {
		out = append(out, LinkView{ID: l.ID, Title: l.Title, URL: l.URL})
	}
	return out
}

// MaxLinks — сколько ссылок можно добавить задаче или встрече.
const MaxLinks = 10

// checkURL пропускает только адреса http и https с именем хоста.
func checkURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("адрес %q: нужна ссылка вида https://…", raw)
	}
	return raw, nil
}

// cleanLinks проверяет ссылки из карточки. Пустое название заменяется на defaultTitle.
func cleanLinks(in []LinkInput, defaultTitle string) ([]store.Link, error) {
	if len(in) > MaxLinks {
		return nil, fmt.Errorf("ссылок больше %d", MaxLinks)
	}
	out := make([]store.Link, 0, len(in))
	for _, l := range in {
		u, err := checkURL(l.URL)
		if err != nil {
			return nil, err
		}
		title := strings.TrimSpace(l.Title)
		if title == "" {
			title = defaultTitle
		}
		out = append(out, store.Link{ID: l.ID, Title: title, URL: u})
	}
	return out, nil
}
