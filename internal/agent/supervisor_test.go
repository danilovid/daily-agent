package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Поддельный opencode: тестовый бинарник, запущенный с DA_FAKE_OPENCODE,
// ведёт себя как «opencode --version» и «opencode serve».
const fakeEnv = "DA_FAKE_OPENCODE"

func TestMain(m *testing.M) {
	if mode := os.Getenv(fakeEnv); mode != "" {
		fakeOpenCode(mode)
		return
	}
	os.Exit(m.Run())
}

func fakeOpenCode(mode string) {
	version := PinnedVersion
	if mode == "old" {
		version = "2.0.20"
	}
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "--version" {
		fmt.Printf("opencode v%s\n", version)
		os.Exit(0)
	}
	if len(args) < 5 || args[0] != "serve" {
		os.Exit(2)
	}
	dir := os.Getenv("DA_FAKE_DIR")
	switch mode {
	case "crash":
		os.Exit(3)
	case "crash-once":
		marker := filepath.Join(dir, "crashed")
		if _, err := os.Stat(marker); err != nil {
			os.WriteFile(marker, nil, 0o600)
			os.Exit(3)
		}
	case "with-child":
		child := exec.Command("/bin/sleep", "300")
		child.Start()
		os.WriteFile(filepath.Join(dir, "child.pid"), []byte(strconv.Itoa(child.Process.Pid)), 0o600)
	}
	password := os.Getenv("OPENCODE_SERVER_PASSWORD")
	http.HandleFunc("/api/info", func(w http.ResponseWriter, r *http.Request) {
		if _, p, ok := r.BasicAuth(); !ok || p != password {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"version": version, "pid": os.Getpid()})
	})
	// Ключ модели, который получил процесс. Только в поддельном сервере.
	http.HandleFunc("/test/model-key", func(w http.ResponseWriter, r *http.Request) {
		if _, p, ok := r.BasicAuth(); !ok || p != password {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, os.Getenv(ModelKeyEnv))
	})
	http.ListenAndServe("127.0.0.1:"+args[4], nil)
}

func testSupervisor(t *testing.T, mode string, tweak func(*Config)) (*Supervisor, string) {
	t.Helper()
	root, err := os.MkdirTemp("", "da-sup-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	cfg := Config{
		Profile:     Profile{Root: root},
		Binary:      os.Args[0],
		ExtraEnv:    []string{fakeEnv + "=" + mode, "DA_FAKE_DIR=" + root},
		MinBackoff:  20 * time.Millisecond,
		MaxBackoff:  100 * time.Millisecond,
		StopTimeout: 2 * time.Second,
		StaleName:   filepath.Base(os.Args[0]),
		Logf:        t.Logf,
	}
	if tweak != nil {
		tweak(&cfg)
	}
	sup := NewSupervisor(cfg)
	t.Cleanup(sup.Stop)
	return sup, root
}

func waitReady(t *testing.T, sup *Supervisor) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := sup.WaitReady(ctx); err != nil {
		t.Fatal(err)
	}
}

func alive(pid int) bool { return syscall.Kill(pid, 0) == nil }

func TestSupervisorStartsAndStops(t *testing.T) {
	sup, root := testSupervisor(t, "ok", nil)
	var states []string
	sup.OnChange(func(s Status) { states = append(states, s.State) })
	sup.Start(context.Background())
	waitReady(t, sup)

	st := sup.Status()
	if st.State != StateReady || st.Version != PinnedVersion || st.Since.IsZero() {
		t.Fatalf("состояние: %+v", st)
	}
	client, err := sup.Client()
	if err != nil {
		t.Fatal(err)
	}
	info, err := client.Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "config", "opencode", "opencode.json")); err != nil {
		t.Errorf("настройки агента не записаны: %v", err)
	}
	if raw, _ := os.ReadFile(sup.cfg.Profile.PIDFile()); strings.TrimSpace(string(raw)) != strconv.Itoa(info.PID) {
		t.Errorf("файл PID: %q, процесс %d", raw, info.PID)
	}
	// Без пароля сервер не отвечает.
	if _, err := NewClient(strings.TrimSuffix(client.baseURL, "/"), "неверный").Info(context.Background()); err == nil {
		t.Error("сервер ответил без пароля")
	}

	sup.Stop()
	if st := sup.Status(); st.State != StateStopped {
		t.Errorf("после остановки: %+v", st)
	}
	if alive(info.PID) {
		t.Errorf("процесс %d жив после остановки", info.PID)
	}
	if _, err := os.Stat(sup.cfg.Profile.PIDFile()); err == nil {
		t.Error("файл PID остался после остановки")
	}
	if _, err := sup.Client(); err == nil {
		t.Error("клиент доступен после остановки")
	}
	if strings.Join(states, ",") != "starting,ready,stopped" {
		t.Errorf("смена состояний: %v", states)
	}
}

