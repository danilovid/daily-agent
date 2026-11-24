package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Кто составил план.
const (
	PlanSourceAgent  = "agent"
	PlanSourceManual = "manual"
)

// DayPlan — план на день.
type DayPlan struct {
	ID      string
	Day     string // ГГГГ-ММ-ДД
	Summary string // пояснение агента
	Source  string
	// SessionID — сессия OpenCode, в которой агент составил план.
	SessionID string
	Items     []DayPlanItem
	CreatedAt time.Time
	UpdatedAt time.Time
}

// DayPlanItem — задача в плане.
type DayPlanItem struct {
	TaskID      string
	EstimateMin int
	Reason      string
}

// DayPlan возвращает план на день или nil, если его нет.
func (s *Store) DayPlan(ctx context.Context, day string) (*DayPlan, error) {
	var p DayPlan
	var createdAt, updatedAt string
	err := s.db.QueryRowContext(ctx, `
		SELECT id, day, summary, source, session_id, created_at, updated_at
		FROM day_plans WHERE day = ? AND deleted_at IS NULL`, day).
		Scan(&p.ID, &p.Day, &p.Summary, &p.Source, &p.SessionID, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("чтение плана: %w", err)
	}
	if p.CreatedAt, err = ParseTime(createdAt); err != nil {
		return nil, err
	}
	if p.UpdatedAt, err = ParseTime(updatedAt); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT i.task_id, i.estimate_min, i.reason FROM day_plan_items i
		JOIN tasks t ON t.id = i.task_id AND t.deleted_at IS NULL
		WHERE i.plan_id = ? ORDER BY i.position`, p.ID)
	if err != nil {
		return nil, fmt.Errorf("чтение плана: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var it DayPlanItem
		if err := rows.Scan(&it.TaskID, &it.EstimateMin, &it.Reason); err != nil {
			return nil, err
		}
		p.Items = append(p.Items, it)
	}
	return &p, rows.Err()
}

// SaveDayPlan записывает план на день, заменяя прежний. Всё в одной транзакции.
func (s *Store) SaveDayPlan(ctx context.Context, p DayPlan) (DayPlan, error) {
	if err := validDate(p.Day); err != nil || p.Day == "" {
		return DayPlan{}, fmt.Errorf("день плана %q: ждали ГГГГ-ММ-ДД", p.Day)
	}
	if p.Source != PlanSourceAgent && p.Source != PlanSourceManual {
		return DayPlan{}, fmt.Errorf("неизвестный источник плана %q", p.Source)
	}
	seen := map[string]bool{}
	for _, it := range p.Items {
		if seen[it.TaskID] {
			return DayPlan{}, fmt.Errorf("задача %s в плане дважды", it.TaskID)
		}
		seen[it.TaskID] = true
		if it.EstimateMin < 0 {
			return DayPlan{}, errors.New("оценка не может быть отрицательной")
		}
	}
	now := s.now()
	p.ID, p.CreatedAt, p.UpdatedAt = NewID(), now, now
	err := s.inTx(ctx, func(q querier) error {
		if _, err := q.ExecContext(ctx, `UPDATE day_plans SET deleted_at = ?, updated_at = ? WHERE day = ? AND deleted_at IS NULL`,
			FormatTime(now), FormatTime(now), p.Day); err != nil {
			return err
		}
		if _, err := q.ExecContext(ctx, `
			INSERT INTO day_plans (id, day, summary, source, session_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			p.ID, p.Day, p.Summary, p.Source, p.SessionID, FormatTime(now), FormatTime(now)); err != nil {
			return err
		}
		for i, it := range p.Items {
			if _, err := q.ExecContext(ctx, `
				INSERT INTO day_plan_items (plan_id, task_id, position, estimate_min, reason) VALUES (?, ?, ?, ?, ?)`,
				p.ID, it.TaskID, i, it.EstimateMin, it.Reason); err != nil {
				return fmt.Errorf("задача %s: %w", it.TaskID, err)
			}
		}
		return nil
	})
	if err != nil {
		return DayPlan{}, fmt.Errorf("запись плана: %w", err)
	}
	return p, nil
}

// AddToDayPlan добавляет задачу в план на день. Если плана нет, создаёт
// план автора. Задача, которая уже в плане, не дублируется.
func (s *Store) AddToDayPlan(ctx context.Context, day, taskID string, estimateMin int) error {
	p, err := s.DayPlan(ctx, day)
	if err != nil {
		return err
	}
	if p == nil {
		_, err := s.SaveDayPlan(ctx, DayPlan{Day: day, Source: PlanSourceManual, Items: []DayPlanItem{{TaskID: taskID, EstimateMin: estimateMin}}})
		return err
	}
	for _, it := range p.Items {
		if it.TaskID == taskID {
			return nil
		}
	}
	now := FormatTime(s.now())
	return s.inTx(ctx, func(q querier) error {
		if _, err := q.ExecContext(ctx, `
			INSERT INTO day_plan_items (plan_id, task_id, position, estimate_min, reason)
			VALUES (?, ?, (SELECT COALESCE(MAX(position), -1) + 1 FROM day_plan_items WHERE plan_id = ?), ?, '')`,
			p.ID, taskID, p.ID, estimateMin); err != nil {
			return err
		}
		_, err := q.ExecContext(ctx, `UPDATE day_plans SET updated_at = ? WHERE id = ?`, now, p.ID)
		return err
	})
}

// RemoveFromDayPlan убирает задачу из плана на день.
func (s *Store) RemoveFromDayPlan(ctx context.Context, day, taskID string) error {
	p, err := s.DayPlan(ctx, day)
	if err != nil || p == nil {
		return err
	}
	now := FormatTime(s.now())
	return s.inTx(ctx, func(q querier) error {
		if _, err := q.ExecContext(ctx, `DELETE FROM day_plan_items WHERE plan_id = ? AND task_id = ?`, p.ID, taskID); err != nil {
			return err
		}
		_, err := q.ExecContext(ctx, `UPDATE day_plans SET updated_at = ? WHERE id = ?`, now, p.ID)
		return err
	})
}

// DataVersion — признак изменений базы другими процессами: меняется, когда
// в базу записал кто-то другой, например MCP-сервер приложения. У базы одно
// соединение, поэтому свои записи его не меняют.
func (s *Store) DataVersion(ctx context.Context) (int64, error) {
	var v int64
	err := s.db.QueryRowContext(ctx, "PRAGMA data_version").Scan(&v)
	return v, err
}
