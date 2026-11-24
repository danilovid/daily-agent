package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	agentprofile "daily-agent/agent"
	"daily-agent/internal/agent"
	"daily-agent/internal/store"
)

// Secrets — хранилище ключей. В приложении это Связка ключей macOS.
type Secrets interface {
	Get(ctx context.Context, service, account string) (string, error)
	Set(ctx context.Context, service, account, secret string) error
	Delete(ctx context.Context, service, account string) error
}

// AgentControl — агент, которому можно сменить модели.
type AgentControl interface {
	AgentBackend
	SetSetup(setup *agent.Setup)
}

// Откуда взята своя модель по адресу.
const (
	ModelSourceSettings = "settings" // экран настроек
	ModelSourceEnv      = "env"      // переменные окружения, для разработки
	ModelSourceStub     = "stub"     // заглушка для разработки
	ModelSourceNone     = "none"     // не задана
)

// Откуда провайдер.
const (
	ProviderSourceOwn      = "own"      // своя модель по адресу, «Селфхост-API»
	ProviderSourceOpenCode = "opencode" // из настроек OpenCode пользователя
	ProviderSourceCatalog  = "catalog"  // из встроенного каталога OpenCode
)

// ModelCheckPrompt — вопрос, которым проверяется модель. Ответ в окно не идёт.
const ModelCheckPrompt = "Ответь одним словом: готов."

// catalogEntry — подключённый провайдер каталога. Ключ — в Связке ключей.
type catalogEntry struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Env — переменная окружения, из которой OpenCode берёт ключ.
	Env string `json:"env"`
}

// providerInfo — провайдер, которым может пользоваться агент.
type providerInfo struct {
	ID       string
	Name     string
	Source   string
	Selfhost bool
	KeySaved bool
	Dropped  int
	Models   int
}

// sources — всё, из чего собираются модели агента.
type sources struct {
	setup     *agent.Setup
	manual    *agent.Model
	source    string
	user      agent.UserConfig
	providers []providerInfo
	catalog   []catalogEntry
	keyed     []string // провайдеры из настроек OpenCode, у которых ключ в Связке
	work      string
	personal  string
	notes     []string
}

func (s sources) provider(id string) (providerInfo, bool) {
	for _, p := range s.providers {
		if p.ID == id {
			return p, true
		}
	}
	return providerInfo{}, false
}

// LoadSetup собирает модели агента: своя модель по адресу (из настроек или,
// без них, из переменных окружения), провайдеры из настроек OpenCode
// пользователя, подключённые провайдеры каталога и выбор моделей для рабочих
// и личных задач. Ключи — из Связки ключей.
func LoadSetup(ctx context.Context, st *store.Store, secrets Secrets, user agent.UserConfig) (*agent.Setup, error) {
	src, err := loadSources(ctx, st, secrets, user, true)
	if err != nil {
		return nil, err
	}
	return src.setup, nil
}

