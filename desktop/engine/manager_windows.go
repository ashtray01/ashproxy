package engine

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Sample struct{ RX, TX float64 }
type Snapshot struct {
	State, Detail string
	Config        Config
	Uptime        int
	RX, TX        uint64
	Totals        Totals
	History       []Sample
	Logs          []string
	Clients       []Client
}
type session struct {
	bucket     bucket
	historyErr error
	cmd        *exec.Cmd
	control    io.WriteCloser
	done       chan struct{}
	started    time.Time
	mu         sync.Mutex
	last       time.Time
	rx, tx     uint64
	baseRX     uint64
	baseTX     uint64
	history    []Sample
	logs       []string
	exitErr    error
}
type Manager struct {
	ledger    *ledger
	mu        sync.Mutex
	exe, dir  string
	config    Config
	current   *session
	configErr error
	stopped   bool
}

func (m *Manager) openHistory() error {
	if m.ledger != nil {
		return nil
	}
	if err := os.MkdirAll(m.dir, 0700); err != nil {
		return err
	}
	var err error
	m.ledger, err = openLedger(m.dir)
	return err
}
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	_ = m.stop()
	if m.ledger != nil {
		m.ledger.db.Close()
		m.ledger = nil
	}
}

func New(exe, dir string) *Manager {
	c, err := load(dir)
	return &Manager{exe: exe, dir: dir, config: c, configErr: err, stopped: true}
}

type output struct {
	session   *session
	partial   bytes.Buffer
	telemetry bool
}

func (w *output) Write(b []byte) (int, error) {
	for _, v := range b {
		if v == '\n' {
			w.line(strings.TrimSpace(w.partial.String()))
			w.partial.Reset()
		} else if w.partial.Len() < 4096 {
			w.partial.WriteByte(v)
		}
	}
	return len(b), nil
}
func (w *output) line(line string) {
	if line == "" {
		return
	}
	s := w.session
	s.mu.Lock()
	defer s.mu.Unlock()
	fields := strings.Fields(line)
	if w.telemetry && len(fields) == 3 && fields[0] == "ASH1" {
		rx, e1 := strconv.ParseUint(fields[1], 10, 64)
		tx, e2 := strconv.ParseUint(fields[2], 10, 64)
		if e1 != nil || e2 != nil {
			return
		}
		now := time.Now()
		previous := s.last
		if previous.IsZero() {
			previous = s.started
		}
		elapsed := now.Sub(previous).Seconds()
		if rx >= s.rx && tx >= s.tx {
			s.historyErr = s.bucket.record(now, rx-s.rx, tx-s.tx)
		}
		if elapsed > 0 && elapsed < 5 && rx >= s.rx && tx >= s.tx {
			s.history = append(s.history, Sample{float64(rx-s.rx) / elapsed, float64(tx-s.tx) / elapsed})
			if len(s.history) > 60 {
				s.history = s.history[1:]
			}
		} else {
			s.history = nil
		}
		s.last, s.rx, s.tx = now, rx, tx
		return
	}
	s.logs = append(s.logs, line)
	if len(s.logs) > 200 {
		s.logs = s.logs[1:]
	}
}

func (m *Manager) start(c Config) error {
	if m.configErr != nil {
		return m.configErr
	}
	if err := c.validate(); err != nil {
		return err
	}
	if m.current != nil {
		select {
		case <-m.current.done:
		default:
			return nil
		}
	}
	for _, port := range []int{c.HTTP, c.SOCKS} {
		if port == 0 {
			continue
		}
		l, err := net.Listen("tcp4", net.JoinHostPort(c.Bind, strconv.Itoa(port)))
		if err != nil {
			return fmt.Errorf("Порт %d недоступен: %w", port, err)
		}
		l.Close()
	}
	if err := os.MkdirAll(m.dir, 0700); err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join(m.dir, "runtime.cfg"), []byte(c.render())); err != nil {
		return err
	}
	cmd := exec.Command(m.exe, "runtime.cfg", "--desktop")
	cmd.Dir = m.dir
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := m.openHistory(); err != nil {
		return err
	}
	s := &session{cmd: cmd, done: make(chan struct{}), started: time.Now(), bucket: bucket{ledger: m.ledger}}
	cmd.Stdout = &output{session: s}
	cmd.Stderr = &output{session: s, telemetry: true}
	pipe, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	s.control = pipe
	if err = cmd.Start(); err != nil {
		pipe.Close()
		return fmt.Errorf("Запуск 3proxy: %w", err)
	}
	m.current = s
	m.stopped = false
	go func() {
		s.exitErr = cmd.Wait()
		s.mu.Lock()
		if err := s.bucket.flush(); err != nil {
			s.historyErr = err
		}
		s.mu.Unlock()
		close(s.done)
	}()
	deadline := time.NewTimer(8 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(80 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return fmt.Errorf("Ядро завершило работу при запуске: %s", s.diagnostic())
		case <-deadline.C:
			msg := s.diagnostic()
			m.stop()
			return fmt.Errorf("Ядро не подтвердило готовность: %s", msg)
		case <-ticker.C:
			s.mu.Lock()
			heartbeat := !s.last.IsZero()
			s.mu.Unlock()
			if heartbeat {
				ready, _, err := endpoints(cmd.Process.Pid, c)
				if err != nil {
					m.stop()
					return err
				}
				if ready {
					return nil
				}
			}
		}
	}
}

