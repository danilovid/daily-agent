package agent

import (
	"sort"
	"strings"

	agentprofile "daily-agent/agent"
)

// MCPToken — секрет MCP-сервера, который не переносится из настроек: значение
// заголовка или переменной окружения. В профиле на его месте ссылка на
// переменную Env, а сам секрет хранится в Связке ключей.
type MCPToken struct {
	// Kind: header — заголовок удалённого сервера, env — переменная локального.
	Kind string
	Key  string
	// Env — переменная окружения процесса OpenCode с секретом.
	Env string
	// Prefix — начало значения перед секретом, например «Bearer ».
	Prefix string
}

// Account — запись Связки ключей с секретом.
func (t MCPToken) Account(server string) string {
	return "mcp:" + server + ":" + t.Kind + ":" + t.Key
}

// UserMCPServer — MCP-сервер из настроек OpenCode пользователя, без секретов.
type UserMCPServer struct {
	// Name — имя для агента: строчная латиница, цифры и «_».
	Name string
	// Title — имя в настройках OpenCode.
	Title   string
	Type    string // local или remote
	URL     string
	Command []string
	// Disabled — сервер выключен в настройках пользователя.
	Disabled bool
	// OAuth — удалённый сервер может просить вход через браузер.
	OAuth bool
	// Tokens — секреты, которые нужно сохранить в приложении.
	Tokens []MCPToken
	// Definition — описание для mcp.servers профиля, со ссылками {env:…}
	// на месте секретов.
	Definition map[string]any
	// secrets — значения секретов из настроек пользователя, по Kind:Key.
	// Только в памяти: переносятся в Связку ключей по просьбе пользователя.
	secrets map[string]string
}

// HasSecret — в настройках пользователя есть значение этого секрета.
func (s UserMCPServer) HasSecret(t MCPToken) bool {
	_, ok := s.secrets[t.Kind+":"+t.Key]
	return ok
}

// Secret — значение секрета из настроек пользователя, без начала вроде «Bearer ».
func (s UserMCPServer) Secret(t MCPToken) (string, bool) {
	v, ok := s.secrets[t.Kind+":"+t.Key]
	return v, ok
}

// MCPName приводит имя сервера к виду для агента. Пусто — не получилось.
func MCPName(raw string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(raw)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case translit[r] != "":
			b.WriteString(translit[r])
		default:
			b.WriteByte('_')
		}
	}
	name := strings.Trim(b.String(), "_")
	for strings.Contains(name, "__") {
		name = strings.ReplaceAll(name, "__", "_")
	}
	if len(name) > 40 {
		name = strings.TrimRight(name[:40], "_")
	}
	if !agentprofile.ValidMCPName(name) {
		return ""
	}
	return name
}

var translit = map[rune]string{
	'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d", 'е': "e", 'ё': "e", 'ж': "zh", 'з': "z", 'и': "i",
	'й': "y", 'к': "k", 'л': "l", 'м': "m", 'н': "n", 'о': "o", 'п': "p", 'р': "r", 'с': "s", 'т': "t",
	'у': "u", 'ф': "f", 'х': "h", 'ц': "ts", 'ч': "ch", 'ш': "sh", 'щ': "sch", 'ъ': "", 'ы': "y", 'ь': "",
	'э': "e", 'ю': "yu", 'я': "ya",
}

// MCPTokenEnv — переменная окружения для секрета MCP-сервера.
func MCPTokenEnv(server, kind, key string) string {
	return strings.Replace(ProviderKeyEnv(server+"_"+kind+"_"+key), "DAILY_AGENT_KEY_", "DAILY_AGENT_MCP_", 1)
}

// splitSecret отделяет начало значения вроде «Bearer » от самого секрета.
func splitSecret(v string) (prefix string) {
	for _, p := range []string{"Bearer ", "Token ", "Basic ", "token ", "bearer "} {
		if strings.HasPrefix(v, p) {
			return p
		}
	}
	return ""
}

