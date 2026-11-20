// SPDX-License-Identifier: Apache-2.0
package host

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ToolEnvironmentConfig is intentionally separate from config.json: GUI settings
// writers and older clients must not erase executable overrides when saving ports.
type ToolEnvironmentConfig struct {
	Executables  map[string]string `json:"executables,omitempty"`
	SearchPaths  []string          `json:"search_paths,omitempty"`
	DisableShell bool              `json:"disable_shell,omitempty"`
}

type toolSearchDirectory struct {
	Path   string `json:"path"`
	Source string `json:"source"`
}

type ToolEnvironmentStatus struct {
	RefreshedAt time.Time             `json:"refreshed_at"`
	ShellStatus string                `json:"shell_status"`
	SearchPaths []toolSearchDirectory `json:"search_paths"`
	Executables map[string]string     `json:"executables,omitempty"`
}

type toolEnvironmentSnapshot struct {
	ToolEnvironmentStatus
	environment []string
}

type toolEnvironment struct {
	dir       string
	base      []string
	mu        sync.Mutex
	snapshot  atomic.Pointer[toolEnvironmentSnapshot]
	lastRetry time.Time
}

// Enabled by the executable entry point, not by library imports. This keeps
// embedders and independently configured identity/plugin environments unchanged.
var activeToolEnvironment atomic.Pointer[toolEnvironment]

var errToolRefreshBusy = errors.New("tool environment refresh is already in progress")

func readToolEnvironmentConfig(dir string) (ToolEnvironmentConfig, error) {
	var c ToolEnvironmentConfig
	data, err := readPrivateLimit(filepath.Join(dir, "tools.json"), 64<<10)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if err := decodeJSON(data, &c); err != nil {
		return c, err
	}
	if len(c.SearchPaths) > 128 || len(c.Executables) > 128 {
		return c, errors.New("too many tool paths")
	}
	for _, path := range c.SearchPaths {
		if !validToolPath(path) || strings.ContainsRune(path, os.PathListSeparator) {
			return c, errors.New("tool search paths must be absolute directories")
		}
	}
	for name, path := range c.Executables {
		if name == "" || strings.ContainsAny(name, "/\\\x00\r\n:=") || !validToolPath(path) {
			return c, errors.New("tool overrides require a command name and absolute executable path")
		}
	}
	if runtime.GOOS == "windows" {
		normalized := make(map[string]string, len(c.Executables))
		for name, path := range c.Executables {
			key := strings.ToLower(name)
			if _, exists := normalized[key]; exists {
				return c, errors.New("duplicate Windows executable override")
			}
			normalized[key] = path
		}
		c.Executables = normalized
	}
	return c, nil
}

func validToolPath(path string) bool {
	return filepath.IsAbs(path) && !strings.ContainsAny(path, "\x00\r\n")
}

func explicitToolCommand(name string) bool {
	return filepath.Base(name) != name || filepath.VolumeName(name) != ""
}

// InitializeToolEnvironment resolves the logged-in account's tools once. Shell
// failure is reported in diagnostics and falls back to platform directories.
// Invalid explicit configuration fails closed and never replaces a good snapshot.
func InitializeToolEnvironment(ctx context.Context, dir string) error {
	r := &toolEnvironment{dir: dir, base: os.Environ()}
	if err := r.refresh(ctx); err != nil {
		return err
	}
	activeToolEnvironment.Store(r)
	return nil
}

func (r *toolEnvironment) refresh(ctx context.Context) error {
	if !r.mu.TryLock() {
		return errToolRefreshBusy
	}
	defer r.mu.Unlock()
	return r.refreshLocked(ctx)
}

