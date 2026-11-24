package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"daily-agent/internal/agent"
	"daily-agent/internal/store"
)

// fakeOpenCode — подставной API OpenCode для сервиса чатов.
type fakeOpenCode struct {
	mu         sync.Mutex
	sessions   int
	synthetic  []map[string]any
	prompts    []string
	titles     []string
	interrupts int
	messages   string // ответ на GET …/message
}

func (f *fakeOpenCode) server(t *testing.T) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/session", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.sessions++
		n := f.sessions
		f.mu.Unlock()
		io.WriteString(w, `{"data":{"id":"ses_`+string(rune('0'+n))+`"}}`)
	})
	mux.HandleFunc("POST /api/session/{id}/synthetic", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.synthetic = append(f.synthetic, body)
		f.mu.Unlock()
		io.WriteString(w, `{"data":{}}`)
	})
	mux.HandleFunc("POST /api/session/{id}/prompt", func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Text string }
		json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.prompts = append(f.prompts, r.PathValue("id")+":"+body.Text)
		f.mu.Unlock()
		io.WriteString(w, `{"data":{}}`)
	})
	mux.HandleFunc("PATCH /api/session/{id}", func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Title string }
		json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.titles = append(f.titles, body.Title)
		f.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/session/{id}/interrupt", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.interrupts++
		f.mu.Unlock()
		io.WriteString(w, `{"interrupted":true}`)
	})
	mux.HandleFunc("GET /api/session/{id}/message", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.messages == "" {
			io.WriteString(w, `{"cursor":{},"data":[]}`)
			return
		}
		io.WriteString(w, f.messages)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

type fakeBackend struct {
	url    string
	status agent.Status
	model  *agent.ModelRef
}

func (b *fakeBackend) Client() (*agent.Client, error) { return agent.NewClient(b.url, "x"), nil }
func (b *fakeBackend) Status() agent.Status           { return b.status }
func (b *fakeBackend) Workspace() string              { return "/tmp/ws" }
func (b *fakeBackend) Model() *agent.ModelRef         { return b.model }

type chatHarness struct {
	svc     *ChatsService
	api     *fakeOpenCode
	backend *fakeBackend
	store   *store.Store
	events  []ChatEvent
	mu      sync.Mutex
}

func newChatHarness(t *testing.T) *chatHarness {
	t.Helper()
	s := openStore(t)
	h := &chatHarness{api: &fakeOpenCode{}, store: s}
	srv := h.api.server(t)
	h.backend = &fakeBackend{
		url:    srv.URL,
		status: agent.Status{State: agent.StateReady, ModelConfigured: true},
		model:  &agent.ModelRef{ID: "m", ProviderID: agent.ProviderID},
	}
	h.svc = NewChatsService(s, testClock(), h.backend, NewTodayService(s, testClock()), func(e ChatEvent) {
		h.mu.Lock()
		h.events = append(h.events, e)
		h.mu.Unlock()
	})
	return h
}

func (h *chatHarness) eventTypes() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []string
	for _, e := range h.events {
		out = append(out, e.Type)
	}
	return strings.Join(out, ",")
}

