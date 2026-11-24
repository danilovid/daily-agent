package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	agentprofile "daily-agent/agent"
)

// EnvUserConfig — где искать настройки OpenCode пользователя: папка или файл.
// Пусто — $XDG_CONFIG_HOME/opencode или ~/.config/opencode. «off» — не читать.
// Нужна для разработки и тестов, чтобы не трогать настоящие настройки.
const EnvUserConfig = "DAILY_AGENT_OPENCODE_CONFIG"

// UserProvider — провайдер из настроек OpenCode пользователя, готовый для
// профиля приложения. Секреты из настроек в него не попадают.
type UserProvider struct {
	ID   string
	Name string
	// BaseURL — адрес API. Пусто — провайдер каталога со своим адресом.
	BaseURL string
	// Selfhost — адрес задан и это не известный облачный сервис.
	Selfhost bool
	Models   []string
	// KeyEnv — переменная, через которую OpenCode получит ключ из Связки ключей.
	KeyEnv string
	// Dropped — сколько полей с секретами не перенесено: ключи, токены,
	// заголовки авторизации. Ключ тогда нужно сохранить в приложении.
	Dropped int
	// Definition — описание для раздела providers профиля.
	Definition map[string]any
}

// UserConfig — провайдеры из настроек OpenCode пользователя.
type UserConfig struct {
	// Paths — прочитанные файлы. Пусто — настроек нет.
	Paths     []string
	Providers []UserProvider
	// Skipped — провайдеры, которые не удалось перенести: например,
	// провайдеры первой версии OpenCode с особым пакетом.
	Skipped int
	// MCP — MCP-серверы без секретов, по имени для агента.
	MCP []UserMCPServer
	// MCPSkipped — MCP-серверы, которые не удалось перенести.
	MCPSkipped int
	// RefEnv — значения переменных, на которые ссылаются описания ({env:ИМЯ}),
	// из оболочки входа пользователя: ИМЯ=значение для процесса OpenCode.
	RefEnv []string
}

// MCPServer находит MCP-сервер по имени для агента.
func (c UserConfig) MCPServer(name string) (UserMCPServer, bool) {
	for _, s := range c.MCP {
		if s.Name == name {
			return s, true
		}
	}
	return UserMCPServer{}, false
}

// Provider находит провайдера по идентификатору.
func (c UserConfig) Provider(id string) (UserProvider, bool) {
	for _, p := range c.Providers {
		if p.ID == id {
			return p, true
		}
	}
	return UserProvider{}, false
}

// UserConfigFiles — файлы настроек OpenCode пользователя, в порядке чтения.
// Пустой список — чтение выключено.
func UserConfigFiles() []string {
	if v := strings.TrimSpace(os.Getenv(EnvUserConfig)); v != "" {
		if v == "off" {
			return nil
		}
		if st, err := os.Stat(v); err == nil && !st.IsDir() {
			return []string{v}
		}
		return []string{filepath.Join(v, "opencode.json"), filepath.Join(v, "opencode.jsonc")}
	}
	dir := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil
		}
		dir = filepath.Join(home, ".config")
	}
	dir = filepath.Join(dir, "opencode")
	return []string{filepath.Join(dir, "opencode.json"), filepath.Join(dir, "opencode.jsonc")}
}

// LoadUserConfig читает провайдеров из настроек OpenCode пользователя.
// Берутся только провайдеры моделей и MCP-серверы, без секретов. Плагины,
// агенты и права пользователя в профиль приложения не попадают. Переменные,
// на которые ссылаются описания, берутся из оболочки входа: так их видит и
// OpenCode пользователя, запущенный из терминала.
func LoadUserConfig() (UserConfig, error) {
	cfg, err := ReadUserConfig(UserConfigFiles()...)
	if err != nil {
		return cfg, err
	}
	cfg.RefEnv = refEnv(cfg.EnvRefs(), ShellEnv())
	return cfg, nil
}