func (r *toolEnvironment) refreshLocked(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c, err := readToolEnvironmentConfig(r.dir)
	if err != nil {
		return fmt.Errorf("read tools.json: %w", err)
	}
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	shell, status := probeToolEnvironment(probeCtx, r.base, c.DisableShell)
	if err := ctx.Err(); err != nil {
		return err
	}
	s := &toolEnvironmentSnapshot{ToolEnvironmentStatus: ToolEnvironmentStatus{RefreshedAt: time.Now().UTC(), ShellStatus: status, Executables: c.Executables}}
	seen := map[string]bool{}
	add := func(source string, paths []string) {
		for _, path := range paths {
			if !validToolPath(path) {
				continue
			}
			path = filepath.Clean(path)
			key := path
			if runtime.GOOS == "windows" {
				key = strings.ToLower(key)
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			s.SearchPaths = append(s.SearchPaths, toolSearchDirectory{path, source})
		}
	}
	add("configured", c.SearchPaths)
	source := "shell"
	if runtime.GOOS == "windows" {
		source = "windows_environment"
	}
	add(source, filepath.SplitList(environmentValue(shell, "PATH")))
	add("inherited", filepath.SplitList(environmentValue(r.base, "PATH")))
	add("platform", platformToolDirectories(r.base))
	var paths []string
	for _, entry := range s.SearchPaths {
		paths = append(paths, entry.Path)
	}
	s.environment = append([]string(nil), r.base...)
	// Only runtime selectors are imported. Never import shell credentials, loader
	// variables, HOME or arbitrary startup-script environment into the daemon.
	for _, key := range toolRuntimeVariables {
		if value := environmentValue(shell, key); value != "" {
			s.environment = replaceEnvironment(s.environment, key, value)
		}
	}
	s.environment = replaceEnvironment(s.environment, "PATH", strings.Join(paths, string(os.PathListSeparator)))
	r.snapshot.Store(s)
	return nil
}

var toolRuntimeVariables = []string{"NVM_DIR", "NVM_HOME", "NVM_SYMLINK", "FNM_DIR", "FNM_MULTISHELL_PATH", "VOLTA_HOME", "ASDF_DATA_DIR", "ASDF_DIR", "MISE_DATA_DIR", "MISE_CONFIG_DIR", "PNPM_HOME", "BUN_INSTALL", "CARGO_HOME", "RUSTUP_HOME"}

func toolOverrideKey(name string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(name)
	}
	return name
}

func environmentValue(env []string, name string) string {
	for i := len(env) - 1; i >= 0; i-- {
		key, value, ok := strings.Cut(env[i], "=")
		if ok && (key == name || runtime.GOOS == "windows" && strings.EqualFold(key, name)) {
			return value
		}
	}
	return ""
}

func replaceEnvironment(env []string, name, value string) []string {
	result := make([]string, 0, len(env)+1)
	for _, entry := range env {
		key, _, _ := strings.Cut(entry, "=")
		if key == name || runtime.GOOS == "windows" && strings.EqualFold(key, name) {
			continue
		}
		result = append(result, entry)
	}
	return append(result, name+"="+value)
}

func (s *toolEnvironmentSnapshot) resolve(name string) (string, string, error) {
	if path, ok := s.Executables[toolOverrideKey(name)]; ok {
		resolved, err := executableToolPath(path, s.environment)
		if err != nil {
			return "", "configured", fmt.Errorf("configured executable for %s is unavailable: %w", name, err)
		}
		return resolved, "configured", nil
	}
	if explicitToolCommand(name) {
		// Explicit paths retain exec.Command's normal relative-path semantics.
		path, err := exec.LookPath(name)
		return path, "explicit", err
	}
	for _, entry := range s.SearchPaths {
		if path, err := executableToolPath(filepath.Join(entry.Path, name), s.environment); err == nil {
			return path, entry.Source, nil
		}
	}
	if path := platformRegisteredTool(name); validToolPath(path) {
		if path, err := executableToolPath(path, s.environment); err == nil {
			return path, "app_paths", nil
		}
	}
	return "", "not_found", fmt.Errorf("%s: %w", name, exec.ErrNotFound)
}

func executableToolPath(path string, env []string) (string, error) {
	if runtime.GOOS != "windows" {
		return exec.LookPath(path)
	}
	// Search absolute paths only; Windows' implicit current-directory lookup must
	// not allow a workspace executable to shadow an installed Agent.
	exts := []string{""}
	if filepath.Ext(path) == "" {
		extensions := environmentValue(env, "PATHEXT")
		if extensions == "" {
			extensions = ".COM;.EXE;.BAT;.CMD"
		}
		for _, ext := range strings.Split(extensions, ";") {
			if strings.HasPrefix(ext, ".") && !strings.ContainsAny(ext, "/\\:") {
				exts = append(exts, ext)
			}
		}
	}
	for _, ext := range exts {
		if p, err := exec.LookPath(path + ext); err == nil {
			return p, nil
		}
	}
	return "", exec.ErrNotFound
}

func resolveTool(name string, launch bool) (string, string, error) {
	path, source, _, err := resolveToolSnapshot(name, launch)
	return path, source, err
}

