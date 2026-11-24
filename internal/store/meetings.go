package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const meetingColumns = `id, source, COALESCE(external_id, ''), title, starts_at, duration_min,
	repeat_rule, note, created_at, updated_at`

func scanMeeting(sc scanner) (Meeting, error) {
	var m Meeting
	var startsAt, createdAt, updatedAt string
	if err := sc.Scan(&m.ID, &m.Source, &m.ExternalID, &m.Title, &startsAt, &m.DurationMin,
		&m.RepeatRule, &m.Note, &createdAt, &updatedAt); err != nil {
		return Meeting{}, err
	}
	var err error
	if m.StartsAt, err = ParseTime(startsAt); err != nil {
		return Meeting{}, err
	}
	if m.CreatedAt, err = ParseTime(createdAt); err != nil {
		return Meeting{}, err
	}
	if m.UpdatedAt, err = ParseTime(updatedAt); err != nil {
		return Meeting{}, err
	}
	return m, nil
}

func validateMeeting(m Meeting) (Meeting, error) {
	m.Title = strings.TrimSpace(m.Title)
	if m.Title == "" {
		return Meeting{}, errors.New("у встречи нет названия")
	}
	if m.StartsAt.IsZero() {
		return Meeting{}, errors.New("у встречи нет времени начала")
	}
	if m.DurationMin <= 0 {
		return Meeting{}, errors.New("длительность встречи должна быть больше нуля")
	}
	return m, nil
}

func (s *Store) prepareNewMeeting(m Meeting) (Meeting, error) {
	if m.DurationMin == 0 {
		m.DurationMin = 30
	}
	m, err := validateMeeting(m)
	if err != nil {
		return Meeting{}, err
	}
	if m.ID == "" {
		m.ID = NewID()
	}
	if m.Source == "" {
		m.Source = MeetingSourceManual
	}
	m.CreatedAt = s.stamp(m.CreatedAt)
	if m.UpdatedAt.IsZero() {
		m.UpdatedAt = m.CreatedAt
	}
	return m, nil
}

func insertMeeting(ctx context.Context, q querier, m Meeting) error {
	_, err := q.ExecContext(ctx, `
		INSERT INTO meetings (id, source, external_id, title, starts_at, duration_min, repeat_rule, note, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ID, m.Source, nullIfEmpty(m.ExternalID), m.Title, FormatTime(m.StartsAt), m.DurationMin,
		m.RepeatRule, m.Note, FormatTime(m.CreatedAt), FormatTime(m.UpdatedAt))
	if err != nil {
		return fmt.Errorf("запись встречи: %w", err)
	}
	return nil
}

// CreateMeeting добавляет встречу.
func (s *Store) CreateMeeting(ctx context.Context, m Meeting) (Meeting, error) {
	m, err := s.prepareNewMeeting(m)
	if err != nil {
		return Meeting{}, err
	}
	if err := insertMeeting(ctx, s.db, m); err != nil {
		return Meeting{}, err
	}
	return m, nil
}

// GetMeeting возвращает неудалённую встречу.
func (s *Store) GetMeeting(ctx context.Context, id string) (Meeting, error) {
	return getMeeting(ctx, s.db, id)
}

func getMeeting(ctx context.Context, q querier, id string) (Meeting, error) {
	m, err := scanMeeting(q.QueryRowContext(ctx,
		`SELECT `+meetingColumns+` FROM meetings WHERE id = ? AND deleted_at IS NULL`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Meeting{}, ErrNotFound
	}
	if err != nil {
		return Meeting{}, fmt.Errorf("чтение встречи: %w", err)
	}
	return m, nil
}

// SaveMeeting создаёт встречу (пустой ID) или обновляет её поля title,
// starts_at, duration_min, repeat_rule и note, а заодно заменяет ссылки.
// Всё в одной транзакции.
func (s *Store) SaveMeeting(ctx context.Context, m Meeting, links []Link) (Meeting, error) {
	var saved Meeting
	err := s.inTx(ctx, func(q querier) error {
		now := s.now()
		if m.ID == "" {
			nm, err := s.prepareNewMeeting(m)
			if err != nil {
				return err
			}
			if err := insertMeeting(ctx, q, nm); err != nil {
				return err
			}
			m.ID = nm.ID
		} else {
			vm, err := validateMeeting(m)
			if err != nil {
				return err
			}
			res, err := q.ExecContext(ctx, `
				UPDATE meetings SET title = ?, starts_at = ?, duration_min = ?, repeat_rule = ?, note = ?, updated_at = ?
				WHERE id = ? AND deleted_at IS NULL`,
				vm.Title, FormatTime(vm.StartsAt), vm.DurationMin, vm.RepeatRule, vm.Note, FormatTime(now), vm.ID)
			if err := affectedOne(res, err); err != nil {
				return err
			}
		}
		if err := replaceLinks(ctx, q, "meeting_links", "meeting_id", m.ID, links, FormatTime(now)); err != nil {
			return err
		}
		var err error
		saved, err = getMeeting(ctx, q, m.ID)
		return err
	})
	return saved, err
}

// DeleteMeeting мягко удаляет встречу. У повторяющейся встречи удаляется вся серия.
func (s *Store) DeleteMeeting(ctx context.Context, id string) error {
	now := FormatTime(s.now())
	res, err := s.db.ExecContext(ctx, `UPDATE meetings SET deleted_at = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL`,
		now, now, id)
	return affectedOne(res, err)
}

// MeetingsForWindow возвращает неудалённые встречи, которые могут попасть
// в промежуток [from, to): разовые с началом в нём и повторяющиеся, чья серия
// началась до to. Серии разворачивает вызывающий код.
func (s *Store) MeetingsForWindow(ctx context.Context, from, to time.Time) ([]Meeting, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+meetingColumns+`
		FROM meetings
		WHERE deleted_at IS NULL AND (
			(repeat_rule = '' AND starts_at >= ? AND starts_at < ?) OR
			(repeat_rule <> '' AND starts_at < ?))
		ORDER BY starts_at, title`,
		FormatTime(from), FormatTime(to), FormatTime(to))
	if err != nil {
		return nil, fmt.Errorf("чтение встреч: %w", err)
	}
	defer rows.Close()
	var out []Meeting
	for rows.Next() {
		m, err := scanMeeting(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
