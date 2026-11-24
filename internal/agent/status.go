package agent

import "time"

// Состояния сервера OpenCode.
const (
	StateStopped    = "stopped"    // не запущен
	StateStarting   = "starting"   // запускается
	StateReady      = "ready"      // на связи
	StateRestarting = "restarting" // упал и будет перезапущен
	StateFailed     = "failed"     // не запускается: нет программы, не та версия
)

// Status — состояние сервера OpenCode для окна. Без адреса и пароля.
type Status struct {
	State   string    `json:"state"`
	Version string    `json:"version"`
	Since   time.Time `json:"since"`
	Error   string    `json:"error"`
	// ModelConfigured — настроена ли модель. Без неё сервер работает,
	// но агент не ответит.
	ModelConfigured bool `json:"modelConfigured"`
	// Restarts — сколько раз сервер перезапускался после падения.
	Restarts int `json:"restarts"`
}
