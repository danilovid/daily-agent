package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"

	"daily-agent/internal/agent"
	"daily-agent/internal/store"
)

// mcpEntry — MCP-сервер агента со всем, что про него знает приложение.
type mcpEntry struct {
	Name    string
	Title   string
	Source  string // app или opencode
	Type    string // remote или local
	URL     string
	Command []string
	Auth    string // oauth, token или none
	Enabled bool
	Tokens  []agent.MCPToken
	Missing []agent.MCPToken // секреты, которых нет в Связке ключей
	// Tools — инструменты, выбранные вручную; Custom = false — по умолчанию чтение.
	Tools      []string
	Custom     bool
	Definition map[string]any
	Row        *store.MCPServer
	// User — сервер из настроек OpenCode пользователя; nil — свой.
	User *agent.UserMCPServer
}

// appMCPToken — токен своего сервера: заголовок Authorization: Bearer.
func appMCPToken(name string) agent.MCPToken {
	return agent.MCPToken{Kind: "header", Key: "Authorization", Prefix: "Bearer ", Env: agent.MCPTokenEnv(name, "header", "Authorization")}
}

// mcpEntries собирает MCP-серверы: из настроек OpenCode пользователя (с
// изменениями из приложения) и свои. withKeys — прочитать секреты для
// процесса OpenCode; без них проверяется только, что секреты сохранены.
func mcpEntries(ctx context.Context, st *store.Store, secrets Secrets, user agent.UserConfig, withKeys bool) ([]mcpEntry, []string, error) {
	rows, err := st.MCPServers(ctx)
	if err != nil {
		return nil, nil, err
	}
	byName := map[string]*store.MCPServer{}
	for i := range rows {
		byName[rows[i].Name] = &rows[i]
	}
	var out []mcpEntry
	for i := range user.MCP {
		s := user.MCP[i]
		if s.Name == InternalMCPServerName {
			continue // имя занято своим сервером приложения
		}
		e := mcpEntry{Name: s.Name, Title: s.Title, Source: store.MCPSourceOpenCode, Type: s.Type, URL: s.URL, Command: s.Command,
			Enabled: !s.Disabled, Tokens: s.Tokens, Definition: s.Definition, Auth: store.MCPAuthNone, User: &user.MCP[i]}
		if s.OAuth {
			e.Auth = store.MCPAuthOAuth
		} else if len(s.Tokens) > 0 {
			e.Auth = store.MCPAuthToken
		}
		if row := byName[s.Name]; row != nil && row.Source == store.MCPSourceOpenCode {
			e.Row, e.Enabled = row, row.Enabled
			e.Tools, e.Custom = row.Tools, row.Tools != nil
		}
		out = append(out, e)
	}
	for i := range rows {
		row := &rows[i]
		if row.Source != store.MCPSourceApp || row.Name == InternalMCPServerName {
			continue
		}
		if _, dup := user.MCPServer(row.Name); dup {
			continue // имя заняли настройки OpenCode: свой сервер не подключается
		}
		e := mcpEntry{Name: row.Name, Title: row.Title, Source: row.Source, Type: row.Type, URL: row.URL, Command: row.Command,
			Auth: row.Auth, Enabled: row.Enabled, Tools: row.Tools, Custom: row.Tools != nil, Row: row}
		def := map[string]any{"type": row.Type}
		if row.Type == "local" {
			def["command"] = row.Command
		} else {
			def["url"] = row.URL
			switch row.Auth {
			case store.MCPAuthToken:
				t := appMCPToken(row.Name)
				e.Tokens = []agent.MCPToken{t}
				def["oauth"] = false
				def["headers"] = map[string]any{t.Key: t.Prefix + "{env:" + t.Env + "}"}
			case store.MCPAuthNone:
				def["oauth"] = false
			}
		}
		e.Definition = def
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })

	var env []string
	for i := range out {
		e := &out[i]
		for _, t := range e.Tokens {
			key, err := secrets.Get(ctx, agent.KeychainService, t.Account(e.Name))
			if err != nil || key == "" {
				e.Missing = append(e.Missing, t)
				continue
			}
			if withKeys {
				env = append(env, t.Env+"="+key)
			}
		}
	}
	return out, env, nil
}

// localStartupMillis — сколько ждать запуска локального MCP-сервера.
const localStartupMillis = 120000