// loadSources читает настройки. withKeys — читать ключи из Связки ключей для
// процесса OpenCode; без них только признаки, что ключи сохранены.
func loadSources(ctx context.Context, st *store.Store, secrets Secrets, user agent.UserConfig, withKeys bool) (sources, error) {
	values, err := st.Settings(ctx, "agent.")
	if err != nil {
		return sources{}, err
	}
	src := sources{user: user, source: ModelSourceNone, work: values[store.SettingWorkModel], personal: values[store.SettingPersonalModel]}
	opts := agentprofile.Options{}
	var env []string
	readKey := func(account string) (string, bool) {
		if !withKeys {
			return "", true
		}
		key, err := secrets.Get(ctx, agent.KeychainService, account)
		if err != nil {
			if !errors.Is(err, agent.ErrKeyNotFound) {
				src.notes = append(src.notes, "Не удалось прочитать Связку ключей: "+err.Error())
			}
			return "", false
		}
		return key, key != ""
	}

	// Своя модель по адресу: из настроек, а без них — из окружения.
	m, err := agent.ModelFrom(ctx, keyReader{secrets, withKeys}, values[store.SettingModelURL], values[store.SettingModelID])
	if err == nil {
		src.manual, src.source = &m, ModelSourceSettings
	} else if errors.Is(err, agent.ErrNoModel) {
		if m, err := agent.ModelFromEnv(ctx, keyReader{secrets, withKeys}); err == nil {
			src.manual, src.source = &m, ModelSourceEnv
		}
	} else {
		src.notes = append(src.notes, "Своя модель: "+err.Error())
	}
	if src.manual != nil {
		opts.Provider = src.manual.Provider()
		env = append(env, src.manual.Env()...)
		src.providers = append(src.providers, providerInfo{
			ID: agent.ProviderID, Name: "Селфхост-API", Source: ProviderSourceOwn, Selfhost: true, Models: 1,
			KeySaved: src.manual.APIKey != "",
		})
	}

	// Провайдеры из настроек OpenCode пользователя.
	_ = json.Unmarshal([]byte(values[store.SettingProviderKeys]), &src.keyed)
	for _, p := range user.Providers {
		if opts.Imported == nil {
			opts.Imported = map[string]map[string]any{}
		}
		opts.Imported[p.ID] = p.Definition
		info := providerInfo{ID: p.ID, Name: p.Name, Source: ProviderSourceOpenCode, Selfhost: p.Selfhost, Dropped: p.Dropped, Models: len(p.Models)}
		if contains(src.keyed, p.ID) {
			if key, ok := readKey(agent.ProviderKeyAccount(p.ID)); ok {
				info.KeySaved = true
				if withKeys {
					env = append(env, p.KeyEnv+"="+key)
				}
			}
		}
		src.providers = append(src.providers, info)
	}

	// Подключённые провайдеры каталога.
	_ = json.Unmarshal([]byte(values[store.SettingCatalog]), &src.catalog)
	for _, c := range src.catalog {
		if _, dup := src.provider(c.ID); dup || !agentprofile.ValidProviderID(c.ID) {
			continue
		}
		opts.Catalog = append(opts.Catalog, c.ID)
		info := providerInfo{ID: c.ID, Name: c.Name, Source: ProviderSourceCatalog}
		if key, ok := readKey(agent.ProviderKeyAccount(c.ID)); ok {
			info.KeySaved = true
			if withKeys {
				env = append(env, c.Env+"="+key)
			}
		}
		src.providers = append(src.providers, info)
	}

	// Выбор моделей. Рабочая — только селфхост.
	setup := &agent.Setup{Options: opts, Env: env}
	if ref, ok := agent.ParseModelRef(src.work); ok {
		if p, found := src.provider(ref.ProviderID); !found {
			src.notes = append(src.notes, fmt.Sprintf("Модель для рабочих задач %s больше недоступна: нет провайдера %s.", ref, ref.ProviderID))
		} else if !p.Selfhost {
			src.notes = append(src.notes, fmt.Sprintf("Модель для рабочих задач %s не селфхост и не используется.", ref))
		} else {
			setup.Work = ref
		}
	} else if src.work == "" && src.manual != nil {
		setup.Work = src.manual.Ref()
	}
	if ref, ok := agent.ParseModelRef(src.personal); ok {
		if _, found := src.provider(ref.ProviderID); found {
			setup.Personal = ref
		} else {
			src.notes = append(src.notes, fmt.Sprintf("Модель для личных задач %s больше недоступна: нет провайдера %s.", ref, ref.ProviderID))
		}
	}
	// MCP-серверы: из настроек OpenCode пользователя и свои, токены — из Связки ключей.
	entries, mcpEnv, err := mcpEntries(ctx, st, secrets, user, withKeys)
	if err != nil {
		return sources{}, err
	}
	mcpOptions(entries, setup)
	setup.Env = append(setup.Env, mcpEnv...)
	src.setup = setup
	return src, nil
}

