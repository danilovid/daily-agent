//go:build integration

// Интеграционные тесты с настоящим OpenCode нужной версии и заглушкой модели.
// Запуск: go test -tags integration ./internal/agent/
// Без OpenCode нужной версии тесты пропускаются. В сеть они не ходят:
// модель — заглушка на 127.0.0.1.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	agentprofile "daily-agent/agent"
	"daily-agent/internal/agent/stubmodel"
)

type harness struct {
	sup     *Supervisor
	client  *Client
	stub    *stubmodel.Server
	profile Profile
}

func startHarness(t *testing.T) *harness {
	t.Helper()
	bin, err := FindBinary(context.Background(), Candidates(), PinnedVersion, nil)
	if err != nil {
		t.Skipf("нет OpenCode %s: %v", PinnedVersion, err)
	}
	stub, err := stubmodel.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stub.Close() })

	// Короткий путь: macOS ограничивает длину пути к сокетам.
	root, err := os.MkdirTemp("", "da-agent-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	profile := Profile{Root: root}

	model := Model{BaseURL: stub.URL, ModelID: stubmodel.ModelID}
	sup := NewSupervisor(Config{
		Profile: profile,
		Binary:  bin,
		Options: agentprofile.Options{Provider: model.Provider()},
		Logf:    t.Logf,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	sup.Start(context.Background())
	t.Cleanup(sup.Stop)
	if err := sup.WaitReady(ctx); err != nil {
		t.Fatal(err)
	}
	client, err := sup.Client()
	if err != nil {
		t.Fatal(err)
	}
	return &harness{sup: sup, client: client, stub: stub, profile: profile}
}

func (h *harness) ask(t *testing.T, text string) Reply {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	reply, err := h.client.Ask(ctx, SessionOptions{
		Title:     "Проверка",
		Directory: h.profile.Workspace(),
		Model:     &ModelRef{ID: stubmodel.ModelID, ProviderID: ProviderID},
	}, text)
	if err != nil {
		t.Fatalf("Ask: %v (ответ: %+v)", err, reply)
	}
	return reply
}

func TestOpenCodeAnswersThroughStubModel(t *testing.T) {
	h := startHarness(t)
	reply := h.ask(t, "Ответь одним словом: готов.")
	if !strings.Contains(reply.Text, stubmodel.Reply) {
		t.Fatalf("ответ агента: %+v", reply)
	}
	if st := h.sup.Status(); st.State != StateReady || st.Version != PinnedVersion {
		t.Errorf("состояние: %+v", st)
	}
	t.Logf("ответ за %s", reply.Duration)
}

func TestAgentCannotRunShellOrEditFiles(t *testing.T) {
	h := startHarness(t)
	outside := t.TempDir()
	victim := filepath.Join(outside, "существующий.txt")
	if err := os.WriteFile(victim, []byte("исходный текст"), 0o600); err != nil {
		t.Fatal(err)
	}
	created := filepath.Join(outside, "создан-агентом.txt")
	inWorkspace := filepath.Join(h.profile.Workspace(), "заметка.txt")

	args := func(v string) string { return strings.ReplaceAll(v, `\`, `\\`) }
	prompt := strings.Join([]string{
		"Выполни инструменты.",
		fmt.Sprintf(`[stub:call name=shell args={"command":"touch %s"}]`, args(created)),
		fmt.Sprintf(`[stub:call name=bash args={"command":"touch %s"}]`, args(created)),
		fmt.Sprintf(`[stub:call name=write args={"filePath":"%s","content":"агент был здесь"}]`, args(inWorkspace)),
		fmt.Sprintf(`[stub:call name=edit args={"filePath":"%s","oldString":"исходный","newString":"изменённый"}]`, args(victim)),
		`[stub:call name=webfetch args={"url":"https://example.test/"}]`,
	}, "\n")
	reply := h.ask(t, prompt)
	t.Logf("вызовы инструментов: %+v", reply.Tools)

	if _, err := os.Stat(created); err == nil {
		t.Error("агент выполнил команду в терминале: файл создан")
	}
	if _, err := os.Stat(inWorkspace); err == nil {
		t.Error("агент создал файл в рабочей папке")
	}
	if raw, _ := os.ReadFile(victim); string(raw) != "исходный текст" {
		t.Errorf("агент изменил файл: %q", raw)
	}
	for _, call := range reply.Tools {
		if call.Status == "completed" {
			t.Errorf("инструмент %s выполнился", call.Name)
		}
	}
	// OpenCode не предлагает модели ни одного инструмента: агенту всё запрещено.
	for _, req := range h.stub.Requests() {
		if len(req.Tools) > 0 {
			t.Errorf("модели предложены инструменты: %v", req.Tools)
		}
	}
}

// TestPermissiveSessionStillCannotRunTools — второй уровень защиты: даже если
// создать сессию с правами «разрешить всё» в обход клиента приложения,
// команду не выполнить — инструмента терминала нет, его плагин выключен.
func TestPermissiveSessionStillCannotRunTools(t *testing.T) {
	h := startHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	var resp struct {
		Data Session `json:"data"`
	}
	if err := h.client.do(ctx, "POST", "/api/session", map[string]any{
		"agent":       agentprofile.AgentID,
		"location":    map[string]any{"directory": h.profile.Workspace()},
		"model":       ModelRef{ID: stubmodel.ModelID, ProviderID: ProviderID},
		"permissions": []agentprofile.Rule{{Action: "*", Resource: "*", Effect: "allow"}},
	}, &resp); err != nil {
		t.Fatal(err)
	}
	created := filepath.Join(t.TempDir(), "создан-агентом.txt")
	if err := h.client.Prompt(ctx, resp.Data.ID,
		fmt.Sprintf(`[stub:call name=shell args={"command":"touch %s"}]`, created)); err != nil {
		t.Fatal(err)
	}
	if err := h.client.Wait(ctx, resp.Data.ID); err != nil {
		t.Fatal(err)
	}
	msgs, err := h.client.Messages(ctx, resp.Data.ID)
	if err != nil {
		t.Fatal(err)
	}
	reply, _ := ReplyFromMessages(msgs)
	t.Logf("вызовы: %+v", reply.Tools)
	if _, err := os.Stat(created); err == nil {
		t.Fatal("команда выполнилась при разрешающей сессии")
	}
	for _, call := range reply.Tools {
		if call.Status == "completed" {
			t.Errorf("инструмент %s выполнился", call.Name)
		}
	}
}

// TestControlDefaultProfileOffersTools — контроль к тесту выше: тот же
// OpenCode без наших запретов предлагает модели терминал и правку файлов.
// Значит, их убирает именно профиль агента. Команды здесь не выполняются:
// заглушка только отвечает текстом.
func TestControlDefaultProfileOffersTools(t *testing.T) {
	bin, err := FindBinary(context.Background(), Candidates(), PinnedVersion, nil)
	if err != nil {
		t.Skipf("нет OpenCode %s: %v", PinnedVersion, err)
	}
	stub, err := stubmodel.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer stub.Close()
	root, _ := os.MkdirTemp("", "da-control-")
	defer os.RemoveAll(root)
	p := Profile{Root: root}
	if err := p.Prepare(agentprofile.Options{}); err != nil {
		t.Fatal(err)
	}
	// Настройки без запретов: только провайдер-заглушка.
	permissive := fmt.Sprintf(`{
  "update": "disable",
  "plugins": ["-opencode.provider.ollama", "-opencode.provider.lmstudio", "-opencode.provider.vllm"],
  "providers": {"%s": {"package": "@opencode/ai/providers/openai-compatible",
    "settings": {"baseURL": %q}, "models": {"%s": {}}}},
  "model": "%s/%s"
}`, ProviderID, stub.URL, stubmodel.ModelID, ProviderID, stubmodel.ModelID)
	if err := os.WriteFile(p.ConfigFile(), []byte(permissive), 0o600); err != nil {
		t.Fatal(err)
	}
	// Надзор при запуске перепишет настройки, поэтому запускаем процесс напрямую.
	port, _ := freePort()
	password, _ := randomPassword()
	cmd := execCommand(bin, "serve", "--hostname", "127.0.0.1", "--port", fmt.Sprint(port))
	cmd.Dir = p.Workspace()
	cmd.Env = p.Env(password)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	client := NewClient(fmt.Sprintf("http://127.0.0.1:%d", port), password)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	for {
		if _, err := client.Info(ctx); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("сервер не ответил")
		case <-time.After(150 * time.Millisecond):
		}
	}
	// Сессия без агента приложения: используется встроенный агент по умолчанию.
	var resp struct {
		Data Session `json:"data"`
	}
	if err := client.do(ctx, "POST", "/api/session", map[string]any{
		"location": map[string]any{"directory": p.Workspace()},
		"model":    ModelRef{ID: stubmodel.ModelID, ProviderID: ProviderID},
	}, &resp); err != nil {
		t.Fatal(err)
	}
	if err := client.Prompt(ctx, resp.Data.ID, "Привет"); err != nil {
		t.Fatal(err)
	}
	if err := client.Wait(ctx, resp.Data.ID); err != nil {
		t.Fatal(err)
	}
	offered := map[string]bool{}
	for _, req := range stub.Requests() {
		for _, tool := range req.Tools {
			offered[tool] = true
		}
	}
	t.Logf("без запретов модели предложены инструменты: %v", offered)
	if !offered["shell"] && !offered["bash"] {
		t.Error("без запретов ждали инструмент терминала")
	}
	if !offered["edit"] && !offered["write"] {
		t.Error("без запретов ждали инструмент правки файлов")
	}
}

func TestUserOpenCodeInstallUntouched(t *testing.T) {
	home, _ := os.UserHomeDir()
	// Только время изменения: содержимое настроек и базы пользователя не читаем.
	paths := []string{
		filepath.Join(home, ".config", "opencode"),
		filepath.Join(home, ".local", "share", "opencode", "opencode.db"),
	}
	before := map[string]time.Time{}
	for _, p := range paths {
		if info, err := os.Stat(p); err == nil {
			before[p] = info.ModTime()
		}
	}
	h := startHarness(t)
	h.ask(t, "Проверка изоляции.")
	h.sup.Stop()
	for p, mt := range before {
		info, err := os.Stat(p)
		if err != nil || !info.ModTime().Equal(mt) {
			t.Errorf("изменилось %s", strings.Replace(p, home, "~", 1))
		}
	}
	// Настройки и база агента приложения лежат в его собственной папке.
	if _, err := os.Stat(filepath.Join(h.profile.Root, "data", "opencode", "opencode.db")); err != nil {
		t.Errorf("база OpenCode агента не в его папке: %v", err)
	}
}

func execCommand(name string, args ...string) *exec.Cmd { return exec.Command(name, args...) }

// TestProfileHasNoConfigWarnings — OpenCode принимает настройки агента целиком:
// ни одна запись не отброшена как неверная.
func TestProfileHasNoConfigWarnings(t *testing.T) {
	bin, err := FindBinary(context.Background(), Candidates(), PinnedVersion, nil)
	if err != nil {
		t.Skipf("нет OpenCode %s: %v", PinnedVersion, err)
	}
	root, _ := os.MkdirTemp("", "da-cfg-")
	defer os.RemoveAll(root)
	p := Profile{Root: root}
	model := Model{BaseURL: "http://127.0.0.1:9/v1", ModelID: "m", APIKey: "x"}
	// MCP-сервер в том виде, как его пишет приложение: Code Mode выключен,
	// у локального — время на запуск.
	opts := agentprofile.Options{Provider: model.Provider(), AllowedTools: []string{"tracker_get_issue"},
		MCP: map[string]map[string]any{
			"tracker": {"type": "local", "command": []string{"/usr/bin/false"}, "disabled": true, "timeout": map[string]any{"startup": 120000}},
			"repo":    {"type": "remote", "url": "http://127.0.0.1:9/mcp", "oauth": false, "disabled": true, "headers": map[string]any{"Authorization": "Bearer {env:DAILY_AGENT_MCP_REPO}"}},
		}}
	if err := p.Prepare(opts); err != nil {
		t.Fatal(err)
	}
	port, _ := freePort()
	password, _ := randomPassword()
	cmd := execCommand(bin, "serve", "--hostname", "127.0.0.1", "--port", fmt.Sprint(port), "--print-logs", "--log-level", "warn")
	cmd.Dir = p.Workspace()
	cmd.Env = p.Env(password, model.Env()...)
	var logs strings.Builder
	cmd.Stdout, cmd.Stderr = &logs, &logs
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	client := NewClient(fmt.Sprintf("http://127.0.0.1:%d", port), password)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for {
		if _, err := client.Info(ctx); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("сервер не ответил")
		case <-time.After(150 * time.Millisecond):
		}
	}
	// Настройки читаются лениво: запрос агентов заставляет их загрузить.
	var agents json.RawMessage
	if err := client.do(ctx, "GET", "/api/agent", nil, &agents); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	for _, line := range strings.Split(logs.String(), "\n") {
		if strings.Contains(line, "configuration") || strings.Contains(line, "invalid") {
			t.Errorf("OpenCode предупреждает о настройках: %s", line)
		}
	}
}