// mcpOptions добавляет MCP-серверы в профиль: выключенные и без секретов
// агенту недоступны. Права агента — шаблоны чтения или выбранные
// инструменты; рабочим чатам точный список даёт Manager.
func mcpOptions(entries []mcpEntry, setup *agent.Setup) {
	for _, e := range entries {
		if setup.Options.MCP == nil {
			setup.Options.MCP = map[string]map[string]any{}
		}
		def := map[string]any{}
		for k, v := range e.Definition {
			def[k] = v
		}
		active := e.Enabled && len(e.Missing) == 0
		if !active {
			def["disabled"] = true
		}
		// «npx -y пакет» при первом запуске скачивает пакет: даём время.
		if def["type"] == "local" && def["timeout"] == nil {
			def["timeout"] = map[string]any{"startup": localStartupMillis}
		}
		delete(def, "enabled")
		setup.Options.MCP[e.Name] = def
		if !active {
			continue
		}
		if e.Custom {
			if setup.MCPTools == nil {
				setup.MCPTools = map[string][]string{}
			}
			var tools []string
			for _, t := range e.Tools {
				if !agent.ToolIsWrite(agent.ShortToolName(e.Name, t)) {
					tools = append(tools, t)
				}
			}
			setup.MCPTools[e.Name] = tools
			setup.Options.AllowedTools = append(setup.Options.AllowedTools, tools...)
		} else {
			setup.Options.AllowedTools = append(setup.Options.AllowedTools, agent.ReadPatterns(e.Name)...)
		}
	}
}

// MCPToolView — инструмент MCP-сервера на экране.
type MCPToolView struct {
	// Name — полное имя для агента, Short — без сервера.
	Name        string `json:"name"`
	Short       string `json:"short"`
	Description string `json:"description"`
	Allowed     bool   `json:"allowed"`
	// Write — инструмент похож на запись: в v0.1 агенту недоступен.
	Write bool `json:"write"`
}

// MCPTokenView — секрет сервера: сохранён ли он. Значение в окно не уходит.
type MCPTokenView struct {
	Kind  string `json:"kind"`
	Key   string `json:"key"`
	Saved bool   `json:"saved"`
}

// MCPServerView — MCP-сервер на экране.
type MCPServerView struct {
	Name  string `json:"name"`
	Title string `json:"title"`
	// Source: app — добавлен здесь, opencode — из настроек OpenCode.
	Source string `json:"source"`
	Type   string `json:"type"`
	// Address — адрес удалённого сервера или команда локального.
	Address string `json:"address"`
	Auth    string `json:"auth"`
	Enabled bool   `json:"enabled"`
	// Status: connected, needs_auth, needs_token, failed, disabled, pending, stopped.
	Status string         `json:"status"`
	Error  string         `json:"error"`
	Tokens []MCPTokenView `json:"tokens"`
	// TokensInConfig — недостающие токены есть в настройках OpenCode
	// пользователя: их можно перенести в Связку ключей одной кнопкой.
	TokensInConfig bool          `json:"tokensInConfig"`
	Tools          []MCPToolView `json:"tools"`
	// Custom — инструменты выбраны вручную.
	Custom bool `json:"custom"`
}

// MCPServerInput — новый MCP-сервер.
type MCPServerInput struct {
	Title string `json:"title"`
	// Type: remote или local.
	Type string `json:"type"`
	URL  string `json:"url"`
	// Command — команда запуска локального сервера, через пробелы.
	Command string `json:"command"`
	// Auth: oauth, token или none.
	Auth  string `json:"auth"`
	Token string `json:"token"`
}

// MCPAgent — то, что сервису MCP нужно от агента.
type MCPAgent interface {
	MCP() []agent.MCPState
	RefreshMCP(ctx context.Context) error
	LoginMCP(ctx context.Context, name string) (string, error)
	AllowedTools() []string
	Status() agent.Status
}

// MCPService — раздел «MCP-серверы» в настройках. Серверы подключаются к
// агенту через OpenCode; ядро к ним само не обращается.
type MCPService struct {
	settings *SettingsService
	agent    MCPAgent
	openURL  func(string) error
	// internal — команда своего MCP-сервера приложения.
	internal []string
}