func TestSupervisorRejectsWrongVersion(t *testing.T) {
	sup, _ := testSupervisor(t, "old", nil)
	sup.Start(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := sup.WaitReady(ctx); err == nil {
		t.Fatal("ждали ошибку версии")
	}
	st := sup.Status()
	if st.State != StateFailed || !strings.Contains(st.Error, "нужна версия "+PinnedVersion) {
		t.Fatalf("состояние: %+v", st)
	}
}

func TestSupervisorRestartsAfterCrash(t *testing.T) {
	sup, _ := testSupervisor(t, "crash-once", nil)
	sup.Start(context.Background())
	waitReady(t, sup)
	if st := sup.Status(); st.Restarts != 1 || st.State != StateReady {
		t.Fatalf("после падения и перезапуска: %+v", st)
	}
}

func TestSupervisorKeepsRetryingWithBackoff(t *testing.T) {
	sup, _ := testSupervisor(t, "crash", nil)
	sup.Start(context.Background())
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && sup.Status().Restarts < 3 {
		time.Sleep(20 * time.Millisecond)
	}
	st := sup.Status()
	if st.Restarts < 3 || st.State != StateRestarting || !strings.Contains(st.Error, "код выхода 3") {
		t.Fatalf("состояние: %+v", st)
	}
	sup.Stop()
	if st := sup.Status(); st.State != StateStopped {
		t.Fatalf("после остановки: %+v", st)
	}
}

func TestSupervisorStopKillsWholeProcessGroup(t *testing.T) {
	sup, root := testSupervisor(t, "with-child", nil)
	sup.Start(context.Background())
	waitReady(t, sup)
	raw, err := os.ReadFile(filepath.Join(root, "child.pid"))
	if err != nil {
		t.Fatal(err)
	}
	child, _ := strconv.Atoi(string(raw))
	if !alive(child) {
		t.Fatal("дочерний процесс не запустился")
	}
	sup.Stop()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && alive(child) {
		time.Sleep(50 * time.Millisecond)
	}
	if alive(child) {
		syscall.Kill(child, syscall.SIGKILL)
		t.Fatal("дочерний процесс OpenCode пережил остановку")
	}
}

func TestSupervisorKillsStaleProcess(t *testing.T) {
	sup, root := testSupervisor(t, "ok", nil)
	// Процесс от «прошлого запуска приложения».
	port, _ := freePort()
	stale := exec.Command(os.Args[0], "serve", "--hostname", "127.0.0.1", "--port", strconv.Itoa(port))
	stale.Env = []string{fakeEnv + "=ok", "DA_FAKE_DIR=" + root}
	stale.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := stale.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	go func() { stale.Wait(); close(exited) }()
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sup.cfg.Profile.PIDFile(), []byte(strconv.Itoa(stale.Process.Pid)), 0o600); err != nil {
		t.Fatal(err)
	}
	sup.Start(context.Background())
	waitReady(t, sup)
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		stale.Process.Kill()
		t.Fatal("процесс от прошлого запуска не завершён")
	}
}

func TestSupervisorIgnoresForeignPID(t *testing.T) {
	// В файле PID чужой процесс: его трогать нельзя.
	sleeper := exec.Command("/bin/sleep", "30")
	if err := sleeper.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { sleeper.Process.Kill(); sleeper.Wait() }()
	sup, root := testSupervisor(t, "ok", nil)
	os.MkdirAll(root, 0o700)
	os.WriteFile(sup.cfg.Profile.PIDFile(), []byte(strconv.Itoa(sleeper.Process.Pid)), 0o600)
	sup.Start(context.Background())
	waitReady(t, sup)
	if !alive(sleeper.Process.Pid) {
		t.Fatal("надзор убил чужой процесс")
	}
}

func TestFindBinary(t *testing.T) {
	dir := t.TempDir()
	env := []string{fakeEnv + "=old"}
	_, err := FindBinary(context.Background(), []string{filepath.Join(dir, "нет"), dir, os.Args[0]}, PinnedVersion, env)
	if err == nil || !strings.Contains(err.Error(), "найдены 2.0.20") {
		t.Fatalf("ждали ошибку с найденной версией, получили %v", err)
	}
	got, err := FindBinary(context.Background(), []string{dir, os.Args[0]}, PinnedVersion, []string{fakeEnv + "=ok"})
	if err != nil || got != os.Args[0] {
		t.Fatalf("FindBinary = %q, %v", got, err)
	}
	if _, err := FindBinary(context.Background(), []string{filepath.Join(dir, "нет")}, PinnedVersion, nil); err == nil {
		t.Fatal("ждали ErrNotFound")
	}
}

func TestParseVersion(t *testing.T) {
	for in, want := range map[string]string{"opencode v2.0.28\n": "2.0.28", "2.4.1": "2.4.1"} {
		if got, err := ParseVersion(in); err != nil || got != want {
			t.Errorf("ParseVersion(%q) = %q, %v", in, got, err)
		}
	}
	if _, err := ParseVersion("opencode dev"); err == nil {
		t.Error("ждали ошибку")
	}
}

func TestProfileEnvIsolatesOpenCode(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://proxy.example.test:3128")
	t.Setenv("SOME_SECRET", "не должен попасть")
	p := Profile{Root: "/tmp/da"}
	env := strings.Join(p.Env("пароль", "EXTRA=1"), "\n")
	for _, want := range []string{
		"XDG_CONFIG_HOME=/tmp/da/config", "XDG_DATA_HOME=/tmp/da/data", "TMPDIR=/tmp/da/tmp/",
		"OPENCODE_SERVER_PASSWORD=пароль", "OPENCODE_DISABLE_MODELS_FETCH=1", "OPENCODE_DISABLE_PROJECT_CONFIG=1",
		"HTTPS_PROXY=http://proxy.example.test:3128", "EXTRA=1",
	} {
		if !strings.Contains(env, want) {
			t.Errorf("в окружении нет %s", want)
		}
	}
	if strings.Contains(env, "SOME_SECRET") {
		t.Error("в окружение OpenCode попала посторонняя переменная")
	}
}