// keyReader читает ключ своей модели, только если ключи нужны.
type keyReader struct {
	secrets  Secrets
	withKeys bool
}

func (k keyReader) Get(ctx context.Context, service, account string) (string, error) {
	if !k.withKeys {
		return "", agent.ErrKeyNotFound
	}
	return k.secrets.Get(ctx, service, account)
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// ModelSettingsView — своя модель по адресу. Ключ в окно не уходит, только
// признак, что он сохранён.
type ModelSettingsView struct {
	URL      string `json:"url"`
	ModelID  string `json:"modelId"`
	KeySaved bool   `json:"keySaved"`
	// Source: settings, env, stub или none.
	Source string `json:"source"`
}

// ProviderView — провайдер модели в настройках.
type ProviderView struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Source: own — своя модель по адресу, opencode — из настроек OpenCode,
	// catalog — облачный провайдер из каталога.
	Source   string `json:"source"`
	Selfhost bool   `json:"selfhost"`
	KeySaved bool   `json:"keySaved"`
	// Dropped — сколько полей с ключами и токенами не перенесено из настроек
	// OpenCode. Тогда ключ нужно сохранить здесь.
	Dropped int `json:"dropped"`
	Models  int `json:"models"`
}

// AgentSettingsView — раздел «Агент» в настройках.
type AgentSettingsView struct {
	Status AgentStatusView `json:"status"`
	// Model — своя модель по адресу.
	Model ModelSettingsView `json:"model"`
	// Work — модель для рабочих задач, провайдер/модель. Пусто — не выбрана.
	Work string `json:"work"`
	// Personal — модель для личных задач. Пусто — та же, что для рабочих.
	Personal  string         `json:"personal"`
	Providers []ProviderView `json:"providers"`
	// UserConfig — откуда взяты провайдеры OpenCode, например
	// ~/.config/opencode/opencode.json. Пусто — настроек OpenCode нет.
	UserConfig string `json:"userConfig"`
	// UserSkipped — провайдеры из настроек OpenCode, которые не удалось перенести.
	UserSkipped int `json:"userSkipped"`
	// Binary — путь к программе opencode, когда она найдена.
	Binary string `json:"binary"`
	// PinnedVersion — версия OpenCode, с которой работает приложение.
	PinnedVersion string `json:"pinnedVersion"`
	Workspace     string `json:"workspace"`
	// Note — пояснения: работает заглушка, модель недоступна и так далее.
	Note string `json:"note"`
}

// ModelSettingsInput — своя модель по адресу из окна.
type ModelSettingsInput struct {
	URL     string `json:"url"`
	ModelID string `json:"modelId"`
	// Key — новый ключ. Пусто — оставить прежний.
	Key string `json:"key"`
	// RemoveKey — удалить ключ из Связки ключей.
	RemoveKey bool `json:"removeKey"`
}

// ModelOptionView — модель в списке выбора.
type ModelOptionView struct {
	Ref          string `json:"ref"`
	Name         string `json:"name"`
	ProviderID   string `json:"providerId"`
	ProviderName string `json:"providerName"`
	// Selfhost — модель можно выбрать для рабочих задач.
	Selfhost bool `json:"selfhost"`
}

// ModelsInput — выбор моделей.
type ModelsInput struct {
	// Work — модель для рабочих задач, только селфхост. Пусто — не выбрана.
	Work string `json:"work"`
	// Personal — модель для личных задач. Пусто — та же, что для рабочих.
	Personal string `json:"personal"`
}

// CatalogOptionView — облачный провайдер из каталога, который можно подключить ключом.
type CatalogOptionView struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ProviderKeyInput — ключ провайдера: из каталога или из настроек OpenCode.
type ProviderKeyInput struct {
	ID  string `json:"id"`
	Key string `json:"key"`
}

