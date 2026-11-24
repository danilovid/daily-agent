package agent

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	agentprofile "daily-agent/agent"
)

// Config — настройки надзора за сервером OpenCode.
type Config struct {
	Profile Profile
	// Binary — путь к opencode. Пусто — искать среди Candidates.
	Binary     string
	Candidates []string
	// Version — нужная версия. Пусто — PinnedVersion.
	Version string
	// Options — права и модель агента.
	Options agentprofile.Options
	// ExtraEnv — дополнительные переменные процесса, например ключ модели.
	ExtraEnv []string

	ReadyTimeout time.Duration // сколько ждать готовности сервера
	MinBackoff   time.Duration // первая пауза перед перезапуском
	MaxBackoff   time.Duration // самая длинная пауза
	StableAfter  time.Duration // после стольких минут работы пауза сбрасывается
	StopTimeout  time.Duration // сколько ждать после SIGTERM до SIGKILL
	// StaleName — имя процесса, который можно убить по файлу PID от прошлого
	// запуска. Пусто — «opencode».
	StaleName string
	// Logf — журнал событий запуска. Только события, без содержимого запросов.
	Logf func(format string, args ...any)
}

func (c *Config) defaults() {
	if c.Version == "" {
		c.Version = PinnedVersion
	}
	if c.ReadyTimeout == 0 {
		c.ReadyTimeout = 30 * time.Second
	}
	if c.MinBackoff == 0 {
		c.MinBackoff = time.Second
	}
	if c.MaxBackoff == 0 {
		c.MaxBackoff = time.Minute
	}
	if c.StableAfter == 0 {
		c.StableAfter = time.Minute
	}
	if c.StopTimeout == 0 {
		c.StopTimeout = 5 * time.Second
	}
	if c.StaleName == "" {
		c.StaleName = "opencode"
	}
	if c.Logf == nil {
		c.Logf = func(string, ...any) {}
	}
}

// Endpoint — адрес и пароль запущенного сервера. Наружу из ядра не уходят.
type Endpoint struct {
	URL      string
	Password string
}

// Supervisor запускает сервер OpenCode, следит за ним и перезапускает после падения.
type Supervisor struct {
	cfg Config

	mu        sync.Mutex
	status    Status
	endpoint  *Endpoint
	listeners []func(Status)
	started   bool
	stopCh    chan struct{}
	done      chan struct{}
	readyCh   chan struct{}
}

// NewSupervisor создаёт надзор. Сервер запускается методом Start.
func NewSupervisor(cfg Config) *Supervisor {
	cfg.defaults()
	return &Supervisor{
		cfg:     cfg,
		status:  Status{State: StateStopped, ModelConfigured: cfg.Options.Provider != nil || cfg.Options.Default != ""},
		stopCh:  make(chan struct{}),
		done:    make(chan struct{}),
		readyCh: make(chan struct{}),
	}
}

// OnChange подписывает на смену состояния.
func (s *Supervisor) OnChange(fn func(Status)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listeners = append(s.listeners, fn)
}

// Status возвращает текущее состояние.
func (s *Supervisor) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

// Client возвращает клиента запущенного сервера.
func (s *Supervisor) Client() (*Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.endpoint == nil || s.status.State != StateReady {
		return nil, errors.New("OpenCode не на связи")
	}
	return NewClient(s.endpoint.URL, s.endpoint.Password), nil
}

// WaitReady ждёт, пока сервер впервые выйдет на связь.
func (s *Supervisor) WaitReady(ctx context.Context) error {
	select {
	case <-s.readyCh:
		return nil
	case <-s.done:
		st := s.Status()
		return fmt.Errorf("OpenCode не запустился: %s", st.Error)
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Supervisor) update(fn func(st *Status)) {
	s.mu.Lock()
	fn(&s.status)
	st := s.status
	listeners := append([]func(Status){}, s.listeners...)
	s.mu.Unlock()
	for _, l := range listeners {
		l(st)
	}
}

// Start запускает сервер в фоне. Повторный вызов ничего не делает.
func (s *Supervisor) Start(ctx context.Context) {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return
	}
	s.started = true
	s.mu.Unlock()
	// «Запускается» ставится сразу, до фоновой работы: состояние после Start
	// никогда не «остановлен».
	s.update(func(st *Status) { st.State = StateStarting; st.Error = ""; st.Since = time.Now() })
	go s.run(ctx)
}

