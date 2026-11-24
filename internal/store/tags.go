package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// CreateTag добавляет тег. Имя уникально без учёта регистра.
func (s *Store) CreateTag(ctx context.Context, t Tag) (Tag, error) {
	t.Name = strings.TrimSpace(t.Name)
	if t.Name == "" {
		return Tag{}, errors.New("у тега нет названия")
	}
	existing, err := s.Tags(ctx)
	if err != nil {
		return Tag{}, err
	}
	for _, e := range existing {
		// SQLite меняет регистр только у латиницы, поэтому сравниваем в Go.
		if strings.EqualFold(e.Name, t.Name) {
			return Tag{}, fmt.Errorf("тег «%s»: %w", e.Name, ErrDuplicate)
		}
	}
	if t.ID == "" {
		t.ID = NewID()
	}
	now := FormatTime(s.now())
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO tags (id, name, color, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		t.ID, t.Name, t.Color, now, now)
	if err != nil {
		return Tag{}, fmt.Errorf("запись тега: %w", err)
	}
	return t, nil
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

// setTaskTags оставляет на задаче ровно теги tagIDs: снятые помечаются
// удалёнными, новые добавляются, снятые раньше возвращаются.
func setTaskTags(ctx context.Context, q querier, taskID string, tagIDs []string, now string) error {
	want := map[string]bool{}
	for _, id := range tagIDs {
		if id != "" {
			want[id] = true
		}
	}
	rows, err := q.QueryContext(ctx, `SELECT tag_id FROM task_tags WHERE task_id = ? AND deleted_at IS NULL`, taskID)
	if err != nil {
		return fmt.Errorf("чтение тегов задачи: %w", err)
	}
	var current []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		current = append(current, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range current {
		if want[id] {
			delete(want, id)
			continue
		}
		if _, err := q.ExecContext(ctx, `UPDATE task_tags SET deleted_at = ?, updated_at = ? WHERE task_id = ? AND tag_id = ?`,
			now, now, taskID, id); err != nil {
			return fmt.Errorf("снятие тега: %w", err)
		}
	}
	for id := range want {
		if _, err := q.ExecContext(ctx, `
			INSERT INTO task_tags (task_id, tag_id, created_at, updated_at) VALUES (?, ?, ?, ?)
			ON CONFLICT (task_id, tag_id) DO UPDATE SET deleted_at = NULL, updated_at = excluded.updated_at`,
			taskID, id, now, now); err != nil {
			return fmt.Errorf("тег задачи: %w", err)
		}
	}
	return nil
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