// NewMCPService создаёт сервис. openURL открывает адрес входа в браузере,
// internal — команда своего MCP-сервера для внешних агентов.
func NewMCPService(settings *SettingsService, ag MCPAgent, openURL func(string) error, internal []string) *MCPService {
	return &MCPService{settings: settings, agent: ag, openURL: openURL, internal: internal}
}

// ExternalAgentView — как подключить свой сервер приложения к внешнему агенту.
type ExternalAgentView struct {
	// Command — команда запуска сервера.
	Command []string `json:"command"`
	// OpenCode — готовый фрагмент настроек OpenCode второй версии.
	OpenCode string `json:"openCode"`
	// Claude — команда для Claude Code.
	Claude string `json:"claude"`
}

// ExternalAgent описывает, как подключить свой MCP-сервер приложения к
// внешнему агенту: тот получит встречи, бэклог и запись плана на день.
func (s *MCPService) ExternalAgent() ExternalAgentView {
	v := ExternalAgentView{Command: s.internal}
	if len(s.internal) == 0 {
		return v
	}
	server := map[string]any{"type": "local", "command": s.internal}
	// Приложение запущено с другой базой — внешнему агенту нужна та же.
	if db := strings.TrimSpace(os.Getenv(store.EnvPath)); db != "" {
		server["environment"] = map[string]any{store.EnvPath: db}
	}
	cfg := map[string]any{"mcp": map[string]any{"servers": map[string]any{"daily-agent": server}}}
	raw, _ := json.MarshalIndent(cfg, "", "  ")
	v.OpenCode = string(raw)
	quoted := make([]string, len(s.internal))
	for i, part := range s.internal {
		quoted[i] = shellQuote(part)
	}
	v.Claude = "claude mcp add daily-agent -- " + strings.Join(quoted, " ")
	return v
}

func shellQuote(v string) string {
	if v != "" && !strings.ContainsAny(v, " \t'\"$`\\") {
		return v
	}
	return "'" + strings.ReplaceAll(v, "'", `'\''`) + "'"
}

func (s *MCPService) entries(ctx context.Context) ([]mcpEntry, error) {
	user, err := s.settings.userConfig()
	if err != nil {
		user = agent.UserConfig{}
	}
	entries, _, err := mcpEntries(ctx, s.settings.store, s.settings.secrets, user, false)
	return entries, err
}

// List возвращает MCP-серверы с состоянием и инструментами.
func (s *MCPService) List(ctx context.Context) ([]MCPServerView, error) {
	entries, err := s.entries(ctx)
	if err != nil {
		return nil, err
	}
	states := map[string]agent.MCPState{}
	for _, st := range s.agent.MCP() {
		states[st.Name] = st
	}
	allowed := map[string]bool{}
	for _, t := range s.agent.AllowedTools() {
		allowed[t] = true
	}
	ready := s.agent.Status().State == agent.StateReady
	out := []MCPServerView{}
	for _, e := range entries {
		v := MCPServerView{Name: e.Name, Title: e.Title, Source: e.Source, Type: e.Type, Auth: e.Auth, Enabled: e.Enabled,
			Custom: e.Custom, Tokens: []MCPTokenView{}, Tools: []MCPToolView{}}
		v.Address = e.URL
		if e.Type == "local" {
			v.Address = strings.Join(e.Command, " ")
		}
		missing := map[string]bool{}
		for _, t := range e.Missing {
			missing[t.Kind+":"+t.Key] = true
		}
		for _, t := range e.Tokens {
			v.Tokens = append(v.Tokens, MCPTokenView{Kind: t.Kind, Key: t.Key, Saved: !missing[t.Kind+":"+t.Key]})
		}
		for _, t := range e.Missing {
			v.TokensInConfig = v.TokensInConfig || e.User != nil && e.User.HasSecret(t)
		}
		st, known := states[e.Name]
		switch {
		case !e.Enabled:
			v.Status = "disabled"
		case len(e.Missing) > 0:
			v.Status = "needs_token"
		case !ready:
			v.Status = "stopped"
		case !known:
			v.Status = "pending"
		default:
			v.Status, v.Error = st.Status, st.Error
		}
		for _, t := range st.Tools {
			short := agent.ShortToolName(e.Name, t.Name)
			v.Tools = append(v.Tools, MCPToolView{Name: t.Name, Short: short, Description: t.Description,
				Allowed: allowed[t.Name], Write: agent.ToolIsWrite(short)})
		}
		out = append(out, v)
	}
	return out, nil
}