// Stop останавливает сервер и ждёт завершения процесса.
func (s *Supervisor) Stop() {
	s.mu.Lock()
	started := s.started
	select {
	case <-s.stopCh:
	default:
		close(s.stopCh)
	}
	s.mu.Unlock()
	if started {
		<-s.done
	}
}

func (s *Supervisor) stopping() bool {
	select {
	case <-s.stopCh:
		return true
	default:
		return false
	}
}

func (s *Supervisor) fail(err error) {
	s.cfg.Logf("OpenCode: %v", err)
	s.update(func(st *Status) {
		st.State = StateFailed
		st.Error = err.Error()
		st.Since = time.Now()
	})
}

func (s *Supervisor) run(ctx context.Context) {
	defer close(s.done)

	bin := s.cfg.Binary
	if bin == "" {
		candidates := s.cfg.Candidates
		if candidates == nil {
			candidates = Candidates()
		}
		found, err := FindBinary(ctx, candidates, s.cfg.Version, s.cfg.Profile.Env("", s.cfg.ExtraEnv...))
		if err != nil {
			s.fail(err)
			return
		}
		bin = found
	} else if v, err := BinaryVersion(ctx, bin, s.cfg.Profile.Env("", s.cfg.ExtraEnv...)); err != nil {
		s.fail(err)
		return
	} else if v != s.cfg.Version {
		s.fail(fmt.Errorf("%w: нужна версия %s, у %s версия %s", ErrNotFound, s.cfg.Version, bin, v))
		return
	}
	// Путь к программе уйдёт в окно со следующим состоянием.
	s.mu.Lock()
	s.status.Binary = bin
	s.mu.Unlock()

	if err := s.cfg.Profile.Prepare(s.cfg.Options); err != nil {
		s.fail(err)
		return
	}
	s.killStale()

	backoff := s.cfg.MinBackoff
	for {
		began := time.Now()
		err := s.runOnce(ctx, bin)
		if s.stopping() || ctx.Err() != nil {
			s.update(func(st *Status) { st.State = StateStopped; st.Error = ""; st.Since = time.Now() })
			return
		}
		if time.Since(began) >= s.cfg.StableAfter {
			backoff = s.cfg.MinBackoff
		}
		s.cfg.Logf("OpenCode завершился: %v; перезапуск через %s", err, backoff)
		s.update(func(st *Status) {
			st.State = StateRestarting
			st.Error = fmt.Sprintf("OpenCode завершился: %v", err)
			st.Since = time.Now()
			st.Restarts++
		})
		select {
		case <-time.After(backoff):
		case <-s.stopCh:
			s.update(func(st *Status) { st.State = StateStopped; st.Error = ""; st.Since = time.Now() })
			return
		case <-ctx.Done():
			s.update(func(st *Status) { st.State = StateStopped; st.Error = ""; st.Since = time.Now() })
			return
		}
		backoff *= 2
		if backoff > s.cfg.MaxBackoff {
			backoff = s.cfg.MaxBackoff
		}
	}
}

