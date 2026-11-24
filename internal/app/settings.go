package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"daily-agent/internal/agent"
	"daily-agent/internal/store"
)

// Secrets — хранилище ключей. В приложении это Связка ключей macOS.
type Secrets interface {
	Get(ctx context.Context, service, account string) (string, error)
	Set(ctx context.Context, service, account, secret string) error
	Delete(ctx context.Context, service, account string) error
}

// AgentControl — агент, которому можно сменить модель.
type AgentControl interface {
	AgentBackend
	SetModel(model *agent.Model)
}

// Откуда взята модель агента.
const (
	ModelSourceSettings = "settings" // экран настроек
	ModelSourceEnv      = "env"      // переменные окружения, для разработки
	ModelSourceStub     = "stub"     // заглушка для разработки
	ModelSourceNone     = "none"     // не настроена
)

// ModelCheckPrompt — вопрос, которым проверяется модель. Ответ в окно не идёт.
const ModelCheckPrompt = "Ответь одним словом: готов."

// LoadModel находит модель агента: сначала настройки приложения, затем
// переменные окружения. Ключ — из Связки ключей. nil — модель не настроена.
func LoadModel(ctx context.Context, st *store.Store, secrets agent.KeyReader) (*agent.Model, string, error) {
	values, err := st.Settings(ctx, "agent.model.")
	if err != nil {
		return nil, ModelSourceNone, err
	}
	m, err := agent.ModelFrom(ctx, secrets, values[store.SettingModelURL], values[store.SettingModelID])
	if err == nil {
		return &m, ModelSourceSettings, nil
	}
	if !errors.Is(err, agent.ErrNoModel) {
		return nil, ModelSourceNone, err
	}
	m, err = agent.ModelFromEnv(ctx, secrets)
	if err == nil {
		return &m, ModelSourceEnv, nil
	}
	if errors.Is(err, agent.ErrNoModel) {
		return nil, ModelSourceNone, nil
	}
	return nil, ModelSourceNone, err
}

// ModelSettingsView — модель агента для окна. Ключ в окно не уходит, только
// признак, что он сохранён.
type ModelSettingsView struct {
	URL      string `json:"url"`
	ModelID  string `json:"modelId"`
	KeySaved bool   `json:"keySaved"`
	// Source: settings, env, stub или none.
	Source string `json:"source"`
}

// AgentSettingsView — раздел «Агент» в настройках.
type AgentSettingsView struct {
	Status AgentStatusView   `json:"status"`
	Model  ModelSettingsView `json:"model"`
	// Binary — путь к программе opencode, когда она найдена.
	Binary string `json:"binary"`
	// PinnedVersion — версия OpenCode, с которой работает приложение.
	PinnedVersion string `json:"pinnedVersion"`
	Workspace     string `json:"workspace"`
	// Note — пояснение к модели, например что сейчас работает заглушка.
	Note string `json:"note"`
}

// ModelSettingsInput — новые настройки модели из окна.
type ModelSettingsInput struct {
	URL     string `json:"url"`
	ModelID string `json:"modelId"`
	// Key — новый ключ. Пусто — оставить прежний.
	Key string `json:"key"`
	// RemoveKey — удалить ключ из Связки ключей.
	RemoveKey bool `json:"removeKey"`
}

// ConnectionCheck — итог проверки связи с OpenCode.
type ConnectionCheck struct {
	OK      bool   `json:"ok"`
	Version string `json:"version"`
	// Millis — сколько шёл ответ, в миллисекундах.
	Millis int64  `json:"millis"`
	Error  string `json:"error"`
	// CheckedAt — время проверки, ЧЧ:ММ.
	CheckedAt string `json:"checkedAt"`
}

// SettingsService — экран настроек. Пока раздел «Агент»: модель и связь с OpenCode.
type SettingsService struct {
	store   *store.Store
	secrets Secrets
	agent   AgentControl
	clock   Clock
	// stub — работает заглушка модели: настройки сохраняются, но вступят
	// в силу после запуска без неё.
	stub bool

	mu sync.Mutex
}

// NewSettingsService создаёт сервис настроек.
func NewSettingsService(st *store.Store, secrets Secrets, ag AgentControl, clock Clock, stub bool) *SettingsService {
	return &SettingsService{store: st, secrets: secrets, agent: ag, clock: clock, stub: stub}
}

