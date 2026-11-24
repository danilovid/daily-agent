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

// ErrNotFound — записи нет или она удалена.
var ErrNotFound = errors.New("запись не найдена")

// ErrDuplicate — запись с таким именем уже есть.
var ErrDuplicate = errors.New("такая запись уже есть")

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

// Meeting — встреча. У повторяющейся встречи StartsAt — начало первой встречи серии.
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

// querier — общее у соединения и транзакции.
type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type scanner interface{ Scan(dest ...any) error }

// inTx выполняет fn в транзакции и откатывает её при ошибке.
func (s *Store) inTx(ctx context.Context, fn func(q querier) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// stamp возвращает время создания: заданное или текущее.
func (s *Store) stamp(t time.Time) time.Time {
	if t.IsZero() {
		return s.now()
	}
	return t
}

func validDate(v string) error {
	if v == "" {
		return nil
	}
	if _, err := time.Parse(DateLayout, v); err != nil {
		return fmt.Errorf("дата %q: ждали формат ГГГГ-ММ-ДД", v)
	}
	return nil
}

// affectedOne превращает «ни одной изменённой строки» в ErrNotFound.
func affectedOne(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// CreateSummary сохраняет сводку за день.
func (s *Store) CreateSummary(ctx context.Context, sm Summary) (Summary, error) {
	if err := validDate(sm.Day); err != nil || sm.Day == "" {
		return Summary{}, fmt.Errorf("день сводки %q: ждали формат ГГГГ-ММ-ДД", sm.Day)
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