// runOnce запускает процесс, ждёт готовности и возвращается, когда процесс
// завершился или надзор остановлен.
func (s *Supervisor) runOnce(ctx context.Context, bin string) error {
	port, err := freePort()
	if err != nil {
		return err
	}
	password, err := randomPassword()
	if err != nil {
		return err
	}
	cmd := exec.Command(bin, "serve", "--hostname", "127.0.0.1", "--port", strconv.Itoa(port))
	cmd.Dir = s.cfg.Profile.Workspace()
	cmd.Env = s.cfg.Profile.Env(password, s.cfg.ExtraEnv...)
	// Своя группа процессов: при остановке завершаем OpenCode вместе с детьми.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("запуск %s: %w", filepath.Base(bin), err)
	}
	pid := cmd.Process.Pid
	_ = os.WriteFile(s.cfg.Profile.PIDFile(), []byte(strconv.Itoa(pid)), 0o600)
	defer os.Remove(s.cfg.Profile.PIDFile())

	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	url := "http://127.0.0.1:" + strconv.Itoa(port)
	client := NewClient(url, password)
	readyCtx, cancel := context.WithTimeout(ctx, s.cfg.ReadyTimeout)
	defer cancel()
	tick := time.NewTicker(150 * time.Millisecond)
	defer tick.Stop()
	for ready := false; !ready; {
		select {
		case err := <-exited:
			return exitError(err)
		case <-s.stopCh:
			return s.terminate(pid, exited)
		case <-readyCtx.Done():
			s.terminate(pid, exited)
			return fmt.Errorf("сервер не ответил за %s", s.cfg.ReadyTimeout)
		case <-tick.C:
			info, err := client.Info(readyCtx)
			if err != nil {
				continue
			}
			if info.Version != s.cfg.Version {
				s.terminate(pid, exited)
				return fmt.Errorf("сервер сообщил версию %s, нужна %s", info.Version, s.cfg.Version)
			}
			ready = true
		}
	}

	s.mu.Lock()
	s.endpoint = &Endpoint{URL: url, Password: password}
	s.mu.Unlock()
	s.cfg.Logf("OpenCode %s на связи, порт %d", s.cfg.Version, port)
	s.update(func(st *Status) {
		st.State = StateReady
		st.Version = s.cfg.Version
		st.Error = ""
		st.Since = time.Now()
	})
	select {
	case <-s.readyCh:
	default:
		close(s.readyCh)
	}

	defer func() {
		s.mu.Lock()
		s.endpoint = nil
		s.mu.Unlock()
	}()
	select {
	case err := <-exited:
		return exitError(err)
	case <-s.stopCh:
		return s.terminate(pid, exited)
	case <-ctx.Done():
		return s.terminate(pid, exited)
	}
}

// terminate завершает группу процессов: SIGTERM, затем SIGKILL.
func (s *Supervisor) terminate(pid int, exited <-chan error) error {
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	select {
	case err := <-exited:
		return exitError(err)
	case <-time.After(s.cfg.StopTimeout):
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		return exitError(<-exited)
	}
}

// killStale завершает OpenCode, который остался от прошлого запуска
// приложения, например после его падения.
func (s *Supervisor) killStale() {
	raw, err := os.ReadFile(s.cfg.Profile.PIDFile())
	if err != nil {
		return
	}
	defer os.Remove(s.cfg.Profile.PIDFile())
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 1 || syscall.Kill(pid, 0) != nil {
		return
	}
	out, err := exec.Command("/bin/ps", "-o", "comm=", "-p", strconv.Itoa(pid)).Output()
	if err != nil || !strings.Contains(filepath.Base(strings.TrimSpace(string(out))), s.cfg.StaleName) {
		return
	}
	s.cfg.Logf("OpenCode от прошлого запуска (PID %d) завершается", pid)
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	deadline := time.Now().Add(s.cfg.StopTimeout)
	for time.Now().Before(deadline) {
		if syscall.Kill(pid, 0) != nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	_ = syscall.Kill(-pid, syscall.SIGKILL)
}

func exitError(err error) error {
	if err == nil {
		return errors.New("процесс завершился без ошибки")
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if exitErr.ExitCode() >= 0 {
			return fmt.Errorf("код выхода %d", exitErr.ExitCode())
		}
		return fmt.Errorf("остановлен сигналом: %v", exitErr.ProcessState)
	}
	return err
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("свободный порт: %w", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func randomPassword() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
