package agent

import (
	"context"
	"reflect"
	"sync"
	"time"

	"daily-agent/internal/agent/stubmodel"
)

// Manager — агент приложения: надзор за OpenCode, который можно перезапустить
// с другими моделями. Сервисы окна берут у него клиента, состояние и модели.
//
// Провайдеры живут в настройках OpenCode, а их ключи — в окружении его
// процесса, поэтому смена провайдеров или ключей — это перезапуск сервера.
// Адрес и пароль после него новые. Выбор модели из уже подключённых
// перезапуска не требует: модель задаётся каждой сессии.
type Manager struct {
	base Config

	// switchMu выстраивает в очередь запуск, остановку и смену модели.
	switchMu sync.Mutex
	// notifyMu выстраивает уведомления, чтобы последнее было свежим.
	notifyMu sync.Mutex

	mu          sync.Mutex
	sup         *Supervisor
	setup       *Setup
	listeners   []func(Status)
	ctx         context.Context
	started     bool
	stopped     bool
	switching   bool
	switchSince time.Time
	binary      string

	// MCP: служебная заглушка модели, прогрев после запуска и состояние серверов.
	discovery  *stubmodel.Server
	discoverMu sync.Mutex
	// warming — сервер, для которого идёт прогрев; warmed — для которого он
	// закончен. Пока не закончен, агент «запускается».
	warming *Supervisor
	warmed  *Supervisor
	mcp     []MCPState
}

// NewManager создаёт агента. base — настройки надзора без моделей,
// setup — модели или nil. Сервер запускается методом Start.
func NewManager(base Config, setup *Setup) *Manager {
	m := &Manager{base: base, setup: normalizeSetup(setup)}
	m.sup = m.newSupervisor(m.setup)
	return m
}

func normalizeSetup(s *Setup) *Setup {
	if s == nil {
		return &Setup{}
	}
	return s
}

// serverConfig — то, что требует перезапуска сервера: провайдеры и ключи.
func (m *Manager) serverConfig(s *Setup) Config {
	cfg := m.base
	cfg.Options = s.Options
	cfg.Options.AllowedTools = append(append([]string(nil), m.base.Options.AllowedTools...), s.Options.AllowedTools...)
	if s.Work != nil {
		cfg.Options.Default = s.Work.String()
	}
	if len(s.Options.MCP) > 0 {
		cfg.Options.Discovery = m.discoveryProvider()
	}
	cfg.ExtraEnv = append(append([]string(nil), m.base.ExtraEnv...), s.Env...)
	return cfg
}

// sameServer — новые модели не требуют перезапуска: те же провайдеры и
// ключи. Модель по умолчанию не в счёт: сессии получают модель явно.
func sameServer(a, b Config) bool {
	a.Options.Default, b.Options.Default = "", ""
	return reflect.DeepEqual(a.Options, b.Options) && reflect.DeepEqual(a.ExtraEnv, b.ExtraEnv)
}

func (m *Manager) newSupervisor(s *Setup) *Supervisor {
	sup := NewSupervisor(m.serverConfig(s))
	sup.OnChange(func(st Status) {
		m.mu.Lock()
		current := sup == m.sup
		if st.Binary != "" {
			m.binary = st.Binary
		}
		// После каждого запуска MCP-серверы подключаются заново: прогрев.
		warm := current && st.State == StateReady && len(m.setup.Options.MCP) > 0 && m.warming != sup && m.warmed != sup
		if warm {
			m.warming = sup
		}
		if current && st.State != StateReady {
			m.mcp = nil
			m.warmed = nil
		}
		m.mu.Unlock()
		if warm {
			go m.warmup(sup)
		}
		// Прежний сервер при смене модели останавливается молча.
		if current {
			m.notify()
		}
	})
	return sup
}

// notify сообщает подписчикам текущее состояние. Состояние читается заново
// под замком: если уведомления идут из разных потоков, последнее не устареет.
func (m *Manager) notify() {
	m.notifyMu.Lock()
	defer m.notifyMu.Unlock()
	st := m.Status()
	m.mu.Lock()
	listeners := append([]func(Status){}, m.listeners...)
	m.mu.Unlock()
	for _, l := range listeners {
		l(st)
	}
}

