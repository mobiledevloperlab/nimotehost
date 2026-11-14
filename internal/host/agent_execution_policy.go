// SPDX-License-Identifier: Apache-2.0
package host

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const executionPolicyOption = "__execution_policy"

func validExecutionPolicy(value string) bool {
	return value == "confirm" || value == "smart" || value == "auto"
}
func (a *structuredAgent) effectiveExecutionPolicy() string {
	if validExecutionPolicy(a.executionPolicy) {
		return a.executionPolicy
	}
	return "confirm"
}
func (a *structuredAgent) publishExecutionPolicy() {
	if a.acp == nil {
		a.controls.Codex = true
	}
	options := a.controls.Options[:0]
	for _, option := range a.controls.Options {
		if option.ID != executionPolicyOption {
			options = append(options, option)
		}
	}
	a.controls.Options = append(options, agentOption{ID: executionPolicyOption, Label: "Execution policy", Category: "execution_policy", Value: a.effectiveExecutionPolicy(), Values: []agentChoice{{"confirm", "Confirm each request"}, {"smart", "Smart execution"}, {"auto", "Automatic approvals"}}})
}
func (a *structuredAgent) configureExecutionPolicy(req AgentMessage) error {
	if a.closed || a.thread == "" || a.busy || a.configuring || len(a.approvals) != 0 {
		return errors.New("execution policy can only change while the session is idle")
	}
	if !validExecutionPolicy(req.Value) {
		return errors.New("unsupported execution policy")
	}
	if a.onExecutionPolicy != nil {
		if err := a.onExecutionPolicy(req.Value); err != nil {
			return err
		}
	}
	a.executionPolicy = req.Value
	a.seen[req.RequestID] = true
	a.publishControls()
	return nil
}

