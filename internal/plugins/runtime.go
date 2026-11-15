// SPDX-License-Identifier: Apache-2.0
// Package plugins implements the Host-independent registry and process runtime.
package plugins

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"sync"
	"time"

	sdk "github.com/mobiledevloperlab/nimotehost/pkg/pluginsdk"
)

const maxPlugins = 16

// Connection is backed by the existing Host process supervisor on every platform.
type Connection struct {
	Input  io.WriteCloser
	Output io.ReadCloser
	Stop   func()
}
type Launcher func(context.Context, string, string) (*Connection, error)
type installed struct {
	Manifest sdk.Manifest `json:"manifest"`
	Enabled  bool         `json:"enabled"`
	Grants   []string     `json:"grants"`
}
type plugin struct {
	installed
	state      string
	generation uint64
	cancel     context.CancelFunc
	queue      chan work
	done       chan struct{}
}
type work struct {
	ctx      context.Context
	request  sdk.Request
	progress func(float64)
	done     chan outcome
}
type outcome struct {
	result json.RawMessage
	err    error
}
type Entry struct {
	ID     string `json:"id"`
	Plugin string `json:"plugin"`
	State  string `json:"state"`
	sdk.Capability
}
type PluginInfo struct {
	ID       string       `json:"id"`
	Version  string       `json:"version"`
	State    string       `json:"state"`
	Manifest sdk.Manifest `json:"manifest"`
	Grants   []string     `json:"grants"`
}
type Event struct {
	Sequence uint64   `json:"sequence"`
	Type     string   `json:"type"`
	ID       string   `json:"id"`
	State    string   `json:"state,omitempty"`
	Progress *float64 `json:"progress,omitempty"`
}
type Page struct {
	Epoch  string  `json:"epoch"`
	Cursor uint64  `json:"cursor"`
	Resync bool    `json:"resync_required"`
	Events []Event `json:"events"`
}
type Invocation struct {
	ID         string          `json:"id"`
	Capability string          `json:"capability"`
	State      string          `json:"state"`
	Result     json.RawMessage `json:"result,omitempty"`
	Error      *sdk.Error      `json:"error,omitempty"`
	cancel     context.CancelFunc
}
type Runtime struct {
	mu       sync.Mutex
	dir      string
	launch   Launcher
	plugins  map[string]*plugin
	runs     map[string]*Invocation
	order    []string
	events   []Event
	sequence uint64
	epoch    string
	next     uint64
	keys     map[string][32]byte
	ids      map[string]string
	closed   bool
	wg       sync.WaitGroup
}