// EnvRefs — имена переменных из ссылок {env:ИМЯ} в описаниях провайдеров и
// MCP-серверов. Свои переменные приложения не в счёт.
func (c UserConfig) EnvRefs() []string {
	seen := map[string]bool{}
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case string:
			for _, m := range envRef.FindAllStringSubmatch(x, -1) {
				if !strings.HasPrefix(m[1], "DAILY_AGENT_") {
					seen[m[1]] = true
				}
			}
		case map[string]any:
			for _, y := range x {
				walk(y)
			}
		case []any:
			for _, y := range x {
				walk(y)
			}
		case []string:
			for _, y := range x {
				walk(y)
			}
		}
	}
	for _, p := range c.Providers {
		walk(p.Definition)
	}
	for _, s := range c.MCP {
		walk(s.Definition)
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

var envRef = regexp.MustCompile(`\{env:([A-Za-z_][A-Za-z0-9_]*)\}`)

// refEnv берёт из окружения оболочки значения только нужных переменных.
func refEnv(names []string, shell map[string]string) []string {
	var out []string
	for _, name := range names {
		if v, ok := shell[name]; ok && v != "" {
			out = append(out, name+"="+v)
		}
	}
	return out
}

// ReadUserConfig читает провайдеров из файлов. Нет файла — не ошибка.
// Провайдер из более позднего файла заменяет одноимённого из раннего.
func ReadUserConfig(paths ...string) (UserConfig, error) {
	var cfg UserConfig
	found := map[string]UserProvider{}
	var order []string
	mcp := map[string]UserMCPServer{}
	var mcpOrder []string
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return cfg, fmt.Errorf("настройки OpenCode: %w", err)
		}
		var doc map[string]any
		if err := json.Unmarshal(StripJSONC(raw), &doc); err != nil {
			return cfg, fmt.Errorf("настройки OpenCode %s: %w", filepath.Base(path), err)
		}
		cfg.Paths = append(cfg.Paths, path)
		add := func(p UserProvider) {
			if _, ok := found[p.ID]; !ok {
				order = append(order, p.ID)
			}
			found[p.ID] = p
		}
		// Первая версия OpenCode: раздел provider. Вторая версия важнее.
		for id, v := range asMap(doc["provider"]) {
			if p, ok := fromV1(id, asMap(v)); ok {
				add(p)
			} else {
				cfg.Skipped++
			}
		}
		for id, v := range asMap(doc["providers"]) {
			if p, ok := fromV2(id, asMap(v)); ok {
				add(p)
			} else {
				cfg.Skipped++
			}
		}
		// MCP-серверы: только описания, без секретов.
		if section := asMap(doc["mcp"]); section != nil {
			servers, skipped := readMCP(section)
			cfg.MCPSkipped += skipped
			for _, s := range servers {
				if _, ok := mcp[s.Name]; !ok {
					mcpOrder = append(mcpOrder, s.Name)
				}
				mcp[s.Name] = s
			}
		}
	}
	sort.Strings(mcpOrder)
	for _, name := range mcpOrder {
		cfg.MCP = append(cfg.MCP, mcp[name])
	}
	sort.Strings(order)
	for _, id := range order {
		cfg.Providers = append(cfg.Providers, found[id])
	}
	return cfg, nil
}

// fromV2 переносит провайдера из раздела providers второй версии.
func fromV2(id string, src map[string]any) (UserProvider, bool) {
	if !agentprofile.ValidProviderID(id) || id == ProviderID || src == nil {
		return UserProvider{}, false
	}
	p := UserProvider{ID: id, Name: id, KeyEnv: ProviderKeyEnv(id)}
	def := map[string]any{}
	for _, field := range []string{"name", "package", "canonical", "compatibility", "models"} {
		if v, ok := src[field]; ok {
			def[field] = v
		}
	}
	if body := asMap(src["body"]); body != nil {
		clean, dropped := withoutSecrets(body)
		p.Dropped += dropped
		def["body"] = clean
	}
	if settings := asMap(src["settings"]); settings != nil {
		clean, dropped := withoutSecrets(settings)
		p.Dropped += dropped
		def["settings"] = clean
		p.BaseURL, _ = clean["baseURL"].(string)
	}
	if headers := asMap(src["headers"]); headers != nil {
		clean, dropped := onlyReferences(headers)
		p.Dropped += dropped
		if len(clean) > 0 {
			def["headers"] = clean
		}
	}
	def["env"] = append([]any{p.KeyEnv}, stringList(src["env"])...)
	p.finish(def)
	return p, true
}

// fromV1 переносит провайдера из раздела provider первой версии: только
// API, совместимые с OpenAI.
func fromV1(id string, src map[string]any) (UserProvider, bool) {
	if !agentprofile.ValidProviderID(id) || id == ProviderID || src == nil {
		return UserProvider{}, false
	}
	npm, _ := src["npm"].(string)
	options := asMap(src["options"])
	baseURL, _ := options["baseURL"].(string)
	if baseURL == "" || (npm != "" && npm != "@ai-sdk/openai-compatible") {
		return UserProvider{}, false
	}
	p := UserProvider{ID: id, Name: id, KeyEnv: ProviderKeyEnv(id), BaseURL: baseURL}
	def := map[string]any{
		"package":  "@opencode/ai/providers/openai-compatible",
		"settings": map[string]any{"baseURL": baseURL},
	}
	if name, ok := src["name"].(string); ok {
		def["name"] = name
	}
	for k, v := range options {
		if k != "baseURL" && k != "headers" {
			if _, dropped := withoutSecrets(map[string]any{k: v}); dropped > 0 {
				p.Dropped++
			}
		}
	}
	if headers := asMap(options["headers"]); headers != nil {
		clean, dropped := onlyReferences(headers)
		p.Dropped += dropped
		if len(clean) > 0 {
			def["headers"] = clean
		}
	}
	models := map[string]any{}
	for mid, mv := range asMap(src["models"]) {
		m := map[string]any{}
		if name, ok := asMap(mv)["name"].(string); ok {
			m["name"] = name
		}
		models[mid] = m
	}
	def["models"] = models
	env := []any{p.KeyEnv}
	env = append(env, stringList(src["env"])...)
	def["env"] = env
	p.finish(def)
	return p, true
}