// Agent возвращает раздел «Агент».
func (s *SettingsService) Agent(ctx context.Context) (AgentSettingsView, error) {
	st := s.agent.Status()
	v := AgentSettingsView{
		Status:        AgentStatusFrom(st, s.clock),
		Binary:        displayPath(st.Binary),
		PinnedVersion: agent.PinnedVersion,
		Workspace:     displayPath(s.agent.Workspace()),
	}
	values, err := s.store.Settings(ctx, "agent.model.")
	if err != nil {
		return v, err
	}
	v.Model = ModelSettingsView{URL: values[store.SettingModelURL], ModelID: values[store.SettingModelID], Source: ModelSourceSettings}
	if v.Model.URL == "" || v.Model.ModelID == "" {
		v.Model.Source = ModelSourceNone
		if url, id := strings.TrimSpace(os.Getenv(agent.EnvModelURL)), strings.TrimSpace(os.Getenv(agent.EnvModelID)); url != "" && id != "" {
			v.Model = ModelSettingsView{URL: url, ModelID: id, Source: ModelSourceEnv}
		}
	}
	key, err := s.secrets.Get(ctx, agent.KeychainService, agent.KeychainAccount)
	switch {
	case err == nil:
		v.Model.KeySaved = key != ""
	case errors.Is(err, agent.ErrKeyNotFound):
	default:
		v.Note = "Не удалось прочитать Связку ключей: " + err.Error()
	}
	switch {
	case s.stub:
		v.Model.Source = ModelSourceStub
		v.Note = "Сейчас работает заглушка модели для разработки. Сохранённые настройки вступят в силу, когда приложение запустится без неё."
	case v.Model.Source == ModelSourceEnv:
		v.Note = "Модель задана переменными окружения. Сохраните её здесь, чтобы она не зависела от них."
	}
	return v, nil
}

// SaveModel проверяет и сохраняет модель агента и перезапускает OpenCode
// с ней. Адрес и модель — в базе приложения, ключ — в Связке ключей.
func (s *SettingsService) SaveModel(ctx context.Context, in ModelSettingsInput) (AgentSettingsView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	url, err := agent.NormalizeModelURL(in.URL)
	if err != nil {
		return AgentSettingsView{}, err
	}
	modelID := strings.TrimSpace(in.ModelID)
	if err := agent.ValidateModelID(modelID); err != nil {
		return AgentSettingsView{}, err
	}
	key := strings.TrimSpace(in.Key)
	if key != "" {
		if in.RemoveKey {
			return AgentSettingsView{}, errors.New("введите новый ключ или удалите прежний, но не всё сразу")
		}
		if err := agent.ValidateKey(key); err != nil {
			return AgentSettingsView{}, err
		}
	}

	before, _, err := LoadModel(ctx, s.store, s.secrets)
	if err != nil {
		return AgentSettingsView{}, err
	}
	// Сначала ключ: если Связка ключей откажет, настройки останутся прежними.
	switch {
	case key != "":
		if err := s.secrets.Set(ctx, agent.KeychainService, agent.KeychainAccount, key); err != nil {
			return AgentSettingsView{}, err
		}
	case in.RemoveKey:
		if err := s.secrets.Delete(ctx, agent.KeychainService, agent.KeychainAccount); err != nil {
			return AgentSettingsView{}, err
		}
	}
	if err := s.store.SetSettings(ctx, map[string]string{
		store.SettingModelURL: url,
		store.SettingModelID:  modelID,
	}); err != nil {
		return AgentSettingsView{}, err
	}
	after, _, err := LoadModel(ctx, s.store, s.secrets)
	if err != nil {
		return AgentSettingsView{}, err
	}
	if !s.stub && !sameModel(before, after) {
		s.agent.SetModel(after)
	}
	return s.Agent(ctx)
}

// displayPath сокращает домашнюю папку до «~», как в макете.
func displayPath(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" || p == "" {
		return p
	}
	if p == home {
		return "~"
	}
	if rest, ok := strings.CutPrefix(p, home+string(os.PathSeparator)); ok {
		return "~/" + rest
	}
	return p
}

func sameModel(a, b *agent.Model) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// CheckConnection проверяет, что OpenCode на связи, и меряет время ответа.
func (s *SettingsService) CheckConnection(ctx context.Context) ConnectionCheck {
	res := ConnectionCheck{CheckedAt: s.clock.now().Format("15:04")}
	c, err := s.agent.Client()
	if err != nil {
		res.Error = serverUnavailable(s.agent.Status())
		return res
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	began := time.Now()
	info, err := c.Info(ctx)
	res.Millis = time.Since(began).Milliseconds()
	if err != nil {
		res.Error = fmt.Sprintf("OpenCode не ответил: %v", err)
		return res
	}
	res.OK, res.Version = true, info.Version
	return res
}

// CheckModel задаёт модели короткий вопрос через OpenCode и меряет время
// ответа. Текст ответа в окно не идёт: только итог и время.
func (s *SettingsService) CheckModel(ctx context.Context) ConnectionCheck {
	res := ConnectionCheck{CheckedAt: s.clock.now().Format("15:04")}
	if reason := unavailableText(s.agent); reason != "" {
		res.Error = reason
		return res
	}
	c, err := s.agent.Client()
	if err != nil {
		res.Error = serverUnavailable(s.agent.Status())
		return res
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	began := time.Now()
	reply, err := c.Ask(ctx, agent.SessionOptions{
		Title:     "Проверка модели",
		Directory: s.agent.Workspace(),
		Model:     s.agent.Model(),
	}, ModelCheckPrompt)
	res.Millis = time.Since(began).Milliseconds()
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		res.Error = "Модель не ответила за 2 минуты"
	case err != nil:
		res.Error = "Модель не ответила: " + strings.TrimPrefix(err.Error(), agent.ErrAgentFailed.Error()+": ")
	case strings.TrimSpace(reply.Text) == "":
		res.Error = "Модель вернула пустой ответ"
	default:
		res.OK = true
	}
	return res
}
