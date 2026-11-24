package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"daily-agent/internal/agent"
)

// fakeMCPAgent — агент с заранее известными MCP-серверами и инструментами.
type fakeMCPAgent struct {
	fakeControl
	states []agent.MCPState
	logins []string
}

func (f *fakeMCPAgent) MCP() []agent.MCPState { return f.states }
func (f *fakeMCPAgent) RefreshMCP(context.Context) error {
	return nil
}
func (f *fakeMCPAgent) LoginMCP(_ context.Context, name string) (string, error) {
	f.logins = append(f.logins, name)
	return "http://127.0.0.1:9/authorize?server=" + name, nil
}

// AllowedTools повторяет правило Manager: выбранные или инструменты чтения.
func (f *fakeMCPAgent) AllowedTools() []string {
	var out []string
	s := f.last()
	for _, st := range f.states {
		if st.Status != "connected" {
			continue
		}
		var explicit []string
		custom := false
		if s != nil {
			explicit, custom = s.MCPTools[st.Name]
		}
		for _, t := range st.Tools {
			short := agent.ShortToolName(st.Name, t.Name)
			if agent.ToolIsWrite(short) {
				continue
			}
			if custom && contains(explicit, t.Name) || !custom && agent.ToolIsRead(short) {
				out = append(out, t.Name)
			}
		}
	}
	return out
}

func trackerTools(server string) []agent.MCPTool {
	return []agent.MCPTool{
		{Name: server + "_create_issue", Description: "Создать задачу"},
		{Name: server + "_get_issue", Description: "Прочитать задачу"},
		{Name: server + "_list_issues", Description: "Список задач"},
	}
}

