//go:build integration

// Чат с настоящим OpenCode нужной версии и заглушкой модели, без сети.
// Запуск: go test -tags integration ./internal/app/
package app

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	agentprofile "daily-agent/agent"
	"daily-agent/internal/agent"
	"daily-agent/internal/agent/stubmodel"
	"daily-agent/internal/store"
)

type liveChat struct {
	t       *testing.T
	svc     *ChatsService
	backend *agent.Backend
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
	profile := agent.Profile{Root: root}
	sup := agent.NewSupervisor(agent.Config{
		Profile: profile, Binary: bin,
		Options: agentprofile.Options{Provider: model.Provider()},
		Logf:    t.Logf,
	})
	sup.Start(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := sup.WaitReady(ctx); err != nil {
		t.Fatal(err)
	}
	lc := &liveChat{t: t, notify: make(chan ChatEvent, 1000)}
	lc.backend = &agent.Backend{Supervisor: sup, Profile: profile, ModelRef: model.Ref()}
	lc.svc = NewChatsService(st, SystemClock(), lc.backend, NewTodayService(st, SystemClock()), func(e ChatEvent) {
		lc.mu.Lock()
		lc.events = append(lc.events, e)
		lc.mu.Unlock()
		lc.notify <- e
	})
	evCtx, evCancel := context.WithCancel(context.Background())
	lc.cancel = func() { evCancel(); sup.Stop() }
	go agent.RunEvents(evCtx, lc.backend.Client, ChatEventHandler(lc.svc))
	time.Sleep(300 * time.Millisecond) // подписка на события открывается
	t.Cleanup(lc.cancel)
	return lc
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
	chat, err := lc.svc.Create(ctx, true)
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
	chat, err := lc.svc.Create(ctx, false)
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
