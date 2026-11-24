// Package agent запускает OpenCode для приложения и разговаривает с ним.
//
// Ядро поднимает свой сервер OpenCode как дочерний процесс: отдельные папки
// настроек, данных и рабочая папка, адрес 127.0.0.1, свободный порт и пароль,
// созданный при запуске. Обычная установка OpenCode при этом не затрагивается.
package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// PinnedVersion — версия OpenCode, с которой работает приложение.
const PinnedVersion = "2.0.28"

// EnvBinary — переменная окружения с путём к программе opencode.
const EnvBinary = "DAILY_AGENT_OPENCODE"

var versionPattern = regexp.MustCompile(`v?(\d+\.\d+\.\d+)`)

// ParseVersion достаёт номер версии из вывода «opencode --version».
func ParseVersion(out string) (string, error) {
	m := versionPattern.FindStringSubmatch(out)
	if m == nil {
		return "", fmt.Errorf("не удалось прочитать версию OpenCode из %q", strings.TrimSpace(out))
	}
	return m[1], nil
}

// BinaryVersion запускает «opencode --version» и возвращает номер версии.
func BinaryVersion(ctx context.Context, path string, env []string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--version")
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s --version: %w", path, err)
	}
	return ParseVersion(string(out))
}

// Candidates возвращает места, где искать opencode, по порядку: путь из
// DAILY_AGENT_OPENCODE, установка в ~/.opencode/bin, PATH, Homebrew.
func Candidates() []string {
	var out []string
	if p := strings.TrimSpace(os.Getenv(EnvBinary)); p != "" {
		out = append(out, p)
	}
	if home, err := os.UserHomeDir(); err == nil {
		out = append(out, filepath.Join(home, ".opencode", "bin", "opencode"))
	}
	if p, err := exec.LookPath("opencode"); err == nil {
		out = append(out, p)
	}
	out = append(out, "/opt/homebrew/bin/opencode", "/usr/local/bin/opencode")
	return out
}

// ErrNotFound — подходящий OpenCode не найден.
var ErrNotFound = errors.New("OpenCode не найден")

// FindBinary ищет opencode нужной версии среди кандидатов.
// Если программа есть, но другой версии, ошибка называет найденные версии.
func FindBinary(ctx context.Context, candidates []string, want string, env []string) (string, error) {
	seen := map[string]bool{}
	var found []string
	for _, c := range candidates {
		path, err := filepath.EvalSymlinks(c)
		if err != nil || seen[path] {
			continue
		}
		seen[path] = true
		if info, err := os.Stat(path); err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
			continue
		}
		v, err := BinaryVersion(ctx, c, env)
		if err != nil {
			continue
		}
		if v == want {
			return c, nil
		}
		found = append(found, fmt.Sprintf("%s (%s)", v, c))
	}
	if len(found) > 0 {
		return "", fmt.Errorf("%w: нужна версия %s, найдены %s", ErrNotFound, want, strings.Join(found, ", "))
	}
	return "", fmt.Errorf("%w: установите OpenCode %s или укажите путь в %s", ErrNotFound, want, EnvBinary)
}
