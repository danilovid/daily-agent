package agent

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"unicode"

	agentprofile "daily-agent/agent"
)

// Переменные окружения с настройками модели. Запасной путь для разработки:
// обычно модель задаётся на экране «Настройки: агент».
const (
	EnvModelURL = "DAILY_AGENT_MODEL_URL" // адрес API, совместимого с OpenAI, с /v1
	EnvModelID  = "DAILY_AGENT_MODEL_ID"  // идентификатор модели
)

// ModelKeyEnv — переменная, через которую ключ попадает в процесс OpenCode.
// В файлы настроек ключ не пишется.
const ModelKeyEnv = "DAILY_AGENT_MODEL_KEY"

// ProviderID — провайдер модели агента в настройках OpenCode.
const ProviderID = "daily-model"

// MaxModelIDLength — самый длинный идентификатор модели.
const MaxModelIDLength = 200

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

// Ref — ссылка на модель для сессий OpenCode.
func (m Model) Ref() *ModelRef { return &ModelRef{ID: m.ModelID, ProviderID: ProviderID} }

// ErrNoModel — модель не настроена.
var ErrNoModel = errors.New("модель агента не настроена")

// KeyReader читает ключ модели. Обычно это Связка ключей.
type KeyReader interface {
	Get(ctx context.Context, service, account string) (string, error)
}

// ModelFromEnv читает адрес и модель из окружения, а ключ — из Связки ключей.
// Без ключа модель тоже работает: например, локальная заглушка.
func ModelFromEnv(ctx context.Context, keys KeyReader) (Model, error) {
	return ModelFrom(ctx, keys, os.Getenv(EnvModelURL), os.Getenv(EnvModelID))
}

// ModelFrom собирает модель из адреса и идентификатора и берёт ключ из Связки ключей.
func ModelFrom(ctx context.Context, keys KeyReader, baseURL, modelID string) (Model, error) {
	baseURL, modelID = strings.TrimSpace(baseURL), strings.TrimSpace(modelID)
	if baseURL == "" || modelID == "" {
		return Model{}, ErrNoModel
	}
	key, err := keys.Get(ctx, KeychainService, KeychainAccount)
	if err != nil && !errors.Is(err, ErrKeyNotFound) {
		return Model{}, err
	}
	return Model{BaseURL: baseURL, ModelID: modelID, APIKey: key}, nil
}

// NormalizeModelURL проверяет адрес API модели и приводит его к одному виду.
// Нужен http или https, без логина и пароля в адресе, без параметров.
// Рабочие задачи идут только в селфхост-модель, поэтому известные облачные
// сервисы не принимаются.
func NormalizeModelURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("введите адрес API")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", errors.New("адрес API должен начинаться с http:// или https://")
	}
	if u.User != nil {
		return "", errors.New("логин и пароль не пишутся в адрес: ключ сохраняется отдельно, в Связке ключей")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("в адресе API не должно быть параметров после «?» и «#»")
	}
	// Два адреса подряд — почти наверняка опечатка при вставке.
	if strings.Contains(u.Path, "://") || strings.Contains(u.Path, "//") {
		return "", errors.New("в адресе API лишний адрес или двойная косая черта: проверьте, что поле содержит один адрес")
	}
	if IsCloudHost(u.Hostname()) {
		return "", fmt.Errorf("адрес %s — облачный сервис. Для рабочих задач нужна селфхост-модель: рабочие данные не уходят в облако", u.Hostname())
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	return u.String(), nil
}

// ValidateModelID проверяет идентификатор модели.
func ValidateModelID(id string) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("введите идентификатор модели")
	}
	if len(id) > MaxModelIDLength {
		return fmt.Errorf("идентификатор модели длиннее %d символов", MaxModelIDLength)
	}
	for _, r := range id {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return errors.New("в идентификаторе модели не должно быть пробелов")
		}
	}
	return nil
}

// cloudSuffixes — адреса известных облачных API моделей. Это защита от
// ошибки, а не гарантия: селфхост определяет сам пользователь.
var cloudSuffixes = []string{
	"openai.com",
	"openai.azure.com",
	"anthropic.com",
	"googleapis.com",
	"aiplatform.googleapis.com",
	"amazonaws.com",
	"openrouter.ai",
	"mistral.ai",
	"groq.com",
	"deepseek.com",
	"together.xyz",
	"together.ai",
	"x.ai",
	"cohere.com",
	"cohere.ai",
	"fireworks.ai",
	"perplexity.ai",
	"deepinfra.com",
	"cerebras.ai",
	"moonshot.cn",
	"moonshot.ai",
	"dashscope.aliyuncs.com",
	"bigmodel.cn",
	"z.ai",
	"huggingface.co",
	"replicate.com",
	"api.yandex.net",
	"llm.api.cloud.yandex.net",
	"gigachat.devices.sberbank.ru",
}

// IsCloudHost — адрес принадлежит известному облачному API моделей.
func IsCloudHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if net.ParseIP(host) != nil {
		return false
	}
	for _, s := range cloudSuffixes {
		if host == s || strings.HasSuffix(host, "."+s) {
			return true
		}
	}
	return false
}
