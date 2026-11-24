package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

const taskColumns = `id, kind, source, COALESCE(external_id, ''), title, note, status,
	COALESCE(due_date, ''), url, created_at, updated_at`

func scanTask(sc scanner) (Task, error) {
	var t Task
	var createdAt, updatedAt string
	if err := sc.Scan(&t.ID, &t.Kind, &t.Source, &t.ExternalID, &t.Title, &t.Note, &t.Status,
		&t.DueDate, &t.URL, &createdAt, &updatedAt); err != nil {
		return Task{}, err
	}
	var err error
	if t.CreatedAt, err = ParseTime(createdAt); err != nil {
		return Task{}, err
	}
	if t.UpdatedAt, err = ParseTime(updatedAt); err != nil {
		return Task{}, err
	}
	return t, nil
}

// prepareNewTask заполняет поля новой задачи значениями по умолчанию и проверяет их.
func (s *Store) prepareNewTask(t Task) (Task, error) {
	t.Title = strings.TrimSpace(t.Title)
	if t.Title == "" {
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
	if err := validDate(t.DueDate); err != nil {
		return Task{}, err
	}
	t.CreatedAt = s.stamp(t.CreatedAt)
	if t.UpdatedAt.IsZero() {
		t.UpdatedAt = t.CreatedAt
	}
	return t, nil
}

func insertTask(ctx context.Context, q querier, t Task) error {
	_, err := q.ExecContext(ctx, `
		INSERT INTO tasks (id, kind, source, external_id, title, note, status, due_date, url, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.Kind, t.Source, nullIfEmpty(t.ExternalID), t.Title, t.Note, t.Status,
		nullIfEmpty(t.DueDate), t.URL, FormatTime(t.CreatedAt), FormatTime(t.UpdatedAt))
	if err != nil {
		return fmt.Errorf("запись задачи: %w", err)
	}
	return nil
}

// CreateTask добавляет задачу. Пустые поля получают значения по умолчанию.
func (s *Store) CreateTask(ctx context.Context, t Task) (Task, error) {
	t, err := s.prepareNewTask(t)
	if err != nil {
		return Task{}, err
	}
	if err := insertTask(ctx, s.db, t); err != nil {
		return Task{}, err
	}
	return t, nil
}

// GetTask возвращает неудалённую задачу.
func (s *Store) GetTask(ctx context.Context, id string) (Task, error) {
	return getTask(ctx, s.db, id)
}

func getTask(ctx context.Context, q querier, id string) (Task, error) {
	t, err := scanTask(q.QueryRowContext(ctx,
		`SELECT `+taskColumns+` FROM tasks WHERE id = ? AND deleted_at IS NULL`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, ErrNotFound
	}
	if err != nil {
		return Task{}, fmt.Errorf("чтение задачи: %w", err)
	}
	return t, nil
}

// TaskFilter — условия выборки задач.
type TaskFilter struct {
	Statuses []string // пусто — любые статусы
	TagID    string   // пусто — любые теги
}

// ListTasks возвращает неудалённые задачи. Сначала задачи со сроком по
// возрастанию срока, затем без срока, по времени создания.
func (s *Store) ListTasks(ctx context.Context, f TaskFilter) ([]Task, error) {
	where := []string{"deleted_at IS NULL"}
	var args []any
	if len(f.Statuses) > 0 {
		where = append(where, "status IN ("+placeholders(len(f.Statuses))+")")
		args = append(args, anySlice(f.Statuses)...)
	}
	if f.TagID != "" {
		where = append(where, `EXISTS (SELECT 1 FROM task_tags tt
			WHERE tt.task_id = tasks.id AND tt.tag_id = ? AND tt.deleted_at IS NULL)`)
		args = append(args, f.TagID)
	}
	q := `SELECT ` + taskColumns + ` FROM tasks WHERE ` + strings.Join(where, " AND ") +
		` ORDER BY due_date IS NULL, due_date, created_at, title`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("чтение задач: %w", err)
	}
	defer rows.Close()
	var out []Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// TasksByStatus возвращает неудалённые задачи с данным статусом.
func (s *Store) TasksByStatus(ctx context.Context, status string) ([]Task, error) {
	return s.ListTasks(ctx, TaskFilter{Statuses: []string{status}})
}

// SaveTask создаёт задачу (пустой ID) или обновляет её поля title, note,
// status, due_date и url, а заодно заменяет её теги и ссылки. Всё в одной транзакции.
func (s *Store) SaveTask(ctx context.Context, t Task, tagIDs []string, links []Link) (Task, error) {
	var saved Task
	err := s.inTx(ctx, func(q querier) error {
		now := s.now()
		if t.ID == "" {
			nt, err := s.prepareNewTask(t)
			if err != nil {
				return err
			}
			if err := insertTask(ctx, q, nt); err != nil {
				return err
			}
			t.ID = nt.ID
		} else {
			t.Title = strings.TrimSpace(t.Title)
			if t.Title == "" {
				return errors.New("у задачи нет названия")
			}
			if err := validDate(t.DueDate); err != nil {
				return err
			}
			res, err := q.ExecContext(ctx, `
				UPDATE tasks SET title = ?, note = ?, status = ?, due_date = ?, url = ?, updated_at = ?
				WHERE id = ? AND deleted_at IS NULL`,
				t.Title, t.Note, t.Status, nullIfEmpty(t.DueDate), t.URL, FormatTime(now), t.ID)
			if err := affectedOne(res, err); err != nil {
				return err
			}
		}
		if err := setTaskTags(ctx, q, t.ID, tagIDs, FormatTime(now)); err != nil {
			return err
		}
		if err := replaceLinks(ctx, q, "task_links", "task_id", t.ID, links, FormatTime(now)); err != nil {
			return err
		}
		var err error
		saved, err = getTask(ctx, q, t.ID)
		return err
	})
	return saved, err
}

// SetTaskStatus меняет статус неудалённой задачи.
func (s *Store) SetTaskStatus(ctx context.Context, id, status string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE tasks SET status = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL`,
		status, FormatTime(s.now()), id)
	return affectedOne(res, err)
}

// DeleteTask мягко удаляет задачу.
func (s *Store) DeleteTask(ctx context.Context, id string) error {
	now := FormatTime(s.now())
	res, err := s.db.ExecContext(ctx, `UPDATE tasks SET deleted_at = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL`,
		now, now, id)
	return affectedOne(res, err)
}
