// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Nimote contributors

package host

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

type processHandle interface {
	Wait() (int, error)
	Kill() error
	Close() error
}

type managed struct {
	resourceError error
	scope         *sessionScope
	runtimeLimit  int64
	activity      *activityJournal
	userCanceled  bool
	session       Session
	process       processHandle
	terminal      io.ReadWriteCloser
	output        *outputWriter
	done          chan struct{}
	agent         *structuredAgent
}

type Manager struct {
	limits       ResourceLimits
	monitorStop  chan struct{}
	monitorDone  chan struct{}
	monitorOnce  sync.Once
	monitorStart sync.Once
	requests     *requestLedger
	mu           sync.Mutex
	dir          string
	sessions     map[string]*managed
	revision     uint64
	closed       bool
	events       []Event
	eventReset   uint64
}

func NewManager(dir string) (*Manager, error) {
	return NewManagerWithLimits(dir, DefaultResourceLimits())
}

func NewManagerWithLimits(dir string, limits ResourceLimits) (*Manager, error) {
	if err := limits.Validate(); err != nil {
		return nil, err
	}
	if err := privateDir(dir); err != nil {
		return nil, err
	}
	m := &Manager{dir: dir, limits: limits, sessions: make(map[string]*managed), revision: uint64(time.Now().UnixNano()), monitorStop: make(chan struct{}), monitorDone: make(chan struct{})}
	requests, err := newRequestLedger(filepath.Join(dir, "requests"))
	if err != nil {
		return nil, err
	}
	m.requests = requests
	if err := m.loadEvents(); err != nil {
		return nil, err
	}
	if data, err := readPrivate(filepath.Join(dir, "event-reset")); err == nil {
		if err := decodeJSON(data, &m.eventReset); err != nil || m.eventReset >= ^uint64(0)-1024 {
			return nil, errors.New("invalid event reset watermark")
		}
		m.revision = max(m.revision, m.eventReset)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		if !validID(id) {
			return nil, errors.New("invalid session filename")
		}
		data, err := readPrivateLimit(filepath.Join(dir, entry.Name()), 2<<20)
		if err != nil {
			return nil, err
		}
		var record Session
		if err := decodeJSON(data, &record); err != nil {
			return nil, fmt.Errorf("load session %s: %w", id, err)
		}
		if record.ID != id {
			return nil, errors.New("session ID mismatch")
		}
		activity, err := loadActivity(filepath.Join(dir, id+".activity"))
		if err != nil {
			slog.Warn("load session activity", "session", id, "error", err)
			activity = &activityJournal{path: filepath.Join(dir, id+".activity"), failure: err}
		}
		var outputErr error
		if f, err := os.Open(filepath.Join(dir, id+".ring")); err == nil {
			total, readErr := ringTotal(f)
			f.Close()
			if readErr != nil {
				// Output is auxiliary data. Keep the session metadata and other
				// sessions available; OutputPage still reports the damaged log.
				outputErr = readErr
			} else {
				record.OutputBytes = total
				record.OutputTruncated = total > maxOutput
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			outputErr = err
		}

		if record.Revision >= m.revision {
			m.revision = record.Revision + 1
		}
		if record.State == "running" || record.State == "canceled" && record.Finished == nil {
			record.State = "interrupted"
			now := time.Now().UTC()
			record.Finished = &now
			record.Error = "Host stopped before process completion"
			if outputErr != nil {
				record.Error += "; output log is unavailable"
			}
			m.revision++
			record.Revision = m.revision
			if err := saveJSON(filepath.Join(dir, entry.Name()), record); err != nil {
				return nil, err
			}
			m.emit(record)
			_ = activity.append("session_interrupted", "disconnected", "", record.OutputBytes)
		}
		if outputErr != nil {
			slog.Warn("session output is unavailable", "session", id, "error", outputErr)
			if !strings.Contains(record.Error, "output log is unavailable") {
				record.Error = strings.TrimPrefix(record.Error+"; output log is unavailable", "; ")
			}
		}
		done := make(chan struct{})
		close(done)
		scope, err := loadSessionScope(dir, id)
		if err != nil {
			return nil, err
		}
		if err := recoverExecutionScope(scope, limits); err != nil {
			return nil, err
		}
		m.sessions[id] = &managed{session: record, done: done, activity: activity, scope: scope}
	}
	return m, nil
}

func validID(id string) bool {
	if len(id) != 64 {
		return false
	}
	for _, c := range id {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// Terminal adapters preserve the installed CLI interface. Structured Codex
// sessions use app-server and explicit semantic operations instead of PTY I/O.
func resolveCommand(req *StartRequest) error {
	if req.ExecutionPolicy != "" && !validExecutionPolicy(req.ExecutionPolicy) {
		return errors.New("unsupported execution policy")
	}
	if req.ExecutionPolicy != "" && req.ExecutionPolicy != "confirm" && (req.Kind != "agent" || req.Interaction != "structured") {
		return errors.New("execution policy requires a structured Agent session")
	}
	if req.Task != nil && (req.Task.ID == "" || len(req.Task.ID) > 512 || len(req.Task.Label) > 160 || strings.ContainsRune(req.Task.ID, '\x00')) {
		return errors.New("invalid task reference")
	}
	if req.Interaction != "" && req.Interaction != "terminal" && req.Interaction != "structured" {
		return errors.New("invalid interaction mode")
	}
	if req.Interaction == "structured" && (req.Kind != "agent" || !supportsStructuredAgent(req.Agent) || !structuredAgentSupported || len(req.Args) != 0) {
		return errors.New("structured mode requires a supported agent/platform without CLI arguments")
	}
	if req.Resume != nil {
		if req.Kind != "agent" || !supportsStructuredAgent(req.Agent) || (req.Interaction != "auto" && req.Interaction != "structured") || len(req.Args) != 0 ||
			req.Resume.ConversationID == "" || len(req.Resume.ConversationID) > 128 || strings.ContainsAny(req.Resume.ConversationID, "/\\\x00") {
			return errors.New("native conversation resume requires a valid provider conversation and structured Agent mode")
		}
	}
	if req.Kind == "" {
		req.Kind = "task"
	}
	switch req.Kind {
	case "task":
		if req.Command == "" {
			return errors.New("command is required")
		}
	case "terminal":
		if !terminalSupported {
			return errors.New("terminal is unsupported on this platform")
		}
		if req.Command == "" {
			req.Command = os.Getenv("SHELL")
			if req.Command == "" {
				if runtime.GOOS == "windows" {
					req.Command = "cmd.exe"
				} else {
					req.Command = "/bin/sh"
				}
			}
		}
	case "agent":
		if !terminalSupported && req.Interaction != "structured" {
			return errors.New("agent terminal is unsupported on this platform")
		}
		switch req.Agent {
		case "codex", "claude", "opencode":
			req.Command = req.Agent
		default:
			return errors.New("unknown agent adapter")
		}
	default:
		return errors.New("kind must be task, terminal, or agent")
	}
	if !filepath.IsAbs(req.Directory) {
		return errors.New("absolute workspace directory is required")
	}
	info, err := os.Stat(req.Directory)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("workspace is not a directory")
	}
	return nil
}

func (m *Manager) start(req StartRequest) (Session, error) {
	if req.Identity != nil {
		identity := *req.Identity
		req.Identity = &identity
	}
	if err := resolveCommand(&req); err != nil {
		return Session{}, err
	}
	if err := validateExecutionIdentity(req.Identity, req.Directory, m.limits); err != nil {
		return Session{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return Session{}, errors.New("Host is shutting down")
	}
	if len(m.sessions) >= m.limits.MaxRetainedSessions {
		return Session{}, fmt.Errorf("%w: session retention limit reached; delete completed sessions", errResourceLimit)
	}
	active := 0
	for _, s := range m.sessions {
		if s.process != nil {
			active++
		}
	}
	if active >= m.limits.MaxActiveSessions {
		return Session{}, fmt.Errorf("%w: active session limit reached", errResourceLimit)
	}
	if err := m.checkDisk(); err != nil {
		return Session{}, err
	}
	id, err := randomID()
	if err != nil {
		return Session{}, err
	}
	limits := m.limits
	var scope *sessionScope
	if req.Identity != nil {
		identity := *req.Identity
		if err := reserveExecutionIdentity(m.dir, id, identity); err != nil {
			return Session{}, err
		}
		limits.identity = &identity
		limits.scopeName = scopeName(m.dir, id)
		if limits.MemoryBytes == 0 {
			limits.MemoryBytes = 512 << 20
		}
		if limits.MaxProcesses == 0 {
			limits.MaxProcesses = 64
		}
		if limits.MaxRuntimeSeconds == 0 {
			limits.MaxRuntimeSeconds = 3600
		}
		scope = &sessionScope{Identity: identity, CgroupRoot: limits.CgroupRoot, Name: limits.scopeName, SessionID: id}
		if err := saveJSON(filepath.Join(m.dir, id+".scope"), scope); err != nil {
			return Session{}, err
		}
	}
	path := filepath.Join(m.dir, id+".ring")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return Session{}, err
	}
	output := &outputWriter{file: f}
	cmd := exec.Command(req.Command, req.Args...)
	cmd.Dir = req.Directory
	cmd.WaitDelay = 2 * time.Second
	record := Session{ExecutionPolicy: req.ExecutionPolicy, CompatibilityFallback: req.fallbackReason, Interaction: req.Interaction, ID: id, ConversationID: resumeConversationID(req.Resume), Kind: req.Kind, Agent: req.Agent, Command: req.Command, Args: append([]string(nil), req.Args...), Directory: req.Directory, State: "running", Started: time.Now().UTC()}
	m.revision++
	record.Revision = m.revision
	record.StartRequestID = req.RequestID
	if req.Task != nil {
		task := *req.Task
		record.Task = &task
	}
	// Persist intent before launching; a crash cannot leave a surviving process
	// with no record. Failed launch removes both files.
	if err := saveJSON(filepath.Join(m.dir, id+".json"), record); err != nil {
		f.Close()
		os.Remove(path)
		return Session{}, err
	}
	item := &managed{session: record, output: output, done: make(chan struct{}), scope: scope, runtimeLimit: limits.MaxRuntimeSeconds}
	item.activity = &activityJournal{path: filepath.Join(m.dir, id+".activity"), events: []Activity{}}
	if err := item.activity.append("session_started", "running", "", 0); err != nil {
		f.Close()
		os.Remove(path)
		os.Remove(filepath.Join(m.dir, id+".json"))
		return Session{}, err
	}
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	if req.Identity != nil {
		cmd.Env = identityEnvironment(req.Identity, req.Directory)
	}
	if req.Interaction == "structured" {
		item.agent = newStructuredAgent(req.Agent, output, req.Directory, record.ConversationID)
		item.agent.executionPolicy = req.ExecutionPolicy
		item.agent.onExecutionPolicy = func(policy string) error { return m.setExecutionPolicy(item, policy) }
		item.agent.onConversationID = func(conversationID string) { m.setConversationID(item, conversationID) }
		item.agent.onActivity = item.agentActivity
		item.process, err = startStructuredAgent(cmd, item.agent, limits)
	} else {
		item.process, item.terminal, err = startProcess(cmd, req.Kind != "task", output, limits)
	}
	if err != nil {
		f.Close()
		os.Remove(path)
		os.Remove(filepath.Join(m.dir, id+".json"))
		os.Remove(filepath.Join(m.dir, id+".activity"))
		return Session{}, fmt.Errorf("start %s: %w", req.Kind, err)
	}
	m.sessions[id] = item
	m.monitorStart.Do(func() { go m.monitorResources() })
	m.emit(record)
	slog.Info("Host session started", "id", id, "kind", req.Kind)
	go m.wait(item)
	record.ActivitySequence = item.activity.cursor()
	return cloneSession(record), nil
}

func resumeConversationID(resume *AgentResume) string {
	if resume == nil {
		return ""
	}
	return resume.ConversationID
}

// Called under the agent lock, before publishing or applying a policy change.
func (m *Manager) setExecutionPolicy(item *managed, policy string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	record := item.session
	record.ExecutionPolicy = policy
	record.Revision = m.revision + 1
	if err := saveJSON(filepath.Join(m.dir, record.ID+".json"), record); err != nil {
		return err
	}
	item.session = record
	m.revision = record.Revision
	m.emit(record)
	return nil
}

func (m *Manager) setConversationID(item *managed, conversationID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessions[item.session.ID] != item || item.session.ConversationID == conversationID {
		return
	}
	item.session.ConversationID = conversationID
	m.revision++
	item.session.Revision = m.revision
	if err := saveJSON(filepath.Join(m.dir, item.session.ID+".json"), item.session); err != nil {
		slog.Warn("could not persist Agent conversation id", "session", item.session.ID, "error", err)
		return
	}
	m.emit(item.session)
}

func (m *Manager) wait(item *managed) {
	var copied chan struct{}
	if item.terminal != nil {
		copied = make(chan struct{})
		go func() { _, _ = io.Copy(item.output, item.terminal); close(copied) }()
	}
	code, err := item.process.Wait()
	finalAgentStatus := ""
	if item.agent != nil {
		if code != 0 {
			if detail := item.agent.diagnostics.text(); detail != "" {
				item.agent.emit("error", detail)
			}
		}
		item.agent.close()
		item.agent.mu.Lock()
		err = errors.Join(err, item.agent.failure)
		finalAgentStatus = structuredTurnResult(item.agent.lastTurnStatus)
		item.agent.mu.Unlock()
	}
	if copied != nil {
		select {
		case <-copied:
		case <-time.After(time.Second):
		}
		_ = item.terminal.Close()
		<-copied
	}
	processErr := item.process.Close()
	outputErr := errors.Join(processErr, item.output.Close())
	m.mu.Lock()
	defer m.mu.Unlock()
	err = errors.Join(err, item.resourceError)
	now := time.Now().UTC()
	item.session.OutputBytes, item.session.OutputTruncated = item.output.stats()
	item.session.Finished = &now
	item.session.ExitCode = &code
	if item.session.State != "canceled" && item.session.State != "interrupted" {
		item.session.State = "succeeded"
		if err != nil || outputErr != nil {
			item.session.State = "failed"
		}
	}
	if err != nil {
		item.session.Error = err.Error()
	}
	if outputErr != nil {
		item.session.Error = outputErr.Error()
	}
	item.process = nil
	if item.session.Kind == "agent" {
		if item.session.Interaction == "structured" && item.session.State == "succeeded" {
			item.session.AgentStatus = finalAgentStatus
		} else {
			item.session.AgentStatus = sessionAgentStatus(item.session, nil)
		}
	}
	m.revision++
	item.session.Revision = m.revision
	if err := saveJSON(filepath.Join(m.dir, item.session.ID+".json"), item.session); err != nil {
		slog.Error("persist session completion", "id", item.session.ID, "error", err)
	}
	m.emit(item.session)
	item.recordActivity("session_finished", item.session.State, "")
	slog.Info("Host session finished", "id", item.session.ID, "state", item.session.State)
	close(item.done)
}

func cloneSession(s Session) Session {
	if s.Task != nil {
		task := *s.Task
		s.Task = &task
	}
	s.Args = append([]string(nil), s.Args...)
	if s.Finished != nil {
		finished := *s.Finished
		s.Finished = &finished
	}
	if s.ExitCode != nil {
		code := *s.ExitCode
		s.ExitCode = &code
	}
	return s
}
func (m *Manager) List(after uint64) []Session {
	m.mu.Lock()
	sessions := make([]Session, 0, len(m.sessions))
	agents := make(map[string]*structuredAgent)
	activity := make(map[string]*activityJournal)
	for _, item := range m.sessions {
		if item.session.Revision > after {
			record := cloneSession(item.session)
			if item.output != nil {
				record.OutputBytes, record.OutputTruncated = item.output.stats()
			}
			sessions = append(sessions, record)
			agents[record.ID] = item.agent
			activity[record.ID] = item.activity
		}
	}
	m.mu.Unlock()
	for i := range sessions {
		sessions[i].AgentStatus = sessionAgentStatus(sessions[i], agents[sessions[i].ID])
		sessions[i].ActivitySequence = activity[sessions[i].ID].cursor()
	}
	sortSessions(sessions)
	return sessions
}
func (m *Manager) Get(id string) (Session, error) {
	m.mu.Lock()
	item, ok := m.sessions[id]
	if !ok {
		m.mu.Unlock()
		return Session{}, os.ErrNotExist
	}
	record := cloneSession(item.session)
	if item.output != nil {
		record.OutputBytes, record.OutputTruncated = item.output.stats()
	}
	agent := item.agent
	activity := item.activity
	m.mu.Unlock()
	record.AgentStatus = sessionAgentStatus(record, agent)
	record.ActivitySequence = activity.cursor()
	return record, nil
}
func (m *Manager) Cancel(id string) error { return m.cancel(id, true) }
func (m *Manager) cancel(id string, userRequested bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	item, ok := m.sessions[id]
	if !ok {
		return os.ErrNotExist
	}
	if userRequested {
		item.userCanceled = true
	}
	if item.process == nil {
		return nil
	}
	if err := item.process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	item.session.State = "canceled"
	m.revision++
	item.session.Revision = m.revision
	if err := saveJSON(filepath.Join(m.dir, id+".json"), item.session); err != nil {
		return err
	}
	m.emit(item.session)
	return nil
}
func (m *Manager) Input(id string, data []byte) error {
	m.mu.Lock()
	item, ok := m.sessions[id]
	if !ok {
		m.mu.Unlock()
		return os.ErrNotExist
	}
	terminal := item.terminal
	if item.agent != nil {
		m.mu.Unlock()
		return errors.New("use agent-message for structured sessions")
	}
	running := item.process != nil
	m.mu.Unlock()
	if !running || terminal == nil {
		return errors.New("session has no active terminal")
	}
	n, err := terminal.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	if err != nil {
		return errors.Join(errRequestUncertain, err)
	}
	return err
}
func (m *Manager) Resize(id string, rows, cols uint16) error {
	if rows == 0 || cols == 0 || rows > 1000 || cols > 1000 {
		return errors.New("invalid terminal size")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	item, ok := m.sessions[id]
	if !ok {
		return os.ErrNotExist
	}
	if item.process == nil || item.terminal == nil {
		return errors.New("session has no active terminal")
	}
	return resizeTerminal(item.terminal, rows, cols)
}

// Output keeps the protocol-1 byte cursor contract. OutputPage also reports
// where retained output starts when an old cursor has fallen behind.
func (m *Manager) Output(id string, offset int64) ([]byte, int64, error) {
	data, _, next, err := m.OutputPage(id, offset)
	return data, next, err
}
func (m *Manager) outputSignal(id string) <-chan struct{} {
	m.mu.Lock()
	item := m.sessions[id]
	m.mu.Unlock()
	if item == nil || item.output == nil {
		return nil
	}
	return item.output.signal()
}

// Read the actual log boundary, not persisted Session.OutputBytes, which older
// prefix-log records may not contain. No Agent state lock is needed for output.
func (m *Manager) outputResync(id string, offset, start int64) bool {
	if start > offset {
		return true
	}
	m.mu.Lock()
	item := m.sessions[id]
	m.mu.Unlock()
	if item == nil {
		return false
	}
	if item.output != nil {
		total, _ := item.output.stats()
		return offset > total
	}
	f, err := os.Open(filepath.Join(m.dir, id+".ring"))
	if err == nil {
		defer f.Close()
		total, err := ringTotal(f)
		return err == nil && offset > total
	}
	if !errors.Is(err, os.ErrNotExist) {
		return false
	}
	info, err := os.Stat(filepath.Join(m.dir, id+".log"))
	return err == nil && offset > info.Size()
}

func (m *Manager) OutputPage(id string, offset int64) ([]byte, int64, int64, error) {
	if offset < 0 {
		return nil, offset, offset, errors.New("negative output offset")
	}
	m.mu.Lock()
	item, ok := m.sessions[id]
	m.mu.Unlock()
	if !ok {
		return nil, offset, offset, os.ErrNotExist
	}
	if item.output != nil {
		return item.output.read(offset)
	}
	f, err := os.Open(filepath.Join(m.dir, id+".ring"))
	if err == nil {
		defer f.Close()
		return readRing(f, offset)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, offset, offset, err
	}
	// Older Hosts stored plain prefix logs. Keep them readable after upgrading.
	f, err = os.Open(filepath.Join(m.dir, id+".log"))
	if err != nil {
		return nil, offset, offset, err
	}
	defer f.Close()
	data := make([]byte, 64<<10)
	n, err := f.ReadAt(data, offset)
	if errors.Is(err, io.EOF) {
		err = nil
	}
	return data[:n], offset, offset + int64(n), err
}
func (m *Manager) Delete(id string) error { return m.delete(id, false) }
func (m *Manager) delete(id string, probeOnly bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	item, ok := m.sessions[id]
	if !ok {
		return os.ErrNotExist
	}
	if probeOnly && item.userCanceled {
		return errProbeCanceled
	}
	if item.process != nil {
		return errors.New("cancel and await session completion before deletion")
	}
	if err := os.Remove(filepath.Join(m.dir, id+".json")); err != nil {
		return err
	}
	delete(m.sessions, id)
	m.revision++
	item.session.Revision = m.revision
	item.session.State = "deleted"
	m.emit(item.session)
	for _, ext := range []string{".ring", ".log", ".activity", ".scope"} {
		if err := os.Remove(filepath.Join(m.dir, id+ext)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// PrepareIdleShutdown atomically rejects shutdown if any process is still owned
// and prevents new sessions from starting once an idle shutdown is accepted.
func (m *Manager) PrepareIdleShutdown() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, item := range m.sessions {
		if item.process != nil {
			return errors.New("Host has active tasks; let them finish before running setup again")
		}
	}
	m.closed = true
	return nil
}

func (m *Manager) Close() {
	m.mu.Lock()
	m.closed = true
	if m.monitorStop != nil {
		// Starting and closing the monitor are serialized with session admission.
		// A Manager used only to read recovered state needs no background worker.
		m.monitorStart.Do(func() { close(m.monitorDone) })
		m.monitorOnce.Do(func() { close(m.monitorStop) })
	}
	var done []chan struct{}
	for _, item := range m.sessions {
		if item.process != nil {
			if item.session.State != "canceled" {
				item.session.State = "interrupted"
			}
			_ = item.process.Kill()
			done = append(done, item.done)
		}
	}
	m.mu.Unlock()
	if m.monitorDone != nil {
		<-m.monitorDone
	}
	for _, ch := range done {
		<-ch
	}
}
