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

func (m *memSecrets) key(account string) string {
	v, _ := m.Get(context.Background(), agent.KeychainService, account)
	return v
}

// fakeControl — агент, который запоминает смены моделей.
type fakeControl struct {
	fakeBackend
	setups []*agent.Setup
}

func (f *fakeControl) SetSetup(s *agent.Setup) {
	f.setups = append(f.setups, s)
	f.model, f.personal = nil, nil
	if s != nil {
		f.model, f.personal = s.Work, s.Personal
	}
}

func (f *fakeControl) last() *agent.Setup {
	if len(f.setups) == 0 {
		return nil
	}
	return f.setups[len(f.setups)-1]
}

type settingsHarness struct {
	svc     *SettingsService
	store   *store.Store
	secrets *memSecrets
	control *fakeControl
	api     *fakeOpenCode
}

func newSettingsHarness(t *testing.T, stub bool, user agent.UserConfig) *settingsHarness {
	t.Helper()
	t.Setenv(agent.EnvModelURL, "")
	t.Setenv(agent.EnvModelID, "")
	h := &settingsHarness{store: openStore(t), secrets: &memSecrets{}, api: &fakeOpenCode{}}
	srv := h.api.server(t)
	h.control = &fakeControl{fakeBackend: fakeBackend{
		url:    srv.URL,
		status: agent.Status{State: agent.StateReady, Version: agent.PinnedVersion, Binary: "/opt/test/opencode"},
	}}
	h.svc = NewSettingsService(h.store, h.secrets, h.control, testClock(), func() (agent.UserConfig, error) { return user, nil }, stub)
	return h
}

// assertNoSecret проверяет, что ключ не попал ни в окно, ни в базу.
func (h *settingsHarness) assertNoSecret(t *testing.T, view AgentSettingsView, secrets ...string) {
	t.Helper()
	raw, _ := json.Marshal(view)
	all, _ := h.store.Settings(context.Background(), "")
	stored, _ := json.Marshal(all)
	for _, s := range secrets {
		if strings.Contains(string(raw), s) {
			t.Errorf("ключ ушёл в окно: %s", raw)
		}
		if strings.Contains(string(stored), s) {
			t.Errorf("ключ записан в базу: %s", stored)
		}
	}
}

