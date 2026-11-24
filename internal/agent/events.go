package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Типы событий OpenCode, которые нужны чату. Формат события в описании API
// не задан (строка JSON), поэтому список сверен с OpenCode 2.0.28 вживую.
const (
	EventTextDelta        = "session.text.delta"
	EventTextEnded        = "session.text.ended"
	EventExecutionStarted = "session.execution.started"
	EventRenamed          = "session.renamed"
	// Окончание работы агента: session.execution.succeeded, .failed, .interrupted.
	eventExecutionPrefix = "session.execution."
)

// Event — событие сессии.
type Event struct {
	Type      string
	SessionID string
	// MessageID — ответ агента, к которому относится текст.
	MessageID string
	Delta     string
	Text      string
	Title     string
	// Outcome — у окончания работы: succeeded, failed, interrupted.
	Outcome string
	Error   string
}

// Finished сообщает, что агент закончил работу над сообщением.
func (e Event) Finished() bool {
	return strings.HasPrefix(e.Type, eventExecutionPrefix) && e.Type != EventExecutionStarted
}

type rawEvent struct {
	Type string `json:"type"`
	Data struct {
		SessionID          string `json:"sessionID"`
		AssistantMessageID string `json:"assistantMessageID"`
		Delta              string `json:"delta"`
		Text               string `json:"text"`
		Title              string `json:"title"`
		Error              *struct {
			Message string `json:"message"`
		} `json:"error"`
		Message string `json:"message"`
	} `json:"data"`
}

// ParseEvent разбирает данные одного события. Ложь — событие чату не нужно.
func ParseEvent(data []byte) (Event, bool) {
	var raw rawEvent
	if err := json.Unmarshal(data, &raw); err != nil || raw.Data.SessionID == "" {
		return Event{}, false
	}
	ev := Event{
		Type:      raw.Type,
		SessionID: raw.Data.SessionID,
		MessageID: raw.Data.AssistantMessageID,
	}
	switch {
	case raw.Type == EventTextDelta:
		ev.Delta = raw.Data.Delta
	case raw.Type == EventTextEnded:
		ev.Text = raw.Data.Text
	case raw.Type == EventRenamed:
		ev.Title = raw.Data.Title
	case raw.Type == EventExecutionStarted:
	case strings.HasPrefix(raw.Type, eventExecutionPrefix):
		ev.Outcome = strings.TrimPrefix(raw.Type, eventExecutionPrefix)
		if raw.Data.Error != nil {
			ev.Error = raw.Data.Error.Message
		} else if raw.Data.Message != "" {
			ev.Error = raw.Data.Message
		}
	default:
		return Event{}, false
	}
	return ev, true
}

// Subscribe читает поток событий сервера и передаёт нужные события в handle.
// Возвращается, когда поток закрылся или отменён контекст.
func (c *Client) Subscribe(ctx context.Context, handle func(Event)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/event", nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth("opencode", c.password)
	req.Header.Set("Accept", "text/event-stream")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return &APIError{Status: resp.StatusCode}
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64<<10), 8<<20)
	var data strings.Builder
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "data:"):
			// Строки data одного события склеиваются через перевод строки.
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		case line == "":
			if data.Len() > 0 {
				if ev, ok := ParseEvent([]byte(data.String())); ok {
					handle(ev)
				}
				data.Reset()
			}
		}
	}
	if err := sc.Err(); err != nil && ctx.Err() == nil {
		return err
	}
	return ctx.Err()
}

// RunEvents держит подписку на события OpenCode, пока не отменён контекст.
// После перезапуска OpenCode меняются порт и пароль, поэтому клиент берётся
// заново перед каждой подпиской.
func RunEvents(ctx context.Context, client func() (*Client, error), handle func(Event)) {
	for ctx.Err() == nil {
		c, err := client()
		if err == nil {
			err = c.Subscribe(ctx, handle)
		}
		if ctx.Err() != nil {
			return
		}
		_ = err
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}

// Synthetic добавляет в сессию текст от приложения, не запуская агента.
// Так в чат попадает контекст: агенда на день. metadata видна в истории.
func (c *Client) Synthetic(ctx context.Context, sessionID, text string, metadata map[string]any) error {
	body := map[string]any{"text": text, "resume": false}
	if metadata != nil {
		body["metadata"] = metadata
	}
	return c.do(ctx, http.MethodPost, "/api/session/"+url.PathEscape(sessionID)+"/synthetic", body, nil)
}

// Interrupt останавливает работу агента в сессии.
func (c *Client) Interrupt(ctx context.Context, sessionID string) (bool, error) {
	var resp struct {
		Interrupted bool `json:"interrupted"`
	}
	err := c.do(ctx, http.MethodPost, "/api/session/"+url.PathEscape(sessionID)+"/interrupt", nil, &resp)
	return resp.Interrupted, err
}

// SetTitle меняет заголовок сессии в OpenCode.
func (c *Client) SetTitle(ctx context.Context, sessionID, title string) error {
	return c.do(ctx, http.MethodPatch, "/api/session/"+url.PathEscape(sessionID), map[string]any{"title": title}, nil)
}

// ErrNotReady — OpenCode не на связи.
var ErrNotReady = errors.New("OpenCode не на связи")
