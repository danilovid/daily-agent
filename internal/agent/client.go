package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	agentprofile "daily-agent/agent"
)

// Client — клиент HTTP API сервера OpenCode v2 на этом же Mac.
// Использует только операции с сессиями; описание API — /openapi.json.
type Client struct {
	baseURL  string
	password string
	http     *http.Client
}

// NewClient создаёт клиента. Адрес должен указывать на 127.0.0.1.
func NewClient(baseURL, password string) *Client {
	return &Client{
		baseURL:  strings.TrimRight(baseURL, "/"),
		password: password,
		// Таймаут одного запроса. Ожидание ответа агента ограничивается контекстом.
		http: &http.Client{Timeout: 0},
	}
}

// APIError — ошибка, которую вернул сервер.
type APIError struct {
	Status  int
	Tag     string
	Message string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("OpenCode: %s (%d %s)", e.Message, e.Status, e.Tag)
	}
	return fmt.Sprintf("OpenCode: ответ %d", e.Status)
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var r io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, r)
	if err != nil {
		return err
	}
	req.SetBasicAuth("opencode", c.password)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		var e struct {
			Tag     string `json:"_tag"`
			Message string `json:"message"`
		}
		_ = json.Unmarshal(raw, &e)
		return &APIError{Status: resp.StatusCode, Tag: e.Tag, Message: e.Message}
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("ответ OpenCode %s %s: %w", method, path, err)
	}
	return nil
}

// Info — сведения о сервере.
type Info struct {
	Version string `json:"version"`
	PID     int    `json:"pid"`
}

// Info возвращает версию сервера. Отвечает, когда сервер готов.
func (c *Client) Info(ctx context.Context) (Info, error) {
	var info Info
	err := c.do(ctx, http.MethodGet, "/api/info", nil, &info)
	return info, err
}

// ModelRef — модель в формате API.
type ModelRef struct {
	ID         string `json:"id"`
	ProviderID string `json:"providerID"`
}

// SessionOptions — параметры новой сессии.
type SessionOptions struct {
	Title string
	// Directory — рабочая папка агента.
	Directory string
	Model     *ModelRef
}

// Session — сессия OpenCode.
type Session struct {
	ID    string    `json:"id"`
	Title string    `json:"title"`
	Agent string    `json:"agent"`
	Model *ModelRef `json:"model"`
}

// GetSession читает сессию.
func (c *Client) GetSession(ctx context.Context, sessionID string) (Session, error) {
	var resp struct {
		Data Session `json:"data"`
	}
	err := c.do(ctx, http.MethodGet, "/api/session/"+url.PathEscape(sessionID), nil, &resp)
	return resp.Data, err
}

// SwitchModel меняет модель сессии. Следующий ответ агента придёт от неё.
func (c *Client) SwitchModel(ctx context.Context, sessionID string, model ModelRef) error {
	return c.do(ctx, http.MethodPost, "/api/session/"+url.PathEscape(sessionID)+"/model",
		map[string]any{"model": model}, nil)
}

// EnsureModel переключает сессию на модель, если у неё другая. Так чат,
// начатый до смены модели в настройках, продолжается с новой.
func (c *Client) EnsureModel(ctx context.Context, sessionID string, model *ModelRef) error {
	if model == nil {
		return nil
	}
	sess, err := c.GetSession(ctx, sessionID)
	if err != nil {
		return err
	}
	if sess.Model != nil && *sess.Model == *model {
		return nil
	}
	return c.SwitchModel(ctx, sessionID, *model)
}

// CreateSession создаёт сессию агента приложения. Права сессии запрещают
// всё: это третий уровень защиты после настроек агента и политики.
func (c *Client) CreateSession(ctx context.Context, opts SessionOptions) (Session, error) {
	body := map[string]any{
		"agent":       agentprofile.AgentID,
		"permissions": agentprofile.DenyAll(),
	}
	if opts.Title != "" {
		body["title"] = opts.Title
	}
	if opts.Directory != "" {
		body["location"] = map[string]any{"directory": opts.Directory}
	}
	if opts.Model != nil {
		body["model"] = opts.Model
	}
	var resp struct {
		Data Session `json:"data"`
	}
	if err := c.do(ctx, http.MethodPost, "/api/session", body, &resp); err != nil {
		return Session{}, err
	}
	if resp.Data.ID == "" {
		return Session{}, errors.New("OpenCode не вернул идентификатор сессии")
	}
	return resp.Data, nil
}

