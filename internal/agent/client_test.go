package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	agentprofile "daily-agent/agent"
	"daily-agent/internal/agent/stubmodel"
)

// fakeAPI — подставной сервер OpenCode с нужными клиенту операциями.
type fakeAPI struct {
	t        *testing.T
	sessions []map[string]any
	prompts  []string
	outcome  string
}

func (f *fakeAPI) handler() http.Handler {
	mux := http.NewServeMux()
	auth := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if u, p, ok := r.BasicAuth(); !ok || u != "opencode" || p != "секрет" {
				w.WriteHeader(http.StatusUnauthorized)
				io.WriteString(w, `{"_tag":"UnauthorizedError","message":"Unauthorized"}`)
				return
			}
			h(w, r)
		}
	}
	mux.HandleFunc("POST /api/session", auth(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		f.sessions = append(f.sessions, body)
		io.WriteString(w, `{"data":{"id":"ses_1","title":"t","agent":"daily"}}`)
	}))
	mux.HandleFunc("POST /api/session/{id}/prompt", auth(func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Text string }
		json.NewDecoder(r.Body).Decode(&body)
		f.prompts = append(f.prompts, r.PathValue("id")+":"+body.Text)
		io.WriteString(w, `{"data":{"id":"msg_u","type":"user"}}`)
	}))
	mux.HandleFunc("POST /api/experimental/session/{id}/wait", auth(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	mux.HandleFunc("GET /api/session/{id}/message", auth(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("order") != "asc" {
			f.t.Errorf("порядок сообщений: %q", r.URL.RawQuery)
		}
		idle := `{"type":"idle","id":"m4","outcome":"` + f.outcome + `"}`
		if f.outcome == "failed" {
			idle = `{"type":"idle","id":"m4","outcome":"failed","error":{"message":"модель недоступна"}}`
		}
		io.WriteString(w, `{"cursor":{},"data":[
			{"type":"user","id":"m0"},
			{"type":"assistant","id":"m-old","content":[{"type":"text","text":"старый ответ"}]},
			{"type":"user","id":"m1"},
			{"type":"assistant","id":"m2","content":[
				{"type":"reasoning","text":"думаю"},
				{"type":"tool","id":"c1","name":"shell","state":{"status":"error"}},
				{"type":"text","text":"Первая часть."}]},
			{"type":"assistant","id":"m3","content":[{"type":"text","text":"Вторая часть."}]},
			`+idle+`]}`)
	}))
	return mux
}

func TestAskSendsLockedDownSession(t *testing.T) {
	f := &fakeAPI{t: t, outcome: "succeeded"}
	srv := httptest.NewServer(f.handler())
	defer srv.Close()
	c := NewClient(srv.URL, "секрет")

	reply, err := c.Ask(context.Background(), SessionOptions{
		Title: "Проверка", Directory: "/tmp/ws", Model: &ModelRef{ID: "m", ProviderID: ProviderID},
	}, "Привет")
	if err != nil {
		t.Fatal(err)
	}
	if reply.Text != "Первая часть.\n\nВторая часть." || reply.Outcome != "succeeded" || reply.SessionID != "ses_1" {
		t.Fatalf("ответ: %+v", reply)
	}
	if len(reply.Tools) != 1 || reply.Tools[0] != (ToolCall{Name: "shell", Status: "error"}) {
		t.Errorf("инструменты: %+v", reply.Tools)
	}
	if len(f.prompts) != 1 || f.prompts[0] != "ses_1:Привет" {
		t.Errorf("сообщения: %v", f.prompts)
	}

	// Сессия: агент приложения, запрет всего, рабочая папка агента, модель.
	s := f.sessions[0]
	raw, _ := json.Marshal(s["permissions"])
	var got []agentprofile.Rule
	json.Unmarshal(raw, &got)
	if s["agent"] != agentprofile.AgentID || len(got) != 1 || got[0] != agentprofile.DenyAll()[0] {
		t.Errorf("сессия: агент %v, права %s", s["agent"], raw)
	}
	if loc, _ := s["location"].(map[string]any); loc["directory"] != "/tmp/ws" {
		t.Errorf("рабочая папка: %v", s["location"])
	}
	if m, _ := s["model"].(map[string]any); m["providerID"] != ProviderID {
		t.Errorf("модель: %v", s["model"])
	}
}