// Called under the same lock as native request validation. Decisions remain
// one-time native responses: no global grants, shell keystrokes or UI dependency.
func (a *structuredAgent) automaticApproval(method string, raw json.RawMessage) (any, bool) {
	policy := a.effectiveExecutionPolicy()
	if policy == "confirm" || a.closed || !a.busy || a.thread == "" {
		return nil, false
	}
	var request map[string]json.RawMessage
	if json.Unmarshal(raw, &request) != nil {
		return nil, false
	}
	if a.acp != nil {
		var p struct {
			SessionID string  `json:"sessionId"`
			Tool      acpTool `json:"toolCall"`
			Options   []struct {
				ID   string `json:"optionId"`
				Kind string `json:"kind"`
			} `json:"options"`
		}
		if a.acp.canceling || method != "session/request_permission" || json.Unmarshal(raw, &p) != nil || p.SessionID != a.thread || p.Tool.ID == "" {
			return nil, false
		}
		option := ""
		for _, offered := range p.Options {
			if offered.Kind == "allow_once" {
				option = offered.ID
				break
			}
		}
		if option == "" {
			return nil, false
		}
		if policy == "smart" && !a.safeACPTool(p.Tool) {
			return nil, false
		}
		a.emit("approval_auto_accepted", map[string]string{"policy": policy, "item_id": p.Tool.ID})
		return map[string]any{"outcome": map[string]string{"outcome": "selected", "optionId": option}}, true
	}
	if method != "item/commandExecution/requestApproval" && method != "item/fileChange/requestApproval" {
		return nil, false
	}
	var threadID string
	_ = json.Unmarshal(request["threadId"], &threadID)
	var turnID string
	_ = json.Unmarshal(request["turnId"], &turnID)
	if threadID != a.thread || (turnID != "" && a.turn != "" && turnID != a.turn) {
		return nil, false
	}
	// Respect the native server's advertised decisions, if present.
	if offered := request["availableDecisions"]; len(offered) > 0 && string(offered) != "null" {
		var decisions []json.RawMessage
		if json.Unmarshal(offered, &decisions) != nil {
			return nil, false
		}
		accepted := false
		for _, decision := range decisions {
			if string(decision) == `"accept"` {
				accepted = true
			}
		}
		if !accepted {
			return nil, false
		}
	}
	if policy == "smart" {
		if method != "item/commandExecution/requestApproval" {
			return nil, false
		}
		for _, key := range []string{"networkApprovalContext", "additionalPermissions", "grantRoot"} {
			if value := request[key]; len(value) > 0 && string(value) != "null" {
				return nil, false
			}
		}
		var kind, command, cwd string
		_ = json.Unmarshal(request["kind"], &kind)
		_ = json.Unmarshal(request["command"], &command)
		_ = json.Unmarshal(request["cwd"], &cwd)
		if (kind != "" && kind != "command") || !a.simpleReadCommand(command, cwd) {
			return nil, false
		}
	}
	a.emit("approval_auto_accepted", map[string]string{"policy": policy, "method": method})
	return map[string]string{"decision": "accept"}, true
}
func (a *structuredAgent) safeACPTool(tool acpTool) bool {
	if tool.Kind == nil {
		return false
	}
	if *tool.Kind != "execute" && len(tool.RawInput) > 0 {
		var input map[string]json.RawMessage
		if json.Unmarshal(tool.RawInput, &input) != nil {
			return false
		}
		for key, value := range input {
			if oneOf(key, "command", "script", "url", "urls") {
				return false
			}
			if oneOf(key, "path", "filePath", "file_path", "directory", "cwd", "workdir") {
				var path string
				if json.Unmarshal(value, &path) != nil || !a.workspacePath(path) {
					return false
				}
			}
		}
	}
	switch *tool.Kind {
	case "read", "search":
		if len(tool.Locations) == 0 {
			return false
		}
		for _, location := range tool.Locations {
			if !a.workspacePath(location.Path) {
				return false
			}
		}
		return true
	case "edit":
		// Require actual structured diffs, not a title or a guessed command.
		if tool.Content == nil || len(*tool.Content) == 0 {
			return false
		}
		for _, content := range *tool.Content {
			if content.Type != "diff" || content.NewText == "" || !a.workspacePath(content.Path) {
				return false
			}
		}
		for _, location := range tool.Locations {
			if !a.workspacePath(location.Path) {
				return false
			}
		}
		return true
	case "execute":
		var input struct {
			Command string `json:"command"`
			Cwd     string `json:"cwd"`
			Workdir string `json:"workdir"`
		}
		if json.Unmarshal(tool.RawInput, &input) != nil {
			return false
		}
		cwd := input.Cwd
		if cwd == "" {
			cwd = input.Workdir
		}
		if cwd == "" {
			cwd = a.cwd
		}
		return a.simpleReadCommand(input.Command, cwd)
	}
	return false
}

