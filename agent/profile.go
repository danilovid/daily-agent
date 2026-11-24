// Package agentprofile — профиль OpenCode для агента приложения.
//
// Базовые настройки (opencode.base.json) защищают в два слоя:
//
//   - встроенные инструменты OpenCode — терминал, чтение и правка файлов,
//     запросы в сеть, субагенты — выключены как плагины. Их нет вовсе, и
//     никакие права сессии их не вернут;
//   - первое правило permissions — deny для любого действия, у всех агентов
//     и у агента приложения. Разрешённые инструменты (MCP-серверы источников)
//     добавляются явно, по одному, после этого правила.
//
// Политику experimental.policies с действием permission OpenCode 2.0.28
// отбрасывает как неверную, поэтому политика задаёт только провайдера модели.
//
// Ядро дописывает к базовым настройкам провайдера и модель и кладёт результат
// в отдельную папку настроек OpenCode, не трогая обычную установку.
package agentprofile

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

//go:embed opencode.base.json
var baseConfig []byte

//go:embed instructions.md
var instructions string

// AgentID — агент приложения в настройках OpenCode.
const AgentID = "daily"

// Instructions — системные инструкции агента.
func Instructions() string { return instructions }

// Rule — правило прав или политики OpenCode.
type Rule struct {
	Action   string `json:"action"`
	Resource string `json:"resource"`
	Effect   string `json:"effect"`
}

// DenyAll — права, которые запрещают агенту всё. Их же ядро передаёт при
// создании каждой сессии.
func DenyAll() []Rule { return []Rule{{Action: "*", Resource: "*", Effect: "deny"}} }

// Provider — модель, к которой обращается агент. Ключ в настройки не попадает:
// OpenCode берёт его из переменной окружения KeyEnv.
type Provider struct {
	ID      string // идентификатор провайдера в настройках, например daily-model
	Name    string
	BaseURL string // адрес API, совместимого с OpenAI, с /v1 на конце
	ModelID string
	KeyEnv  string // имя переменной окружения с ключом; пусто — без ключа
}

var (
	idPattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,40}$`)
	toolPattern = regexp.MustCompile(`^[A-Za-z0-9_]+\*?$`)
)

func (p Provider) validate() error {
	if !idPattern.MatchString(p.ID) {
		return fmt.Errorf("идентификатор провайдера %q: только латиница, цифры и дефис", p.ID)
	}
	if !strings.HasPrefix(p.BaseURL, "http://") && !strings.HasPrefix(p.BaseURL, "https://") {
		return errors.New("адрес модели должен начинаться с http:// или https://")
	}
	if strings.TrimSpace(p.ModelID) == "" {
		return errors.New("не указана модель")
	}
	return nil
}

// Options — то, что ядро добавляет к базовым настройкам.
type Options struct {
	// Provider — модель агента. Nil — модель не настроена: OpenCode
	// запустится, но отвечать не сможет.
	Provider *Provider
	// AllowedTools — явный список разрешённых инструментов вида
	// <сервер>_<инструмент> или <сервер>_*. Пока пустой.
	AllowedTools []string
}

// Render собирает opencode.json для агента приложения.
func Render(opts Options) ([]byte, error) {
	var cfg map[string]any
	if err := json.Unmarshal(baseConfig, &cfg); err != nil {
		return nil, fmt.Errorf("базовые настройки агента: %w", err)
	}

	agents := cfg["agents"].(map[string]any)
	daily := agents[AgentID].(map[string]any)
	daily["system"] = instructions

	experimental := cfg["experimental"].(map[string]any)
	policies, _ := experimental["policies"].([]any)

	// Разрешённые инструменты идут после запретов: в OpenCode побеждает
	// последнее подходящее правило.
	for _, tool := range opts.AllowedTools {
		if !toolPattern.MatchString(tool) {
			return nil, fmt.Errorf("инструмент %q: ждали <сервер>_<инструмент>", tool)
		}
		allow := map[string]any{"action": tool, "resource": "*", "effect": "allow"}
		daily["permissions"] = append(daily["permissions"].([]any), allow)
		cfg["permissions"] = append(cfg["permissions"].([]any), allow)
	}

	// Пользоваться можно только своим провайдером. Любой другой, найденный
	// в каталоге или в окружении, OpenCode не покажет.
	policies = append(policies, map[string]any{"action": "provider.use", "resource": "*", "effect": "deny"})
	if p := opts.Provider; p != nil {
		if err := p.validate(); err != nil {
			return nil, err
		}
		name := p.Name
		if name == "" {
			name = p.ID
		}
		provider := map[string]any{
			"name":     name,
			"package":  "@opencode/ai/providers/openai-compatible",
			"settings": map[string]any{"baseURL": p.BaseURL},
			"models":   map[string]any{p.ModelID: map[string]any{"name": p.ModelID}},
		}
		if p.KeyEnv != "" {
			provider["env"] = []string{p.KeyEnv}
		}
		cfg["providers"] = map[string]any{p.ID: provider}
		cfg["model"] = p.ID + "/" + p.ModelID
		daily["model"] = p.ID + "/" + p.ModelID
		policies = append(policies, map[string]any{"action": "provider.use", "resource": p.ID, "effect": "allow"})
	}
	experimental["policies"] = policies

	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}