func TestAskReportsAgentFailure(t *testing.T) {
	f := &fakeAPI{t: t, outcome: "failed"}
	srv := httptest.NewServer(f.handler())
	defer srv.Close()
	_, err := NewClient(srv.URL, "секрет").Ask(context.Background(), SessionOptions{}, "x")
	if !errors.Is(err, ErrAgentFailed) || !strings.Contains(err.Error(), "модель недоступна") {
		t.Fatalf("ждали ErrAgentFailed с причиной, получили %v", err)
	}
}

func TestClientReportsAPIErrors(t *testing.T) {
	f := &fakeAPI{t: t}
	srv := httptest.NewServer(f.handler())
	defer srv.Close()
	_, err := NewClient(srv.URL, "неверный").CreateSession(context.Background(), SessionOptions{})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusUnauthorized || apiErr.Tag != "UnauthorizedError" {
		t.Fatalf("ошибка: %#v", err)
	}
}

func TestStubModelScriptsToolCalls(t *testing.T) {
	stub, err := stubmodel.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer stub.Close()
	post := func(body string) map[string]any {
		resp, err := http.Post(stub.URL+"/chat/completions", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		json.NewDecoder(resp.Body).Decode(&out)
		return out
	}
	plain := post(`{"messages":[{"role":"user","content":"Привет"}],"tools":[{"function":{"name":"read"}}]}`)
	msg := plain["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)
	if msg["content"] != stubmodel.Reply {
		t.Errorf("обычный ответ: %v", msg)
	}
	call := post(`{"messages":[{"role":"user","content":[{"type":"text","text":"[stub:call name=shell args={\"command\":\"ls\"}]"}]}]}`)
	choice := call["choices"].([]any)[0].(map[string]any)
	if choice["finish_reason"] != "tool_calls" {
		t.Errorf("вызов инструмента: %v", choice)
	}
	reqs := stub.Requests()
	if len(reqs) != 2 || len(reqs[0].Tools) != 1 || reqs[0].Tools[0] != "read" || reqs[1].ToolCalls[0].Name != "shell" {
		t.Errorf("запросы: %+v", reqs)
	}
}

func TestEnsureModel(t *testing.T) {
	current := `{"id":"old-model","providerID":"daily-model"}`
	var switched []string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/session/{id}", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"data":{"id":"`+r.PathValue("id")+`","model":`+current+`}}`)
	})
	mux.HandleFunc("POST /api/session/{id}/model", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model ModelRef `json:"model"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		switched = append(switched, r.PathValue("id")+":"+body.Model.ProviderID+"/"+body.Model.ID)
		raw, _ := json.Marshal(body.Model)
		current = string(raw)
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := NewClient(srv.URL, "x")
	ctx := context.Background()

	if err := c.EnsureModel(ctx, "ses_1", nil); err != nil || len(switched) != 0 {
		t.Fatalf("без модели: %v %v", err, switched)
	}
	want := &ModelRef{ID: "new-model", ProviderID: ProviderID}
	if err := c.EnsureModel(ctx, "ses_1", want); err != nil {
		t.Fatal(err)
	}
	if err := c.EnsureModel(ctx, "ses_1", want); err != nil {
		t.Fatal(err)
	}
	if strings.Join(switched, ",") != "ses_1:daily-model/new-model" {
		t.Fatalf("переключения: %v", switched)
	}
	// У сессии без модели она ставится.
	current = `null`
	if err := c.EnsureModel(ctx, "ses_2", want); err != nil || len(switched) != 2 {
		t.Fatalf("сессия без модели: %v %v", err, switched)
	}
}

func TestListModelsAndIntegrations(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/model", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"location":{},"data":[
			{"id":"big","providerID":"lab","name":"Big","status":"active","enabled":true},
			{"id":"off","providerID":"lab","name":"Off","enabled":false},
			{"id":"gpt","providerID":"openai","name":"GPT"}]}`)
	})
	mux.HandleFunc("GET /api/integration", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"location":{},"data":[
			{"id":"openai","name":"OpenAI","methods":[{"type":"key"},{"type":"env","names":["OPENAI_API_KEY"]},{"type":"oauth"}],"connections":[]},
			{"id":"copilot","name":"Copilot","methods":[{"type":"oauth"}],"connections":[]}]}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := NewClient(srv.URL, "x")
	models, err := c.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0].Ref().String() != "lab/big" || models[1].Name != "GPT" {
		t.Fatalf("модели: %+v", models)
	}
	ints, err := c.Integrations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ints) != 2 || strings.Join(ints[0].KeyEnv, ",") != "OPENAI_API_KEY" || len(ints[1].KeyEnv) != 0 {
		t.Fatalf("каталог: %+v", ints)
	}
}
