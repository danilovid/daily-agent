package agent

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Приложение из Finder получает урезанное окружение: без PATH из оболочки и
// без переменных, которые пользователь задаёт в ~/.zshrc. Локальные
// MCP-серверы вроде «npx -y …» без них не запускаются. Поэтому окружение
// берётся один раз у оболочки входа пользователя, как это делает терминал.

var (
	shellOnce sync.Once
	shellEnv  map[string]string
)

// shellMarker отделяет окружение от того, что оболочка печатает при входе.
const shellMarker = "__DAILY_AGENT_ENV__"

// ShellEnv — окружение оболочки входа пользователя ($SHELL -ilc). Читается
// один раз; при ошибке — пустое. Целиком наружу не отдаётся: берутся только
// нужные переменные.
func ShellEnv() map[string]string {
	shellOnce.Do(func() {
		shellEnv = map[string]string{}
		shell := os.Getenv("SHELL")
		if shell == "" || !filepath.IsAbs(shell) {
			shell = "/bin/zsh"
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, shell, "-ilc", "printf '%s\\0' "+shellMarker+"; /usr/bin/env -0")
		cmd.Stdin = nil
		out, err := cmd.Output()
		if err != nil && len(out) == 0 {
			return
		}
		shellEnv = parseShellEnv(out)
	})
	return shellEnv
}

// parseShellEnv разбирает вывод «маркер\0 ИМЯ=значение\0…».
func parseShellEnv(out []byte) map[string]string {
	env := map[string]string{}
	i := bytes.Index(out, []byte(shellMarker+"\x00"))
	if i < 0 {
		return env
	}
	for _, kv := range bytes.Split(out[i+len(shellMarker)+1:], []byte{0}) {
		k, v, ok := strings.Cut(string(kv), "=")
		if ok && k != "" && !strings.ContainsAny(k, "\n ") {
			env[k] = v
		}
	}
	return env
}

// defaultPath — PATH, если оболочку прочитать не удалось: системные папки
// и папки Homebrew.
var defaultPath = []string{"/opt/homebrew/bin", "/usr/local/bin", "/usr/bin", "/bin", "/usr/sbin", "/sbin"}

// LoginPath — PATH для OpenCode и его локальных MCP-серверов: из оболочки
// входа, а без неё — системные папки и Homebrew.
func LoginPath() string { return loginPath(ShellEnv()) }

func loginPath(env map[string]string) string {
	if p := strings.TrimSpace(env["PATH"]); p != "" {
		return p
	}
	return strings.Join(defaultPath, string(os.PathListSeparator))
}
