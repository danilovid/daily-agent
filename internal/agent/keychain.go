package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Ключ модели хранится в Связке ключей macOS.
const (
	KeychainService = "Daily Agent"
	KeychainAccount = "model-api-key"
)

// EnvKeychainPath — другой файл связки ключей вместо связки по умолчанию.
// Для разработки и проверок, чтобы не трогать настоящий ключ.
const EnvKeychainPath = "DAILY_AGENT_KEYCHAIN"

// MaxKeyLength — самый длинный ключ модели, в символах.
const MaxKeyLength = 2048

// ErrKeyNotFound — в Связке ключей нет такой записи.
var ErrKeyNotFound = errors.New("в Связке ключей нет ключа модели")

// securityTool — системная утилита Связки ключей. Через неё и пишем, и читаем:
// запись, созданная утилитой, доверяет ей, и macOS не спрашивает доступ
// после каждой пересборки приложения.
const securityTool = "/usr/bin/security"

// Keychain — Связка ключей macOS.
type Keychain struct {
	// Path — файл связки ключей. Пусто — связка по умолчанию, обычно «Вход».
	// Тесты задают временную связку, чтобы не трогать настоящую.
	Path string
}

// check не даёт записать в связку по умолчанию вместо заданной: утилита
// в интерактивном режиме молча берёт её, если файла нет.
func (k Keychain) check() error {
	if k.Path == "" {
		return nil
	}
	if _, err := os.Stat(k.Path); err != nil {
		return fmt.Errorf("Связка ключей: нет файла связки: %w", err)
	}
	return nil
}

func (k Keychain) args(args ...string) []string {
	if k.Path != "" {
		args = append(args, k.Path)
	}
	return args
}

// Get читает пароль записи.
func (k Keychain) Get(ctx context.Context, service, account string) (string, error) {
	if err := k.check(); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, securityTool, k.args("find-generic-password", "-s", service, "-a", account, "-w")...).Output()
	if err != nil {
		if exitCode(err) == 44 { // записи нет
			return "", ErrKeyNotFound
		}
		return "", fmt.Errorf("Связка ключей: %w", err)
	}
	return strings.TrimRight(string(out), "\n"), nil
}

// Set сохраняет пароль записи, заменяя прежний. Команда идёт утилите через
// стандартный ввод, чтобы ключ не попал в список процессов.
func (k Keychain) Set(ctx context.Context, service, account, secret string) error {
	if err := ValidateKey(secret); err != nil {
		return err
	}
	if err := k.check(); err != nil {
		return err
	}
	for _, v := range []string{service, account, k.Path} {
		if !safeForCommand(v) {
			return errors.New("Связка ключей: недопустимое имя записи")
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	quote := func(v string) string { return `"` + v + `"` }
	line := "add-generic-password -U -s " + quote(service) + " -a " + quote(account) + " -w " + quote(secret)
	if k.Path != "" {
		line += " " + quote(k.Path)
	}
	cmd := exec.CommandContext(ctx, securityTool, "-i")
	cmd.Stdin = strings.NewReader(line + "\n")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("Связка ключей: %w", err)
	}
	// В интерактивном режиме утилита сообщает об ошибке текстом, а код выхода — 0.
	// Текст в ошибку не берём: в нём может оказаться команда с ключом.
	if strings.TrimSpace(string(out)) != "" {
		return errors.New("Связка ключей: не удалось сохранить ключ")
	}
	return nil
}

// Delete удаляет запись. Если записи нет, это не ошибка.
func (k Keychain) Delete(ctx context.Context, service, account string) error {
	if err := k.check(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	err := exec.CommandContext(ctx, securityTool, k.args("delete-generic-password", "-s", service, "-a", account)...).Run()
	if err != nil && exitCode(err) != 44 {
		return fmt.Errorf("Связка ключей: %w", err)
	}
	return nil
}

// ValidateKey проверяет ключ модели: печатные символы ASCII без пробелов,
// кавычек и обратной косой черты. Такие ключи выдают API, совместимые с OpenAI.
func ValidateKey(key string) error {
	if key == "" {
		return errors.New("ключ пустой")
	}
	if len(key) > MaxKeyLength {
		return fmt.Errorf("ключ длиннее %d символов", MaxKeyLength)
	}
	for _, r := range key {
		if r <= ' ' || r > '~' || r == '"' || r == '\\' {
			return errors.New("в ключе недопустимые символы: пробелы, кавычки, обратная косая черта или не латиница")
		}
	}
	return nil
}

// safeForCommand — строку можно поставить в кавычки в команде утилиты.
func safeForCommand(v string) bool {
	for _, r := range v {
		if r < ' ' || r == '"' || r == '\\' || r == 0x7f {
			return false
		}
	}
	return true
}

func exitCode(err error) int {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}
