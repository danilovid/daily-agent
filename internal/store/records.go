package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Значения перечислений из модели данных.
const (
	KindTask = "task"
	KindMR   = "mr"

	SourceOwn     = "own"
	SourceTracker = "tracker"
	SourceRepo    = "repo"

	StatusInbox    = "inbox"
	StatusBacklog  = "backlog"
	StatusDone     = "done"
	StatusRejected = "rejected"

	MeetingSourceManual   = "manual"
	MeetingSourceMeetings = "meetings"
)

// Task — своя задача или внешняя запись: задача спринта или мерж-реквест.
type Task struct {
	ID         string
	Kind       string
	Source     string
	ExternalID string // пусто у своих задач
	Title      string
	Note       string
	Status     string
	DueDate    string // YYYY-MM-DD или пусто
	URL        string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Meeting — встреча.
type Meeting struct {
	ID          string
	Source      string
	ExternalID  string
	Title       string
	StartsAt    time.Time
	DurationMin int
	RepeatRule  string
	Note        string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Link — ссылка задачи или встречи.
type Link struct {
	ID       string
	OwnerID  string // task_id или meeting_id
	Title    string
	URL      string
	Position int
}

// Tag — тег.
type Tag struct {
	ID    string
	Name  string
	Color string
}

// Summary — утренняя сводка за день.
type Summary struct {
	ID        string
	Day       string
	Text      string
	SessionID string
	CreatedAt time.Time
}

// stamp возвращает время создания: заданное или текущее.
func (s *Store) stamp(t time.Time) time.Time {
	if t.IsZero() {
		return s.now()
	}
	return t
}

// CreateTask добавляет задачу. Пустые поля получают значения по умолчанию.
func (s *Store) CreateTask(ctx context.Context, t Task) (Task, error) {
	if strings.TrimSpace(t.Title) == "" {
		return Task{}, errors.New("у задачи нет названия")
	}
	if t.ID == "" {
		t.ID = NewID()
	}
	if t.Kind == "" {
		t.Kind = KindTask
	}
	if t.Source == "" {
		t.Source = SourceOwn
	}
	if t.Status == "" {
		t.Status = StatusInbox
	}
	if t.DueDate != "" {
		if _, err := time.Parse(DateLayout, t.DueDate); err != nil {
			return Task{}, fmt.Errorf("срок задачи %q: %w", t.DueDate, err)
		}
	}
	t.CreatedAt = s.stamp(t.CreatedAt)
	if t.UpdatedAt.IsZero() {
		t.UpdatedAt = t.CreatedAt
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO tasks (id, kind, source, external_id, title, note, status, due_date, url, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.Kind, t.Source, nullIfEmpty(t.ExternalID), t.Title, t.Note, t.Status,
		nullIfEmpty(t.DueDate), t.URL, FormatTime(t.CreatedAt), FormatTime(t.UpdatedAt))
	if err != nil {
		return Task{}, fmt.Errorf("запись задачи: %w", err)
	}
	return t, nil
}

// CreateMeeting добавляет встречу.
func (s *Store) CreateMeeting(ctx context.Context, m Meeting) (Meeting, error) {
	if strings.TrimSpace(m.Title) == "" {
		return Meeting{}, errors.New("у встречи нет названия")
	}
	if m.StartsAt.IsZero() {
		return Meeting{}, errors.New("у встречи нет времени начала")
	}
	if m.ID == "" {
		m.ID = NewID()
	}
	if m.Source == "" {
		m.Source = MeetingSourceManual
	}
	if m.DurationMin <= 0 {
		m.DurationMin = 30
	}
	m.CreatedAt = s.stamp(m.CreatedAt)
	if m.UpdatedAt.IsZero() {
		m.UpdatedAt = m.CreatedAt
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO meetings (id, source, external_id, title, starts_at, duration_min, repeat_rule, note, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ID, m.Source, nullIfEmpty(m.ExternalID), m.Title, FormatTime(m.StartsAt), m.DurationMin,
		m.RepeatRule, m.Note, FormatTime(m.CreatedAt), FormatTime(m.UpdatedAt))
	if err != nil {
		return Meeting{}, fmt.Errorf("запись встречи: %w", err)
	}
	return m, nil
}

// CreateTag добавляет тег.
func (s *Store) CreateTag(ctx context.Context, t Tag) (Tag, error) {
	if strings.TrimSpace(t.Name) == "" {
		return Tag{}, errors.New("у тега нет названия")
	}
	if t.ID == "" {
		t.ID = NewID()
	}
	now := FormatTime(s.now())
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO tags (id, name, color, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		t.ID, t.Name, t.Color, now, now)
	if err != nil {
		return Tag{}, fmt.Errorf("запись тега: %w", err)
	}
	return t, nil
}

// AddTaskTag ставит тег на задачу.
func (s *Store) AddTaskTag(ctx context.Context, taskID, tagID string) error {
	now := FormatTime(s.now())
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO task_tags (task_id, tag_id, created_at, updated_at) VALUES (?, ?, ?, ?)
		ON CONFLICT (task_id, tag_id) DO UPDATE SET deleted_at = NULL, updated_at = excluded.updated_at`,
		taskID, tagID, now, now)
	if err != nil {
		return fmt.Errorf("тег задачи: %w", err)
	}
	return nil
}

// AddMeetingLink добавляет ссылку на созвон.
func (s *Store) AddMeetingLink(ctx context.Context, l Link) (Link, error) {
	return s.addLink(ctx, "meeting_links", "meeting_id", l)
}

// AddTaskLink добавляет ссылку-кнопку задачи.
func (s *Store) AddTaskLink(ctx context.Context, l Link) (Link, error) {
	return s.addLink(ctx, "task_links", "task_id", l)
}

func (s *Store) addLink(ctx context.Context, table, ownerCol string, l Link) (Link, error) {
	if l.OwnerID == "" || strings.TrimSpace(l.URL) == "" {
		return Link{}, errors.New("у ссылки нет владельца или адреса")
	}
	if l.ID == "" {
		l.ID = NewID()
	}
	now := FormatTime(s.now())
	q := fmt.Sprintf(`INSERT INTO %s (id, %s, title, url, position, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, table, ownerCol)
	if _, err := s.db.ExecContext(ctx, q, l.ID, l.OwnerID, l.Title, l.URL, l.Position, now, now); err != nil {
		return Link{}, fmt.Errorf("запись ссылки: %w", err)
	}
	return l, nil
}

// CreateSummary сохраняет сводку за день.
func (s *Store) CreateSummary(ctx context.Context, sm Summary) (Summary, error) {
	if _, err := time.Parse(DateLayout, sm.Day); err != nil {
		return Summary{}, fmt.Errorf("день сводки %q: %w", sm.Day, err)
	}
	if sm.ID == "" {
		sm.ID = NewID()
	}
	sm.CreatedAt = s.stamp(sm.CreatedAt)
	ts := FormatTime(sm.CreatedAt)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO summaries (id, day, text, session_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		sm.ID, sm.Day, sm.Text, sm.SessionID, ts, ts)
	if err != nil {
		return Summary{}, fmt.Errorf("запись сводки: %w", err)
	}
	return sm, nil
}

// MeetingsBetween возвращает неудалённые встречи с началом в [from, to),
// по порядку начала.
func (s *Store) MeetingsBetween(ctx context.Context, from, to time.Time) ([]Meeting, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, source, COALESCE(external_id, ''), title, starts_at, duration_min, repeat_rule, note, created_at, updated_at
		FROM meetings
		WHERE deleted_at IS NULL AND starts_at >= ? AND starts_at < ?
		ORDER BY starts_at, title`,
		FormatTime(from), FormatTime(to))
	if err != nil {
		return nil, fmt.Errorf("чтение встреч: %w", err)
	}
	defer rows.Close()

	var out []Meeting
	for rows.Next() {
		var m Meeting
		var startsAt, createdAt, updatedAt string
		if err := rows.Scan(&m.ID, &m.Source, &m.ExternalID, &m.Title, &startsAt, &m.DurationMin,
			&m.RepeatRule, &m.Note, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		if m.StartsAt, err = ParseTime(startsAt); err != nil {
			return nil, err
		}
		if m.CreatedAt, err = ParseTime(createdAt); err != nil {
			return nil, err
		}
		if m.UpdatedAt, err = ParseTime(updatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// MeetingLinks возвращает неудалённые ссылки встреч по идентификатору встречи.
func (s *Store) MeetingLinks(ctx context.Context, meetingIDs []string) (map[string][]Link, error) {
	return s.links(ctx, "meeting_links", "meeting_id", meetingIDs)
}

func (s *Store) links(ctx context.Context, table, ownerCol string, ids []string) (map[string][]Link, error) {
	out := make(map[string][]Link, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	q := fmt.Sprintf(`SELECT id, %s, title, url, position FROM %s
		WHERE deleted_at IS NULL AND %s IN (%s)
		ORDER BY position, title`, ownerCol, table, ownerCol, placeholders(len(ids)))
	rows, err := s.db.QueryContext(ctx, q, anySlice(ids)...)
	if err != nil {
		return nil, fmt.Errorf("чтение ссылок: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var l Link
		if err := rows.Scan(&l.ID, &l.OwnerID, &l.Title, &l.URL, &l.Position); err != nil {
			return nil, err
		}
		out[l.OwnerID] = append(out[l.OwnerID], l)
	}
	return out, rows.Err()
}

// TasksByStatus возвращает неудалённые задачи с данным статусом.
// Сначала задачи со сроком по возрастанию срока, затем без срока, по времени создания.
func (s *Store) TasksByStatus(ctx context.Context, status string) ([]Task, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, kind, source, COALESCE(external_id, ''), title, note, status, COALESCE(due_date, ''), url, created_at, updated_at
		FROM tasks
		WHERE deleted_at IS NULL AND status = ?
		ORDER BY due_date IS NULL, due_date, created_at, title`, status)
	if err != nil {
		return nil, fmt.Errorf("чтение задач: %w", err)
	}
	defer rows.Close()

	var out []Task
	for rows.Next() {
		var t Task
		var createdAt, updatedAt string
		if err := rows.Scan(&t.ID, &t.Kind, &t.Source, &t.ExternalID, &t.Title, &t.Note, &t.Status,
			&t.DueDate, &t.URL, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		if t.CreatedAt, err = ParseTime(createdAt); err != nil {
			return nil, err
		}
		if t.UpdatedAt, err = ParseTime(updatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// TaskTags возвращает неудалённые теги задач по идентификатору задачи.
func (s *Store) TaskTags(ctx context.Context, taskIDs []string) (map[string][]Tag, error) {
	out := make(map[string][]Tag, len(taskIDs))
	if len(taskIDs) == 0 {
		return out, nil
	}
	q := `SELECT tt.task_id, t.id, t.name, t.color
		FROM task_tags tt JOIN tags t ON t.id = tt.tag_id
		WHERE tt.deleted_at IS NULL AND t.deleted_at IS NULL AND tt.task_id IN (` + placeholders(len(taskIDs)) + `)
		ORDER BY t.name`
	rows, err := s.db.QueryContext(ctx, q, anySlice(taskIDs)...)
	if err != nil {
		return nil, fmt.Errorf("чтение тегов задач: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var taskID string
		var t Tag
		if err := rows.Scan(&taskID, &t.ID, &t.Name, &t.Color); err != nil {
			return nil, err
		}
		out[taskID] = append(out[taskID], t)
	}
	return out, rows.Err()
}

// Tags возвращает все неудалённые теги в порядке создания.
func (s *Store) Tags(ctx context.Context) ([]Tag, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, color FROM tags WHERE deleted_at IS NULL ORDER BY created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("чтение тегов: %w", err)
	}
	defer rows.Close()
	var out []Tag
	for rows.Next() {
		var t Tag
		if err := rows.Scan(&t.ID, &t.Name, &t.Color); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// SummaryForDay возвращает последнюю неудалённую сводку за день или nil.
func (s *Store) SummaryForDay(ctx context.Context, day string) (*Summary, error) {
	var sm Summary
	var createdAt string
	err := s.db.QueryRowContext(ctx, `
		SELECT id, day, text, session_id, created_at FROM summaries
		WHERE deleted_at IS NULL AND day = ?
		ORDER BY created_at DESC LIMIT 1`, day).
		Scan(&sm.ID, &sm.Day, &sm.Text, &sm.SessionID, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("чтение сводки: %w", err)
	}
	if sm.CreatedAt, err = ParseTime(createdAt); err != nil {
		return nil, err
	}
	return &sm, nil
}

// CountTasks возвращает число неудалённых задач по статусам.
func (s *Store) CountTasks(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT status, COUNT(*) FROM tasks WHERE deleted_at IS NULL GROUP BY status`)
	if err != nil {
		return nil, fmt.Errorf("подсчёт задач: %w", err)
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			return nil, err
		}
		out[status] = n
	}
	return out, rows.Err()
}

// CountChats возвращает число неудалённых чатов.
func (s *Store) CountChats(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM chats WHERE deleted_at IS NULL`).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("подсчёт чатов: %w", err)
	}
	return n, nil
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?, ", n), ", ")
}

func anySlice(v []string) []any {
	out := make([]any, len(v))
	for i, s := range v {
		out[i] = s
	}
	return out
}
