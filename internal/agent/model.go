package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	agentprofile "daily-agent/agent"
)

// Переменные окружения с настройками модели. Экран настроек появится на шаге 7.
const (
	EnvModelURL = "DAILY_AGENT_MODEL_URL" // адрес API, совместимого с OpenAI, с /v1
	EnvModelID  = "DAILY_AGENT_MODEL_ID"  // идентификатор модели
)

// Ключ модели хранится в Связке ключей macOS.
const (
	KeychainService = "Daily Agent"
	KeychainAccount = "model-api-key"
	// ModelKeyEnv — переменная, через которую ключ попадает в процесс OpenCode.
	// В файлы настроек ключ не пишется.
	ModelKeyEnv = "DAILY_AGENT_MODEL_KEY"
)

// ProviderID — провайдер модели агента в настройках OpenCode.
const ProviderID = "daily-model"

// Model — модель агента и её ключ.
type Model struct {
	BaseURL string
	ModelID string
	APIKey  string
}

// Provider переводит модель в настройки профиля.
func (m Model) Provider() *agentprofile.Provider {
	p := &agentprofile.Provider{ID: ProviderID, Name: "Модель Daily Agent", BaseURL: m.BaseURL, ModelID: m.ModelID}
	if m.APIKey != "" {
		p.KeyEnv = ModelKeyEnv
	}
	return p
}

// Env — переменная с ключом для процесса OpenCode.
func (m Model) Env() []string {
	if m.APIKey == "" {
		return nil
	}
	return []string{ModelKeyEnv + "=" + m.APIKey}
}

// ErrNoModel — модель не настроена.
var ErrNoModel = errors.New("модель агента не настроена")

// ModelFromEnv читает адрес и модель из окружения, а ключ — из Связки ключей.
// Без ключа модель тоже работает: например, локальная заглушка.
func ModelFromEnv(ctx context.Context) (Model, error) {
	url := strings.TrimSpace(os.Getenv(EnvModelURL))
	id := strings.TrimSpace(os.Getenv(EnvModelID))
	if url == "" || id == "" {
		return Model{}, ErrNoModel
	}
	key, err := KeychainPassword(ctx, KeychainService, KeychainAccount)
	if err != nil && !errors.Is(err, ErrKeyNotFound) {
		return Model{}, err
	}
	return Model{BaseURL: url, ModelID: id, APIKey: key}, nil
}

// ErrKeyNotFound — в Связке ключей нет такой записи.
var ErrKeyNotFound = errors.New("в Связке ключей нет ключа модели")

// KeychainPassword читает пароль из Связки ключей системной утилитой security.
func KeychainPassword(ctx context.Context, service, account string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/bin/security", "find-generic-password", "-s", service, "-a", account, "-w").Output()
	if err != nil {
		var exitErr *exec.ExitError
		// Код 44 — записи нет.
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 44 {
			return "", ErrKeyNotFound
		}
		return "", fmt.Errorf("Связка ключей: %w", err)
	}
	return strings.TrimRight(string(out), "\n"), nil
}
