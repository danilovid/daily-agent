package agent

import (
	"strings"
	"testing"
)

func TestParseShellEnv(t *testing.T) {
	out := []byte("Последний вход: вчера\nприветствие оболочки\x00" + shellMarker + "\x00PATH=/opt/homebrew/bin:/usr/bin\x00MULTI=строка\nдве\x00BAD KEY=x\x00EMPTY=\x00")
	env := parseShellEnv(out)
	if env["PATH"] != "/opt/homebrew/bin:/usr/bin" || env["MULTI"] != "строка\nдве" || len(env) != 3 {
		t.Fatalf("окружение: %#v", env)
	}
	if len(parseShellEnv([]byte("нет маркера"))) != 0 {
		t.Fatal("без маркера окружение пустое")
	}
	if loginPath(env) != "/opt/homebrew/bin:/usr/bin" || !strings.HasPrefix(loginPath(nil), "/opt/homebrew/bin:") {
		t.Fatalf("PATH: %s / %s", loginPath(env), loginPath(nil))
	}
}

func TestProfileEnvPath(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "")
	p := Profile{Root: t.TempDir(), Path: "/opt/test/bin:/usr/bin", Inherit: map[string]string{"HTTPS_PROXY": "http://proxy.example.test:3128", "SECRET": "x"}}
	env := strings.Join(p.Env("pw"), "\n")
	if !strings.Contains(env, "PATH=/opt/test/bin:/usr/bin") || !strings.Contains(env, "HTTPS_PROXY=http://proxy.example.test:3128") || strings.Contains(env, "SECRET") {
		t.Fatalf("окружение OpenCode:\n%s", env)
	}
	if !strings.Contains(strings.Join(Profile{Root: t.TempDir()}.Env(""), "\n"), "PATH=/usr/bin:/bin:/usr/sbin:/sbin") {
		t.Fatal("без Path — системные папки")
	}
}
