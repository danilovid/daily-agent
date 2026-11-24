package agent

import (
	"context"
	"sync"
	"time"
)

// Manager — агент приложения: надзор за OpenCode, который можно перезапустить
// с другой моделью. Сервисы окна берут у него клиента, состояние и модель.
//
// Модель живёт в настройках OpenCode и в окружении его процесса, поэтому
// смена модели — это перезапуск сервера. Адрес и пароль после него новые.
type Manager struct {
	base Config

	// switchMu выстраивает в очередь запуск, остановку и смену модели.
	switchMu sync.Mutex
	// notifyMu выстраивает уведомления, чтобы последнее было свежим.
	notifyMu sync.Mutex

	mu          sync.Mutex
	sup         *Supervisor
	model       *Model
	listeners   []func(Status)
	ctx         context.Context
	started     bool
	stopped     bool
	switching   bool
	switchSince time.Time
	binary      string
}

// NewManager создаёт агента. base — настройки надзора без модели,
// model — модель или nil. Сервер запускается методом Start.
func NewManager(base Config, model *Model) *Manager {
	m := &Manager{base: base, model: model}
	m.sup = m.newSupervisor(model)
	return m
}

func (m *Manager) newSupervisor(model *Model) *Supervisor {
	cfg := m.base
	cfg.Options.Provider = nil
	cfg.ExtraEnv = append([]string(nil), m.base.ExtraEnv...)
	if model != nil {
		cfg.Options.Provider = model.Provider()
		cfg.ExtraEnv = append(cfg.ExtraEnv, model.Env()...)
	}
	sup := NewSupervisor(cfg)
	sup.OnChange(func(st Status) {
		m.mu.Lock()
		current := sup == m.sup
		if st.Binary != "" {
			m.binary = st.Binary
		}
		m.mu.Unlock()
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
	m.mu.Unlock()
	sup.Stop()
}

// SetModel меняет модель агента и перезапускает сервер, если он запущен.
// nil — модели нет: сервер работает, но агент не ответит.
func (m *Manager) SetModel(model *Model) {
	m.switchMu.Lock()
	defer m.switchMu.Unlock()
	next := m.newSupervisor(model)
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return
	}
	old := m.sup
	m.sup = next
	m.model = model
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
	m.mu.Unlock()
	st := sup.Status()
	if switching {
		st.State = StateStarting
		st.Error = ""
		st.Since = since
	}
	if st.Binary == "" {
		st.Binary = binary
	}
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

// Model — ссылка на модель для сессий или nil, если модель не настроена.
func (m *Manager) Model() *ModelRef {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.model == nil {
		return nil
	}
	return m.model.Ref()
}
