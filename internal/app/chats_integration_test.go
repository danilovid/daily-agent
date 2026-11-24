//go:build integration

// Чат с настоящим OpenCode нужной версии и заглушкой модели, без сети.
// Запуск: go test -tags integration ./internal/app/
package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"daily-agent/internal/agent"
	"daily-agent/internal/agent/stubmodel"
	"daily-agent/internal/store"
)

type liveChat struct {
	t       *testing.T
	svc     *ChatsService
	backend *agent.Manager
	cancel  context.CancelFunc
	mu      sync.Mutex
	events  []ChatEvent
	notify  chan ChatEvent
}

func startLiveChat(t *testing.T, st *store.Store, root string, stub *stubmodel.Server) *liveChat {
	t.Helper()
	bin, err := agent.FindBinary(context.Background(), agent.Candidates(), agent.PinnedVersion, nil)
	if err != nil {
		t.Skipf("нет OpenCode %s: %v", agent.PinnedVersion, err)
	}
	model := agent.Model{BaseURL: stub.URL, ModelID: stubmodel.ModelID}
	lc := &liveChat{t: t, notify: make(chan ChatEvent, 1000)}
	lc.backend = agent.NewManager(agent.Config{Profile: agent.Profile{Root: root}, Binary: bin, Logf: t.Logf}, model.Setup())
	lc.backend.Start(context.Background())
	lc.waitReady()
	lc.svc = NewChatsService(st, SystemClock(), lc.backend, NewTodayService(st, SystemClock()), func(e ChatEvent) {
		lc.mu.Lock()
		lc.events = append(lc.events, e)
		lc.mu.Unlock()
		lc.notify <- e
	})
	lc.backend.OnChange(ChatStatusHandler(lc.svc))
	evCtx, evCancel := context.WithCancel(context.Background())
	lc.cancel = func() { evCancel(); lc.backend.Stop() }
	go agent.RunEvents(evCtx, lc.backend.Client, ChatEventHandler(lc.svc))
	time.Sleep(300 * time.Millisecond) // подписка на события открывается
	t.Cleanup(lc.cancel)
	return lc
}

func (lc *liveChat) waitReady() {
	lc.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := lc.backend.WaitReady(ctx); err != nil {
		lc.t.Fatal(err)
	}
}

// waitFor ждёт событие окна нужного типа.
func (lc *liveChat) waitFor(typ string) ChatEvent {
	lc.t.Helper()
	timeout := time.After(60 * time.Second)
	for {
		select {
		case e := <-lc.notify:
			if e.Type == typ {
				return e
			}
		case <-timeout:
			lc.t.Fatalf("не дождались события %s; были: %+v", typ, lc.events)
		}
	}
}