// Add добавляет свой MCP-сервер и перезапускает OpenCode с ним.
func (s *MCPService) Add(ctx context.Context, in MCPServerInput) ([]MCPServerView, error) {
	s.settings.mu.Lock()
	defer s.settings.mu.Unlock()
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return nil, errors.New("введите название сервера")
	}
	name := agent.MCPName(title)
	if name == "" {
		return nil, errors.New("название должно содержать буквы или цифры")
	}
	if name == InternalMCPServerName {
		return nil, fmt.Errorf("имя %s занято сервером приложения, выберите другое название", name)
	}
	entries, err := s.entries(ctx)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.Name == name {
			return nil, fmt.Errorf("сервер с именем %s уже есть, выберите другое название", name)
		}
	}
	row := store.MCPServer{Name: name, Title: title, Source: store.MCPSourceApp, Enabled: true, Auth: in.Auth}
	switch in.Type {
	case "remote":
		row.Type = "remote"
		u, err := url.Parse(strings.TrimSpace(in.URL))
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return nil, errors.New("адрес сервера должен начинаться с http:// или https://")
		}
		if u.User != nil {
			return nil, errors.New("логин и пароль не пишутся в адрес: используйте вход через браузер или токен")
		}
		row.URL = u.String()
		if row.Auth != store.MCPAuthOAuth && row.Auth != store.MCPAuthToken && row.Auth != store.MCPAuthNone {
			return nil, errors.New("выберите способ входа")
		}
	case "local":
		row.Type = "local"
		row.Command = strings.Fields(in.Command)
		if len(row.Command) == 0 {
			return nil, errors.New("введите команду запуска сервера")
		}
		row.Auth = store.MCPAuthNone
	default:
		return nil, errors.New("выберите тип сервера")
	}
	if row.Auth == store.MCPAuthToken {
		token := strings.TrimSpace(in.Token)
		if err := agent.ValidateKey(token); err != nil {
			return nil, fmt.Errorf("токен: %w", err)
		}
		// Сначала токен: если Связка ключей откажет, сервер не появится.
		if err := s.settings.secrets.Set(ctx, agent.KeychainService, appMCPToken(name).Account(name), token); err != nil {
			return nil, err
		}
	}
	if _, err := s.settings.store.SaveMCPServer(ctx, row); err != nil {
		return nil, err
	}
	return s.applied(ctx)
}

// override — запись с изменениями сервера: своя или для сервера из OpenCode.
func (s *MCPService) override(ctx context.Context, name string) (store.MCPServer, mcpEntry, error) {
	entries, err := s.entries(ctx)
	if err != nil {
		return store.MCPServer{}, mcpEntry{}, err
	}
	for _, e := range entries {
		if e.Name != name {
			continue
		}
		if e.Row != nil {
			return *e.Row, e, nil
		}
		return store.MCPServer{Name: e.Name, Title: e.Title, Source: store.MCPSourceOpenCode, Type: e.Type, Enabled: e.Enabled}, e, nil
	}
	return store.MCPServer{}, mcpEntry{}, fmt.Errorf("нет сервера %s", name)
}

// SetEnabled включает или выключает сервер. Выключенный агенту недоступен.
func (s *MCPService) SetEnabled(ctx context.Context, name string, enabled bool) ([]MCPServerView, error) {
	s.settings.mu.Lock()
	defer s.settings.mu.Unlock()
	row, _, err := s.override(ctx, name)
	if err != nil {
		return nil, err
	}
	row.Enabled = enabled
	if _, err := s.settings.store.SaveMCPServer(ctx, row); err != nil {
		return nil, err
	}
	return s.applied(ctx)
}

// SetTools выбирает инструменты сервера вручную. reset — вернуть выбор по
// умолчанию: все инструменты чтения. Инструменты записи выбрать нельзя.
func (s *MCPService) SetTools(ctx context.Context, name string, tools []string, reset bool) ([]MCPServerView, error) {
	s.settings.mu.Lock()
	defer s.settings.mu.Unlock()
	row, _, err := s.override(ctx, name)
	if err != nil {
		return nil, err
	}
	if reset {
		row.Tools = nil
	} else {
		known := map[string]bool{}
		for _, st := range s.agent.MCP() {
			if st.Name == name {
				for _, t := range st.Tools {
					known[t.Name] = true
				}
			}
		}
		row.Tools = []string{}
		for _, t := range tools {
			if !known[t] {
				return nil, fmt.Errorf("у сервера нет инструмента %s", t)
			}
			if agent.ToolIsWrite(agent.ShortToolName(name, t)) {
				return nil, fmt.Errorf("%s похож на запись: в этой версии агент только читает", agent.ShortToolName(name, t))
			}
			row.Tools = append(row.Tools, t)
		}
	}
	if _, err := s.settings.store.SaveMCPServer(ctx, row); err != nil {
		return nil, err
	}
	return s.applied(ctx)
}

