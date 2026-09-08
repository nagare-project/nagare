package sourceplugin

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	startupTimeout   = 10 * time.Second
	shutdownTimeout  = 3 * time.Second
	maximumReadyLine = 16 * 1024
)

type Manager struct {
	opMu sync.Mutex
	mu   sync.RWMutex

	status Status
	client *Client
	cmd    *exec.Cmd
	cancel context.CancelFunc
}

func NewManager() *Manager {
	return &Manager{status: Status{Phase: "disabled"}}
}

func (m *Manager) Start(config LaunchConfig) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	m.stopLocked("disabled")

	if err := validateLaunchConfig(config); err != nil {
		m.setFailed(err)
		return err
	}
	m.mu.Lock()
	m.status = Status{Phase: "starting"}
	m.mu.Unlock()

	processContext, cancel := context.WithCancel(context.Background())
	arguments := config.Arguments
	if arguments == nil {
		arguments = []string{"serve", "--root", config.Root, "--listen", "127.0.0.1:0", "--version", config.Version}
	}
	command := exec.CommandContext(processContext, config.Executable, arguments...)
	command.Stdin = nil
	stdout, err := command.StdoutPipe()
	if err != nil {
		cancel()
		m.setFailed(err)
		return err
	}
	// 子进程 stderr 可能包含上游 URL 或临时凭据，不能进入 nagare 状态或普通
	// 日志。插件应自行把已脱敏诊断写到它的日志中。
	command.Stderr = io.Discard
	if err := command.Start(); err != nil {
		cancel()
		m.setFailed(err)
		return err
	}
	wait := make(chan error, 1)
	go func() { wait <- command.Wait() }()
	ready := readReadyLine(stdout)

	timer := time.NewTimer(startupTimeout)
	defer timer.Stop()
	var line string
	reaped := false
	select {
	case result := <-ready:
		if result.err != nil {
			err = result.err
		} else {
			line = result.line
		}
	case waitErr := <-wait:
		reaped = true
		err = fmt.Errorf("source plugin exited before readiness: %v", waitErr)
	case <-timer.C:
		err = errors.New("source plugin readiness timed out")
	}
	if err != nil {
		cancel()
		if !reaped {
			killAndWait(command, wait)
		}
		m.setFailed(err)
		return err
	}

	event, err := parseReadyEvent(line)
	if err != nil {
		cancel()
		killAndWait(command, wait)
		m.setFailed(err)
		return err
	}
	client, err := NewClient(event.URL)
	if err == nil {
		ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		var manifest Manifest
		manifest, err = client.Negotiate(ctx)
		stop()
		if err == nil {
			now := time.Now().UnixMilli()
			m.mu.Lock()
			m.client, m.cmd, m.cancel = client, command, cancel
			m.status = Status{Phase: "ready", URL: event.URL, Manifest: &manifest, StartedAt: &now}
			m.mu.Unlock()
			go m.watch(command, wait)
			return nil
		}
	}
	cancel()
	killAndWait(command, wait)
	m.setFailed(err)
	return err
}

func validateLaunchConfig(config LaunchConfig) error {
	if !filepath.IsAbs(config.Executable) || !filepath.IsAbs(config.Root) {
		return errors.New("plugin executable and repository root must be absolute paths")
	}
	info, err := os.Stat(config.Executable)
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("plugin executable does not exist or is not a regular file")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		return errors.New("plugin executable is not executable")
	}
	root, err := os.Stat(config.Root)
	if err != nil || !root.IsDir() {
		return errors.New("plugin repository root does not exist or is not a directory")
	}
	if strings.TrimSpace(config.Version) == "" {
		return errors.New("host version is required")
	}
	return nil
}

type readyResult struct {
	line string
	err  error
}

func readReadyLine(reader io.Reader) <-chan readyResult {
	result := make(chan readyResult, 1)
	go func() {
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 1024), maximumReadyLine)
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				result <- readyResult{err: fmt.Errorf("read plugin readiness: %w", err)}
			} else {
				result <- readyResult{err: errors.New("plugin closed stdout before readiness")}
			}
			return
		}
		result <- readyResult{line: scanner.Text()}
		// 继续排空 stdout，避免异常健谈的插件把 pipe 写满后死锁。
		for scanner.Scan() {
		}
	}()
	return result
}

func parseReadyEvent(line string) (ReadyEvent, error) {
	var event ReadyEvent
	if err := decodeStrict([]byte(line), &event); err != nil {
		return ReadyEvent{}, errors.New("plugin readiness is not valid JSON")
	}
	if event.Event != "ready" || event.Protocol != LaunchProtocol || event.URL == "" {
		return ReadyEvent{}, errors.New("plugin readiness contract is unsupported")
	}
	if _, err := validateLoopbackURL(event.URL); err != nil {
		return ReadyEvent{}, err
	}
	return event, nil
}

func (m *Manager) Stop() {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	m.stopLocked("stopped")
}

func (m *Manager) stopLocked(phase string) {
	m.mu.Lock()
	cancel, command, client := m.cancel, m.cmd, m.client
	m.cancel, m.cmd, m.client = nil, nil, nil
	m.status = Status{Phase: phase}
	m.mu.Unlock()
	if client != nil {
		client.Close()
	}
	if cancel != nil {
		cancel()
	}
	if command != nil && command.Process != nil {
		_ = command.Process.Kill()
	}
}

func killAndWait(command *exec.Cmd, wait <-chan error) {
	if command.Process != nil {
		_ = command.Process.Kill()
	}
	select {
	case <-wait:
	case <-time.After(shutdownTimeout):
	}
}

func (m *Manager) watch(command *exec.Cmd, wait <-chan error) {
	err := <-wait
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cmd != command {
		return
	}
	if m.client != nil {
		m.client.Close()
	}
	m.client, m.cmd, m.cancel = nil, nil, nil
	if err == nil {
		m.status = Status{Phase: "stopped", Error: "source plugin exited"}
	} else {
		m.status = Status{Phase: "failed", Error: "source plugin exited unexpectedly"}
	}
}

func (m *Manager) setFailed(err error) {
	message := "source plugin failed"
	if err != nil {
		message = err.Error()
	}
	m.mu.Lock()
	m.status = Status{Phase: "failed", Error: message}
	m.mu.Unlock()
}

func (m *Manager) Status() Status {
	m.mu.RLock()
	defer m.mu.RUnlock()
	status := m.status
	if status.Manifest != nil {
		manifest := *status.Manifest
		status.Manifest = &manifest
	}
	return status
}

func (m *Manager) Sources(ctx context.Context) ([]Source, error) {
	client, err := m.readyClient()
	if err != nil {
		return nil, err
	}
	return client.Sources(ctx)
}

func (m *Manager) Candidates(ctx context.Context, request ResolveRequest, emit func(Event) error) error {
	client, err := m.readyClient()
	if err != nil {
		return err
	}
	return client.Candidates(ctx, request, emit)
}

func (m *Manager) readyClient() (*Client, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.client == nil || m.status.Phase != "ready" {
		return nil, errors.New("source plugin is not ready")
	}
	return m.client, nil
}