func New(dir, epoch string, launch Launcher) (*Runtime, error) {
	r := &Runtime{dir: dir, epoch: epoch, launch: launch, plugins: map[string]*plugin{}, runs: map[string]*Invocation{}}
	info, err := os.Lstat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return r, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("unsafe plugin root")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !sdk.Name.MatchString(e.Name()) {
			continue
		}
		if len(r.plugins) >= maxPlugins {
			break
		}
		rec, err := loadInstalled(filepath.Join(dir, e.Name()), e.Name())
		if err != nil {
			slog.Warn("plugin package rejected", "id", e.Name(), "code", "invalid_package")
			continue
		}
		r.plugins[e.Name()] = &plugin{installed: rec, state: "disabled"}
	}
	for id, p := range r.plugins {
		if p.Enabled {
			r.startLocked(id, p)
		}
	}
	return r, nil
}
func loadInstalled(path, id string) (installed, error) {
	var rec installed
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return rec, errors.New("unsafe plugin directory")
	}
	b, err := readRegular(filepath.Join(path, "installed.json"), 64<<10)
	if err != nil {
		return rec, err
	}
	if err = json.Unmarshal(b, &rec); err != nil {
		return rec, err
	}
	if err = checkManifest(rec.Manifest); err != nil {
		return rec, err
	}
	if rec.Manifest.ID != id {
		return rec, errors.New("plugin identity mismatch")
	}
	if err = verify(path, rec.Manifest); err != nil {
		return rec, err
	}
	if rec.Enabled && !samePermissions(rec.Grants, rec.Manifest.Permissions) {
		return rec, errors.New("invalid stored grants")
	}
	return rec, nil
}
func readRegular(path string, max int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > max {
		return nil, errors.New("unsafe or oversized plugin file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, max+1))
}
func checkManifest(m sdk.Manifest) error {
	if err := m.Validate(); err != nil {
		return err
	}
	for _, c := range m.Capabilities {
		if _, err := compile(c.Input); err != nil {
			return err
		}
		if _, err := compile(c.Output); err != nil {
			return err
		}
	}
	return nil
}
func verify(dir string, m sdk.Manifest) error {
	b, err := readRegular(filepath.Join(dir, m.Entrypoint), 64<<20)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(b)
	if hex.EncodeToString(sum[:]) != m.SHA256 {
		return errors.New("plugin integrity mismatch")
	}
	return nil
}
func samePermissions(a, b []string) bool {
	a = slices.Clone(a)
	b = slices.Clone(b)
	sort.Strings(a)
	sort.Strings(b)
	return slices.Equal(a, b)
}
func (r *Runtime) persist(id string, p *plugin) error {
	path := filepath.Join(r.dir, id, "installed.json")
	b, err := json.Marshal(p.installed)
	if err != nil {
		return err
	}
	// Host state directory is private and held by the existing daemon lock.
	f, err := os.CreateTemp(filepath.Dir(path), ".record-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

// Install copies a local, reviewed two-file bundle. No shell, archive extraction,
// URL download or install hook runs. Updates require disable and revoke old grants.
func (r *Runtime) Install(source, expected string, update bool) error {
	if !filepath.IsAbs(source) {
		return errors.New("absolute bundle directory required")
	}
	info, err := os.Lstat(source)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("unsafe bundle directory")
	}
	b, err := readRegular(filepath.Join(source, "manifest.json"), 60<<10)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(b)
	if expected == "" || hex.EncodeToString(sum[:]) != expected {
		return errors.New("reviewed manifest SHA256 required")
	}
	var m sdk.Manifest
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&m); err != nil {
		return err
	}
	if dec.Decode(new(any)) != io.EOF {
		return errors.New("expected one manifest object")
	}
	if err = checkManifest(m); err != nil {
		return err
	}
	if err = verify(source, m); err != nil {
		return err
	}
	binary, err := readRegular(filepath.Join(source, m.Entrypoint), 64<<20)
	if err != nil {
		return err
	}
	sum = sha256.Sum256(binary)
	if hex.EncodeToString(sum[:]) != m.SHA256 {
		return errors.New("bundle changed during installation")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return errors.New("runtime closed")
	}
	old, exists := r.plugins[m.ID]
	if exists && (!update || old.Enabled) {
		return errors.New("disable plugin before explicit update")
	}
	if !exists && update {
		return errors.New("plugin not installed")
	}
	if !exists && len(r.plugins) >= maxPlugins {
		return errors.New("plugin capacity reached")
	}
	if err = os.MkdirAll(r.dir, 0700); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(r.dir, ".install-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err = os.WriteFile(filepath.Join(stage, m.Entrypoint), binary, 0700); err != nil {
		return err
	}
	p := &plugin{installed: installed{Manifest: m, Grants: []string{}}, state: "disabled"}
	rec, _ := json.Marshal(p.installed)
	if err = os.WriteFile(filepath.Join(stage, "installed.json"), rec, 0600); err != nil {
		return err
	}
	target := filepath.Join(r.dir, m.ID)
	if exists {
		// Keep old package in a private backup until the replacement has committed.
		backup, err := os.MkdirTemp(r.dir, ".previous-")
		if err != nil {
			return err
		}
		os.Remove(backup)
		if err = os.Rename(target, backup); err != nil {
			return err
		}
		if err = os.Rename(stage, target); err != nil {
			_ = os.Rename(backup, target)
			return err
		}
		_ = os.RemoveAll(backup)
	} else if err = os.Rename(stage, target); err != nil {
		return err
	}
	r.plugins[m.ID] = p
	r.emitLocked(Event{Type: "plugin.installed", ID: m.ID, State: "disabled"})
	return nil
}
func (r *Runtime) Enable(id string, grants []string, confirmed bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	p := r.plugins[id]
	if r.closed || p == nil {
		return errors.New("plugin unavailable")
	}
	if !confirmed || !samePermissions(grants, p.Manifest.Permissions) {
		return errors.New("explicit confirmation and exact permissions required")
	}
	if p.Enabled {
		return nil
	}
	if err := verify(filepath.Join(r.dir, id), p.Manifest); err != nil {
		return err
	}
	p.Enabled = true
	p.Grants = slices.Clone(grants)
	if err := r.persist(id, p); err != nil {
		p.Enabled = false
		p.Grants = nil
		return err
	}
	r.startLocked(id, p)
	return nil
}
func (r *Runtime) startLocked(id string, p *plugin) {
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	p.Enabled = true
	p.generation++
	gen := p.generation
	p.queue = make(chan work, 8)
	p.state = "starting"
	r.emitLocked(Event{Type: "plugin.state", ID: id, State: p.state})
	queue, manifest := p.queue, p.Manifest
	done := make(chan struct{})
	p.done = done
	r.wg.Add(1)
	go func() { defer r.wg.Done(); defer close(done); r.supervise(ctx, id, p, gen, queue, manifest) }()
}
func (r *Runtime) Disable(id string) error {
	r.mu.Lock()
	p := r.plugins[id]
	if p == nil {
		r.mu.Unlock()
		return errors.New("plugin not found")
	}
	old := p.installed
	p.Enabled = false
	p.Grants = []string{}
	if err := r.persist(id, p); err != nil {
		p.installed = old
		r.mu.Unlock()
		return err
	}
	done := p.done
	if p.cancel != nil {
		p.cancel()
	}
	p.generation++
	p.state = "disabled"
	for _, run := range r.runs {
		if run.State == "running" && namespace(run.Capability) == id {
			run.cancel()
		}
	}
	r.emitLocked(Event{Type: "plugin.disabled", ID: id, State: p.state})
	r.mu.Unlock()
	if done != nil {
		<-done
	}
	return nil
}
func (r *Runtime) Uninstall(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	p := r.plugins[id]
	if p == nil || p.Enabled {
		return errors.New("plugin must be installed and disabled")
	}
	if !sdk.Name.MatchString(id) {
		return errors.New("invalid plugin id")
	}
	if err := os.RemoveAll(filepath.Join(r.dir, id)); err != nil {
		return err
	}
	delete(r.plugins, id)
	r.emitLocked(Event{Type: "plugin.uninstalled", ID: id})
	return nil
}
func (r *Runtime) emitLocked(e Event) {
	r.sequence++
	e.Sequence = r.sequence
	r.events = append(r.events, e)
	if len(r.events) > 256 {
		r.events = append([]Event(nil), r.events[len(r.events)-256:]...)
	}
	if e.Type == "invocation.progress" {
		return
	}
	slog.Info("plugin audit", "type", e.Type, "id", e.ID, "state", e.State)
}
func (r *Runtime) state(id string, p *plugin, gen uint64, state string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.plugins[id] == p && p.generation == gen && p.Enabled {
		p.state = state
		r.emitLocked(Event{Type: "plugin.state", ID: id, State: state})
	}
}
func (r *Runtime) List() []PluginInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]PluginInfo, 0, len(r.plugins))
	for id, p := range r.plugins {
		out = append(out, PluginInfo{id, p.Manifest.Version, p.state, p.Manifest, slices.Clone(p.Grants)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return clone(out)
}
func clone[T any](v T) T { b, _ := json.Marshal(v); var out T; _ = json.Unmarshal(b, &out); return out }
func (r *Runtime) Capabilities() []Entry {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []Entry{}
	for id, p := range r.plugins {
		for _, c := range p.Manifest.Capabilities {
			out = append(out, Entry{id + "/" + c.Name, id, p.state, c})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return clone(out)
}
func (r *Runtime) Events(epoch string, after uint64) Page {
	r.mu.Lock()
	defer r.mu.Unlock()
	resync := epoch != r.epoch || after > r.sequence || (len(r.events) > 0 && after < r.events[0].Sequence-1)
	page := Page{r.epoch, r.sequence, resync, []Event{}}
	if !resync {
		for _, e := range r.events {
			if e.Sequence > after {
				page.Events = append(page.Events, e)
			}
		}
	}
	return clone(page)
}
func namespace(id string) string {
	for i, c := range id {
		if c == '/' {
			return id[:i]
		}
	}
	return ""
}
func (r *Runtime) Invoke(capability string, input json.RawMessage, confirmed bool, requestKeys ...string) (Invocation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p := r.plugins[namespace(capability)]
	if r.closed || p == nil || !p.Enabled || p.state != "ready" {
		return Invocation{}, errors.New("capability unavailable")
	}
	key := ""
	if len(requestKeys) > 0 {
		key = requestKeys[0]
	}
	fingerprint := sha256.Sum256(append(append([]byte(capability+fmt.Sprint(confirmed)), 0), input...))
	if key != "" {
		if r.keys == nil {
			r.keys = map[string][32]byte{}
			r.ids = map[string]string{}
		}
		if previous, ok := r.keys[key]; ok {
			if previous != fingerprint {
				return Invocation{}, errors.New("request_id content conflict")
			}
			if run := r.runs[r.ids[key]]; run != nil {
				return clone(*run), nil
			}
			return Invocation{}, errors.New("result_retired; invocation will not be replayed")
		}
		if len(r.keys) >= 4096 {
			return Invocation{}, errors.New("request receipt capacity reached for this epoch")
		}
	}

	var cap *sdk.Capability
	for _, c := range p.Manifest.Capabilities {
		if p.Manifest.ID+"/"+c.Name == capability {
			cc := c
			cap = &cc
			break
		}
	}
	if cap == nil {
		return Invocation{}, errors.New("capability not found")
	}
	if cap.ACP != 0 {
		return Invocation{}, errors.New("ACP capabilities use capabilities.invoke with session input at Host boundary")
	}
	if cap.Sensitive && !confirmed {
		return Invocation{}, errors.New("explicit confirmation required")
	}
	if err := validate(cap.Input, input); err != nil {
		return Invocation{}, errors.New("input schema violation")
	}
	// Bound retained results without ever evicting active work.
	if len(r.order) >= 32 {
		found := false
		for i, id := range r.order {
			if r.runs[id].State != "running" {
				delete(r.runs, id)
				r.order = append(r.order[:i], r.order[i+1:]...)
				found = true
				break
			}
		}
		if !found {
			return Invocation{}, errors.New("invocation capacity reached")
		}
	}
	r.next++
	id := fmt.Sprintf("%s-%d", r.epoch, r.next)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	run := &Invocation{ID: id, Capability: capability, State: "running", cancel: cancel}
	req := work{ctx: ctx, request: sdk.Request{ID: id, Method: "invoke", Capability: cap.Name, Input: slices.Clone(input)}, done: make(chan outcome, 1), progress: func(progress float64) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.emitLocked(Event{Type: "invocation.progress", ID: id, Progress: &progress})
	}}
	select {
	case p.queue <- req:
	default:
		cancel()
		return Invocation{}, errors.New("plugin busy")
	}
	if key != "" {
		r.keys[key] = fingerprint
		r.ids[key] = id
	}
	r.runs[id] = run
	r.order = append(r.order, id)
	r.emitLocked(Event{Type: "invocation.state", ID: id, State: "running"})
	outputSchema := slices.Clone(cap.Output)
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		defer cancel()
		var result outcome
		select {
		case result = <-req.done:
		case <-ctx.Done():
			result.err = ctx.Err()
		}
		r.mu.Lock()
		defer r.mu.Unlock()
		run.State = "succeeded"
		if ctx.Err() != nil {
			run.State = "canceled"
			run.Error = &sdk.Error{Code: "canceled", Message: "invocation canceled or timed out"}
		} else if result.err != nil || validate(outputSchema, result.result) != nil {
			run.State = "failed"
			run.Error = &sdk.Error{Code: "plugin_error", Message: "plugin failed or returned invalid output"}
		} else {
			run.Result = slices.Clone(result.result)
		}
		r.emitLocked(Event{Type: "invocation.state", ID: id, State: run.State})
	}()
	return clone(*run), nil
}
func (r *Runtime) GetInvocation(id string) (Invocation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	run := r.runs[id]
	if run == nil {
		return Invocation{}, errors.New("invocation not found")
	}
	return clone(*run), nil
}
func (r *Runtime) Cancel(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	run := r.runs[id]
	if run == nil {
		return errors.New("invocation not found")
	}
	run.cancel()
	r.emitLocked(Event{Type: "invocation.cancel", ID: id})
	return nil
}
func (r *Runtime) Close() {
	r.mu.Lock()
	r.closed = true
	for _, p := range r.plugins {
		if p.cancel != nil {
			p.cancel()
		}
	}
	for _, run := range r.runs {
		run.cancel()
	}
	r.mu.Unlock()
	r.wg.Wait()
}

// Exchange owns exactly one transaction. Cancellation closes the transport via
// the supervisor, including a plugin that blocks reading stdin or writing stdout.
func exchange(ctx context.Context, c *Connection, scan *bufio.Scanner, req sdk.Request, progress func(float64)) (json.RawMessage, error) {
	stop := context.AfterFunc(ctx, c.Stop)
	defer stop()
	b, err := json.Marshal(req)
	if err != nil || len(b) > sdk.MaxFrame-1 {
		return nil, errors.New("request too large")
	}
	if _, err = c.Input.Write(append(b, '\n')); err != nil {
		return nil, err
	}
	progressCount := 0
	for scan.Scan() {
		var res sdk.Response
		if err = json.Unmarshal(scan.Bytes(), &res); err != nil || res.ID != req.ID {
			return nil, errors.New("invalid plugin response")
		}
		if res.Progress != nil {
			progressCount++
			if progressCount > 100 {
				return nil, errors.New("excessive progress frames")
			}
			if progress != nil && *res.Progress >= 0 && *res.Progress <= 1 {
				progress(*res.Progress)
			}
			continue
		}
		if res.Error != nil {
			return nil, res.Error
		}
		if req.Method == "initialize" && res.Protocol != sdk.Protocol {
			return nil, errors.New("incompatible plugin protocol")
		}
		if !json.Valid(res.Result) {
			return nil, errors.New("invalid plugin result")
		}
		return res.Result, nil
	}
	return nil, errors.New("plugin disconnected")
}
func (r *Runtime) supervise(ctx context.Context, id string, p *plugin, gen uint64, queue chan work, manifest sdk.Manifest) {
	failures := 0
	for ctx.Err() == nil {
		if failures >= 3 {
			r.state(id, p, gen, "failed")
			return
		}
		if failures > 0 {
			r.state(id, p, gen, "recovering")
			timer := time.NewTimer(time.Duration(failures) * time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
		dir := filepath.Join(r.dir, id)
		if err := verify(dir, manifest); err != nil {
			r.state(id, p, gen, "failed")
			return
		}
		conn, err := r.launch(ctx, filepath.Join(dir, manifest.Entrypoint), dir)
		if err != nil {
			failures++
			continue
		}
		scan := bufio.NewScanner(conn.Output)
		scan.Buffer(make([]byte, 4096), sdk.MaxFrame)
		initCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		_, err = exchange(initCtx, conn, scan, sdk.Request{ID: "initialize", Method: "initialize", Protocol: sdk.Protocol}, nil)
		cancel()
		if err != nil {
			conn.Stop()
			failures++
			continue
		}
		r.state(id, p, gen, "ready")
		ticker := time.NewTicker(5 * time.Second)
		healthy := true
		for healthy && ctx.Err() == nil {
			select {
			case <-ctx.Done():
				healthy = false
			case req := <-queue:
				if req.ctx.Err() != nil {
					req.done <- outcome{err: req.ctx.Err()}
					continue
				}
				callCtx, cancel := context.WithCancel(req.ctx)
				stop := context.AfterFunc(ctx, cancel)
				value, callErr := exchange(callCtx, conn, scan, req.request, req.progress)
				stop()
				cancel()
				var pluginError *sdk.Error
				if callErr != nil && !errors.As(callErr, &pluginError) {
					r.state(id, p, gen, "recovering")
					healthy = false
				}
				req.done <- outcome{value, callErr}
				// Errors are never replayed, even if side effects may have happened.

			case <-ticker.C:
				pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
				_, err = exchange(pingCtx, conn, scan, sdk.Request{ID: "ping", Method: "ping"}, nil)
				cancel()
				if err != nil {
					healthy = false
				}
			}
		}
		ticker.Stop()
		conn.Stop()
		failures++
	}
}

// VerifyCapability pins integrity checks to an enabled, ready manifest.
func (r *Runtime) VerifyCapability(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	p := r.plugins[namespace(id)]
	if r.closed || p == nil || !p.Enabled || p.state != "ready" {
		return errors.New("capability unavailable")
	}
	return verify(filepath.Join(r.dir, p.Manifest.ID), p.Manifest)
}