func resolveToolSnapshot(name string, launch bool) (string, string, *toolEnvironmentSnapshot, error) {
	r := activeToolEnvironment.Load()
	if r == nil {
		path, err := exec.LookPath(name)
		return path, "inherited", nil, err
	}
	s := r.snapshot.Load()
	path, source, err := s.resolve(name)
	if err == nil || !launch || source == "configured" || explicitToolCommand(name) {
		return path, source, s, err
	}
	// Capabilities never spawn a shell. A launch miss may refresh once per 30s;
	// simultaneous launches share the refresh and an installed new CLI is visible
	// immediately without any negative executable cache.
	r.mu.Lock()
	defer r.mu.Unlock()
	if time.Since(r.lastRetry) >= 30*time.Second {
		r.lastRetry = time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = r.refreshLocked(ctx)
		cancel()
	}
	s = r.snapshot.Load()
	path, source, err = s.resolve(name)
	return path, source, s, err
}

func toolEnvironmentFor(cmd *exec.Cmd, path string) []string {
	r := activeToolEnvironment.Load()
	if r == nil {
		return cmd.Environ()
	}
	return toolEnvironmentFromSnapshot(cmd, path, r.snapshot.Load())
}

func toolEnvironmentFromSnapshot(cmd *exec.Cmd, path string, s *toolEnvironmentSnapshot) []string {
	if s == nil {
		return cmd.Environ()
	}
	env := cmd.Environ()
	for _, key := range append([]string{"PATH"}, toolRuntimeVariables...) {
		if value := environmentValue(s.environment, key); value != "" {
			env = replaceEnvironment(env, key, value)
		}
	}
	// Keep the user's runtime precedence. A discovered /usr/bin/git must not move
	// /usr/bin ahead of NVM and change the runtime used by its hooks.
	if filepath.IsAbs(path) {
		directory, present := filepath.Dir(path), false
		for _, entry := range filepath.SplitList(environmentValue(env, "PATH")) {
			if filepath.Clean(entry) == directory || runtime.GOOS == "windows" && strings.EqualFold(filepath.Clean(entry), directory) {
				present = true
				break
			}
		}
		if !present {
			env = prependToolDirectory(env, directory)
		}
	}
	return env
}

func prependToolDirectory(env []string, directory string) []string {
	return replaceEnvironment(env, "PATH", directory+string(os.PathListSeparator)+environmentValue(env, "PATH"))
}

func configureToolCommand(cmd *exec.Cmd, name string) error {
	return configureToolCommandWithRefresh(cmd, name, true)
}

func configureToolCommandWithRefresh(cmd *exec.Cmd, name string, refresh bool) error {
	if explicitToolCommand(name) {
		// Relative commands are evaluated against cmd.Dir by the process backend,
		// not against the daemon's working directory during discovery.
		setToolCommandPath(cmd, name)
		cmd.Env = toolEnvironmentFor(cmd, name)
		if filepath.IsAbs(name) {
			cmd.Env = prependToolDirectory(cmd.Env, filepath.Dir(name))
		}
		return nil
	}
	path, _, snapshot, err := resolveToolSnapshot(name, refresh)
	if err != nil {
		return err
	}
	setToolCommandPath(cmd, path)
	cmd.Env = toolEnvironmentFromSnapshot(cmd, path, snapshot)
	if snapshot != nil {
		// An explicit version pin also selects its sibling runtime. Normal search
		// results retain shell/configured PATH order instead.
		if _, pinned := snapshot.Executables[toolOverrideKey(name)]; pinned {
			cmd.Env = prependToolDirectory(cmd.Env, filepath.Dir(path))
		}
	}
	return nil
}

func setToolCommandPath(cmd *exec.Cmd, path string) {
	cmd.Path, cmd.Err = path, nil
	if len(cmd.Args) == 0 {
		cmd.Args = []string{path}
	} else {
		cmd.Args[0] = path
	}
}

func (s *Server) serveToolEnvironment(w http.ResponseWriter, r *http.Request, owner bool) {
	if !owner {
		fail(w, 403, "tool diagnostics require Host owner authorization")
		return
	}
	environment := activeToolEnvironment.Load()
	if environment == nil || environment.dir != s.dir {
		fail(w, 503, "tool environment is not initialized")
		return
	}
	if r.URL.Path == "/v1/tools/refresh" && r.Method == http.MethodPost {
		if err := environment.refresh(r.Context()); err != nil {
			status := http.StatusBadRequest
			if errors.Is(err, errToolRefreshBusy) {
				status = http.StatusConflict
			}
			fail(w, status, err.Error())
			return
		}
	} else if r.URL.Path != "/v1/tools" || r.Method != http.MethodGet {
		fail(w, 405, "unsupported tool operation")
		return
	}
	respond(w, 200, environment.snapshot.Load().ToolEnvironmentStatus)
}