func TestSettingsAgentEmpty(t *testing.T) {
	h := newSettingsHarness(t, false, agent.UserConfig{})
	v, err := h.svc.Agent(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if v.Model.Source != ModelSourceNone || v.Model.KeySaved || v.Work != "" || v.Personal != "" || len(v.Providers) != 0 || v.Note != "" {
		t.Fatalf("пустые настройки: %+v", v)
	}
	if v.Binary != "/opt/test/opencode" || v.PinnedVersion != agent.PinnedVersion || v.Workspace != "/tmp/ws" || v.Status.State != agent.StateReady {
		t.Fatalf("OpenCode: %+v", v)
	}
}

func TestSettingsSaveOwnModel(t *testing.T) {
	h := newSettingsHarness(t, false, agent.UserConfig{})
	ctx := context.Background()
	const key = "test-key-1"

	v, err := h.svc.SaveModel(ctx, ModelSettingsInput{URL: " http://127.0.0.1:9/v1/ ", ModelID: " test-model ", Key: key})
	if err != nil {
		t.Fatal(err)
	}
	if v.Model != (ModelSettingsView{URL: "http://127.0.0.1:9/v1", ModelID: "test-model", KeySaved: true, Source: ModelSourceSettings}) {
		t.Fatalf("после сохранения: %+v", v.Model)
	}
	if v.Work != "daily-model/test-model" || len(v.Providers) != 1 || !v.Providers[0].Selfhost || !v.Providers[0].KeySaved {
		t.Fatalf("своя модель — рабочая: %+v", v)
	}
	h.assertNoSecret(t, v, key)
	if h.secrets.key(agent.KeychainAccount) != key {
		t.Fatal("ключ не в хранилище")
	}
	s := h.control.last()
	if s == nil || s.Options.Provider.BaseURL != "http://127.0.0.1:9/v1" || strings.Join(s.Env, ",") != agent.ModelKeyEnv+"="+key || s.Work.String() != "daily-model/test-model" {
		t.Fatalf("модели агента: %+v", s)
	}

	// Удаление ключа: агент без ключа.
	v, err = h.svc.SaveModel(ctx, ModelSettingsInput{URL: "http://127.0.0.1:9/v1", ModelID: "test-model", RemoveKey: true})
	if err != nil {
		t.Fatal(err)
	}
	if v.Model.KeySaved || h.secrets.key(agent.KeychainAccount) != "" || len(h.control.last().Env) != 0 {
		t.Fatalf("после удаления ключа: %+v", v.Model)
	}
}

func TestSettingsSaveOwnModelRejects(t *testing.T) {
	h := newSettingsHarness(t, false, agent.UserConfig{})
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
	if len(all) != 0 || len(h.control.setups) != 0 || h.secrets.key(agent.KeychainAccount) != "" {
		t.Fatalf("после отказов: настройки %v, смены %d", all, len(h.control.setups))
	}

	// Связка ключей отказала — настройки прежние.
	h.secrets.setErr = errors.New("Связка ключей заблокирована")
	if _, err := h.svc.SaveModel(ctx, ModelSettingsInput{URL: "http://127.0.0.1:9/v1", ModelID: "m", Key: "test-key"}); err == nil {
		t.Fatal("ждали ошибку Связки ключей")
	}
	all, _ = h.store.Settings(ctx, "")
	if len(all) != 0 || len(h.control.setups) != 0 {
		t.Fatalf("после ошибки Связки ключей: %v", all)
	}
}

func TestSettingsStubAndEnv(t *testing.T) {
	h := newSettingsHarness(t, true, agent.UserConfig{})
	ctx := context.Background()
	v, err := h.svc.SaveModel(ctx, ModelSettingsInput{URL: "http://127.0.0.1:9/v1", ModelID: "test-model", Key: "test-key"})
	if err != nil {
		t.Fatal(err)
	}
	if v.Model.Source != ModelSourceStub || v.Note == "" || len(h.control.setups) != 0 {
		t.Fatalf("с заглушкой настройки сохраняются, но не применяются: %+v, смены %d", v, len(h.control.setups))
	}

	h = newSettingsHarness(t, false, agent.UserConfig{})
	t.Setenv(agent.EnvModelURL, "http://127.0.0.1:8/v1")
	t.Setenv(agent.EnvModelID, "env-model")
	v, _ = h.svc.Agent(ctx)
	if v.Model.Source != ModelSourceEnv || v.Model.ModelID != "env-model" || v.Work != "daily-model/env-model" || v.Note == "" {
		t.Fatalf("модель из окружения: %+v", v)
	}
	setup, err := LoadSetup(ctx, h.store, h.secrets, agent.UserConfig{})
	if err != nil || setup.Work.String() != "daily-model/env-model" {
		t.Fatalf("LoadSetup из окружения: %+v %v", setup, err)
	}
	// Настройки приложения важнее окружения.
	if _, err := h.svc.SaveModel(ctx, ModelSettingsInput{URL: "http://127.0.0.1:9/v1", ModelID: "test-model"}); err != nil {
		t.Fatal(err)
	}
	setup, _ = LoadSetup(ctx, h.store, h.secrets, agent.UserConfig{})
	if setup.Work.String() != "daily-model/test-model" || setup.Options.Provider.BaseURL != "http://127.0.0.1:9/v1" {
		t.Fatalf("LoadSetup из настроек: %+v", setup)
	}
}

// testUserConfig — вымышленные настройки OpenCode: селфхост и облачный провайдеры.
func testUserConfig(t *testing.T) agent.UserConfig {
	t.Helper()
	path := filepath.Join(t.TempDir(), "opencode.json")
	if err := os.WriteFile(path, []byte(`{"providers": {
		"lab": {"name": "Лаборатория", "package": "@opencode/ai/providers/openai-compatible",
			"settings": {"baseURL": "https://llm.lab.example.test/v1", "apiKey": "test-secret-lab"},
			"models": {"big": {"name": "Big"}}},
		"router": {"name": "Роутер", "package": "@opencode/ai/providers/openai-compatible",
			"settings": {"baseURL": "https://openrouter.ai/api/v1"}, "models": {"some/model": {}}}
	}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := agent.ReadUserConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestSettingsProvidersAndModels(t *testing.T) {
	h := newSettingsHarness(t, false, testUserConfig(t))
	ctx := context.Background()
	h.api.modelList = `
		{"id":"big","providerID":"lab","name":"Big"},
		{"id":"some/model","providerID":"router","name":"Some"},
		{"id":"gpt","providerID":"openai","name":"GPT"}`
	h.api.catalog = `
		{"id":"openai","name":"OpenAI","methods":[{"type":"key"},{"type":"env","names":["OPENAI_API_KEY"]}],"connections":[]},
		{"id":"copilot","name":"Copilot","methods":[{"type":"oauth"}],"connections":[]},
		{"id":"lab","name":"Лаборатория","methods":[{"type":"env","names":["LAB_KEY"]}],"connections":[]}`

	v, err := h.svc.Agent(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Providers) != 2 || v.Providers[0].ID != "lab" || !v.Providers[0].Selfhost || v.Providers[0].Dropped != 1 || v.Providers[1].Selfhost {
		t.Fatalf("провайдеры из настроек OpenCode: %+v", v.Providers)
	}
	if !strings.HasSuffix(v.UserConfig, "opencode.json") {
		t.Fatalf("путь к настройкам OpenCode: %q", v.UserConfig)
	}

	// Модели — только подключённых провайдеров; openai пока не подключён.
	models, err := h.svc.Models(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0].Ref != "lab/big" || !models[0].Selfhost || models[1].Ref != "router/some/model" || models[1].Selfhost {
		t.Fatalf("модели: %+v", models)
	}

	// Для рабочих задач облачная модель не выбирается.
	if _, err := h.svc.SaveModels(ctx, ModelsInput{Work: "router/some/model"}); err == nil || !strings.Contains(err.Error(), "облачная") {
		t.Fatalf("облако для рабочих задач: %v", err)
	}
	if _, err := h.svc.SaveModels(ctx, ModelsInput{Work: "nobody/m"}); err == nil {
		t.Fatal("модель неизвестного провайдера")
	}
	v, err = h.svc.SaveModels(ctx, ModelsInput{Work: "lab/big", Personal: "router/some/model"})
	if err != nil {
		t.Fatal(err)
	}
	s := h.control.last()
	if v.Work != "lab/big" || v.Personal != "router/some/model" || s.Work.String() != "lab/big" || s.Personal.String() != "router/some/model" {
		t.Fatalf("выбор моделей: %+v / %+v", v, s)
	}
	if _, ok := s.Options.Imported["lab"]; !ok || len(s.Options.Imported) != 2 {
		t.Fatalf("провайдеры в профиле: %+v", s.Options.Imported)
	}

	// Ключ провайдера из настроек OpenCode — в Связку ключей и в окружение OpenCode.
	v, err = h.svc.SaveProviderKey(ctx, ProviderKeyInput{ID: "lab", Key: "test-key-lab"})
	if err != nil {
		t.Fatal(err)
	}
	if !v.Providers[0].KeySaved || h.secrets.key(agent.ProviderKeyAccount("lab")) != "test-key-lab" {
		t.Fatalf("ключ lab: %+v", v.Providers)
	}
	if !contains(h.control.last().Env, "DAILY_AGENT_KEY_LAB=test-key-lab") {
		t.Fatalf("окружение OpenCode: %v", h.control.last().Env)
	}

	// Облачный провайдер каталога подключается ключом.
	catalog, err := h.svc.CatalogProviders(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog) != 1 || catalog[0].ID != "openai" {
		t.Fatalf("каталог: %+v", catalog)
	}
	if _, err := h.svc.SaveProviderKey(ctx, ProviderKeyInput{ID: "copilot", Key: "test-key"}); err == nil {
		t.Fatal("провайдер без входа по ключу")
	}
	v, err = h.svc.SaveProviderKey(ctx, ProviderKeyInput{ID: "openai", Key: "test-key-cloud"})
	if err != nil {
		t.Fatal(err)
	}
	s = h.control.last()
	if len(s.Options.Catalog) != 1 || s.Options.Catalog[0] != "openai" || !contains(s.Env, "OPENAI_API_KEY=test-key-cloud") {
		t.Fatalf("каталог в профиле: %+v", s)
	}
	if len(v.Providers) != 3 || v.Providers[2].Source != ProviderSourceCatalog || !v.Providers[2].KeySaved {
		t.Fatalf("провайдеры: %+v", v.Providers)
	}
	if _, err := h.svc.SaveModels(ctx, ModelsInput{Work: "openai/gpt"}); err == nil {
		t.Fatal("облачный провайдер каталога для рабочих задач")
	}
	if v, err = h.svc.SaveModels(ctx, ModelsInput{Work: "lab/big", Personal: "openai/gpt"}); err != nil || v.Personal != "openai/gpt" {
		t.Fatalf("личная облачная модель: %+v, %v", v, err)
	}
	h.assertNoSecret(t, v, "test-key-lab", "test-key-cloud", "test-secret-lab")

	// Отключение облачного провайдера сбрасывает личную модель и удаляет ключ.
	v, err = h.svc.RemoveProvider(ctx, "openai")
	if err != nil {
		t.Fatal(err)
	}
	if v.Personal != "" || len(v.Providers) != 2 || h.secrets.key(agent.ProviderKeyAccount("openai")) != "" || len(h.control.last().Options.Catalog) != 0 {
		t.Fatalf("после отключения: %+v", v)
	}
	// У провайдера из настроек OpenCode удаляется только ключ.
	v, err = h.svc.RemoveProvider(ctx, "lab")
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Providers) != 2 || v.Providers[0].KeySaved || h.secrets.key(agent.ProviderKeyAccount("lab")) != "" || v.Work != "lab/big" {
		t.Fatalf("после удаления ключа lab: %+v", v)
	}
}

func TestSettingsWorkModelMustStaySelfhost(t *testing.T) {
	h := newSettingsHarness(t, false, testUserConfig(t))
	ctx := context.Background()
	// Выбор, записанный в обход проверки, всё равно не даёт облака для работы.
	if err := h.store.SetSettings(ctx, map[string]string{store.SettingWorkModel: "router/some/model"}); err != nil {
		t.Fatal(err)
	}
	setup, err := LoadSetup(ctx, h.store, h.secrets, testUserConfig(t))
	if err != nil || setup.Work != nil {
		t.Fatalf("облачная рабочая модель: %+v, %v", setup, err)
	}
	v, _ := h.svc.Agent(ctx)
	if v.Work != "" || !strings.Contains(v.Note, "не селфхост") {
		t.Fatalf("пояснение: %+v", v)
	}
}

func TestSettingsChecks(t *testing.T) {
	h := newSettingsHarness(t, false, agent.UserConfig{})
	ctx := context.Background()

	c := h.svc.CheckConnection(ctx)
	if !c.OK || c.Version != agent.PinnedVersion || c.CheckedAt != testClock().now().Format("15:04") || c.Error != "" {
		t.Fatalf("связь: %+v", c)
	}
	if c := h.svc.CheckModel(ctx, false); c.OK || c.Error != "Модель для рабочих задач не выбрана" {
		t.Fatalf("без модели: %+v", c)
	}

	h.control.model = &agent.ModelRef{ID: "m", ProviderID: agent.ProviderID}
	h.api.messages = `{"cursor":{},"data":[
		{"type":"user","id":"u1","text":"` + ModelCheckPrompt + `"},
		{"type":"assistant","id":"a1","content":[{"type":"text","text":"Готов"}]},
		{"type":"idle","id":"i1","outcome":"succeeded"}
	]}`
	if c := h.svc.CheckModel(ctx, false); !c.OK || c.Error != "" {
		t.Fatalf("модель ответила: %+v", c)
	}
	// Личная модель без своей — та же, что рабочая.
	if c := h.svc.CheckModel(ctx, true); !c.OK {
		t.Fatalf("личная модель: %+v", c)
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
	if c := h.svc.CheckModel(ctx, false); c.OK || c.Error != "Модель не ответила: 401 Unauthorized" {
		t.Fatalf("модель отказала: %+v", c)
	}

	h.control.status = agent.Status{State: agent.StateRestarting}
	if c := h.svc.CheckConnection(ctx); c.OK || c.Error != "OpenCode перезапускается" {
		t.Fatalf("OpenCode перезапускается: %+v", c)
	}
	if _, err := h.svc.Models(ctx); err == nil {
		t.Fatal("список моделей без OpenCode")
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
