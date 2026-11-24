package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	agentprofile "daily-agent/agent"
	"daily-agent/internal/agent/stubmodel"
)

// DiscoveryModelID — модель служебной заглушки.
const DiscoveryModelID = "discovery"

// discoveryPrompt — вопрос служебной сессии. Заглушка на него просто отвечает
// и ничего не вызывает.
const discoveryPrompt = "Служебная проверка Daily Agent: список инструментов."

// MCPTool — инструмент MCP-сервера, который OpenCode предлагает агенту.
type MCPTool struct {
	// Name — полное имя: <сервер>_<инструмент>.
	Name        string `json:"name"`
	Description string `json:"description"`
}

// MCPState — MCP-сервер в запущенном OpenCode.
type MCPState struct {
	Name string `json:"name"`
	// Status: connected, pending, disabled, failed, needs_auth.
	Status        string `json:"status"`
	Error         string `json:"error"`
	IntegrationID string `json:"integrationID"`
	// Tools — все инструменты сервера, в том числе запрещённые агенту.
	Tools []MCPTool `json:"tools"`
}

// mcpWait — сколько ждать подключения MCP-серверов после запуска OpenCode.
const mcpWait = 15 * time.Second

// discoveryProvider — служебная заглушка в профиле. Запускается один раз,
// чтобы адрес не менялся и не требовал перезапуска OpenCode.
func (m *Manager) discoveryProvider() *agentprofile.Provider {
	if m.discovery == nil {
		srv, err := stubmodel.Start()
		if err != nil {
			m.base.Logf("служебная заглушка: %v", err)
			return nil
		}
		m.discovery = srv
	}
	return &agentprofile.Provider{ID: agentprofile.DiscoveryProviderID, BaseURL: m.discovery.URL, ModelID: DiscoveryModelID}
}

// warmup ждёт MCP-серверы после запуска сервера и узнаёт их инструменты.
// Пока он идёт, агент «запускается»: первая сессия иначе не видит инструментов.
func (m *Manager) warmup(sup *Supervisor) {
	defer func() {
		m.mu.Lock()
		if m.warming == sup {
			m.warming = nil
		}
		if m.sup == sup {
			m.warmed = sup
		}
		m.mu.Unlock()
		m.notify()
	}()
	c, err := sup.Client()
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*mcpWait)
	defer cancel()
	state := m.discover(ctx, c)
	m.mu.Lock()
	if m.sup == sup {
		m.mcp = state
	}
	m.mu.Unlock()
	for _, s := range state {
		if s.Status == "pending" {
			go m.followUp(sup)
			break
		}
	}
}

// followUp ждёт серверы, которые не успели подключиться за прогрев
// (например, «npx» скачивает пакет), и потом обновляет список инструментов.
func (m *Manager) followUp(sup *Supervisor) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	for ctx.Err() == nil {
		time.Sleep(3 * time.Second)
		m.mu.Lock()
		current := m.sup == sup
		m.mu.Unlock()
		if !current {
			return
		}
		c, err := sup.Client()
		if err != nil {
			return
		}
		list, err := c.MCPServers(ctx, m.Workspace())
		if err != nil {
			continue
		}
		pending := false
		for _, s := range list {
			pending = pending || s.Status == "pending"
		}
		if !pending {
			_ = m.RefreshMCP(ctx)
			return
		}
	}
}

