package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"daily-agent/internal/agent"
	"daily-agent/internal/store"
)

// memSecrets — хранилище ключей в памяти вместо Связки ключей.
type memSecrets struct {
	mu     sync.Mutex
	values map[string]string
	setErr error
}

func (m *memSecrets) Get(_ context.Context, service, account string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.values[service+"/"+account]
	if !ok {
		return "", agent.ErrKeyNotFound
	}
	return v, nil
}

func (m *memSecrets) Set(_ context.Context, service, account, secret string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.setErr != nil {
		return m.setErr
	}
	if m.values == nil {
		m.values = map[string]string{}
	}
	m.values[service+"/"+account] = secret
	return nil
}

func (m *memSecrets) Delete(_ context.Context, service, account string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.values, service+"/"+account)
	return nil
}

func (m *memSecrets) key() string {
	v, _ := m.Get(context.Background(), agent.KeychainService, agent.KeychainAccount)
	return v
}

// fakeControl — агент, который запоминает смены модели.
type fakeControl struct {
	fakeBackend
	switches []*agent.Model
}

func (f *fakeControl) SetModel(m *agent.Model) {
	f.switches = append(f.switches, m)
	f.model = nil
	if m != nil {
		f.model = m.Ref()
	}
}

type settingsHarness struct {
	svc     *SettingsService
	store   *store.Store
	secrets *memSecrets
	control *fakeControl
	api     *fakeOpenCode
}

func newSettingsHarness(t *testing.T, stub bool) *settingsHarness {
	t.Helper()
	t.Setenv(agent.EnvModelURL, "")
	t.Setenv(agent.EnvModelID, "")
	h := &settingsHarness{store: openStore(t), secrets: &memSecrets{}, api: &fakeOpenCode{}}
	srv := h.api.server(t)
	h.control = &fakeControl{fakeBackend: fakeBackend{
		url:    srv.URL,
		status: agent.Status{State: agent.StateReady, Version: agent.PinnedVersion, Binary: "/opt/test/opencode"},
	}}
	h.svc = NewSettingsService(h.store, h.secrets, h.control, testClock(), stub)
	return h
}

