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
	"sync/atomic"
	"testing"
	"time"

	"daily-agent/internal/agent"
	"daily-agent/internal/agent/mcpstub"
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
	return startLiveChatWith(t, st, root, stub, nil)
}

// startLiveChatWith — живой чат со своим MCP-сервером приложения internal.
func startLiveChatWith(t *testing.T, st *store.Store, root string, stub *stubmodel.Server, internal *agent.InternalMCP) *liveChat {
	t.Helper()
	bin, err := agent.FindBinary(context.Background(), agent.Candidates(), agent.PinnedVersion, nil)
	if err != nil {
		t.Skipf("нет OpenCode %s: %v", agent.PinnedVersion, err)
	}
	model := agent.Model{BaseURL: stub.URL, ModelID: stubmodel.ModelID}
	lc := &liveChat{t: t, notify: make(chan ChatEvent, 1000)}
	lc.backend = agent.NewManager(agent.Config{Profile: agent.Profile{Root: root}, Binary: bin, Logf: t.Logf, Internal: internal}, model.Setup())
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

// waitReady ждёт, пока агент выйдет на связь, включая прогрев MCP-серверов.
func (lc *liveChat) waitReady() {
	lc.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := lc.backend.WaitReady(ctx); err != nil {
		lc.t.Fatal(err)
	}
	for lc.backend.Status().State != agent.StateReady {
		if ctx.Err() != nil {
			lc.t.Fatalf("агент не вышел на связь: %+v", lc.backend.Status())
		}
		time.Sleep(100 * time.Millisecond)
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
  "mcp": {"tracker": {"type": "local", "command": ["false"], "enabled": false}},
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

func TestLiveMCPInChats(t *testing.T) {
	model, err := stubmodel.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer model.Close()
	tracker, err := mcpstub.StartHTTP(false)
	if err != nil {
		t.Fatal(err)
	}
	defer tracker.Close()
	t.Setenv(agent.EnvModelURL, "")
	t.Setenv(agent.EnvModelID, "")
	cfgPath := filepath.Join(t.TempDir(), "opencode.json")
	os.WriteFile(cfgPath, []byte(`{"mcp": {"servers": {"tracker": {"type": "remote", "url": "`+tracker.URL+`",
		"oauth": false, "headers": {"Authorization": "Bearer test-secret-in-config"}}}}}`), 0o600)
	user, err := agent.ReadUserConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	st := openStore(t)
	tmp, _ := os.MkdirTemp("", "da-mcpchat-")
	defer os.RemoveAll(tmp)
	root, _ := filepath.EvalSymlinks(tmp)
	ctx := context.Background()

	lc := startLiveChat(t, st, root, model)
	secrets := &memSecrets{}
	settings := NewSettingsService(st, secrets, lc.backend, SystemClock(), func() (agent.UserConfig, error) { return user, nil }, false)
	mcp := NewMCPService(settings, lc.backend, func(string) error { return nil }, nil)
	if _, err := settings.SaveModel(ctx, ModelSettingsInput{URL: model.URL, ModelID: stubmodel.ModelID}); err != nil {
		t.Fatal(err)
	}
	list, err := mcp.List(ctx)
	if err != nil || len(list) != 1 || list[0].Status != "needs_token" {
		t.Fatalf("сервер без токена: %+v %v", list, err)
	}
	if _, err := mcp.SetToken(ctx, "tracker", "header", "Authorization", "test-token-live"); err != nil {
		t.Fatal(err)
	}
	lc.waitReady()
	list, _ = mcp.List(ctx)
	var allowed []string
	for _, tool := range list[0].Tools {
		if tool.Allowed {
			allowed = append(allowed, tool.Short)
		}
	}
	if list[0].Status != "connected" || strings.Join(allowed, ",") != "get_issue,list_issues" {
		t.Fatalf("сервер с токеном: %+v", list[0])
	}
	time.Sleep(1500 * time.Millisecond) // подписка на события открывается заново после перезапуска

	work, err := lc.svc.Create(ctx, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := lc.svc.Send(ctx, work.ID, `[stub:call name=tracker_get_issue args={"id":"TEST-1"}]`); err != nil {
		t.Fatal(err)
	}
	if d := lc.waitFor("done"); d.Outcome != "succeeded" {
		t.Fatalf("рабочий чат: %+v", d)
	}
	if calls := tracker.Calls(); len(calls) != 1 || calls[0] != "get_issue" {
		t.Fatalf("вызовы трекера из рабочего чата: %v", calls)
	}
	if h := tracker.Headers(); h[len(h)-1].Get("Authorization") != "Bearer test-token-live" {
		t.Fatalf("токен трекера: %q", h[len(h)-1].Get("Authorization"))
	}

	// Личный чат: те же слова, но инструментов рабочих систем нет.
	personal, err := lc.svc.Create(ctx, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := lc.svc.Send(ctx, personal.ID, `[stub:call name=tracker_get_issue args={"id":"TEST-1"}]`); err != nil {
		t.Fatal(err)
	}
	lc.waitFor("done")
	if calls := tracker.Calls(); len(calls) != 1 {
		t.Fatalf("личный чат вызвал трекер: %v", calls)
	}
	// Запись из рабочего чата не проходит.
	if err := lc.svc.Send(ctx, work.ID, `[stub:call name=tracker_create_issue args={"title":"x"}]`); err != nil {
		t.Fatal(err)
	}
	lc.waitFor("done")
	if calls := tracker.Calls(); len(calls) != 1 {
		t.Fatalf("запись дошла до трекера: %v", calls)
	}
	if cfg, _ := os.ReadFile(filepath.Join(root, "config", "opencode", "opencode.json")); strings.Contains(string(cfg), "test-token-live") || strings.Contains(string(cfg), "test-secret-in-config") {
		t.Fatal("токен в настройках OpenCode")
	}
}

func TestLivePlanDay(t *testing.T) {
	model, err := stubmodel.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer model.Close()
	tmp, _ := os.MkdirTemp("", "da-plan-")
	defer os.RemoveAll(tmp)
	root, _ := filepath.EvalSymlinks(tmp)
	ctx := context.Background()
	dbPath := filepath.Join(root, store.FileName)
	st, err := store.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	clock := SystemClock()
	today := clock.today()
	urgent, _ := st.CreateTask(ctx, store.Task{Title: "Ответить про сроки", Status: store.StatusBacklog, DueDate: today})
	review, _ := st.CreateTask(ctx, store.Task{Title: "Заметки к ревью", Status: store.StatusBacklog})
	st.CreateTask(ctx, store.Task{Title: "Новая просьба", Status: store.StatusInbox})

	// Свой сервер — тестовый бинарник в роли «daily-agent mcp» на той же базе.
	lc := startLiveChatWith(t, st, filepath.Join(root, "agent"), model, &agent.InternalMCP{
		Name:       InternalMCPServerName,
		Definition: map[string]any{"type": "local", "command": []string{os.Args[0]}, "environment": map[string]any{internalMCPEnv: dbPath}},
		Tools:      InternalMCPTools,
	})
	lc.waitReady()
	var changes atomic.Int32
	old := WatchInterval
	WatchInterval = 50 * time.Millisecond
	defer func() { WatchInterval = old }()
	watchCtx, stopWatch := context.WithCancel(ctx)
	defer stopWatch()
	go WatchData(watchCtx, st, func() { changes.Add(1) })

	// «Составить план»: рабочий чат с агендой, у модели — инструменты плана.
	before := len(model.Requests())
	plan, err := lc.svc.PlanDay(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if d := lc.waitFor("done"); d.Outcome != "succeeded" {
		t.Fatalf("план: %+v", d)
	}
	var offered []string
	for _, r := range model.Requests()[before:] {
		if len(r.Tools) > len(offered) {
			offered = r.Tools
		}
	}
	if strings.Join(offered, ",") != "daily_get_day,daily_list_tasks,daily_set_day_plan" {
		t.Fatalf("инструменты плана: %v", offered)
	}

	// Агент (заглушка) читает день и записывает план своим сервером.
	args := `{"summary":"Начни со сроков.","items":[{"task_id":"` + urgent.ID + `","estimate_min":30,"reason":"срок сегодня"},{"task_id":"` + review.ID + `","estimate_min":60,"reason":"ревью"}]}`
	if err := lc.svc.Send(ctx, plan.ID, "[stub:call name=daily_get_day args={}] [stub:call name=daily_set_day_plan args="+args+"]"); err != nil {
		t.Fatal(err)
	}
	if d := lc.waitFor("done"); d.Outcome != "succeeded" {
		t.Fatalf("запись плана: %+v", d)
	}
	saved, err := st.DayPlan(ctx, today)
	if err != nil || saved == nil || saved.Source != store.PlanSourceAgent || len(saved.Items) != 2 || saved.Summary != "Начни со сроков." {
		t.Fatalf("план в базе: %+v %v", saved, err)
	}
	view, _ := NewTodayService(st, clock).Today(ctx, "")
	if view.Plan == nil || view.Plan.TotalMin != 90 || view.Plan.Items[0].Reason != "срок сегодня" {
		t.Fatalf("план на экране «Сегодня»: %+v", view.Plan)
	}
	// Окно узнаёт о записи другого процесса.
	deadline := time.Now().Add(2 * time.Second)
	for changes.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if changes.Load() == 0 {
		t.Fatal("наблюдение за базой не заметило план")
	}

	// Личный чат инструментов приложения не получает.
	personal, _ := lc.svc.Create(ctx, false, true)
	before = len(model.Requests())
	lc.svc.Send(ctx, personal.ID, "[stub:call name=daily_list_tasks args={}]")
	lc.waitFor("done")
	for _, r := range model.Requests()[before:] {
		if len(r.Tools) > 0 {
			t.Fatalf("личному чату предложены инструменты: %v", r.Tools)
		}
	}
}