// discover ждёт, пока серверы перестанут подключаться, и спрашивает у
// OpenCode, какие инструменты он предложит агенту. Ядро само к серверам не
// обращается: список берётся из служебной сессии с заглушкой модели.
func (m *Manager) discover(ctx context.Context, c *Client) []MCPState {
	m.discoverMu.Lock()
	defer m.discoverMu.Unlock()
	dir := m.Workspace()
	m.mu.Lock()
	expected := make([]string, 0, len(m.setup.Options.MCP))
	for name := range m.setup.Options.MCP {
		expected = append(expected, name)
	}
	m.mu.Unlock()
	// OpenCode читает настройки лениво: запрос агентов заставляет их загрузить.
	var agents json.RawMessage
	_ = c.do(ctx, http.MethodGet, "/api/agent"+locationQuery(dir), nil, &agents)
	var statuses []MCPStatus
	deadline := time.Now().Add(mcpWait)
	for {
		list, err := c.MCPServers(ctx, dir)
		if err == nil {
			statuses = list
			settled := true
			for _, s := range list {
				settled = settled && s.Status != "pending"
			}
			for _, name := range expected {
				found := false
				for _, s := range list {
					found = found || s.Name == name
				}
				settled = settled && found
			}
			if settled {
				break
			}
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	var connected []string
	for _, s := range statuses {
		if s.Status == "connected" {
			connected = append(connected, s.Name)
		}
	}
	tools := map[string][]MCPTool{}
	m.mu.Lock()
	stub := m.discovery
	m.mu.Unlock()
	if len(connected) > 0 && stub != nil {
		allow := make([]string, 0, len(connected))
		for _, name := range connected {
			allow = append(allow, name+"_*")
		}
		// Инструменты появляются в OpenCode чуть позже, чем сервер подключён.
		for attempt := 0; attempt < 10 && ctx.Err() == nil; attempt++ {
			tools = groupTools(m.offeredTools(ctx, c, stub, allow), connected)
			if len(tools) == len(connected) {
				break
			}
			time.Sleep(400 * time.Millisecond)
		}
	}
	out := make([]MCPState, 0, len(statuses))
	for _, s := range statuses {
		out = append(out, MCPState{Name: s.Name, Status: s.Status, Error: s.Error, IntegrationID: s.IntegrationID, Tools: tools[s.Name]})
	}
	return out
}

// offeredTools — инструменты, которые OpenCode предложил заглушке в
// служебной сессии с разрешёнными allow. Сессия потом удаляется.
func (m *Manager) offeredTools(ctx context.Context, c *Client, stub *stubmodel.Server, allow []string) []stubmodel.ToolDef {
	sess, err := c.CreateSession(ctx, SessionOptions{
		Title:     "Служебная проверка инструментов",
		Directory: m.Workspace(),
		Model:     &ModelRef{ID: DiscoveryModelID, ProviderID: agentprofile.DiscoveryProviderID},
		Allow:     allow,
	})
	if err != nil {
		return nil
	}
	defer c.DeleteSession(context.Background(), sess.ID)
	before := len(stub.Requests())
	if err := c.Prompt(ctx, sess.ID, discoveryPrompt); err != nil {
		return nil
	}
	_ = c.Wait(ctx, sess.ID)
	var best []stubmodel.ToolDef
	for _, r := range stub.Requests()[before:] {
		if len(r.ToolInfo) > len(best) {
			best = r.ToolInfo
		}
	}
	return best
}

// groupTools раскладывает инструменты по серверам: имя — <сервер>_<инструмент>,
// а имена серверов сами могут содержать «_», поэтому берётся самое длинное.
func groupTools(defs []stubmodel.ToolDef, servers []string) map[string][]MCPTool {
	sorted := append([]string(nil), servers...)
	sort.Slice(sorted, func(i, j int) bool { return len(sorted[i]) > len(sorted[j]) })
	out := map[string][]MCPTool{}
	for _, d := range defs {
		for _, s := range sorted {
			if strings.HasPrefix(d.Name, s+"_") {
				out[s] = append(out[s], MCPTool{Name: d.Name, Description: d.Description})
				break
			}
		}
	}
	for _, list := range out {
		sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	}
	return out
}

// MCP возвращает MCP-серверы запущенного OpenCode и их инструменты.
func (m *Manager) MCP() []MCPState {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]MCPState(nil), m.mcp...)
}

// RefreshMCP заново узнаёт состояние MCP-серверов и их инструменты,
// например после входа.
func (m *Manager) RefreshMCP(ctx context.Context) error {
	m.mu.Lock()
	sup := m.sup
	m.mu.Unlock()
	c, err := sup.Client()
	if err != nil {
		return err
	}
	state := m.discover(ctx, c)
	m.mu.Lock()
	if m.sup == sup {
		m.mcp = state
	}
	m.mu.Unlock()
	m.notify()
	return nil
}

// LoginMCP начинает вход на MCP-сервер через браузер и возвращает адрес
// входа. Когда вход закончится, список инструментов обновится сам.
func (m *Manager) LoginMCP(ctx context.Context, name string) (string, error) {
	var integration string
	for _, s := range m.MCP() {
		if s.Name == name {
			integration = s.IntegrationID
		}
	}
	if integration == "" {
		return "", errors.New("сервер не просит вход через браузер")
	}
	c, err := m.Client()
	if err != nil {
		return "", err
	}
	attempt, err := c.StartOAuth(ctx, integration, m.Workspace())
	if err != nil {
		return "", err
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		for ctx.Err() == nil {
			time.Sleep(time.Second)
			st, err := c.OAuthStatus(ctx, attempt, m.Workspace())
			if err != nil || st == "failed" || st == "expired" {
				return
			}
			if st == "complete" {
				_ = m.RefreshMCP(ctx)
				return
			}
		}
	}()
	return attempt.URL, nil
}

// AllowedTools — точный список инструментов, разрешённых рабочим чатам:
// найденные при прогреве инструменты подключённых серверов, выбранные вручную
// или, по умолчанию, инструменты чтения. Инструменты записи — никогда.
// Права сессии важнее прав агента, поэтому решает именно этот список.
func (m *Manager) AllowedTools() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for _, st := range m.mcp {
		if st.Status != "connected" {
			continue
		}
		explicit, custom := m.setup.MCPTools[st.Name]
		for _, t := range st.Tools {
			short := ShortToolName(st.Name, t.Name)
			if ToolIsWrite(short) {
				continue
			}
			if custom && containsString(explicit, t.Name) || !custom && ToolIsRead(short) {
				out = append(out, t.Name)
			}
		}
	}
	return out
}

func containsString(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
