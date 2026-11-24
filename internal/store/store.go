// Package store хранит данные приложения в локальной базе SQLite.
//
// База — один файл в папке данных приложения. Схема создаётся встроенными
// миграциями при открытии.
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite" // драйвер SQLite без cgo
)

// AppDirName — имя папки приложения внутри ~/Library/Application Support.
const AppDirName = "Daily Agent"

// FileName — имя файла базы.
const FileName = "daily-agent.db"

// EnvPath — переменная окружения, которая задаёт другой путь к базе.
// Нужна для разработки и тестов, чтобы не трогать основную базу.
const EnvPath = "DAILY_AGENT_DB"

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Store — открытая база приложения.
type Store struct {
	db  *sql.DB
	now func() time.Time
}

// DefaultPath возвращает путь к базе: значение DAILY_AGENT_DB или
// ~/Library/Application Support/Daily Agent/daily-agent.db.
func DefaultPath() (string, error) {
	if p := strings.TrimSpace(os.Getenv(EnvPath)); p != "" {
		return p, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("папка данных приложения: %w", err)
	}
	return filepath.Join(dir, AppDirName, FileName), nil
}

// Open открывает базу по пути path, создаёт папку и файл, если их нет,
// и применяет миграции.
func Open(ctx context.Context, path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("пустой путь к базе")
	}
	if strings.ContainsRune(path, '?') {
		return nil, errors.New("путь к базе не должен содержать «?»")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("папка базы: %w", err)
	}

	dsn := path +
		"?_pragma=foreign_keys(1)" +
		"&_pragma=journal_mode(WAL)" +
		"&_pragma=synchronous(NORMAL)" +
		"&_pragma=busy_timeout(5000)" +
		"&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("открытие базы: %w", err)
	}
	// Одно соединение: приложение однопользовательское, а так не бывает
	// конфликтов блокировок между своими же запросами.
	db.SetMaxOpenConns(1)

	s := &Store{db: db, now: time.Now}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close закрывает базу.
func (s *Store) Close() error { return s.db.Close() }

// DB отдаёт соединение для служебных задач: тестовых данных и проверок.
func (s *Store) DB() *sql.DB { return s.db }

// SetClock подменяет часы, по которым ставятся created_at и updated_at.
func (s *Store) SetClock(now func() time.Time) { s.now = now }

// SchemaVersion возвращает номер последней применённой миграции.
func (s *Store) SchemaVersion(ctx context.Context) (int, error) {
	var v int
	err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&v)
	return v, err
}

type migration struct {
	version int
	name    string
	sql     string
}

func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return nil, err
	}
	var out []migration
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".sql") {
			continue
		}
		num, _, ok := strings.Cut(name, "_")
		if !ok {
			return nil, fmt.Errorf("миграция %s: имя должно начинаться с номера и «_»", name)
		}
		v, err := strconv.Atoi(num)
		if err != nil || v <= 0 {
			return nil, fmt.Errorf("миграция %s: неверный номер", name)
		}
		body, err := fs.ReadFile(migrationsFS, "migrations/"+name)
		if err != nil {
			return nil, err
		}
		out = append(out, migration{version: v, name: name, sql: string(body)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	for i, m := range out {
		if m.version != i+1 {
			return nil, fmt.Errorf("миграции должны идти подряд с 1, а %s имеет номер %d", m.name, m.version)
		}
	}
	return out, nil
}

// migrate применяет миграции, номер которых больше PRAGMA user_version.
// Каждая миграция идёт в своей транзакции вместе с новым номером версии.
func (s *Store) migrate(ctx context.Context) error {
	migrations, err := loadMigrations()
	if err != nil {
		return err
	}
	current, err := s.SchemaVersion(ctx)
	if err != nil {
		return fmt.Errorf("версия схемы: %w", err)
	}
	if current > len(migrations) {
		return fmt.Errorf("схема базы версии %d новее приложения (%d)", current, len(migrations))
	}
	for _, m := range migrations[current:] {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, m.sql); err != nil {
			tx.Rollback()
			return fmt.Errorf("миграция %s: %w", m.name, err)
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", m.version)); err != nil {
			tx.Rollback()
			return fmt.Errorf("миграция %s: версия: %w", m.name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("миграция %s: %w", m.name, err)
		}
	}
	return nil
}

// TimeLayout — формат времени в базе: UTC, миллисекунды, фиксированная ширина.
const TimeLayout = "2006-01-02T15:04:05.000Z07:00"

// DateLayout — формат даты без времени.
const DateLayout = "2006-01-02"

// FormatTime переводит время в формат базы.
func FormatTime(t time.Time) string { return t.UTC().Format(TimeLayout) }

// ParseTime читает время из базы.
func ParseTime(v string) (time.Time, error) { return time.Parse(time.RFC3339Nano, v) }

// NewID создаёт идентификатор записи: UUID версии 7, упорядоченный по времени.
func NewID() string { return uuid.Must(uuid.NewV7()).String() }

func nullIfEmpty(v string) any {
	if v == "" {
		return nil
	}
	return v
}
