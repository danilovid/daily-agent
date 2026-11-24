package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStripJSONC(t *testing.T) {
	src := `{
  // комментарий
  "a": "строка с // не комментарием и /* тоже */", /* блок */
  "b": [1, 2, /* x */ ],
  "c": {"d": "\"кавычка\", ", },
}`
	var v map[string]any
	if err := json.Unmarshal(StripJSONC([]byte(src)), &v); err != nil {
		t.Fatalf("%v\n%s", err, StripJSONC([]byte(src)))
	}
	if v["a"] != "строка с // не комментарием и /* тоже */" || len(v["b"].([]any)) != 2 || v["c"].(map[string]any)["d"] != `"кавычка", ` {
		t.Fatalf("разбор: %+v", v)
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestReadUserConfig(t *testing.T) {
	dir := t.TempDir()
	// Первая версия: раздел provider. Вымышленные адреса и ключи.
	writeFile(t, filepath.Join(dir, "opencode.json"), `{
  "provider": {
    "old-lab": {
      "npm": "@ai-sdk/openai-compatible",
      "name": "Старая лаборатория",
      "options": {"baseURL": "http://10.0.0.5:8000/v1", "apiKey": "test-secret-1", "headers": {"X-Token": "test-secret-2"}},
      "models": {"m1": {"name": "Модель 1"}}
    },
    "special": {"npm": "@ai-sdk/anthropic", "options": {"baseURL": "https://proxy.example.test"}},
    "lab": {"npm": "@ai-sdk/openai-compatible", "options": {"baseURL": "http://old.example.test/v1"}}
  },
  "mcp": {"tracker": {"type": "remote", "url": "https://tracker.example.test/mcp"}},
  "plugins": ["something"]
}`)
	// Вторая версия: раздел providers, с комментариями.
	writeFile(t, filepath.Join(dir, "opencode.jsonc"), `{
  // свои модели
  "providers": {
    "lab": {
      "name": "Лаборатория",
      "env": ["LAB_API_KEY"],
      "package": "@opencode/ai/providers/openai-compatible",
      "settings": {"baseURL": "https://llm.lab.example.test/v1", "apiKey": "test-secret-3", "timeout": 30},
      "headers": {"Authorization": "Bearer test-secret-4", "X-Team": "{env:LAB_TEAM}"},
      "models": {"big": {"name": "Big"}, "small": {}},
    },
    "router": {
      "package": "@opencode/ai/providers/openai-compatible",
      "settings": {"baseURL": "https://openrouter.ai/api/v1"},
      "models": {"some/model": {}}
    },
    "anthropic": {"settings": {"apiKey": "{env:MY_ANTHROPIC}"}},
    "bad id!": {},
  },
}`)
	cfg, err := ReadUserConfig(filepath.Join(dir, "opencode.json"), filepath.Join(dir, "opencode.jsonc"), filepath.Join(dir, "missing.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Paths) != 2 || cfg.Skipped != 2 {
		t.Fatalf("файлы %v, пропущено %d", cfg.Paths, cfg.Skipped)
	}
	var ids []string
	for _, p := range cfg.Providers {
		ids = append(ids, p.ID)
	}
	if strings.Join(ids, ",") != "anthropic,lab,old-lab,router" {
		t.Fatalf("провайдеры: %v", ids)
	}

	lab, _ := cfg.Provider("lab")
	if lab.Name != "Лаборатория" || !lab.Selfhost || lab.BaseURL != "https://llm.lab.example.test/v1" || lab.Dropped != 2 {
		t.Fatalf("lab: %+v", lab)
	}
	if strings.Join(lab.Models, ",") != "big,small" || lab.KeyEnv != "DAILY_AGENT_KEY_LAB" {
		t.Fatalf("lab: модели %v, ключ %s", lab.Models, lab.KeyEnv)
	}
	env := lab.Definition["env"].([]any)
	if len(env) != 2 || env[0] != "DAILY_AGENT_KEY_LAB" || env[1] != "LAB_API_KEY" {
		t.Fatalf("lab env: %v", env)
	}
	if h := lab.Definition["headers"].(map[string]any); len(h) != 1 || h["X-Team"] != "{env:LAB_TEAM}" {
		t.Fatalf("lab заголовки: %v", h)
	}

	old, _ := cfg.Provider("old-lab")
	if old.Name != "Старая лаборатория" || !old.Selfhost || old.Dropped != 2 || old.Definition["package"] != "@opencode/ai/providers/openai-compatible" {
		t.Fatalf("old-lab: %+v", old)
	}
	if router, _ := cfg.Provider("router"); router.Selfhost {
		t.Fatal("openrouter.ai — облако")
	}
	if a, _ := cfg.Provider("anthropic"); a.Selfhost || a.BaseURL != "" || a.Dropped != 0 {
		t.Fatalf("anthropic: %+v", a)
	}

	// Ни один секрет не попал в описания для профиля.
	raw, _ := json.Marshal(cfg.Providers)
	for _, secret := range []string{"test-secret-1", "test-secret-2", "test-secret-3", "test-secret-4", "tracker", "something"} {
		if strings.Contains(string(raw), secret) {
			t.Errorf("в профиль попало %q", secret)
		}
	}
}

func TestUserConfigFiles(t *testing.T) {
	t.Setenv(EnvUserConfig, "off")
	if files := UserConfigFiles(); len(files) != 0 {
		t.Fatalf("выключено: %v", files)
	}
	dir := t.TempDir()
	t.Setenv(EnvUserConfig, dir)
	if files := UserConfigFiles(); len(files) != 2 || files[0] != filepath.Join(dir, "opencode.json") {
		t.Fatalf("папка: %v", files)
	}
	file := filepath.Join(dir, "custom.json")
	writeFile(t, file, `{}`)
	t.Setenv(EnvUserConfig, file)
	if files := UserConfigFiles(); len(files) != 1 || files[0] != file {
		t.Fatalf("файл: %v", files)
	}
	t.Setenv(EnvUserConfig, "")
	t.Setenv("XDG_CONFIG_HOME", dir)
	if files := UserConfigFiles(); files[0] != filepath.Join(dir, "opencode", "opencode.json") {
		t.Fatalf("XDG: %v", files)
	}
	if got := ProviderKeyEnv("my.lab-1"); got != "DAILY_AGENT_KEY_MY_LAB_1" {
		t.Fatalf("переменная ключа: %s", got)
	}
}

func TestReadUserMCP(t *testing.T) {
	dir := t.TempDir()
	// Первая версия: mcp.<имя>, enabled. Вымышленные адреса и токены.
	writeFile(t, filepath.Join(dir, "opencode.json"), `{"mcp": {
		"Tracker-Prod": {"type": "remote", "url": "https://tracker.example.test/mcp",
			"headers": {"Authorization": "Bearer test-secret-1", "X-Team": "{env:TEAM}"}, "oauth": false},
		"old-repo": {"type": "local", "command": ["repo-mcp", "--stdio"], "enabled": false,
			"environment": {"REPO_TOKEN": "test-secret-2", "REPO_URL": "https://repo.example.test"}},
		"broken": {"type": "remote", "url": "ftp://x"}
	}}`)
	// Вторая версия: mcp.servers, disabled; заменяет одноимённый сервер.
	writeFile(t, filepath.Join(dir, "opencode.jsonc"), `{"mcp": {"servers": {
		"Календарь": {"type": "local", "command": ["cal-mcp"], "disabled": true},
		"docs": {"type": "remote", "url": "https://docs.example.test/mcp", "oauth": {"clientId": "test-client", "clientSecret": "test-secret-3"}},
	}}}`)
	cfg, err := ReadUserConfig(filepath.Join(dir, "opencode.json"), filepath.Join(dir, "opencode.jsonc"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, s := range cfg.MCP {
		names = append(names, s.Name)
	}
	if strings.Join(names, ",") != "docs,kalendar,old_repo,tracker_prod" || cfg.MCPSkipped != 1 {
		t.Fatalf("серверы: %v, пропущено %d", names, cfg.MCPSkipped)
	}

	tr, _ := cfg.MCPServer("tracker_prod")
	if tr.Title != "Tracker-Prod" || tr.OAuth || tr.Disabled || len(tr.Tokens) != 1 {
		t.Fatalf("трекер: %+v", tr)
	}
	tok := tr.Tokens[0]
	if tok.Kind != "header" || tok.Key != "Authorization" || tok.Prefix != "Bearer " || tok.Env != "DAILY_AGENT_MCP_TRACKER_PROD_HEADER_AUTHORIZATION" {
		t.Fatalf("токен трекера: %+v", tok)
	}
	h := tr.Definition["headers"].(map[string]any)
	if h["Authorization"] != "Bearer {env:"+tok.Env+"}" || h["X-Team"] != "{env:TEAM}" || tr.Definition["oauth"] != false {
		t.Fatalf("заголовки трекера: %+v", tr.Definition)
	}
	if tok.Account("tracker_prod") != "mcp:tracker_prod:header:Authorization" {
		t.Fatalf("запись Связки ключей: %s", tok.Account("tracker_prod"))
	}

	repo, _ := cfg.MCPServer("old_repo")
	env := repo.Definition["environment"].(map[string]any)
	if !repo.Disabled || len(repo.Tokens) != 1 || env["REPO_URL"] != "https://repo.example.test" || !strings.HasPrefix(env["REPO_TOKEN"].(string), "{env:DAILY_AGENT_MCP_OLD_REPO_ENV_") {
		t.Fatalf("репозиторий: %+v", repo)
	}
	cal, _ := cfg.MCPServer("kalendar")
	if cal.Title != "Календарь" || !cal.Disabled || cal.Type != "local" {
		t.Fatalf("календарь: %+v", cal)
	}
	docs, _ := cfg.MCPServer("docs")
	if !docs.OAuth || docs.Definition["oauth"].(map[string]any)["client_id"] != "test-client" {
		t.Fatalf("документы: %+v", docs)
	}

	raw, _ := json.Marshal(cfg.MCP)
	for _, secret := range []string{"test-secret-1", "test-secret-2", "test-secret-3"} {
		if strings.Contains(string(raw), secret) {
			t.Errorf("в профиль попало %q", secret)
		}
	}
	for in, want := range map[string]string{"Трекер задач": "treker_zadach", "GitLab": "gitlab", "my-mcp.server": "my_mcp_server", "!!!": ""} {
		if got := MCPName(in); got != want {
			t.Errorf("имя %q → %q, ждали %q", in, got, want)
		}
	}
}