// SetToken сохраняет секрет сервера в Связке ключей.
func (s *MCPService) SetToken(ctx context.Context, name, kind, key, token string) ([]MCPServerView, error) {
	s.settings.mu.Lock()
	defer s.settings.mu.Unlock()
	token = strings.TrimSpace(token)
	if err := agent.ValidateKey(token); err != nil {
		return nil, fmt.Errorf("токен: %w", err)
	}
	_, e, err := s.override(ctx, name)
	if err != nil {
		return nil, err
	}
	for _, t := range e.Tokens {
		if t.Kind == kind && t.Key == key {
			if err := s.settings.secrets.Set(ctx, agent.KeychainService, t.Account(name), token); err != nil {
				return nil, err
			}
			return s.applied(ctx)
		}
	}
	return nil, fmt.Errorf("у сервера %s нет секрета %s", name, key)
}

// ImportTokens переносит недостающие токены сервера из настроек OpenCode
// пользователя в Связку ключей. Только по просьбе пользователя: в файлы
// приложения и в настройки OpenCode агента токены не попадают.
func (s *MCPService) ImportTokens(ctx context.Context, name string) ([]MCPServerView, error) {
	s.settings.mu.Lock()
	defer s.settings.mu.Unlock()
	_, e, err := s.override(ctx, name)
	if err != nil {
		return nil, err
	}
	if e.User == nil {
		return nil, errors.New("сервер добавлен в приложении: его токен вводится здесь")
	}
	moved := 0
	for _, t := range e.Missing {
		v, ok := e.User.Secret(t)
		if !ok {
			continue
		}
		if err := agent.ValidateKey(v); err != nil {
			return nil, fmt.Errorf("токен %s из настроек OpenCode: %w", t.Key, err)
		}
		if err := s.settings.secrets.Set(ctx, agent.KeychainService, t.Account(name), v); err != nil {
			return nil, err
		}
		moved++
	}
	if moved == 0 {
		return nil, errors.New("в настройках OpenCode нет токенов этого сервера")
	}
	return s.applied(ctx)
}

// Remove удаляет свой сервер и его токен. Серверы из настроек OpenCode
// удаляются там; здесь их можно выключить.
func (s *MCPService) Remove(ctx context.Context, name string) ([]MCPServerView, error) {
	s.settings.mu.Lock()
	defer s.settings.mu.Unlock()
	_, e, err := s.override(ctx, name)
	if err != nil {
		return nil, err
	}
	if e.Source != store.MCPSourceApp || e.Row == nil {
		return nil, errors.New("сервер из настроек OpenCode удаляется там, здесь его можно выключить")
	}
	for _, t := range e.Tokens {
		if err := s.settings.secrets.Delete(ctx, agent.KeychainService, t.Account(name)); err != nil {
			return nil, err
		}
	}
	if err := s.settings.store.DeleteMCPServer(ctx, e.Row.ID); err != nil {
		return nil, userError(err)
	}
	return s.applied(ctx)
}

// Login начинает вход на сервер через браузер. Данные входа хранит OpenCode
// в папке данных агента; когда вход закончится, список обновится сам.
func (s *MCPService) Login(ctx context.Context, name string) error {
	u, err := s.agent.LoginMCP(ctx, name)
	if err != nil {
		return err
	}
	return s.openURL(u)
}

// Refresh заново узнаёт состояние серверов и их инструменты.
func (s *MCPService) Refresh(ctx context.Context) ([]MCPServerView, error) {
	if err := s.agent.RefreshMCP(ctx); err != nil {
		return nil, err
	}
	return s.List(ctx)
}

func (s *MCPService) applied(ctx context.Context) ([]MCPServerView, error) {
	if err := s.settings.apply(ctx); err != nil {
		return nil, err
	}
	return s.List(ctx)
}
