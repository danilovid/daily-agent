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
	// Allow — инструменты, которые разрешены после запрета всего: только
	// у рабочих чатов и служебных сессий.
	Allow []string
}

// SessionPermissions — права сессии: запретить всё, затем разрешить
// перечисленные инструменты. В OpenCode побеждает последнее подходящее правило.
func SessionPermissions(allow []string) []agentprofile.Rule {
	rules := agentprofile.DenyAll()
	for _, tool := range allow {
		rules = append(rules, agentprofile.Rule{Action: tool, Resource: "*", Effect: "allow"})
	}
	return rules
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
		"permissions": SessionPermissions(opts.Allow),
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

// ModelInfo — модель, которой может пользоваться агент.
type ModelInfo struct {
	ID         string `json:"id"`
	ProviderID string `json:"providerID"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Enabled    *bool  `json:"enabled"`
}

// Ref — ссылка на модель для сессий.
func (m ModelInfo) Ref() ModelRef { return ModelRef{ID: m.ID, ProviderID: m.ProviderID} }

// ListModels возвращает модели разрешённых провайдеров. Провайдеров, которых
// запрещает политика профиля, в списке нет.
func (c *Client) ListModels(ctx context.Context) ([]ModelInfo, error) {
	var resp struct {
		Data []ModelInfo `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/model", nil, &resp); err != nil {
		return nil, err
	}
	out := resp.Data[:0]
	for _, m := range resp.Data {
		if m.Enabled != nil && !*m.Enabled {
			continue
		}
		out = append(out, m)
	}
	return out, nil
}

// Integration — провайдер из встроенного каталога OpenCode.
type Integration struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// KeyEnv — переменные окружения, из которых OpenCode берёт ключ.
	// Пусто — провайдер подключается только входом через браузер.
	KeyEnv []string `json:"-"`
}

// Integrations возвращает каталог провайдеров.
func (c *Client) Integrations(ctx context.Context) ([]Integration, error) {
	var resp struct {
		Data []struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Methods []struct {
				Type  string   `json:"type"`
				Names []string `json:"names"`
			} `json:"methods"`
		} `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/integration", nil, &resp); err != nil {
		return nil, err
	}
	out := make([]Integration, 0, len(resp.Data))
	for _, d := range resp.Data {
		in := Integration{ID: d.ID, Name: d.Name}
		for _, m := range d.Methods {
			if m.Type == "env" {
				in.KeyEnv = append(in.KeyEnv, m.Names...)
			}
		}
		out = append(out, in)
	}
	return out, nil
}

// SetPermissions заменяет права сессии. Так чат получает текущий список
// разрешённых инструментов: права сессии важнее прав агента.
func (c *Client) SetPermissions(ctx context.Context, sessionID string, allow []string) error {
	return c.do(ctx, http.MethodPatch, "/api/session/"+url.PathEscape(sessionID),
		map[string]any{"permissions": SessionPermissions(allow)}, nil)
}

// DeleteSession удаляет сессию из базы OpenCode.
func (c *Client) DeleteSession(ctx context.Context, sessionID string) error {
	return c.do(ctx, http.MethodDelete, "/api/session/"+url.PathEscape(sessionID), nil, nil)
}

// MCPStatus — состояние MCP-сервера в OpenCode.
type MCPStatus struct {
	Name string `json:"name"`
	// Status: connected, pending, disabled, failed, needs_auth.
	Status        string `json:"status"`
	Error         string `json:"error"`
	IntegrationID string `json:"integrationID"`
}

// locationQuery — параметр рабочей папки для запросов OpenCode.
func locationQuery(dir string) string {
	if dir == "" {
		return ""
	}
	return "?location%5Bdirectory%5D=" + url.QueryEscape(dir)
}

// MCPServers возвращает состояние MCP-серверов для рабочей папки. Запрос
// заставляет OpenCode подключить их, если он ещё не начал.
func (c *Client) MCPServers(ctx context.Context, dir string) ([]MCPStatus, error) {
	var resp struct {
		Data []struct {
			Name   string `json:"name"`
			Status struct {
				Status string `json:"status"`
				Error  string `json:"error"`
			} `json:"status"`
			IntegrationID string `json:"integrationID"`
		} `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/mcp"+locationQuery(dir), nil, &resp); err != nil {
		return nil, err
	}
	out := make([]MCPStatus, 0, len(resp.Data))
	for _, d := range resp.Data {
		out = append(out, MCPStatus{Name: d.Name, Status: d.Status.Status, Error: d.Status.Error, IntegrationID: d.IntegrationID})
	}
	return out, nil
}

// OAuthAttempt — начатый вход через браузер.
type OAuthAttempt struct {
	IntegrationID string
	ID            string
	// URL — адрес входа, его открывают в браузере.
	URL string
}

// StartOAuth начинает вход на MCP-сервер через браузер. OpenCode сам примет
// ответ сервера авторизации на 127.0.0.1 и сохранит токены у себя.
func (c *Client) StartOAuth(ctx context.Context, integrationID, dir string) (OAuthAttempt, error) {
	base := "/api/integration/" + url.PathEscape(integrationID)
	var info struct {
		Data struct {
			Methods []struct {
				ID   string `json:"id"`
				Type string `json:"type"`
			} `json:"methods"`
		} `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, base+locationQuery(dir), nil, &info); err != nil {
		return OAuthAttempt{}, err
	}
	method := ""
	for _, m := range info.Data.Methods {
		if m.Type == "oauth" {
			method = m.ID
			break
		}
	}
	if method == "" {
		return OAuthAttempt{}, errors.New("сервер не поддерживает вход через браузер")
	}
	var resp struct {
		Data struct {
			AttemptID string `json:"attemptID"`
			URL       string `json:"url"`
		} `json:"data"`
	}
	if err := c.do(ctx, http.MethodPost, base+"/connect/oauth"+locationQuery(dir), map[string]any{"methodID": method}, &resp); err != nil {
		return OAuthAttempt{}, err
	}
	if !strings.HasPrefix(resp.Data.URL, "https://") && !strings.HasPrefix(resp.Data.URL, "http://") {
		return OAuthAttempt{}, errors.New("OpenCode не вернул адрес входа")
	}
	return OAuthAttempt{IntegrationID: integrationID, ID: resp.Data.AttemptID, URL: resp.Data.URL}, nil
}

// OAuthStatus — состояние входа: pending, complete, failed или expired.
func (c *Client) OAuthStatus(ctx context.Context, a OAuthAttempt, dir string) (string, error) {
	var resp struct {
		Data struct {
			Status string `json:"status"`
		} `json:"data"`
	}
	err := c.do(ctx, http.MethodGet, "/api/integration/"+url.PathEscape(a.IntegrationID)+"/connect/oauth/"+url.PathEscape(a.ID)+locationQuery(dir), nil, &resp)
	return resp.Data.Status, err
}