func (p *UserProvider) finish(def map[string]any) {
	if name, ok := def["name"].(string); ok && strings.TrimSpace(name) != "" {
		p.Name = name
	}
	for mid := range asMap(def["models"]) {
		p.Models = append(p.Models, mid)
	}
	sort.Strings(p.Models)
	p.Selfhost = isSelfhostURL(p.BaseURL)
	p.Definition = def
}

// isSelfhostURL — адрес задан явно и это не известный облачный сервис.
// Адрес из переменной окружения проверить нельзя, он селфхостом не считается.
func isSelfhostURL(raw string) bool {
	if raw == "" || strings.Contains(raw, "{") {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	return !IsCloudHost(u.Hostname())
}

// ProviderKeyEnv — переменная окружения с ключом провайдера для процесса OpenCode.
func ProviderKeyEnv(id string) string {
	var b strings.Builder
	b.WriteString("DAILY_AGENT_KEY_")
	for _, r := range strings.ToUpper(id) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

// ProviderKeyAccount — запись Связки ключей с ключом провайдера.
func ProviderKeyAccount(id string) string { return "provider:" + id }

var secretName = regexp.MustCompile(`(?i)key|token|secret|password|auth|cookie|credential`)

// isReference — значение берётся OpenCode из окружения или файла, сам секрет
// в настройках не лежит.
func isReference(v any) bool {
	s, ok := v.(string)
	return ok && (strings.HasPrefix(s, "{env:") || strings.HasPrefix(s, "{file:"))
}

// withoutSecrets убирает поля, похожие на ключи и токены, кроме ссылок на
// окружение и файлы.
func withoutSecrets(m map[string]any) (map[string]any, int) {
	out := map[string]any{}
	dropped := 0
	for k, v := range m {
		if secretName.MatchString(k) && !isReference(v) {
			dropped++
			continue
		}
		out[k] = v
	}
	return out, dropped
}

// onlyReferences оставляет заголовки, значения которых — ссылки на окружение
// или файлы. Остальные могут содержать токены и не переносятся.
func onlyReferences(m map[string]any) (map[string]any, int) {
	out := map[string]any{}
	dropped := 0
	for k, v := range m {
		if isReference(v) {
			out[k] = v
		} else {
			dropped++
		}
	}
	return out, dropped
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func stringList(v any) []any {
	var out []any
	list, _ := v.([]any)
	for _, x := range list {
		if s, ok := x.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

// StripJSONC превращает JSON с комментариями и висячими запятыми в обычный JSON.
func StripJSONC(src []byte) []byte {
	var out bytes.Buffer
	inString, escaped := false, false
	for i := 0; i < len(src); i++ {
		c := src[i]
		if inString {
			out.WriteByte(c)
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}
		switch {
		case c == '"':
			inString = true
			out.WriteByte(c)
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			for i < len(src) && src[i] != '\n' {
				i++
			}
			out.WriteByte('\n')
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			i += 2
			for i+1 < len(src) && !(src[i] == '*' && src[i+1] == '/') {
				i++
			}
			i++
		case c == ',':
			// Висячая запятая: дальше только пробелы и комментарии до } или ].
			j := i + 1
		scan:
			for j < len(src) {
				switch {
				case src[j] == ' ' || src[j] == '\t' || src[j] == '\n' || src[j] == '\r':
					j++
					continue
				case src[j] == '/' && j+1 < len(src) && src[j+1] == '/':
					for j < len(src) && src[j] != '\n' {
						j++
					}
					continue
				case src[j] == '/' && j+1 < len(src) && src[j+1] == '*':
					j += 2
					for j+1 < len(src) && !(src[j] == '*' && src[j+1] == '/') {
						j++
					}
					j += 2
					continue
				}
				break scan
			}
			if j < len(src) && (src[j] == '}' || src[j] == ']') {
				continue
			}
			out.WriteByte(c)
		default:
			out.WriteByte(c)
		}
	}
	return out.Bytes()
}
