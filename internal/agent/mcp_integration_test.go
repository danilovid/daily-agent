//go:build integration

package agent

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"daily-agent/internal/agent/mcpstub"
	"daily-agent/internal/agent/stubmodel"
)

// waitAgent ждёт, пока агент выйдет на связь, включая прогрев MCP.
func waitAgent(t *testing.T, m *Manager) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for m.Status().State != StateReady {
		if time.Now().After(deadline) {
			t.Fatalf("агент не вышел на связь: %+v", m.Status())
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func mcpState(m *Manager, name string) MCPState {
	for _, s := range m.MCP() {
		if s.Name == name {
			return s
		}
	}
	return MCPState{}
}

func toolNames(tools []MCPTool) string {
	var out []string
	for _, t := range tools {
		out = append(out, t.Name)
	}
	return strings.Join(out, ",")
}

func TestLiveMCPTools(t *testing.T) {
	bin, err := FindBinary(context.Background(), Candidates(), PinnedVersion, nil)
	if err != nil {
		t.Skipf("нет OpenCode %s: %v", PinnedVersion, err)
	}
	model, err := stubmodel.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer model.Close()
	remote, _ := mcpstub.StartHTTP(false)
	defer remote.Close()
	secured, _ := mcpstub.StartHTTP(true)
	defer secured.Close()

	tmp, _ := os.MkdirTemp("", "da-mcp-")
	defer os.RemoveAll(tmp)
	root, _ := filepath.EvalSymlinks(tmp)
	logPath := filepath.Join(root, "local-mcp.log")

	setup := Model{BaseURL: model.URL, ModelID: stubmodel.ModelID}.Setup()
	setup.Options.MCP = map[string]map[string]any{
		"local": {"type": "local", "command": []string{os.Args[0]},
			"environment": map[string]any{fakeMCPEnv: logPath, "DA_FAKE_MCP_ECHO": "TRACKER_TOKEN", "TRACKER_TOKEN": "{env:DAILY_AGENT_MCP_LOCAL_TOKEN}"}},
		"remote":  {"type": "remote", "url": remote.URL, "oauth": false, "headers": map[string]any{"Authorization": "Bearer {env:DAILY_AGENT_MCP_REMOTE_TOKEN}"}},
		"secured": {"type": "remote", "url": secured.URL},
	}
	setup.Options.AllowedTools = append(append(ReadPatterns("local"), ReadPatterns("remote")...), ReadPatterns("secured")...)
	// У удалённого сервера инструменты выбраны вручную: запись не пройдёт и так.
	setup.MCPTools = map[string][]string{"remote": {"remote_get_issue", "remote_create_issue"}}
	setup.Env = append(setup.Env, "DAILY_AGENT_MCP_LOCAL_TOKEN=test-token-local", "DAILY_AGENT_MCP_REMOTE_TOKEN=test-token-remote")

	m := NewManager(Config{Profile: Profile{Root: root}, Binary: bin, Logf: t.Logf}, setup)
	defer m.Stop()
	m.Start(context.Background())
	waitAgent(t, m)

	local, rem, sec := mcpState(m, "local"), mcpState(m, "remote"), mcpState(m, "secured")
	if local.Status != "connected" || toolNames(local.Tools) != "local_create_issue,local_get_issue,local_issue_details,local_list_issues" || local.Tools[1].Description == "" {
		t.Fatalf("локальный сервер: %+v", local)
	}
	if rem.Status != "connected" || len(rem.Tools) != 4 {
		t.Fatalf("удалённый сервер: %+v", rem)
	}
	if sec.Status != "needs_auth" || sec.IntegrationID == "" || len(sec.Tools) != 0 {
		t.Fatalf("сервер со входом: %+v", sec)
	}
	// Токены дошли до серверов из окружения OpenCode, а не из файла настроек.
	if raw, _ := os.ReadFile(logPath); !strings.Contains(string(raw), "env TRACKER_TOKEN=test-token-local") {
		t.Fatalf("журнал локального сервера: %s", raw)
	}
	if h := remote.Headers(); len(h) == 0 || h[0].Get("Authorization") != "Bearer test-token-remote" {
		t.Fatalf("заголовок удалённого сервера: %v", h)
	}
	if cfg, _ := os.ReadFile(filepath.Join(root, "config", "opencode", "opencode.json")); strings.Contains(string(cfg), "test-token") {
		t.Fatal("токен попал в настройки OpenCode")
	}

	// Сессия агента: предложены только разрешённые инструменты, без execute.
	c, _ := m.Client()
	ask := func(text string) (Reply, []string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		before := len(model.Requests())
		reply, _ := c.Ask(ctx, SessionOptions{Directory: m.Workspace(), Model: m.Model(), Allow: m.AllowedTools()}, text)
		var offered []string
		for _, r := range model.Requests()[before:] {
			if len(r.Tools) > len(offered) {
				offered = r.Tools
			}
		}
		return reply, offered
	}
	reply, offered := ask(`[stub:call name=local_get_issue args={"id":"TEST-1"}]`)
	if strings.Join(offered, ",") != "local_get_issue,local_issue_details,local_list_issues,remote_get_issue" {
		t.Fatalf("предложены инструменты: %v", offered)
	}
	// Инструмент с именем «сначала существительное» не подходит под шаблоны
	// профиля (local_get* и подобные): его открывают права сессии.
	reply, _ = ask(`[stub:call name=local_issue_details args={"id":"TEST-1"}]`)
	if len(reply.Tools) != 1 || reply.Tools[0].Status != "completed" {
		t.Fatalf("issue_details: %+v", reply.Tools)
	}
	if len(reply.Tools) != 1 || reply.Tools[0].Status != "completed" {
		t.Fatalf("разрешённый вызов: %+v", reply.Tools)
	}
	reply, _ = ask(`[stub:call name=local_create_issue args={"title":"x"}]`)
	if len(reply.Tools) != 1 || reply.Tools[0].Status != "error" {
		t.Fatalf("запрещённый вызов: %+v", reply.Tools)
	}
	reply, _ = ask(`[stub:call name=execute args={"code":"return 1"}]`)
	if len(reply.Tools) != 1 || reply.Tools[0].Status != "error" {
		t.Fatalf("выполнение кода: %+v", reply.Tools)
	}
	raw, _ := os.ReadFile(logPath)
	if !strings.Contains(string(raw), "call get_issue") || !strings.Contains(string(raw), "call issue_details") || strings.Contains(string(raw), "call create_issue") {
		t.Fatalf("вызовы локального сервера: %s", raw)
	}

	// Вход через браузер: «браузер» идёт по адресу, сервер авторизации сразу
	// возвращает код в OpenCode.
	url, err := m.LoginMCP(context.Background(), "secured")
	if err != nil {
		t.Fatal(err)
	}
	if resp, err := http.Get(url); err == nil {
		resp.Body.Close()
	} else {
		t.Fatal(err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for mcpState(m, "secured").Status != "connected" || len(mcpState(m, "secured").Tools) == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("после входа: %+v", mcpState(m, "secured"))
		}
		time.Sleep(200 * time.Millisecond)
	}
	if _, offered := ask("привет"); !strings.Contains(strings.Join(offered, ","), "secured_get_issue") {
		t.Fatalf("после входа предложены: %v", offered)
	}

	// Выключенный сервер агенту недоступен.
	next := *setup
	next.Options.MCP = map[string]map[string]any{}
	for k, v := range setup.Options.MCP {
		next.Options.MCP[k] = v
	}
	next.Options.MCP["remote"] = map[string]any{"type": "remote", "url": remote.URL, "oauth": false, "disabled": true}
	m.SetSetup(&next)
	waitAgent(t, m)
	c, _ = m.Client()
	if st := mcpState(m, "remote"); st.Status != "disabled" || len(st.Tools) != 0 {
		t.Fatalf("выключенный сервер: %+v", st)
	}
	if _, offered := ask("привет"); strings.Contains(strings.Join(offered, ","), "remote_") {
		t.Fatalf("выключенный сервер предложен: %v", offered)
	}
}

// Локальный сервер запускается по имени команды, как «npx …»: он находится,
// только если его папка есть в PATH профиля. Из Finder приложение получает
// урезанный PATH, поэтому PATH берётся из оболочки входа.
func TestLiveMCPLocalCommandFromPath(t *testing.T) {
	bin, err := FindBinary(context.Background(), Candidates(), PinnedVersion, nil)
	if err != nil {
		t.Skipf("нет OpenCode %s: %v", PinnedVersion, err)
	}
	model, _ := stubmodel.Start()
	defer model.Close()
	tmp, _ := os.MkdirTemp("", "da-path-")
	defer os.RemoveAll(tmp)
	root, _ := filepath.EvalSymlinks(tmp)
	tools := filepath.Join(root, "tools")
	os.MkdirAll(tools, 0o700)
	self, _ := os.ReadFile(os.Args[0])
	os.WriteFile(filepath.Join(tools, "fake-mcp"), self, 0o700)

	run := func(path string) MCPState {
		setup := Model{BaseURL: model.URL, ModelID: stubmodel.ModelID}.Setup()
		setup.Options.MCP = map[string]map[string]any{
			"local": {"type": "local", "command": []string{"fake-mcp"}, "timeout": map[string]any{"startup": 120000},
				"environment": map[string]any{fakeMCPEnv: filepath.Join(root, "mcp.log")}},
		}
		m := NewManager(Config{Profile: Profile{Root: filepath.Join(root, "profile"), Path: path}, Binary: bin, Logf: t.Logf}, setup)
		defer m.Stop()
		m.Start(context.Background())
		waitAgent(t, m)
		return mcpState(m, "local")
	}
	if st := run(""); st.Status == "connected" {
		t.Fatalf("без папки в PATH сервер не должен находиться: %+v", st)
	}
	if st := run(tools + ":/usr/bin:/bin"); st.Status != "connected" || len(st.Tools) != 4 {
		t.Fatalf("с папкой в PATH: %+v", st)
	}
}