func (s *session) diagnostic() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.logs) > 0 {
		return s.logs[len(s.logs)-1]
	}
	select {
	case <-s.done:
		if s.exitErr != nil {
			return s.exitErr.Error()
		}
	default:
	}
	return "нет сообщения от ядра"
}

func (m *Manager) stop() error {
	s := m.current
	if s == nil {
		return nil
	}
	m.stopped = true
	select {
	case <-s.done:
		return nil
	default:
	}
	s.control.Close()
	select {
	case <-s.done:
		return s.exitErr
	case <-time.After(6 * time.Second):
	}
	if err := s.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	<-s.done
	return nil
}
func (m *Manager) Start() error { m.mu.Lock(); defer m.mu.Unlock(); return m.start(m.config) }
func (m *Manager) Stop() error  { m.mu.Lock(); defer m.mu.Unlock(); return m.stop() }
func (m *Manager) Restart() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.stop(); err != nil {
		return err
	}
	return m.start(m.config)
}
func (m *Manager) Configuration() Config { m.mu.Lock(); defer m.mu.Unlock(); return m.config }

func (m *Manager) Save(c Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c.User, c.verifier = m.config.User, m.config.verifier
	return m.save(c)
}

func (m *Manager) SetCredential(user, password string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.config
	if user == "" && password == "" {
		c.User, c.verifier = "", ""
		return m.save(c)
	}
	if !usernamePattern.MatchString(user) || len(password) < 1 || len(password) > 200 || strings.ContainsAny(password, "\x00\r\n") {
		return errors.New("Проверьте логин (латиница, цифры, ._-) и пароль (1–200 байт)")
	}
	var salt [16]byte
	if _, err := rand.Read(salt[:]); err != nil {
		return err
	}
	cmd := exec.Command(m.exe, "--desktop-hash", hex.EncodeToString(salt[:]))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	cmd.Stdin = strings.NewReader(password + "\n")
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("Ошибка создания учётной записи: %w", err)
	}
	c.User, c.verifier = user, strings.TrimSpace(string(out))
	return m.save(c)
}
func (m *Manager) save(c Config) error {
	c = c.normalize()
	if err := c.validate(); err != nil {
		return err
	}
	if m.configErr != nil {
		return m.configErr
	}
	if err := os.MkdirAll(m.dir, 0700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(storedConfig{Config: c, Verifier: c.verifier}, "", "  ")
	if err != nil {
		return err
	}
	if err = backupCurrent(m.dir); err != nil {
		return err
	}
	running := false
	if m.current != nil {
		select {
		case <-m.current.done:
		default:
			running = true
		}
	}
	restart := running && c.render() != m.config.render()
	if restart {
		if err = m.stop(); err != nil {
			return err
		}
		if err = m.start(c); err != nil {
			return errors.Join(err, m.start(m.config))
		}
	}
	if err = atomicWrite(filepath.Join(m.dir, "settings.json"), b); err != nil {
		if restart {
			return errors.Join(err, m.stop(), m.start(m.config))
		}
		return err
	}
	m.config = c
	return nil
}

func (m *Manager) Status() Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	v := Snapshot{Config: m.config, State: "stopped"}
	v.Totals = m.totals()
	if m.configErr != nil {
		v.State = "error"
		v.Detail = m.configErr.Error()
		return v
	}
	s := m.current
	if s == nil {
		return v
	}
	s.mu.Lock()
	if s.rx >= s.baseRX {
		v.RX = s.rx - s.baseRX
	}
	if s.tx >= s.baseTX {
		v.TX = s.tx - s.baseTX
	}
	v.History = append([]Sample{}, s.history...)
	v.Logs = append([]string{}, s.logs...)
	last := s.last
	if s.historyErr != nil {
		v.Detail = s.historyErr.Error()
	}
	s.mu.Unlock()
	select {
	case <-s.done:
		if !m.stopped {
			v.State = "error"
			v.Detail = s.diagnostic()
		}
		return v
	default:
	}
	v.Uptime = int(time.Since(s.started).Seconds())
	ready, clients, err := endpoints(s.cmd.Process.Pid, m.config)
	v.Clients = clients
	if err != nil {
		v.State = "error"
		v.Detail = err.Error()
	} else if !ready || time.Since(last) > 4*time.Second {
		v.State = "lost"
	} else {
		v.State = "running"
	}
	return v
}