// ConnectionCheck — итог проверки связи с OpenCode или моделью.
type ConnectionCheck struct {
	OK      bool   `json:"ok"`
	Version string `json:"version"`
	// Millis — сколько шёл ответ, в миллисекундах.
	Millis int64  `json:"millis"`
	Error  string `json:"error"`
	// CheckedAt — время проверки, ЧЧ:ММ.
	CheckedAt string `json:"checkedAt"`
}

// SettingsService — экран настроек. Пока раздел «Агент»: модели и связь с OpenCode.
type SettingsService struct {
	store   *store.Store
	secrets Secrets
	agent   AgentControl
	clock   Clock
	// userConfig читает провайдеров из настроек OpenCode пользователя.
	userConfig func() (agent.UserConfig, error)
	// stub — работает заглушка модели: настройки сохраняются, но вступят
	// в силу после запуска без неё.
	stub bool

	mu sync.Mutex
}

// NewSettingsService создаёт сервис настроек.
func NewSettingsService(st *store.Store, secrets Secrets, ag AgentControl, clock Clock, userConfig func() (agent.UserConfig, error), stub bool) *SettingsService {
	if userConfig == nil {
		userConfig = func() (agent.UserConfig, error) { return agent.UserConfig{}, nil }
	}
	return &SettingsService{store: st, secrets: secrets, agent: ag, clock: clock, userConfig: userConfig, stub: stub}
}

func (s *SettingsService) sources(ctx context.Context, withKeys bool) (sources, error) {
	user, err := s.userConfig()
	src, lerr := loadSources(ctx, s.store, s.secrets, user, withKeys)
	if lerr != nil {
		return src, lerr
	}
	if err != nil {
		src.notes = append(src.notes, "Не удалось прочитать настройки OpenCode: "+err.Error())
	}
	return src, nil
}

// apply перезапускает агента с новыми моделями, если это нужно.
func (s *SettingsService) apply(ctx context.Context) error {
	if s.stub {
		return nil
	}
	src, err := s.sources(ctx, true)
	if err != nil {
		return err
	}
	s.agent.SetSetup(src.setup)
	return nil
}

// Agent возвращает раздел «Агент».
func (s *SettingsService) Agent(ctx context.Context) (AgentSettingsView, error) {
	st := s.agent.Status()
	v := AgentSettingsView{
		Status:        AgentStatusFrom(st, s.clock),
		Binary:        displayPath(st.Binary),
		PinnedVersion: agent.PinnedVersion,
		Workspace:     displayPath(s.agent.Workspace()),
		Providers:     []ProviderView{},
	}
	src, err := s.sources(ctx, false)
	if err != nil {
		return v, err
	}
	values, err := s.store.Settings(ctx, "agent.model.")
	if err != nil {
		return v, err
	}
	v.Model = ModelSettingsView{URL: values[store.SettingModelURL], ModelID: values[store.SettingModelID], Source: src.source}
	if src.source == ModelSourceEnv {
		v.Model.URL, v.Model.ModelID = src.manual.BaseURL, src.manual.ModelID
	}
	key, err := s.secrets.Get(ctx, agent.KeychainService, agent.KeychainAccount)
	switch {
	case err == nil:
		v.Model.KeySaved = key != ""
	case errors.Is(err, agent.ErrKeyNotFound):
	default:
		src.notes = append(src.notes, "Не удалось прочитать Связку ключей: "+err.Error())
	}
	for _, p := range src.providers {
		if p.Source == ProviderSourceOwn {
			p.KeySaved = v.Model.KeySaved
		}
		v.Providers = append(v.Providers, ProviderView{
			ID: p.ID, Name: p.Name, Source: p.Source, Selfhost: p.Selfhost, KeySaved: p.KeySaved, Dropped: p.Dropped, Models: p.Models,
		})
	}
	if src.setup.Work != nil {
		v.Work = src.setup.Work.String()
	}
	if src.setup.Personal != nil {
		v.Personal = src.setup.Personal.String()
	}
	var paths []string
	for _, p := range src.user.Paths {
		paths = append(paths, displayPath(p))
	}
	v.UserConfig = strings.Join(paths, ", ")
	v.UserSkipped = src.user.Skipped

	switch {
	case s.stub:
		v.Model.Source = ModelSourceStub
		src.notes = append([]string{"Сейчас работает заглушка модели для разработки. Сохранённые настройки вступят в силу, когда приложение запустится без неё."}, src.notes...)
	case src.source == ModelSourceEnv:
		src.notes = append(src.notes, "Своя модель задана переменными окружения. Сохраните её здесь, чтобы она не зависела от них.")
	}
	v.Note = strings.Join(src.notes, " ")
	return v, nil
}