func testMCPConfig(t *testing.T) agent.UserConfig {
	t.Helper()
	path := filepath.Join(t.TempDir(), "opencode.json")
	os.WriteFile(path, []byte(`{"mcp": {"servers": {
		"Трекер": {"type": "remote", "url": "https://tracker.example.test/mcp", "oauth": false,
			"headers": {"Authorization": "Bearer test-secret-tracker"}},
		"repo": {"type": "local", "command": ["repo-mcp"]}
	}}}`), 0o600)
	cfg, err := agent.ReadUserConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestMCPService(t *testing.T) {
	h := newSettingsHarness(t, false, agent.UserConfig{})
	user := testMCPConfig(t)
	h.svc.userConfig = func() (agent.UserConfig, error) { return user, nil }
	ag := &fakeMCPAgent{fakeControl: *h.control}
	ag.states = []agent.MCPState{
		{Name: "repo", Status: "connected", Tools: trackerTools("repo")},
		{Name: "treker", Status: "failed", Error: "401"},
	}
	h.svc.agent = ag
	var opened []string
	svc := NewMCPService(h.svc, ag, func(u string) error { opened = append(opened, u); return nil }, []string{"/Applications/Daily Agent.app/Contents/MacOS/daily-agent", "mcp"})
	ctx := context.Background()

	list, err := svc.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Name != "repo" || list[1].Name != "treker" || list[1].Title != "Трекер" {
		t.Fatalf("серверы: %+v", list)
	}
	repo, tracker := list[0], list[1]
	if repo.Status != "connected" || repo.Address != "repo-mcp" || len(repo.Tools) != 3 {
		t.Fatalf("репозиторий: %+v", repo)
	}
	var allowed, write []string
	for _, tool := range repo.Tools {
		if tool.Allowed {
			allowed = append(allowed, tool.Short)
		}
		if tool.Write {
			write = append(write, tool.Short)
		}
	}
	if strings.Join(allowed, ",") != "get_issue,list_issues" || strings.Join(write, ",") != "create_issue" {
		t.Fatalf("инструменты: разрешены %v, запись %v", allowed, write)
	}
	// Токен трекера не перенесён: сервер ждёт его и агенту недоступен.
	if tracker.Status != "needs_token" || len(tracker.Tokens) != 1 || tracker.Tokens[0].Saved || tracker.Auth != "token" {
		t.Fatalf("трекер без токена: %+v", tracker)
	}

	// Токен есть в настройках OpenCode: его можно перенести одной кнопкой.
	if !tracker.TokensInConfig {
		t.Fatalf("токен в настройках OpenCode: %+v", tracker)
	}
	if _, err := svc.ImportTokens(ctx, "repo"); err == nil {
		t.Fatal("у репозитория нет токенов в настройках")
	}
	list, err = svc.ImportTokens(ctx, "treker")
	if err != nil {
		t.Fatal(err)
	}
	if h.secrets.key("mcp:treker:header:Authorization") != "test-secret-tracker" || list[1].Status == "needs_token" || list[1].TokensInConfig {
		t.Fatalf("после переноса: %+v", list[1])
	}
	if !contains(ag.last().Env, "DAILY_AGENT_MCP_TREKER_HEADER_AUTHORIZATION=test-secret-tracker") {
		t.Fatalf("токен в окружении OpenCode: %v", ag.last().Env)
	}

	// Новый токен вручную — в Связку ключей, OpenCode перезапускается с ним.
	if _, err := svc.SetToken(ctx, "treker", "header", "Authorization", "test-token-tracker"); err != nil {
		t.Fatal(err)
	}
	s := ag.last()
	if s.Options.MCP["treker"]["disabled"] == true || !contains(s.Env, "DAILY_AGENT_MCP_TREKER_HEADER_AUTHORIZATION=test-token-tracker") {
		t.Fatalf("после токена: %+v / %v", s.Options.MCP["treker"], s.Env)
	}
	if h := s.Options.MCP["treker"]["headers"].(map[string]any); h["Authorization"] != "Bearer {env:DAILY_AGENT_MCP_TREKER_HEADER_AUTHORIZATION}" {
		t.Fatalf("заголовок трекера: %v", h)
	}
	if !contains(s.Options.AllowedTools, "treker_get*") {
		t.Fatalf("шаблоны чтения: %v", s.Options.AllowedTools)
	}

	// Выбор инструментов вручную; запись выбрать нельзя.
	if _, err := svc.SetTools(ctx, "repo", []string{"repo_create_issue"}, false); err == nil || !strings.Contains(err.Error(), "запись") {
		t.Fatalf("запись: %v", err)
	}
	if _, err := svc.SetTools(ctx, "repo", []string{"repo_unknown"}, false); err == nil {
		t.Fatal("неизвестный инструмент")
	}
	list, err = svc.SetTools(ctx, "repo", []string{"repo_get_issue"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !list[0].Custom || strings.Join(ag.last().MCPTools["repo"], ",") != "repo_get_issue" || strings.Join(ag.AllowedTools(), ",") != "repo_get_issue" {
		t.Fatalf("выбор вручную: %+v / %v", list[0], ag.last().MCPTools)
	}
	if _, err := svc.SetTools(ctx, "repo", nil, true); err != nil || ag.last().MCPTools["repo"] != nil {
		t.Fatalf("сброс выбора: %v %v", err, ag.last().MCPTools)
	}

	// Выключенный сервер в профиле выключен и инструментов не даёт.
	list, err = svc.SetEnabled(ctx, "repo", false)
	if err != nil {
		t.Fatal(err)
	}
	if list[0].Status != "disabled" || ag.last().Options.MCP["repo"]["disabled"] != true || contains(ag.last().Options.AllowedTools, "repo_get*") {
		t.Fatalf("выключенный сервер: %+v / %v", list[0], ag.last().Options.MCP["repo"])
	}
	if _, err := svc.Remove(ctx, "repo"); err == nil {
		t.Fatal("сервер из настроек OpenCode здесь не удаляется")
	}

	// Свой сервер с токеном.
	if _, err := svc.Add(ctx, MCPServerInput{Title: "Репозиторий", Type: "remote", URL: "ftp://x", Auth: "none"}); err == nil {
		t.Fatal("адрес ftp")
	}
	if _, err := svc.Add(ctx, MCPServerInput{Title: "repo", Type: "local", Command: "x"}); err == nil {
		t.Fatal("имя занято сервером из настроек OpenCode")
	}
	if _, err := svc.Add(ctx, MCPServerInput{Title: "GitLab", Type: "remote", URL: "https://gitlab.example.test/api/mcp", Auth: "token", Token: "test-token-gitlab"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Add(ctx, MCPServerInput{Title: "Календарь", Type: "local", Command: "cal-mcp --stdio"}); err != nil {
		t.Fatal(err)
	}
	s = ag.last()
	gl := s.Options.MCP["gitlab"]
	if gl["url"] != "https://gitlab.example.test/api/mcp" || gl["oauth"] != false || !contains(s.Env, "DAILY_AGENT_MCP_GITLAB_HEADER_AUTHORIZATION=test-token-gitlab") {
		t.Fatalf("свой сервер: %+v", gl)
	}
	if cal := s.Options.MCP["kalendar"]; strings.Join(cal["command"].([]string), " ") != "cal-mcp --stdio" {
		t.Fatalf("локальный сервер: %+v", cal)
	}

	// Имя daily занято своим сервером приложения.
	if _, err := svc.Add(ctx, MCPServerInput{Title: "Daily", Type: "local", Command: "x"}); err == nil {
		t.Fatal("имя daily занято")
	}
	// Как подключить свой сервер к внешнему агенту.
	ext := svc.ExternalAgent()
	if !strings.Contains(ext.OpenCode, `"daily-agent"`) || !strings.Contains(ext.OpenCode, `"mcp"`) ||
		ext.Claude != "claude mcp add daily-agent -- '/Applications/Daily Agent.app/Contents/MacOS/daily-agent' mcp" {
		t.Fatalf("внешний агент: %+v", ext)
	}

	// Вход через браузер.
	if err := svc.Login(ctx, "gitlab"); err != nil || len(opened) != 1 || ag.logins[0] != "gitlab" {
		t.Fatalf("вход: %v %v", err, opened)
	}

	if _, err := svc.Remove(ctx, "gitlab"); err != nil {
		t.Fatal(err)
	}
	if _, ok := ag.last().Options.MCP["gitlab"]; ok || h.secrets.key("mcp:gitlab:header:Authorization") != "" {
		t.Fatal("удалённый сервер остался")
	}

	// Секретов нет ни в базе, ни в описаниях для профиля.
	all, _ := h.store.Settings(ctx, "")
	rows, _ := h.store.MCPServers(ctx)
	raw, _ := json.Marshal([]any{all, rows, ag.last().Options})
	for _, secret := range []string{"test-secret-tracker", "test-token-tracker", "test-token-gitlab"} {
		if strings.Contains(string(raw), secret) {
			t.Errorf("секрет %s попал в базу или профиль", secret)
		}
	}
}
