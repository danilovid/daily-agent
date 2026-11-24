package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

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

// replaceLinks приводит ссылки владельца к списку links: ссылки с известным ID
// обновляются, новые добавляются, отсутствующие в списке мягко удаляются.
// Порядок в списке становится полем position.
func replaceLinks(ctx context.Context, q querier, table, ownerCol, ownerID string, links []Link, now string) error {
	rows, err := q.QueryContext(ctx, fmt.Sprintf(
		`SELECT id FROM %s WHERE %s = ? AND deleted_at IS NULL`, table, ownerCol), ownerID)
	if err != nil {
		return fmt.Errorf("чтение ссылок: %w", err)
	}
	existing := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		existing[id] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	kept := map[string]bool{}
	for i, l := range links {
		if strings.TrimSpace(l.URL) == "" {
			return errors.New("у ссылки нет адреса")
		}
		if l.ID != "" && existing[l.ID] {
			_, err := q.ExecContext(ctx, fmt.Sprintf(
				`UPDATE %s SET title = ?, url = ?, position = ?, updated_at = ? WHERE id = ?`, table),
				l.Title, l.URL, i, now, l.ID)
			if err != nil {
				return fmt.Errorf("обновление ссылки: %w", err)
			}
			kept[l.ID] = true
			continue
		}
		_, err := q.ExecContext(ctx, fmt.Sprintf(
			`INSERT INTO %s (id, %s, title, url, position, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			table, ownerCol), NewID(), ownerID, l.Title, l.URL, i, now, now)
		if err != nil {
			return fmt.Errorf("запись ссылки: %w", err)
		}
	}
	for id := range existing {
		if kept[id] {
			continue
		}
		if _, err := q.ExecContext(ctx, fmt.Sprintf(
			`UPDATE %s SET deleted_at = ?, updated_at = ? WHERE id = ?`, table), now, now, id); err != nil {
			return fmt.Errorf("удаление ссылки: %w", err)
		}
	}
	return nil
}

// MeetingLinks возвращает неудалённые ссылки встреч по идентификатору встречи.
func (s *Store) MeetingLinks(ctx context.Context, meetingIDs []string) (map[string][]Link, error) {
	return s.links(ctx, "meeting_links", "meeting_id", meetingIDs)
}

// TaskLinks возвращает неудалённые ссылки задач по идентификатору задачи.
func (s *Store) TaskLinks(ctx context.Context, taskIDs []string) (map[string][]Link, error) {
	return s.links(ctx, "task_links", "task_id", taskIDs)
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

// MeetingLink возвращает неудалённую ссылку неудалённой встречи.
func (s *Store) MeetingLink(ctx context.Context, id string) (Link, error) {
	return s.link(ctx, "meeting_links", "meeting_id", "meetings", id)
}

// TaskLink возвращает неудалённую ссылку неудалённой задачи.
func (s *Store) TaskLink(ctx context.Context, id string) (Link, error) {
	return s.link(ctx, "task_links", "task_id", "tasks", id)
}

func (s *Store) link(ctx context.Context, table, ownerCol, ownerTable, id string) (Link, error) {
	var l Link
	err := s.db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT l.id, l.%s, l.title, l.url, l.position FROM %s l
		JOIN %s o ON o.id = l.%s
		WHERE l.id = ? AND l.deleted_at IS NULL AND o.deleted_at IS NULL`, ownerCol, table, ownerTable, ownerCol), id).
		Scan(&l.ID, &l.OwnerID, &l.Title, &l.URL, &l.Position)
	if errors.Is(err, sql.ErrNoRows) {
		return Link{}, ErrNotFound
	}
	if err != nil {
		return Link{}, fmt.Errorf("чтение ссылки: %w", err)
	}
	return l, nil
}
