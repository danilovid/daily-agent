package app

import (
	"daily-agent/internal/agent"
)

// AgentStatusEvent — событие окна о смене состояния агента.
const AgentStatusEvent = "agent:status"

// AgentStatusView — состояние OpenCode для боковой панели.
type AgentStatusView struct {
	// State: stopped, starting, ready, restarting, failed.
	State string `json:"state"`
	// Since — с какого момента это состояние, ЧЧ:ММ по местному времени.
	Since           string `json:"since"`
	Version         string `json:"version"`
	Error           string `json:"error"`
	ModelConfigured bool   `json:"modelConfigured"`
	// PersonalConfigured — есть модель для личных задач.
	PersonalConfigured bool `json:"personalConfigured"`
	Restarts           int  `json:"restarts"`
}

// AgentStatusFrom переводит состояние надзора в вид для окна.
func AgentStatusFrom(st agent.Status, clock Clock) AgentStatusView {
	v := AgentStatusView{
		State:              st.State,
		Version:            st.Version,
		Error:              st.Error,
		ModelConfigured:    st.ModelConfigured,
		PersonalConfigured: st.PersonalConfigured,
		Restarts:           st.Restarts,
	}
	if !st.Since.IsZero() {
		v.Since = st.Since.In(clock.loc()).Format("15:04")
	}
	return v
}

// AgentService сообщает окну, на связи ли OpenCode.
type AgentService struct {
	status func() agent.Status
	clock  Clock
}

// NewAgentService создаёт сервис. status — источник состояния, обычно надзор.
func NewAgentService(status func() agent.Status, clock Clock) *AgentService {
	return &AgentService{status: status, clock: clock}
}

// Status возвращает текущее состояние OpenCode.
func (s *AgentService) Status() AgentStatusView {
	if s.status == nil {
		return AgentStatusView{State: agent.StateStopped}
	}
	return AgentStatusFrom(s.status(), s.clock)
}

// unavailableText объясняет, почему агенту сейчас нельзя написать в рабочий
// или личный чат. Пусто — можно.
func unavailableText(b AgentBackend, personal bool) string {
	st := b.Status()
	if st.State == agent.StateReady {
		if b.ModelFor(personal) == nil {
			if personal {
				return "Модель для личных задач не выбрана"
			}
			return "Модель для рабочих задач не выбрана"
		}
		return ""
	}
	return serverUnavailable(st)
}

// serverUnavailable объясняет, почему OpenCode не на связи.
func serverUnavailable(st agent.Status) string {
	switch st.State {
	case agent.StateReady:
		return "OpenCode не на связи"
	case agent.StateStarting:
		return "OpenCode запускается"
	case agent.StateRestarting:
		return "OpenCode перезапускается"
	case agent.StateFailed:
		if st.Error != "" {
			return "OpenCode недоступен: " + st.Error
		}
		return "OpenCode недоступен"
	default:
		return "Агент не подключён"
	}
}