// Models возвращает модели подключённых провайдеров. Список даёт OpenCode,
// поэтому он должен быть на связи.
func (s *SettingsService) Models(ctx context.Context) ([]ModelOptionView, error) {
	c, err := s.server()
	if err != nil {
		return nil, err
	}
	src, err := s.sources(ctx, false)
	if err != nil {
		return nil, err
	}
	models, err := c.ListModels(ctx)
	if err != nil {
		return nil, fmt.Errorf("список моделей OpenCode: %w", err)
	}
	out := []ModelOptionView{}
	for _, m := range models {
		p, ok := src.provider(m.ProviderID)
		if !ok {
			continue
		}
		name := m.Name
		if name == "" {
			name = m.ID
		}
		out = append(out, ModelOptionView{Ref: m.Ref().String(), Name: name, ProviderID: p.ID, ProviderName: p.Name, Selfhost: p.Selfhost})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ProviderName != out[j].ProviderName {
			return out[i].ProviderName < out[j].ProviderName
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// SaveModels сохраняет выбор моделей. Для рабочих задач — только селфхост.
// Перезапуск OpenCode не нужен: модель задаётся каждой сессии.
func (s *SettingsService) SaveModels(ctx context.Context, in ModelsInput) (AgentSettingsView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	src, err := s.sources(ctx, false)
	if err != nil {
		return AgentSettingsView{}, err
	}
	work, personal := strings.TrimSpace(in.Work), strings.TrimSpace(in.Personal)
	if work != "" {
		ref, ok := agent.ParseModelRef(work)
		if !ok {
			return AgentSettingsView{}, fmt.Errorf("модель %q: ждали провайдер/модель", work)
		}
		p, found := src.provider(ref.ProviderID)
		if !found {
			return AgentSettingsView{}, fmt.Errorf("нет подключённого провайдера %s", ref.ProviderID)
		}
		if !p.Selfhost {
			return AgentSettingsView{}, fmt.Errorf("%s — облачная модель. Для рабочих задач можно выбрать только селфхост: рабочие данные не уходят в облако", ref)
		}
		work = ref.String()
	}
	if personal != "" {
		ref, ok := agent.ParseModelRef(personal)
		if !ok {
			return AgentSettingsView{}, fmt.Errorf("модель %q: ждали провайдер/модель", personal)
		}
		if _, found := src.provider(ref.ProviderID); !found {
			return AgentSettingsView{}, fmt.Errorf("нет подключённого провайдера %s", ref.ProviderID)
		}
		personal = ref.String()
	}
	// Своя модель без явного выбора остаётся рабочей; «не выбрана» — пробел.
	stored := work
	if stored == "" {
		stored = "-"
	}
	if err := s.store.SetSettings(ctx, map[string]string{store.SettingWorkModel: stored, store.SettingPersonalModel: personal}); err != nil {
		return AgentSettingsView{}, err
	}
	if err := s.apply(ctx); err != nil {
		return AgentSettingsView{}, err
	}
	return s.Agent(ctx)
}

// CatalogProviders возвращает облачных провайдеров каталога OpenCode,
// которых можно подключить ключом. Уже подключённых в списке нет.
func (s *SettingsService) CatalogProviders(ctx context.Context) ([]CatalogOptionView, error) {
	c, err := s.server()
	if err != nil {
		return nil, err
	}
	src, err := s.sources(ctx, false)
	if err != nil {
		return nil, err
	}
	list, err := c.Integrations(ctx)
	if err != nil {
		return nil, fmt.Errorf("каталог OpenCode: %w", err)
	}
	out := []CatalogOptionView{}
	for _, in := range list {
		if _, connected := src.provider(in.ID); connected || len(in.KeyEnv) == 0 || !agentprofile.ValidProviderID(in.ID) {
			continue
		}
		name := in.Name
		if name == "" {
			name = in.ID
		}
		out = append(out, CatalogOptionView{ID: in.ID, Name: name})
	}
	sort.SliceStable(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, nil
}

// SaveProviderKey сохраняет ключ провайдера в Связке ключей: провайдера из
// настроек OpenCode или облачного из каталога, который так и подключается.
// OpenCode перезапускается с ключом.
func (s *SettingsService) SaveProviderKey(ctx context.Context, in ProviderKeyInput) (AgentSettingsView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, key := strings.TrimSpace(in.ID), strings.TrimSpace(in.Key)
	if err := agent.ValidateKey(key); err != nil {
		return AgentSettingsView{}, err
	}
	src, err := s.sources(ctx, false)
	if err != nil {
		return AgentSettingsView{}, err
	}
	values := map[string]string{}
	if p, found := src.provider(id); found {
		switch p.Source {
		case ProviderSourceOwn:
			return AgentSettingsView{}, errors.New("ключ своей модели меняется в окне «Селфхост-API»")
		case ProviderSourceOpenCode:
			if !contains(src.keyed, id) {
				raw, _ := json.Marshal(append(src.keyed, id))
				values[store.SettingProviderKeys] = string(raw)
			}
		}
	} else {
		entry, err := s.catalogEntry(ctx, id)
		if err != nil {
			return AgentSettingsView{}, err
		}
		raw, _ := json.Marshal(append(src.catalog, entry))
		values[store.SettingCatalog] = string(raw)
	}
	// Сначала ключ: если Связка ключей откажет, настройки останутся прежними.
	if err := s.secrets.Set(ctx, agent.KeychainService, agent.ProviderKeyAccount(id), key); err != nil {
		return AgentSettingsView{}, err
	}
	if len(values) > 0 {
		if err := s.store.SetSettings(ctx, values); err != nil {
			return AgentSettingsView{}, err
		}
	}
	if err := s.apply(ctx); err != nil {
		return AgentSettingsView{}, err
	}
	return s.Agent(ctx)
}

// catalogEntry находит провайдера в каталоге OpenCode.
func (s *SettingsService) catalogEntry(ctx context.Context, id string) (catalogEntry, error) {
	c, err := s.server()
	if err != nil {
		return catalogEntry{}, err
	}
	list, err := c.Integrations(ctx)
	if err != nil {
		return catalogEntry{}, fmt.Errorf("каталог OpenCode: %w", err)
	}
	for _, in := range list {
		if in.ID != id {
			continue
		}
		if len(in.KeyEnv) == 0 || !agentprofile.ValidProviderID(id) {
			return catalogEntry{}, fmt.Errorf("%s подключается только входом через браузер, приложение так пока не умеет", in.Name)
		}
		name := in.Name
		if name == "" {
			name = id
		}
		return catalogEntry{ID: id, Name: name, Env: in.KeyEnv[0]}, nil
	}
	return catalogEntry{}, fmt.Errorf("в каталоге OpenCode нет провайдера %s", id)
}

// RemoveProvider отключает облачного провайдера каталога или удаляет ключ
// провайдера из настроек OpenCode. Выбор моделей этого провайдера сбрасывается.
func (s *SettingsService) RemoveProvider(ctx context.Context, id string) (AgentSettingsView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	src, err := s.sources(ctx, false)
	if err != nil {
		return AgentSettingsView{}, err
	}
	p, found := src.provider(id)
	if !found {
		return AgentSettingsView{}, fmt.Errorf("нет подключённого провайдера %s", id)
	}
	values := map[string]string{}
	switch p.Source {
	case ProviderSourceOwn:
		return AgentSettingsView{}, errors.New("своя модель меняется в окне «Селфхост-API»")
	case ProviderSourceCatalog:
		var rest []catalogEntry
		for _, c := range src.catalog {
			if c.ID != id {
				rest = append(rest, c)
			}
		}
		raw, _ := json.Marshal(rest)
		values[store.SettingCatalog] = string(raw)
		if ref, ok := agent.ParseModelRef(src.work); ok && ref.ProviderID == id {
			values[store.SettingWorkModel] = "-"
		}
		if ref, ok := agent.ParseModelRef(src.personal); ok && ref.ProviderID == id {
			values[store.SettingPersonalModel] = ""
		}
	case ProviderSourceOpenCode:
		var rest []string
		for _, k := range src.keyed {
			if k != id {
				rest = append(rest, k)
			}
		}
		raw, _ := json.Marshal(rest)
		values[store.SettingProviderKeys] = string(raw)
	}
	if err := s.secrets.Delete(ctx, agent.KeychainService, agent.ProviderKeyAccount(id)); err != nil {
		return AgentSettingsView{}, err
	}
	if err := s.store.SetSettings(ctx, values); err != nil {
		return AgentSettingsView{}, err
	}
	if err := s.apply(ctx); err != nil {
		return AgentSettingsView{}, err
	}
	return s.Agent(ctx)
}

// SaveModel проверяет и сохраняет свою модель по адресу (селфхост-API) и
// перезапускает OpenCode с ней. Адрес и модель — в базе приложения, ключ —
// в Связке ключей.
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
	values := map[string]string{store.SettingModelURL: url, store.SettingModelID: modelID}
	// Рабочая модель была своей или не выбрана — теперь это своя новая модель.
	prev, _, _ := s.store.Setting(ctx, store.SettingWorkModel)
	if prev == "" || prev == "-" || strings.HasPrefix(prev, agent.ProviderID+"/") {
		values[store.SettingWorkModel] = ""
	}
	if err := s.store.SetSettings(ctx, values); err != nil {
		return AgentSettingsView{}, err
	}
	if err := s.apply(ctx); err != nil {
		return AgentSettingsView{}, err
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

func (s *SettingsService) server() (*agent.Client, error) {
	st := s.agent.Status()
	if st.State != agent.StateReady {
		return nil, errors.New(serverUnavailable(st))
	}
	return s.agent.Client()
}

// CheckConnection проверяет, что OpenCode на связи, и меряет время ответа.
func (s *SettingsService) CheckConnection(ctx context.Context) ConnectionCheck {
	res := ConnectionCheck{CheckedAt: s.clock.now().Format("15:04")}
	c, err := s.server()
	if err != nil {
		res.Error = err.Error()
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

// CheckModel задаёт модели для рабочих или личных задач короткий вопрос через
// OpenCode и меряет время ответа. Текст ответа в окно не идёт: только итог и время.
func (s *SettingsService) CheckModel(ctx context.Context, personal bool) ConnectionCheck {
	res := ConnectionCheck{CheckedAt: s.clock.now().Format("15:04")}
	if reason := unavailableText(s.agent, personal); reason != "" {
		res.Error = reason
		return res
	}
	c, err := s.server()
	if err != nil {
		res.Error = err.Error()
		return res
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	began := time.Now()
	reply, err := c.Ask(ctx, agent.SessionOptions{
		Title:     "Проверка модели",
		Directory: s.agent.Workspace(),
		Model:     s.agent.ModelFor(personal),
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
