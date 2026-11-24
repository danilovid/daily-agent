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
	// ModelConfigured — выбрана ли модель для рабочих задач. Без неё сервер
	// работает, но в рабочем чате агент не ответит.
	ModelConfigured bool `json:"modelConfigured"`
	// PersonalConfigured — есть ли модель для личных задач: своя или та же,
	// что для рабочих.
	PersonalConfigured bool `json:"personalConfigured"`
	// Restarts — сколько раз сервер перезапускался после падения.
	Restarts int `json:"restarts"`
	// Binary — путь к программе opencode, когда она найдена.
	Binary string `json:"binary"`
}