// OnChange подписывает на смену состояния, в том числе при смене модели.
func (m *Manager) OnChange(fn func(Status)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.listeners = append(m.listeners, fn)
}

// Start запускает сервер в фоне.
func (m *Manager) Start(ctx context.Context) {
	m.switchMu.Lock()
	defer m.switchMu.Unlock()
	m.mu.Lock()
	if m.started || m.stopped {
		m.mu.Unlock()
		return
	}
	m.started = true
	m.ctx = ctx
	sup := m.sup
	m.mu.Unlock()
	sup.Start(ctx)
}

// Stop останавливает сервер и ждёт завершения процесса. После Stop агент
// больше не запускается.
func (m *Manager) Stop() {
	m.switchMu.Lock()
	defer m.switchMu.Unlock()
	m.mu.Lock()
	m.stopped = true
	sup := m.sup
	discovery := m.discovery
	m.mu.Unlock()
	sup.Stop()
	if discovery != nil {
		discovery.Close()
	}
}

// SetModel ставит одну свою модель для всех задач. nil — модели нет:
// сервер работает, но агент не ответит.
func (m *Manager) SetModel(model *Model) {
	if model == nil {
		m.SetSetup(nil)
		return
	}
	m.SetSetup(model.Setup())
}

// SetSetup меняет модели агента. Если изменились провайдеры или ключи,
// запущенный сервер перезапускается; если только выбор модели — нет.
func (m *Manager) SetSetup(setup *Setup) {
	setup = normalizeSetup(setup)
	m.switchMu.Lock()
	defer m.switchMu.Unlock()
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return
	}
	same := sameServer(m.serverConfig(m.setup), m.serverConfig(setup))
	if same {
		m.setup = setup
		m.mu.Unlock()
		m.notify()
		return
	}
	m.mu.Unlock()
	next := m.newSupervisor(setup)
	m.mu.Lock()
	old := m.sup
	m.sup = next
	m.setup = setup
	started := m.started
	if started {
		m.switching = true
		m.switchSince = time.Now()
	}
	ctx := m.ctx
	m.mu.Unlock()
	if !started {
		return
	}
	m.notify()
	// Прежний сервер останавливается до запуска нового: у них общий профиль.
	old.Stop()
	next.Start(ctx)
	m.mu.Lock()
	m.switching = false
	m.mu.Unlock()
	m.notify()
}

// Status возвращает состояние сервера. Пока меняется модель — «запускается».
func (m *Manager) Status() Status {
	m.mu.Lock()
	sup, switching, since, binary := m.sup, m.switching, m.switchSince, m.binary
	warming := len(m.setup.Options.MCP) > 0 && m.warmed != m.sup
	m.mu.Unlock()
	st := sup.Status()
	if warming && st.State == StateReady {
		st.State = StateStarting
	}
	if switching {
		st.State = StateStarting
		st.Error = ""
		st.Since = since
	}
	if st.Binary == "" {
		st.Binary = binary
	}
	st.ModelConfigured = m.ModelFor(false) != nil
	st.PersonalConfigured = m.ModelFor(true) != nil
	return st
}

// Client возвращает клиента запущенного сервера.
func (m *Manager) Client() (*Client, error) {
	m.mu.Lock()
	sup, switching := m.sup, m.switching
	m.mu.Unlock()
	if switching {
		return nil, ErrNotReady
	}
	return sup.Client()
}

// WaitReady ждёт, пока сервер выйдет на связь.
func (m *Manager) WaitReady(ctx context.Context) error {
	m.mu.Lock()
	sup := m.sup
	m.mu.Unlock()
	return sup.WaitReady(ctx)
}

// Workspace — рабочая папка агента.
func (m *Manager) Workspace() string { return m.base.Profile.Workspace() }

// Model — модель для рабочих задач или nil, если она не выбрана.
func (m *Manager) Model() *ModelRef { return m.ModelFor(false) }

// ModelFor — модель для рабочих или личных задач. Для личных без своей
// модели — та же, что для рабочих. Nil — модели нет.
func (m *Manager) ModelFor(personal bool) *ModelRef {
	m.mu.Lock()
	defer m.mu.Unlock()
	ref := m.setup.Work
	if personal && m.setup.Personal != nil {
		ref = m.setup.Personal
	}
	if ref == nil {
		return nil
	}
	out := *ref
	return &out
}