func TestLiveChatStreamsAnswerAndKeepsHistory(t *testing.T) {
	stub, err := stubmodel.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer stub.Close()
	st := openStore(t)
	root, _ := os.MkdirTemp("", "da-chat-")
	defer os.RemoveAll(root)
	ctx := context.Background()

	lc := startLiveChat(t, st, root, stub)
	chat, err := lc.svc.Create(ctx, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := lc.svc.Send(ctx, chat.ID, "Что срочное сегодня?"); err != nil {
		t.Fatal(err)
	}
	done := lc.waitFor("done")
	if done.Outcome != "succeeded" {
		t.Fatalf("итог: %+v", done)
	}
	var streamed strings.Builder
	deltas := 0
	lc.mu.Lock()
	for _, e := range lc.events {
		if e.Type == "delta" {
			deltas++
			streamed.WriteString(e.Delta)
		}
	}
	lc.mu.Unlock()
	// Быстрый ответ OpenCode может прислать одним куском; по частям — в TestLiveChatStop.
	if deltas < 1 || strings.TrimSpace(streamed.String()) != stubmodel.Reply {
		t.Fatalf("ответ по кускам: %d кусков, текст %q", deltas, streamed.String())
	}

	view, err := lc.svc.Open(ctx, chat.ID)
	if err != nil {
		t.Fatal(err)
	}
	roles := func(v ChatView) string {
		var out []string
		for _, m := range v.Messages {
			out = append(out, m.Role)
		}
		return strings.Join(out, ",")
	}
	if roles(view) != "context,user,agent" || view.Messages[2].Text != stubmodel.Reply || view.Context == "" {
		t.Fatalf("переписка: %+v", view.Messages)
	}
	// Агенда дошла до модели вместе с вопросом.
	if reqs := stub.Requests(); len(reqs) == 0 {
		t.Fatal("модель не получила запрос")
	}

	// История сохраняется после перезапуска OpenCode.
	lc.cancel()
	lc2 := startLiveChat(t, st, root, stub)
	after, err := lc2.svc.Open(ctx, chat.ID)
	if err != nil {
		t.Fatal(err)
	}
	if roles(after) != "context,user,agent" || after.Messages[1].Text != "Что срочное сегодня?" {
		t.Fatalf("после перезапуска: %+v", after.Messages)
	}
	list, _ := lc2.svc.List(ctx)
	if len(list) != 1 || list[0].Title != "Что срочное сегодня?" || list[0].Preview != stubmodel.Reply {
		t.Fatalf("список после перезапуска: %+v", list)
	}
}

func TestLiveChatStop(t *testing.T) {
	stub, err := stubmodel.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer stub.Close()
	st := openStore(t)
	root, _ := os.MkdirTemp("", "da-stop-")
	defer os.RemoveAll(root)
	ctx := context.Background()

	lc := startLiveChat(t, st, root, stub)
	chat, err := lc.svc.Create(ctx, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := lc.svc.Send(ctx, chat.ID, "Расскажи подробно "+stubmodel.SlowMarker); err != nil {
		t.Fatal(err)
	}
	// Медленный ответ приходит в окно по частям, пока агент ещё работает.
	first := lc.waitFor("delta")
	second := lc.waitFor("delta")
	if first.Delta == "" || second.Delta == "" {
		t.Fatalf("куски ответа: %+v, %+v", first, second)
	}
	if err := lc.svc.Stop(ctx, chat.ID); err != nil {
		t.Fatal(err)
	}
	done := lc.waitFor("done")
	t.Logf("итог после остановки: %+v", done)
	if done.Outcome == "succeeded" {
		t.Fatal("ответ дошёл до конца, остановка не сработала")
	}
	view, _ := lc.svc.Open(ctx, chat.ID)
	last := view.Messages[len(view.Messages)-1]
	if last.Role != "error" || last.Text != "Ответ остановлен" {
		t.Errorf("последняя строка: %+v (все: %+v)", last, view.Messages)
	}
	// После остановки можно спросить снова.
	if err := lc.svc.Send(ctx, chat.ID, "Коротко"); err != nil {
		t.Fatal(err)
	}
	if d := lc.waitFor("done"); d.Outcome != "succeeded" {
		t.Fatalf("второй вопрос: %+v", d)
	}
}

func TestLiveChatFollowsModelChange(t *testing.T) {
	first, err := stubmodel.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := stubmodel.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	st := openStore(t)
	root, _ := os.MkdirTemp("", "da-switch-")
	defer os.RemoveAll(root)
	ctx := context.Background()

	lc := startLiveChat(t, st, root, first)
	chat, err := lc.svc.Create(ctx, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := lc.svc.Send(ctx, chat.ID, "Первый вопрос"); err != nil {
		t.Fatal(err)
	}
	if d := lc.waitFor("done"); d.Outcome != "succeeded" {
		t.Fatalf("первый ответ: %+v", d)
	}
	before := len(first.Requests())

	// Модель сменилась в настройках: OpenCode перезапущен с другим адресом и моделью.
	lc.backend.SetModel(&agent.Model{BaseURL: second.URL, ModelID: "stub-model-2"})
	lc.waitReady()
	time.Sleep(1500 * time.Millisecond) // подписка на события открывается заново

	if err := lc.svc.Send(ctx, chat.ID, "Второй вопрос"); err != nil {
		t.Fatal(err)
	}
	if d := lc.waitFor("done"); d.Outcome != "succeeded" {
		t.Fatalf("второй ответ: %+v", d)
	}
	if len(first.Requests()) != before {
		t.Fatalf("прежняя модель получила запрос после смены")
	}
	reqs := second.Requests()
	if len(reqs) == 0 || reqs[len(reqs)-1].Model != "stub-model-2" {
		t.Fatalf("новая модель: %+v", reqs)
	}
	view, err := lc.svc.Open(ctx, chat.ID)
	if err != nil {
		t.Fatal(err)
	}
	var roles []string
	for _, m := range view.Messages {
		roles = append(roles, m.Role)
	}
	if strings.Join(roles, ",") != "user,agent,user,agent" {
		t.Fatalf("переписка после смены модели: %+v", view.Messages)
	}
}

func TestLiveSettingsSaveAndCheck(t *testing.T) {
	first, err := stubmodel.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := stubmodel.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	t.Setenv(agent.EnvModelURL, "")
	t.Setenv(agent.EnvModelID, "")
	st := openStore(t)
	root, _ := os.MkdirTemp("", "da-settings-")
	defer os.RemoveAll(root)
	ctx := context.Background()

	lc := startLiveChat(t, st, root, first)
	secrets := &memSecrets{}
	svc := NewSettingsService(st, secrets, lc.backend, SystemClock(), nil, false)

	if c := svc.CheckConnection(ctx); !c.OK || c.Version != agent.PinnedVersion {
		t.Fatalf("связь: %+v", c)
	}
	if c := svc.CheckModel(ctx, false); !c.OK {
		t.Fatalf("модель: %+v", c)
	}
	view, err := svc.Agent(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if view.Binary == "" || view.Status.State != agent.StateReady {
		t.Fatalf("раздел «Агент»: %+v", view)
	}

	// Сохранение настроек перезапускает OpenCode с новой моделью и ключом.
	if _, err := svc.SaveModel(ctx, ModelSettingsInput{URL: second.URL, ModelID: "stub-model-2", Key: "test-key-live"}); err != nil {
		t.Fatal(err)
	}
	lc.waitReady()
	if c := svc.CheckModel(ctx, false); !c.OK {
		t.Fatalf("новая модель: %+v", c)
	}
	reqs := second.Requests()
	if len(reqs) == 0 || reqs[len(reqs)-1].Model != "stub-model-2" || reqs[len(reqs)-1].LastText != ModelCheckPrompt || reqs[len(reqs)-1].APIKey != "test-key-live" {
		t.Fatalf("запросы новой модели: %+v", reqs)
	}
	if secrets.key(agent.KeychainAccount) != "test-key-live" {
		t.Fatal("ключ не сохранён")
	}
}

func TestLiveWorkAndPersonalModels(t *testing.T) {
	work, err := stubmodel.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer work.Close()
	home, err := stubmodel.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer home.Close()
	t.Setenv(agent.EnvModelURL, "")
	t.Setenv(agent.EnvModelID, "")
	// Вымышленные настройки OpenCode пользователя: два провайдера на заглушках.
	cfgPath := filepath.Join(t.TempDir(), "opencode.jsonc")
	os.WriteFile(cfgPath, []byte(`{
  // провайдеры пользователя
  "providers": {
    "lab": {"name": "Лаборатория", "package": "@opencode/ai/providers/openai-compatible",
      "settings": {"baseURL": "`+work.URL+`", "apiKey": "test-secret-in-config"},
      "models": {"stub-model": {"name": "Рабочая"}}},
    "home": {"name": "Дом", "package": "@opencode/ai/providers/openai-compatible",
      "settings": {"baseURL": "`+home.URL+`"},
      "models": {"stub-home": {"name": "Личная"}}},
  },
  "mcp": {"tracker": {"type": "remote", "url": "https://tracker.example.test/mcp"}},
}`), 0o600)
	user, err := agent.ReadUserConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	st := openStore(t)
	root, _ := os.MkdirTemp("", "da-models-")
	defer os.RemoveAll(root)
	ctx := context.Background()

	lc := startLiveChat(t, st, root, work)
	secrets := &memSecrets{}
	svc := NewSettingsService(st, secrets, lc.backend, SystemClock(), func() (agent.UserConfig, error) { return user, nil }, false)

	// Ключ провайдера из настроек OpenCode — в Связку ключей; OpenCode перезапускается.
	if _, err := svc.SaveProviderKey(ctx, ProviderKeyInput{ID: "lab", Key: "test-key-lab"}); err != nil {
		t.Fatal(err)
	}
	lc.waitReady()
	models, err := svc.Models(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var refs []string
	for _, m := range models {
		refs = append(refs, m.Ref)
	}
	if !contains(refs, "lab/stub-model") || !contains(refs, "home/stub-home") {
		t.Fatalf("модели OpenCode: %v", refs)
	}
	// Встроенный каталог OpenCode доступен без сети.
	catalog, err := svc.CatalogProviders(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range catalog {
		found = found || c.ID == "openai"
	}
	if !found || len(catalog) < 10 {
		t.Fatalf("каталог: %d провайдеров, openai: %v", len(catalog), found)
	}

	if _, err := svc.SaveModels(ctx, ModelsInput{Work: "lab/stub-model", Personal: "home/stub-home"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1500 * time.Millisecond) // подписка на события открывается заново после перезапуска

	workChat, err := lc.svc.Create(ctx, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := lc.svc.Send(ctx, workChat.ID, "Рабочий вопрос"); err != nil {
		t.Fatal(err)
	}
	if d := lc.waitFor("done"); d.Outcome != "succeeded" {
		t.Fatalf("рабочий ответ: %+v", d)
	}
	personalChat, err := lc.svc.Create(ctx, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := lc.svc.Send(ctx, personalChat.ID, "Личный вопрос"); err != nil {
		t.Fatal(err)
	}
	if d := lc.waitFor("done"); d.Outcome != "succeeded" {
		t.Fatalf("личный ответ: %+v", d)
	}

	last := func(reqs []stubmodel.Request) stubmodel.Request {
		if len(reqs) == 0 {
			t.Fatal("заглушка не получила запросов")
		}
		return reqs[len(reqs)-1]
	}
	w, h := last(work.Requests()), last(home.Requests())
	if w.Model != "stub-model" || w.APIKey != "test-key-lab" || w.LastText != "Рабочий вопрос" {
		t.Fatalf("рабочая модель: %+v", w)
	}
	if h.Model != "stub-home" || h.LastText != "Личный вопрос" || h.APIKey == "test-secret-in-config" {
		t.Fatalf("личная модель: %+v", h)
	}
	for _, r := range home.Requests() {
		if strings.Contains(r.LastText, "Рабочий") {
			t.Fatal("рабочий вопрос ушёл в личную модель")
		}
	}
}