// Prompt ставит сообщение в очередь сессии и запускает агента.
func (c *Client) Prompt(ctx context.Context, sessionID, text string) error {
	return c.do(ctx, http.MethodPost, "/api/session/"+url.PathEscape(sessionID)+"/prompt",
		map[string]any{"text": text}, nil)
}

// Wait ждёт, пока агент сессии закончит работу.
func (c *Client) Wait(ctx context.Context, sessionID string) error {
	return c.do(ctx, http.MethodPost, "/api/experimental/session/"+url.PathEscape(sessionID)+"/wait", nil, nil)
}

// Message — сообщение сессии: вопрос (user), контекст от приложения
// (synthetic), ответ агента (assistant), запись о завершении (idle) и другие.
type Message struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	// Text — у user и synthetic.
	Text     string         `json:"text"`
	Metadata map[string]any `json:"metadata"`
	Time     struct {
		Created float64 `json:"created"` // миллисекунды Unix
	} `json:"time"`
	Content []Content `json:"content"`
	Finish  string    `json:"finish"`
	Outcome string    `json:"outcome"` // у idle: succeeded, failed, interrupted
	Error   *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Content — часть ответа агента: текст, рассуждение или вызов инструмента.
type Content struct {
	Type  string `json:"type"` // text, reasoning, tool
	Text  string `json:"text"`
	Name  string `json:"name"` // имя инструмента
	State *struct {
		Status string `json:"status"` // streaming, running, completed, error
	} `json:"state"`
}

// Messages возвращает сообщения сессии от старых к новым.
func (c *Client) Messages(ctx context.Context, sessionID string) ([]Message, error) {
	var resp struct {
		Data []Message `json:"data"`
	}
	q := url.Values{"order": {"asc"}, "limit": {"200"}}
	err := c.do(ctx, http.MethodGet, "/api/session/"+url.PathEscape(sessionID)+"/message?"+q.Encode(), nil, &resp)
	return resp.Data, err
}

// Reply — ответ агента на одно сообщение.
type Reply struct {
	SessionID string
	Text      string
	// Tools — вызовы инструментов в ответе с итоговым состоянием.
	Tools    []ToolCall
	Outcome  string
	Duration time.Duration
}

// ToolCall — вызов инструмента агентом.
type ToolCall struct {
	Name   string
	Status string
}

// ErrAgentFailed — агент завершил работу с ошибкой.
var ErrAgentFailed = errors.New("агент не ответил")

// Ask создаёт сессию, отправляет сообщение и ждёт ответа агента.
func (c *Client) Ask(ctx context.Context, opts SessionOptions, text string) (Reply, error) {
	began := time.Now()
	sess, err := c.CreateSession(ctx, opts)
	if err != nil {
		return Reply{}, err
	}
	if err := c.Prompt(ctx, sess.ID, text); err != nil {
		return Reply{}, err
	}
	if err := c.Wait(ctx, sess.ID); err != nil {
		return Reply{}, err
	}
	msgs, err := c.Messages(ctx, sess.ID)
	if err != nil {
		return Reply{}, err
	}
	reply, errText := ReplyFromMessages(msgs)
	reply.SessionID = sess.ID
	reply.Duration = time.Since(began)
	if reply.Outcome == "failed" || reply.Outcome == "interrupted" {
		if errText == "" {
			errText = reply.Outcome
		}
		return reply, fmt.Errorf("%w: %s", ErrAgentFailed, errText)
	}
	return reply, nil
}

// ReplyFromMessages собирает ответ из сообщений после последнего сообщения
// пользователя: текст ответов агента, вызовы инструментов и итог. Второе
// значение — текст ошибки из записи о завершении.
func ReplyFromMessages(msgs []Message) (Reply, string) {
	start := 0
	for i, m := range msgs {
		if m.Type == "user" {
			start = i + 1
		}
	}
	var r Reply
	var errText string
	var texts []string
	for _, m := range msgs[start:] {
		switch m.Type {
		case "assistant":
			for _, part := range m.Content {
				switch part.Type {
				case "text":
					if t := strings.TrimSpace(part.Text); t != "" {
						texts = append(texts, t)
					}
				case "tool":
					st := ""
					if part.State != nil {
						st = part.State.Status
					}
					r.Tools = append(r.Tools, ToolCall{Name: part.Name, Status: st})
				}
			}
		case "idle":
			r.Outcome = m.Outcome
			if m.Error != nil {
				errText = m.Error.Message
			}
		}
	}
	r.Text = strings.Join(texts, "\n\n")
	return r, errText
}
