package agent

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func testManager(t *testing.T, model *Model) (*Manager, string) {
	t.Helper()
	root, err := os.MkdirTemp("", "da-man-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	m := NewManager(Config{
		Profile:     Profile{Root: root},
		Binary:      os.Args[0],
		ExtraEnv:    []string{fakeEnv + "=ok", "DA_FAKE_DIR=" + root},
		MinBackoff:  20 * time.Millisecond,
		StopTimeout: 2 * time.Second,
		StaleName:   filepath.Base(os.Args[0]),
		Logf:        t.Logf,
	}, model)
	t.Cleanup(m.Stop)
	return m, root
}

func managerReady(t *testing.T, m *Manager) Info {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := m.WaitReady(ctx); err != nil {
		t.Fatal(err)
	}
	c, err := m.Client()
	if err != nil {
		t.Fatal(err)
	}
	info, err := c.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return info
}

// modelKey спрашивает у поддельного сервера, какой ключ модели он получил.
func modelKey(t *testing.T, m *Manager) string {
	t.Helper()
	c, err := m.Client()
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodGet, c.baseURL+"/test/model-key", nil)
	req.SetBasicAuth("opencode", c.password)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return string(raw)
}

func TestManagerSwitchesModel(t *testing.T) {
	m, root := testManager(t, nil)
	var mu sync.Mutex
	var states []string
	m.OnChange(func(st Status) {
		mu.Lock()
		states = append(states, st.State)
		mu.Unlock()
	})
	m.Start(context.Background())
	first := managerReady(t, m)

	st := m.Status()
	if st.State != StateReady || st.ModelConfigured || st.Binary != os.Args[0] {
		t.Fatalf("без модели: %+v", st)
	}
	if m.Model() != nil {
		t.Fatalf("модель без настроек: %+v", m.Model())
	}
	if key := modelKey(t, m); key != "" {
		t.Fatalf("ключ без модели: %q", key)
	}
	if m.Workspace() != filepath.Join(root, "workspace") {
		t.Errorf("рабочая папка: %s", m.Workspace())
	}

	mu.Lock()
	states = nil
	mu.Unlock()
	m.SetModel(&Model{BaseURL: "http://127.0.0.1:9/v1", ModelID: "test-model", APIKey: "test-key-1"})
	second := managerReady(t, m)

	if second.PID == first.PID || alive(first.PID) {
		t.Fatalf("сервер не перезапущен: был %d, стал %d", first.PID, second.PID)
	}
	st = m.Status()
	if st.State != StateReady || !st.ModelConfigured || st.Binary == "" {
		t.Fatalf("с моделью: %+v", st)
	}
	if ref := m.Model(); ref == nil || *ref != (ModelRef{ID: "test-model", ProviderID: ProviderID}) {
		t.Fatalf("модель: %+v", ref)
	}
	if key := modelKey(t, m); key != "test-key-1" {
		t.Fatalf("ключ модели в процессе: %q", key)
	}
	raw, err := os.ReadFile(filepath.Join(root, "config", "opencode", "opencode.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "http://127.0.0.1:9/v1") || strings.Contains(string(raw), "test-key-1") {
		t.Fatalf("настройки OpenCode: адрес модели должен быть, ключа — нет:\n%s", raw)
	}

	// Окно видит «запускается» и «на связи», а остановку прежнего сервера — нет.
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		got := strings.Join(states, ",")
		mu.Unlock()
		if strings.HasSuffix(got, StateReady) || time.Now().After(deadline) {
			if strings.Contains(got, StateStopped) || !strings.HasPrefix(got, StateStarting) || !strings.HasSuffix(got, StateReady) {
				t.Fatalf("состояния при смене модели: %s", got)
			}
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Без ключа процесс ключа не получает.
	m.SetModel(&Model{BaseURL: "http://127.0.0.1:9/v1", ModelID: "test-model"})
	managerReady(t, m)
	if key := modelKey(t, m); key != "" {
		t.Fatalf("ключ остался после удаления: %q", key)
	}

	m.Stop()
	if st := m.Status(); st.State != StateStopped {
		t.Fatalf("после остановки: %+v", st)
	}
	// После остановки смена модели сервер не поднимает.
	m.SetModel(nil)
	time.Sleep(100 * time.Millisecond)
	if st := m.Status(); st.State != StateStopped {
		t.Fatalf("сервер поднялся после остановки: %+v", st)
	}
}

func TestManagerModelBeforeStart(t *testing.T) {
	m, _ := testManager(t, nil)
	m.SetModel(&Model{BaseURL: "http://127.0.0.1:9/v1", ModelID: "test-model", APIKey: "test-key-2"})
	if st := m.Status(); st.State != StateStopped {
		t.Fatalf("до запуска сервер не поднимается: %+v", st)
	}
	m.Start(context.Background())
	managerReady(t, m)
	if key := modelKey(t, m); key != "test-key-2" {
		t.Fatalf("ключ: %q", key)
	}
}
