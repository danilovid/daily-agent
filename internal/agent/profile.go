package agent

import (
	"fmt"
	"os"
	"path/filepath"

	agentprofile "daily-agent/agent"
)

// Profile — папки OpenCode агента приложения. Все лежат внутри Root.
//
//	config/opencode/opencode.json  настройки (XDG_CONFIG_HOME)
//	data/                          база сессий и журналы OpenCode (XDG_DATA_HOME)
//	state/, cache/, tmp/           служебные папки
//	workspace/                     рабочая папка агента, пустая
type Profile struct {
	Root string
}

func (p Profile) dir(name string) string { return filepath.Join(p.Root, name) }

// Workspace — рабочая папка агента.
func (p Profile) Workspace() string { return p.dir("workspace") }

// ConfigFile — файл настроек OpenCode агента.
func (p Profile) ConfigFile() string {
	return filepath.Join(p.dir("config"), "opencode", "opencode.json")
}

// PIDFile — файл с номером запущенного процесса OpenCode.
func (p Profile) PIDFile() string { return p.dir("opencode.pid") }

// Prepare создаёт папки и записывает настройки.
func (p Profile) Prepare(opts agentprofile.Options) error {
	if p.Root == "" {
		return fmt.Errorf("не задана папка агента")
	}
	for _, d := range []string{"config/opencode", "data", "state", "cache", "tmp", "workspace"} {
		if err := os.MkdirAll(p.dir(d), 0o700); err != nil {
			return fmt.Errorf("папка агента %s: %w", d, err)
		}
	}
	cfg, err := agentprofile.Render(opts)
	if err != nil {
		return err
	}
	tmp := p.ConfigFile() + ".tmp"
	if err := os.WriteFile(tmp, cfg, 0o600); err != nil {
		return fmt.Errorf("настройки агента: %w", err)
	}
	return os.Rename(tmp, p.ConfigFile())
}

// passthroughEnv — переменные окружения приложения, которые нужны OpenCode
// для выхода к модели через прокси компании. Остальное окружение не передаётся.
var passthroughEnv = []string{
	"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "no_proxy",
	"NODE_EXTRA_CA_CERTS", "LANG",
}

// Env собирает окружение процесса OpenCode с нуля: свои папки XDG, свой
// TMPDIR, выключенные обращения в сеть самого OpenCode и пароль сервера.
// extra дописывается в конец, например ключ модели.
func (p Profile) Env(password string, extra ...string) []string {
	home, _ := os.UserHomeDir()
	env := []string{
		"HOME=" + home,
		"PATH=/usr/bin:/bin:/usr/sbin:/sbin",
		"XDG_CONFIG_HOME=" + p.dir("config"),
		"XDG_DATA_HOME=" + p.dir("data"),
		"XDG_STATE_HOME=" + p.dir("state"),
		"XDG_CACHE_HOME=" + p.dir("cache"),
		"TMPDIR=" + p.dir("tmp") + string(os.PathSeparator),
		// Без загрузки каталога моделей, проверки обновлений и настроек
		// проектов из родительских папок.
		"OPENCODE_DISABLE_MODELS_FETCH=1",
		"OPENCODE_DISABLE_AUTOUPDATE=1",
		"OPENCODE_DISABLE_PROJECT_CONFIG=1",
	}
	if password != "" {
		env = append(env, "OPENCODE_SERVER_PASSWORD="+password)
	}
	for _, name := range passthroughEnv {
		if v, ok := os.LookupEnv(name); ok && v != "" {
			env = append(env, name+"="+v)
		}
	}
	return append(env, extra...)
}