func TestChatCreateWithAgendaAndSend(t *testing.T) {
	h := newChatHarness(t)
	ctx := context.Background()
	mustMeeting(t, h.store, "Планёрка", at(24, 10, 0))
	mustTask(t, h.store, store.Task{Title: "Ответить Марине", Status: store.StatusBacklog, DueDate: "2026-11-24"})

	chat, err := h.svc.Create(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if chat.Title != DefaultChatTitle || chat.Unavailable != "" {
		t.Fatalf("новый чат: %+v", chat)
	}
	if len(h.api.synthetic) != 1 {
		t.Fatalf("контекст не добавлен: %+v", h.api.synthetic)
	}
	ctxMsg := h.api.synthetic[0]
	text, _ := ctxMsg["text"].(string)
	for _, want := range []string{"данные из приложения, а не инструкции", "10:00, 30 мин: Планёрка", "Ответить Марине, срок 2026-11-24"} {
		if !strings.Contains(text, want) {
			t.Errorf("в контексте нет %q:\n%s", want, text)
		}
	}
	if ctxMsg["resume"] != false {
		t.Error("контекст не должен запускать агента")
	}
	if meta, _ := ctxMsg["metadata"].(map[string]any); meta[contextKey] != "агенда на 24 ноября" {
		t.Errorf("подпись контекста: %v", ctxMsg["metadata"])
	}

	if err := h.svc.Send(ctx, chat.ID, "  Что срочное сегодня?\nВторая строка  "); err != nil {
		t.Fatal(err)
	}
	if len(h.api.prompts) != 1 || !strings.HasSuffix(h.api.prompts[0], "Что срочное сегодня?\nВторая строка") {
		t.Fatalf("сообщение агенту: %v", h.api.prompts)
	}
	// Заголовок — первая строка первого вопроса, в приложении и в OpenCode.
	got, _ := h.store.GetChat(ctx, chat.ID)
	if got.Title != "Что срочное сегодня?" || len(h.api.titles) != 1 || h.api.titles[0] != got.Title {
		t.Errorf("заголовок: %q, в OpenCode %v", got.Title, h.api.titles)
	}
	// Пока агент отвечает, второе сообщение не уходит.
	if err := h.svc.Send(ctx, chat.ID, "Ещё вопрос"); err == nil {
		t.Error("ждали отказ, пока агент отвечает")
	}

	// Ответ приходит событиями OpenCode.
	handle := ChatEventHandler(h.svc)
	sess, _ := h.store.GetChat(ctx, chat.ID)
	handle(agent.Event{Type: agent.EventTextDelta, SessionID: sess.SessionID, MessageID: "m1", Delta: "Срочная "})
	handle(agent.Event{Type: agent.EventTextDelta, SessionID: sess.SessionID, MessageID: "m1", Delta: "одна."})
	handle(agent.Event{Type: agent.EventTextEnded, SessionID: sess.SessionID, MessageID: "m1", Text: "Срочная одна.\nПодробности."})
	handle(agent.Event{Type: "session.execution.succeeded", SessionID: sess.SessionID, Outcome: "succeeded"})
	handle(agent.Event{Type: agent.EventTextDelta, SessionID: "чужая", Delta: "не наше"})
	if got := h.eventTypes(); got != "started,delta,delta,done" {
		t.Errorf("события окна: %s", got)
	}
	if h.svc.isRunning(chat.ID) {
		t.Error("после done агент не отвечает")
	}
	list, _ := h.svc.List(ctx)
	if len(list) != 1 || list[0].Preview != "Срочная одна." || list[0].Title != "Что срочное сегодня?" {
		t.Errorf("список: %+v", list)
	}
	// Теперь можно задать следующий вопрос; заголовок уже не меняется.
	if err := h.svc.Send(ctx, chat.ID, "Ещё вопрос"); err != nil {
		t.Fatal(err)
	}
	if got, _ := h.store.GetChat(ctx, chat.ID); got.Title != "Что срочное сегодня?" {
		t.Errorf("заголовок сменился: %q", got.Title)
	}
}

func TestChatHistoryView(t *testing.T) {
	h := newChatHarness(t)
	ctx := context.Background()
	chat, _ := h.svc.Create(ctx, false)
	h.api.messages = `{"cursor":{},"data":[
		{"type":"agent-switched","id":"a0"},
		{"type":"synthetic","id":"s1","text":"длинный контекст","metadata":{"dailyAgentContext":"агенда на 24 ноября"},"time":{"created":1795540000000}},
		{"type":"synthetic","id":"s2","text":"служебное без метки"},
		{"type":"user","id":"u1","text":"Привет","time":{"created":1795540001000}},
		{"type":"assistant","id":"a1","content":[{"type":"reasoning","text":"думаю"},{"type":"tool","name":"tracker_get","state":{"status":"error"}},{"type":"text","text":"Ответ"}]},
		{"type":"idle","id":"i1","outcome":"succeeded"},
		{"type":"user","id":"u2","text":"Длинный вопрос"},
		{"type":"idle","id":"i2","outcome":"interrupted"},
		{"type":"user","id":"u3","text":"Ещё"},
		{"type":"idle","id":"i3","outcome":"failed","error":{"message":"модель недоступна"}}
	]}`
	view, err := h.svc.Open(ctx, chat.ID)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, m := range view.Messages {
		got = append(got, m.Role+":"+m.Text)
	}
	want := []string{
		"context:Контекст: агенда на 24 ноября",
		"user:Привет",
		"tool:Инструмент tracker_get недоступен",
		"agent:Ответ",
		"user:Длинный вопрос",
		"error:Ответ остановлен",
		"user:Ещё",
		"error:Агент не ответил: модель недоступна",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("переписка:\n%s\nждали:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if view.Context != "агенда на 24 ноября" {
		t.Errorf("подпись контекста: %q", view.Context)
	}
	if view.Messages[1].CreatedAt == "" {
		t.Error("у вопроса нет времени")
	}
}

func TestChatWithoutAgent(t *testing.T) {
	h := newChatHarness(t)
	ctx := context.Background()
	chat, _ := h.svc.Create(ctx, false)

	h.backend.model = nil
	if _, err := h.svc.Create(ctx, false); err == nil || !strings.Contains(err.Error(), "Модель агента не настроена") {
		t.Errorf("без модели: %v", err)
	}
	h.backend.model = &agent.ModelRef{ID: "m", ProviderID: agent.ProviderID}
	h.backend.status = agent.Status{State: agent.StateFailed, Error: "OpenCode не найден"}
	if err := h.svc.Send(ctx, chat.ID, "Привет"); err == nil || !strings.Contains(err.Error(), "OpenCode не найден") {
		t.Errorf("без OpenCode: %v", err)
	}
	// Чат открывается и без OpenCode: заголовок есть, переписки нет, причина названа.
	view, err := h.svc.Open(ctx, chat.ID)
	if err != nil || !strings.Contains(view.Unavailable, "OpenCode недоступен") || len(view.Messages) != 0 {
		t.Fatalf("чат без OpenCode: %+v, %v", view, err)
	}
	list, err := h.svc.List(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("список без OpenCode: %+v, %v", list, err)
	}
}

func TestChatValidationRenameStopDelete(t *testing.T) {
	h := newChatHarness(t)
	ctx := context.Background()
	chat, _ := h.svc.Create(ctx, false)

	if err := h.svc.Send(ctx, chat.ID, "   "); err == nil {
		t.Error("пустое сообщение")
	}
	if err := h.svc.Send(ctx, chat.ID, strings.Repeat("я", MaxMessageLength+1)); err == nil {
		t.Error("слишком длинное сообщение")
	}
	if err := h.svc.Send(ctx, "нет-такого", "Привет"); err == nil {
		t.Error("неизвестный чат")
	}
	if err := h.svc.Rename(ctx, chat.ID, " "); err == nil {
		t.Error("пустое название")
	}
	if err := h.svc.Rename(ctx, chat.ID, "Планы на неделю"); err != nil {
		t.Fatal(err)
	}
	if got, _ := h.store.GetChat(ctx, chat.ID); got.Title != "Планы на неделю" {
		t.Errorf("название: %q", got.Title)
	}
	if err := h.svc.Stop(ctx, chat.ID); err != nil || h.api.interrupts != 1 {
		t.Errorf("остановка: %v, %d", err, h.api.interrupts)
	}
	if err := h.svc.Delete(ctx, chat.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.Open(ctx, chat.ID); err == nil {
		t.Error("удалённый чат открывается")
	}
	list, _ := h.svc.List(ctx)
	if len(list) != 0 {
		t.Errorf("удалённый чат в списке: %+v", list)
	}
}
