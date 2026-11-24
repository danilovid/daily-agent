package agent

// Backend — агент приложения для сервисов окна: клиент запущенного сервера,
// его состояние, рабочая папка и модель.
type Backend struct {
	Supervisor *Supervisor
	Profile    Profile
	// ModelRef — модель агента; nil, если она не настроена.
	ModelRef *ModelRef
}

// Client возвращает клиента запущенного сервера.
func (b *Backend) Client() (*Client, error) { return b.Supervisor.Client() }

// Status возвращает состояние сервера.
func (b *Backend) Status() Status { return b.Supervisor.Status() }

// Workspace — рабочая папка агента.
func (b *Backend) Workspace() string { return b.Profile.Workspace() }

// Model — модель агента или nil.
func (b *Backend) Model() *ModelRef { return b.ModelRef }

// Ref — ссылка на модель для сессий OpenCode.
func (m Model) Ref() *ModelRef { return &ModelRef{ID: m.ModelID, ProviderID: ProviderID} }