// readMCP разбирает раздел mcp: вторая версия — mcp.servers, первая — mcp.<имя>.
func readMCP(section map[string]any) (servers []UserMCPServer, skipped int) {
	entries := asMap(section["servers"])
	v2 := entries != nil
	if !v2 {
		entries = section
	}
	seen := map[string]bool{}
	titles := make([]string, 0, len(entries))
	for k := range entries {
		titles = append(titles, k)
	}
	sort.Strings(titles)
	for _, title := range titles {
		src := asMap(entries[title])
		name := MCPName(title)
		if src == nil || name == "" || seen[name] {
			skipped++
			continue
		}
		s, ok := mcpFrom(name, title, src, v2)
		if !ok {
			skipped++
			continue
		}
		seen[name] = true
		servers = append(servers, s)
	}
	return servers, skipped
}

func mcpFrom(name, title string, src map[string]any, v2 bool) (UserMCPServer, bool) {
	s := UserMCPServer{Name: name, Title: title, secrets: map[string]string{}}
	s.Type, _ = src["type"].(string)
	def := map[string]any{"type": s.Type}
	if v2 {
		s.Disabled, _ = src["disabled"].(bool)
	} else if enabled, ok := src["enabled"].(bool); ok {
		s.Disabled = !enabled
	}
	switch s.Type {
	case "local":
		for _, part := range stringList(src["command"]) {
			s.Command = append(s.Command, part.(string))
		}
		if len(s.Command) == 0 {
			return s, false
		}
		def["command"] = s.Command
		if cwd, ok := src["cwd"].(string); ok && cwd != "" {
			def["cwd"] = cwd
		}
		env := map[string]any{}
		for k, v := range asMap(src["environment"]) {
			str, _ := v.(string)
			if secretName.MatchString(k) && !isReference(str) && str != "" {
				t := MCPToken{Kind: "env", Key: k, Env: MCPTokenEnv(name, "env", k)}
				s.Tokens = append(s.Tokens, t)
				s.secrets[t.Kind+":"+t.Key] = str
				env[k] = "{env:" + t.Env + "}"
				continue
			}
			env[k] = str
		}
		if len(env) > 0 {
			def["environment"] = env
		}
	case "remote":
		s.URL, _ = src["url"].(string)
		if !strings.HasPrefix(s.URL, "http://") && !strings.HasPrefix(s.URL, "https://") {
			return s, false
		}
		def["url"] = s.URL
		headers := map[string]any{}
		for k, v := range asMap(src["headers"]) {
			str, _ := v.(string)
			if isReference(str) || str == "" {
				headers[k] = str
				continue
			}
			// Значение заголовка может быть секретом: переносится только ссылкой.
			t := MCPToken{Kind: "header", Key: k, Env: MCPTokenEnv(name, "header", k), Prefix: splitSecret(str)}
			s.Tokens = append(s.Tokens, t)
			s.secrets[t.Kind+":"+t.Key] = strings.TrimPrefix(str, t.Prefix)
			headers[k] = t.Prefix + "{env:" + t.Env + "}"
		}
		if len(headers) > 0 {
			def["headers"] = headers
		}
		s.OAuth = true
		switch o := src["oauth"].(type) {
		case bool:
			if !o {
				s.OAuth = false
				def["oauth"] = false
			}
		case map[string]any:
			oauth := map[string]any{}
			for _, k := range []string{"client_id", "clientId"} {
				if id, ok := o[k].(string); ok && id != "" {
					oauth["client_id"] = id
				}
			}
			if scope, ok := o["scope"].(string); ok && scope != "" {
				oauth["scope"] = scope
			}
			def["oauth"] = oauth
		}
	default:
		return s, false
	}
	sort.Slice(s.Tokens, func(i, j int) bool { return s.Tokens[i].Key < s.Tokens[j].Key })
	s.Definition = def
	return s, true
}