func TestSettingsAgentEmpty(t *testing.T) {
	h := newSettingsHarness(t, false)
	v, err := h.svc.Agent(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if v.Model.Source != ModelSourceNone || v.Model.KeySaved || v.Model.URL != "" || v.Note != "" {
		t.Fatalf("модель: %+v", v)
	}
	if v.Binary != "/opt/test/opencode" || v.PinnedVersion != agent.PinnedVersion || v.Workspace != "/tmp/ws" || v.Status.State != agent.StateReady {
		t.Fatalf("OpenCode: %+v", v)
	}
}

func TestSettingsSaveModel(t *testing.T) {
	h := newSettingsHarness(t, false)
	ctx := context.Background()
	const key = "test-key-1"

	v, err := h.svc.SaveModel(ctx, ModelSettingsInput{URL: " http://127.0.0.1:9/v1/ ", ModelID: " test-model ", Key: key})
	if err != nil {
		t.Fatal(err)
	}
	if v.Model != (ModelSettingsView{URL: "http://127.0.0.1:9/v1", ModelID: "test-model", KeySaved: true, Source: ModelSourceSettings}) {
		t.Fatalf("после сохранения: %+v", v.Model)
	}
	raw, _ := json.Marshal(v)
	if strings.Contains(string(raw), key) {
		t.Fatalf("ключ ушёл в окно: %s", raw)
	}
	if h.secrets.key() != key {
		t.Fatalf("ключ в хранилище: %q", h.secrets.key())
	}
	all, _ := h.store.Settings(ctx, "")
	for k, val := range all {
		if strings.Contains(val, key) {
			t.Fatalf("ключ в базе: %s", k)
		}
	}
	if len(h.control.switches) != 1 || *h.control.switches[0] != (agent.Model{BaseURL: "http://127.0.0.1:9/v1", ModelID: "test-model", APIKey: key}) {
		t.Fatalf("смена модели: %+v", h.control.switches)
	}

	// То же самое без нового ключа: ключ прежний, перезапуска нет.
	if _, err := h.svc.SaveModel(ctx, ModelSettingsInput{URL: "http://127.0.0.1:9/v1", ModelID: "test-model"}); err != nil {
		t.Fatal(err)
	}
	if len(h.control.switches) != 1 || h.secrets.key() != key {
		t.Fatalf("без изменений: %d смен, ключ %q", len(h.control.switches), h.secrets.key())
	}

	// Удаление ключа перезапускает агента без ключа.
	v, err = h.svc.SaveModel(ctx, ModelSettingsInput{URL: "http://127.0.0.1:9/v1", ModelID: "test-model", RemoveKey: true})
	if err != nil {
		t.Fatal(err)
	}
	if v.Model.KeySaved || h.secrets.key() != "" || len(h.control.switches) != 2 || h.control.switches[1].APIKey != "" {
		t.Fatalf("после удаления ключа: %+v, смены %d", v.Model, len(h.control.switches))
	}

	// Новая модель — новый перезапуск.
	if _, err := h.svc.SaveModel(ctx, ModelSettingsInput{URL: "http://127.0.0.1:9/v1", ModelID: "test-model-2"}); err != nil {
		t.Fatal(err)
	}
	if len(h.control.switches) != 3 || h.control.switches[2].ModelID != "test-model-2" {
		t.Fatalf("новая модель: %+v", h.control.switches)
	}
}

func TestSettingsSaveModelRejects(t *testing.T) {
	h := newSettingsHarness(t, false)
	ctx := context.Background()
	bad := []ModelSettingsInput{
		{URL: "", ModelID: "m"},
		{URL: "https://api.openai.com/v1", ModelID: "m"},
		{URL: "https://user:pass@llm.example.test/v1", ModelID: "m"},
		{URL: "http://127.0.0.1:9/v1", ModelID: ""},
		{URL: "http://127.0.0.1:9/v1", ModelID: "with space"},
		{URL: "http://127.0.0.1:9/v1", ModelID: "m", Key: "ключ"},
		{URL: "http://127.0.0.1:9/v1", ModelID: "m", Key: "test-key", RemoveKey: true},
	}
	for _, in := range bad {
		if _, err := h.svc.SaveModel(ctx, in); err == nil {
			t.Errorf("сохранено: %+v", in)
		}
	}
	all, _ := h.store.Settings(ctx, "")
	if len(all) != 0 || len(h.control.switches) != 0 || h.secrets.key() != "" {
		t.Fatalf("после отказов: настройки %v, смены %d", all, len(h.control.switches))
	}

	// Связка ключей отказала — настройки прежние.
	h.secrets.setErr = errors.New("Связка ключей заблокирована")
	if _, err := h.svc.SaveModel(ctx, ModelSettingsInput{URL: "http://127.0.0.1:9/v1", ModelID: "m", Key: "test-key"}); err == nil {
		t.Fatal("ждали ошибку Связки ключей")
	}
	all, _ = h.store.Settings(ctx, "")
	if len(all) != 0 || len(h.control.switches) != 0 {
		t.Fatalf("после ошибки Связки ключей: %v", all)
	}
}

func TestSettingsStubAndEnv(t *testing.T) {
	h := newSettingsHarness(t, true)
	ctx := context.Background()
	v, err := h.svc.SaveModel(ctx, ModelSettingsInput{URL: "http://127.0.0.1:9/v1", ModelID: "test-model", Key: "test-key"})
	if err != nil {
		t.Fatal(err)
	}
	if v.Model.Source != ModelSourceStub || v.Note == "" || len(h.control.switches) != 0 {
		t.Fatalf("с заглушкой настройки сохраняются, но не применяются: %+v, смены %d", v, len(h.control.switches))
	}

	h = newSettingsHarness(t, false)
	t.Setenv(agent.EnvModelURL, "http://127.0.0.1:8/v1")
	t.Setenv(agent.EnvModelID, "env-model")
	v, _ = h.svc.Agent(ctx)
	if v.Model.Source != ModelSourceEnv || v.Model.ModelID != "env-model" || v.Note == "" {
		t.Fatalf("модель из окружения: %+v", v)
	}
	m, src, err := LoadModel(ctx, h.store, h.secrets)
	if err != nil || src != ModelSourceEnv || m.ModelID != "env-model" {
		t.Fatalf("LoadModel из окружения: %+v %s %v", m, src, err)
	}
	// Настройки приложения важнее окружения.
	if _, err := h.svc.SaveModel(ctx, ModelSettingsInput{URL: "http://127.0.0.1:9/v1", ModelID: "test-model"}); err != nil {
		t.Fatal(err)
	}
	m, src, _ = LoadModel(ctx, h.store, h.secrets)
	if src != ModelSourceSettings || m.ModelID != "test-model" {
		t.Fatalf("LoadModel из настроек: %+v %s", m, src)
	}
	if len(h.control.switches) != 1 {
		t.Fatalf("смена с модели из окружения на модель из настроек: %d", len(h.control.switches))
	}
}

func TestSettingsChecks(t *testing.T) {
	h := newSettingsHarness(t, false)
	ctx := context.Background()

	c := h.svc.CheckConnection(ctx)
	if !c.OK || c.Version != agent.PinnedVersion || c.CheckedAt != testClock().now().Format("15:04") || c.Error != "" {
		t.Fatalf("связь: %+v", c)
	}
	if c := h.svc.CheckModel(ctx); c.OK || c.Error != "Модель агента не настроена" {
		t.Fatalf("без модели: %+v", c)
	}

	h.control.model = &agent.ModelRef{ID: "m", ProviderID: agent.ProviderID}
	h.api.messages = `{"cursor":{},"data":[
		{"type":"user","id":"u1","text":"` + ModelCheckPrompt + `"},
		{"type":"assistant","id":"a1","content":[{"type":"text","text":"Готов"}]},
		{"type":"idle","id":"i1","outcome":"succeeded"}
	]}`
	if c := h.svc.CheckModel(ctx); !c.OK || c.Error != "" {
		t.Fatalf("модель ответила: %+v", c)
	}
	h.api.mu.Lock()
	prompt := h.api.prompts[len(h.api.prompts)-1]
	h.api.mu.Unlock()
	if !strings.HasSuffix(prompt, ModelCheckPrompt) {
		t.Fatalf("вопрос модели: %q", prompt)
	}

	h.api.messages = `{"cursor":{},"data":[
		{"type":"user","id":"u1","text":"q"},
		{"type":"idle","id":"i1","outcome":"failed","error":{"message":"401 Unauthorized"}}
	]}`
	if c := h.svc.CheckModel(ctx); c.OK || c.Error != "Модель не ответила: 401 Unauthorized" {
		t.Fatalf("модель отказала: %+v", c)
	}

	h.control.status = agent.Status{State: agent.StateRestarting}
	if c := h.svc.CheckConnection(ctx); c.OK || c.Error != "OpenCode перезапускается" {
		t.Fatalf("OpenCode перезапускается: %+v", c)
	}
}

func TestDisplayPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip(err)
	}
	cases := map[string]string{
		"":                                "",
		home:                              "~",
		filepath.Join(home, "DailyAgent"): "~/DailyAgent",
		"/opt/test/opencode":              "/opt/test/opencode",
		home + "-other/x":                 home + "-other/x",
	}
	for in, want := range cases {
		if got := displayPath(in); got != want {
			t.Errorf("%q → %q, ждали %q", in, got, want)
		}
	}
}
