package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Откуда MCP-сервер.
const (
	MCPSourceApp      = "app"      // добавлен в приложении
	MCPSourceOpenCode = "opencode" // из настроек OpenCode пользователя
)

// Способ входа на MCP-сервер.
const (
	MCPAuthOAuth = "oauth" // через браузер, данные входа хранит OpenCode
	MCPAuthToken = "token" // токен в Связке ключей
	MCPAuthNone  = "none"
)

// MCPServer — MCP-сервер агента.
type MCPServer struct {
	ID string
	// Name — имя для агента, входит в имена инструментов.
	Name   string
	Title  string
	Source string
	// Type: remote или local.
	Type    string
	URL     string
	Command []string
	Auth    string
	Enabled bool
	// Tools — разрешённые инструменты. Nil — по умолчанию, только чтение.
	Tools     []string
	CreatedAt time.Time
	UpdatedAt time.Time
}

const mcpColumns = `id, name, title, source, type, url, command, auth, enabled, tools, created_at, updated_at`

func scanMCP(sc scanner) (MCPServer, error) {
	var m MCPServer
	var command string
	var tools sql.NullString
	var enabled int
	var createdAt, updatedAt string
	if err := sc.Scan(&m.ID, &m.Name, &m.Title, &m.Source, &m.Type, &m.URL, &command, &m.Auth, &enabled, &tools, &createdAt, &updatedAt); err != nil {
		return MCPServer{}, err
	}
	m.Enabled = enabled != 0
	_ = json.Unmarshal([]byte(command), &m.Command)
	if tools.Valid {
		m.Tools = []string{}
		_ = json.Unmarshal([]byte(tools.String), &m.Tools)
	}
	var err error
	if m.CreatedAt, err = ParseTime(createdAt); err != nil {
		return MCPServer{}, err
	}
	if m.UpdatedAt, err = ParseTime(updatedAt); err != nil {
		return MCPServer{}, err
	}
	return m, nil
}

// SaveMCPServer создаёт или обновляет сервер. Имя среди неудалённых — одно.
func (s *Store) SaveMCPServer(ctx context.Context, m MCPServer) (MCPServer, error) {
	m.Name, m.Title = strings.TrimSpace(m.Name), strings.TrimSpace(m.Title)
	if m.Name == "" {
		return MCPServer{}, errors.New("у сервера нет имени")
	}
	if m.Title == "" {
		m.Title = m.Name
	}
	if m.Source != MCPSourceApp && m.Source != MCPSourceOpenCode {
		return MCPServer{}, fmt.Errorf("неизвестный источник сервера %q", m.Source)
	}
	if m.Type == "" {
		m.Type = "remote"
	}
	if m.Auth == "" {
		m.Auth = MCPAuthNone
	}
	command, _ := json.Marshal(m.Command)
	if m.Command == nil {
		command = []byte("[]")
	}
	var tools any
	if m.Tools != nil {
		raw, _ := json.Marshal(m.Tools)
		tools = string(raw)
	}
	enabled := 0
	if m.Enabled {
		enabled = 1
	}
	now := s.now()
	m.UpdatedAt = now
	if m.ID == "" {
		m.ID = NewID()
		m.CreatedAt = now
		_, err := s.db.ExecContext(ctx, `INSERT INTO mcp_servers (`+mcpColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			m.ID, m.Name, m.Title, m.Source, m.Type, m.URL, string(command), m.Auth, enabled, tools, FormatTime(now), FormatTime(now))
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				return MCPServer{}, fmt.Errorf("сервер с именем %s уже есть", m.Name)
			}
			return MCPServer{}, fmt.Errorf("запись сервера: %w", err)
		}
		return m, nil
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE mcp_servers SET name = ?, title = ?, source = ?, type = ?, url = ?, command = ?, auth = ?, enabled = ?, tools = ?, updated_at = ?
		WHERE id = ? AND deleted_at IS NULL`,
		m.Name, m.Title, m.Source, m.Type, m.URL, string(command), m.Auth, enabled, tools, FormatTime(now), m.ID)
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return MCPServer{}, fmt.Errorf("сервер с именем %s уже есть", m.Name)
	}
	if err := affectedOne(res, err); err != nil {
		return MCPServer{}, err
	}
	return s.MCPServerByID(ctx, m.ID)
}

// MCPServerByID возвращает неудалённый сервер.
func (s *Store) MCPServerByID(ctx context.Context, id string) (MCPServer, error) {
	m, err := scanMCP(s.db.QueryRowContext(ctx, `SELECT `+mcpColumns+` FROM mcp_servers WHERE id = ? AND deleted_at IS NULL`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return MCPServer{}, ErrNotFound
	}
	return m, err
}

// MCPServers возвращает неудалённые серверы по имени.
func (s *Store) MCPServers(ctx context.Context) ([]MCPServer, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+mcpColumns+` FROM mcp_servers WHERE deleted_at IS NULL ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("чтение серверов: %w", err)
	}
	defer rows.Close()
	var out []MCPServer
	for rows.Next() {
		m, err := scanMCP(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// DeleteMCPServer мягко удаляет сервер.
func (s *Store) DeleteMCPServer(ctx context.Context, id string) error {
	now := FormatTime(s.now())
	res, err := s.db.ExecContext(ctx, `UPDATE mcp_servers SET deleted_at = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL`, now, now, id)
	return affectedOne(res, err)
}
