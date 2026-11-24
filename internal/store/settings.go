package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// Ключи настроек приложения. Ключи и токены сюда не попадают: они в Связке ключей.
const (
	SettingModelURL = "agent.model.url" // адрес своей модели (селфхост-API)
	SettingModelID  = "agent.model.id"  // идентификатор своей модели
	// SettingWorkModel — модель для рабочих задач, провайдер/модель. Пусто —
	// своя модель, если она задана; «-» — не выбрана.
	SettingWorkModel = "agent.work.model"
	// SettingPersonalModel — модель для личных задач. Пусто — та же, что для рабочих.
	SettingPersonalModel = "agent.personal.model"
	// SettingCatalog — подключённые облачные провайдеры каталога OpenCode,
	// JSON: идентификатор, название, переменная для ключа. Сами ключи — в Связке ключей.
	SettingCatalog = "agent.catalog"
	// SettingProviderKeys — провайдеры из настроек OpenCode пользователя,
	// чьи ключи сохранены в Связке ключей, JSON-список идентификаторов.
	SettingProviderKeys = "agent.keys"
)

// Settings читает настройки, имена которых начинаются с prefix.
// Пустой prefix — все настройки.
func (s *Store) Settings(ctx context.Context, prefix string) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM settings WHERE substr(key, 1, ?) = ?`, len(prefix), prefix)
	if err != nil {
		return nil, fmt.Errorf("настройки: %w", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// Setting читает одну настройку. ok = false, если её нет.
func (s *Store) Setting(ctx context.Context, key string) (string, bool, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("настройка %s: %w", key, err)
	}
	return v, true, nil
}

// SetSettings сохраняет несколько настроек одной транзакцией и ставит им
// время изменения. Значение может быть пустым.
func (s *Store) SetSettings(ctx context.Context, values map[string]string) error {
	for k := range values {
		if strings.TrimSpace(k) == "" {
			return errors.New("пустое имя настройки")
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := FormatTime(s.now())
	for k, v := range values {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
			ON CONFLICT (key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
			WHERE value <> excluded.value`, k, v, now); err != nil {
			return fmt.Errorf("настройка %s: %w", k, err)
		}
	}
	return tx.Commit()
}
