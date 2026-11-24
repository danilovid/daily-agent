package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Виды чатов.
const (
	ChatKindUser      = "user"
	ChatKindScheduled = "scheduled"
)

// Chat — ссылка на сессию OpenCode. Переписка лежит в базе OpenCode.
type Chat struct {
	ID        string
	SessionID string
	Title     string
	Kind      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

const chatColumns = `id, session_id, title, kind, created_at, updated_at`

func scanChat(sc scanner) (Chat, error) {
	var c Chat
	var createdAt, updatedAt string
	if err := sc.Scan(&c.ID, &c.SessionID, &c.Title, &c.Kind, &createdAt, &updatedAt); err != nil {
		return Chat{}, err
	}
	var err error
	if c.CreatedAt, err = ParseTime(createdAt); err != nil {
		return Chat{}, err
	}
	if c.UpdatedAt, err = ParseTime(updatedAt); err != nil {
		return Chat{}, err
	}
	return c, nil
}

// CreateChat сохраняет ссылку на сессию OpenCode.
func (s *Store) CreateChat(ctx context.Context, c Chat) (Chat, error) {
	if strings.TrimSpace(c.SessionID) == "" {
		return Chat{}, errors.New("у чата нет сессии")
	}
	if c.ID == "" {
		c.ID = NewID()
	}
	if c.Kind == "" {
		c.Kind = ChatKindUser
	}
	c.CreatedAt = s.stamp(c.CreatedAt)
	c.UpdatedAt = c.CreatedAt
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO chats (id, session_id, title, kind, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		c.ID, c.SessionID, c.Title, c.Kind, FormatTime(c.CreatedAt), FormatTime(c.UpdatedAt))
	if err != nil {
		return Chat{}, fmt.Errorf("запись чата: %w", err)
	}
	return c, nil
}

// GetChat возвращает неудалённый чат.
func (s *Store) GetChat(ctx context.Context, id string) (Chat, error) {
	return s.chatWhere(ctx, "id = ?", id)
}

// ChatBySession возвращает неудалённый чат по сессии OpenCode.
func (s *Store) ChatBySession(ctx context.Context, sessionID string) (Chat, error) {
	return s.chatWhere(ctx, "session_id = ?", sessionID)
}

func (s *Store) chatWhere(ctx context.Context, cond string, arg any) (Chat, error) {
	c, err := scanChat(s.db.QueryRowContext(ctx,
		`SELECT `+chatColumns+` FROM chats WHERE `+cond+` AND deleted_at IS NULL`, arg))
	if errors.Is(err, sql.ErrNoRows) {
		return Chat{}, ErrNotFound
	}
	if err != nil {
		return Chat{}, fmt.Errorf("чтение чата: %w", err)
	}
	return c, nil
}

// ListChats возвращает неудалённые чаты, свежие первыми.
func (s *Store) ListChats(ctx context.Context, limit int) ([]Chat, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+chatColumns+` FROM chats WHERE deleted_at IS NULL
		ORDER BY updated_at DESC, created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("чтение чатов: %w", err)
	}
	defer rows.Close()
	var out []Chat
	for rows.Next() {
		c, err := scanChat(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SetChatTitle меняет заголовок чата.
func (s *Store) SetChatTitle(ctx context.Context, id, title string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE chats SET title = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL`,
		title, FormatTime(s.now()), id)
	return affectedOne(res, err)
}

// TouchChat отмечает, что в чате было сообщение: чат поднимается в списке.
func (s *Store) TouchChat(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE chats SET updated_at = ? WHERE id = ? AND deleted_at IS NULL`,
		FormatTime(s.now()), id)
	return affectedOne(res, err)
}

// DeleteChat мягко удаляет чат. Сессия в OpenCode остаётся.
func (s *Store) DeleteChat(ctx context.Context, id string) error {
	now := FormatTime(s.now())
	res, err := s.db.ExecContext(ctx, `UPDATE chats SET deleted_at = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL`,
		now, now, id)
	return affectedOne(res, err)
}