// A deliberately small recognizer, not a second general shell policy engine.
// Compound/unknown commands are left to native approvals. Shell expansions,
// redirection and execution-capable options are never inferred from a title.
func simpleCommandWords(command string) ([]string, bool) {
	if len(command) == 0 || len(command) > 16384 || strings.ContainsAny(command, "$`\\;&|<>\n\r\x00(){}*?[]!~") {
		return nil, false
	}
	var words []string
	var word strings.Builder
	var quote rune
	started := false
	for _, char := range command {
		if char == '\'' || char == '"' {
			started = true
			if quote == 0 {
				quote = char
			} else if quote == char {
				quote = 0
			} else {
				word.WriteRune(char)
			}
			continue
		}
		if (char == ' ' || char == '\t') && quote == 0 {
			if started {
				words = append(words, word.String())
				word.Reset()
				started = false
			}
			continue
		}
		word.WriteRune(char)
		started = true
	}
	if quote != 0 {
		return nil, false
	}
	if started {
		words = append(words, word.String())
	}
	return words, len(words) > 0 && len(words) <= 128
}
func (a *structuredAgent) simpleReadCommand(command, cwd string) bool {
	// Windows shell parsing differs; structured reads/edits still work there.
	if runtime.GOOS == "windows" || cwd == "" || !a.workspacePath(cwd) {
		return false
	}
	words, ok := simpleCommandWords(command)
	if !ok {
		return false
	}
	base := words[0]
	if filepath.Base(base) != base {
		return false
	}
	args := words[1:]
	var paths []string
	switch base {
	case "pwd":
		return len(args) == 0

	case "ls":
		for _, arg := range args {
			if strings.HasPrefix(arg, "-") {
				if !oneOf(arg, "-l", "-a", "-h", "-la", "-al", "-lah", "-alh", "--") {
					return false
				}
			} else {
				paths = append(paths, arg)
			}
		}
	case "cat", "wc", "head", "tail":
		for i := 0; i < len(args); i++ {
			arg := args[i]
			if strings.HasPrefix(arg, "-") {
				if (base == "head" || base == "tail") && (arg == "-n" || arg == "-c") {
					i++
					if i >= len(args) || strings.Trim(args[i], "0123456789") != "" || args[i] == "" {
						return false
					}
					continue
				}
				if base == "wc" && oneOf(arg, "-l", "-w", "-c", "-m") {
					continue
				}
				if arg != "--" {
					return false
				}
			} else {
				paths = append(paths, arg)
			}
		}
		if len(paths) == 0 {
			return false
		}
	case "rg", "grep":
		noConfig := false
		options := true
		pattern := false
		files := false
		for _, arg := range args {
			if options && arg == "--" {
				options = false
				continue
			}
			if options && strings.HasPrefix(arg, "-") {
				if !oneOf(arg, "-n", "-i", "-l", "-c", "-F", "-w", "-S", "--line-number", "--no-config", "--files", "--") {
					return false
				}
				if arg == "--files" {
					if base != "rg" {
						return false
					}
					files = true
				}
				if arg == "--no-config" {
					if base != "rg" {
						return false
					}
					noConfig = true
				}
				continue
			}
			if !pattern && !files {
				pattern = true
				continue
			}
			paths = append(paths, arg)
		}
		if base == "rg" && os.Getenv("RIPGREP_CONFIG_PATH") != "" && !noConfig {
			return false
		}
		if !pattern && !files {
			return false
		}
		if base == "grep" && len(paths) == 0 {
			return false
		}
	default:
		return false
	}
	for _, path := range paths {
		if hasParentSegment(path) {
			return false
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(cwd, path)
		}
		if !a.workspacePath(path) {
			return false
		}
	}
	return true
}
func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func (a *structuredAgent) workspacePath(path string) bool {
	// Cleaning link/../file before resolving symlinks changes its filesystem
	// meaning. Leave parent traversal to a native approval instead.
	if path == "" || hasParentSegment(path) {
		return false
	}
	if !filepath.IsAbs(path) {
		if runtime.GOOS == "windows" && (filepath.VolumeName(path) != "" || strings.HasPrefix(path, "/") || strings.HasPrefix(path, `\`)) {
			return false
		}
		path = filepath.Join(a.cwd, path)
	}
	lexical, err := filepath.Rel(a.cwd, filepath.Clean(path))
	if err != nil {
		return false
	}
	if lexical != ".." && !strings.HasPrefix(lexical, ".."+string(filepath.Separator)) && !safeWorkspaceRelative(lexical) {
		return false
	}
	root, err := filepath.EvalSymlinks(a.cwd)
	if err != nil {
		return false
	}
	// Canonicalize the nearest existing parent for edits creating new files.
	clean := filepath.Clean(path)
	ancestor := clean
	var suffix []string
	for {
		resolved, err := filepath.EvalSymlinks(ancestor)
		if err == nil {
			clean = filepath.Join(append([]string{resolved}, suffix...)...)
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return false
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return false
		}
		suffix = append([]string{filepath.Base(ancestor)}, suffix...)
		ancestor = parent
	}
	rel, err := filepath.Rel(root, clean)
	return err == nil && safeWorkspaceRelative(rel)
}
func hasParentSegment(path string) bool {
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if part == ".." {
			return true
		}
	}
	return false
}
func safeWorkspaceRelative(rel string) bool {
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return false
	}
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		part = strings.ToLower(part)
		if oneOf(part, ".git", ".ssh", ".aws", ".codex", ".agents", "credentials", "id_rsa", "id_ed25519") || strings.HasPrefix(part, ".env") {
			return false
		}
	}
	return true
}
