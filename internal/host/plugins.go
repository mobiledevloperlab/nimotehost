// SPDX-License-Identifier: Apache-2.0
package host

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mobiledevloperlab/nimotehost/internal/plugins"
	sdk "github.com/mobiledevloperlab/nimotehost/pkg/pluginsdk"
)

func pluginLauncher(limits ResourceLimits) plugins.Launcher {
	return func(ctx context.Context, path, dir string) (*plugins.Connection, error) {
		var cancel context.CancelFunc
		if limits.MaxRuntimeSeconds > 0 {
			ctx, cancel = context.WithTimeout(ctx, time.Duration(limits.MaxRuntimeSeconds)*time.Second)
		} else {
			ctx, cancel = context.WithCancel(ctx)
		}
		cmd := exec.Command(path)
		cmd.Dir = dir
		cmd.WaitDelay = time.Second
		cmd.Env = pluginEnvironment(dir)
		reader, writer := io.Pipe()
		// Raw stderr is discarded: arbitrary third-party text cannot be reliably redacted.
		process, input, err := startProtocolProcess(cmd, writer, io.Discard, limits)
		if err != nil {
			cancel()
			reader.Close()
			writer.Close()
			return nil, err
		}
		done := make(chan struct{})
		go func() { _, err := process.Wait(); _ = writer.CloseWithError(err); close(done) }()
		var once sync.Once
		stop := func() {
			once.Do(func() {
				cancel()
				_ = input.Close()
				_ = reader.Close()
				_ = writer.Close()
				_ = process.Kill()
				<-done
				_ = process.Close()
			})
		}
		stopContext := context.AfterFunc(ctx, stop)
		return &plugins.Connection{Input: input, Output: reader, Stop: func() { stopContext(); stop() }}, nil
	}
}
func pluginEnvironment(dir string) []string {
	env := []string{"HOME=" + dir, "USERPROFILE=" + dir, "TMPDIR=" + dir, "TEMP=" + dir, "TMP=" + dir}
	for _, key := range []string{"PATH", "SystemRoot", "WINDIR"} {
		if v := os.Getenv(key); v != "" {
			env = append(env, key+"="+v)
		}
	}
	return env
}

type capabilityRequest struct {
	RequestID string          `json:"request_id,omitempty"`
	Protocol  string          `json:"protocol"`
	Operation string          `json:"operation"`
	ID        string          `json:"id,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	Confirmed bool            `json:"confirmed,omitempty"`
	Grants    []string        `json:"grants,omitempty"`
	Source    string          `json:"source,omitempty"`
	SHA256    string          `json:"sha256,omitempty"`
	Epoch     string          `json:"epoch,omitempty"`
	After     uint64          `json:"after,omitempty"`
}

func (s *Server) servePlugins(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		fail(w, 405, "POST required")
		return
	}
	if s.plugins == nil {
		fail(w, 503, "plugin runtime unavailable")
		return
	}
	var req capabilityRequest
	if !readJSON(w, r, &req) {
		return
	}
	if req.Protocol != sdk.Protocol {
		respond(w, 426, map[string]any{"code": "incompatible_protocol", "supported_protocols": []string{sdk.Protocol}})
		return
	}
	var result any
	var err error
	switch req.Operation {
	case "capabilities.list":
		result = s.plugins.Capabilities()
	case "capabilities.get":
		err = os.ErrNotExist
		for _, c := range s.plugins.Capabilities() {
			if c.ID == req.ID {
				result = c
				err = nil
				break
			}
		}
	case "plugins.list":
		result = s.plugins.List()
	case "plugins.install", "plugins.update":
		if !req.Confirmed {
			fail(w, 403, "explicit source review confirmation required")
			return
		}
		err = s.plugins.Install(req.Source, req.SHA256, req.Operation == "plugins.update")
	case "plugins.enable":
		err = s.plugins.Enable(req.ID, req.Grants, req.Confirmed)
	case "plugins.disable":
		err = s.plugins.Disable(req.ID)
		if err == nil && s.manager != nil {
			for _, session := range s.manager.List(0) {
				if session.Kind == "agent" && len(session.Agent) > len(req.ID) && session.Agent[:len(req.ID)+1] == req.ID+"/" {
					_ = s.manager.Cancel(session.ID)
				}
			}
		}
	case "plugins.uninstall":
		if !req.Confirmed {
			fail(w, 403, "explicit uninstall confirmation required")
			return
		}
		err = s.plugins.Uninstall(req.ID)
	case "capabilities.invoke":
		var acp *plugins.Entry
		for _, c := range s.plugins.Capabilities() {
			if c.ID == req.ID && c.ACP != 0 {
				entry := c
				acp = &entry
				break
			}
		}
		if acp != nil {
			if acp.State != "ready" || !req.Confirmed {
				fail(w, 403, "ready ACP capability and session confirmation required")
				return
			}
			var input struct {
				Directory string `json:"directory"`
				RequestID string `json:"request_id"`
			}
			if err = decodeJSON(req.Input, &input); err == nil {
				result, err = s.manager.Start(StartRequest{Kind: "agent", Agent: req.ID, Interaction: "structured", Directory: input.Directory, RequestID: input.RequestID})
			}
		} else {
			if !validID(req.RequestID) || s.plugins.Events(req.Epoch, 0).Epoch != req.Epoch {
				fail(w, 409, "current epoch and 64-hex request_id required")
				return
			}
			result, err = s.plugins.Invoke(req.ID, req.Input, req.Confirmed, req.RequestID)
		}
	case "capabilities.cancel":
		err = s.plugins.Cancel(req.ID)
		if err != nil && s.manager != nil && validID(req.ID) {
			session, getErr := s.manager.Get(req.ID)
			if getErr == nil && session.Kind == "agent" && strings.Contains(session.Agent, "/") {
				err = s.manager.Cancel(req.ID)
			}
		}
	case "capabilities.result":
		result, err = s.plugins.GetInvocation(req.ID)
	case "capabilities.subscribe":
		result = s.plugins.Events(req.Epoch, req.After)
	default:
		fail(w, 400, "unknown capability operation")
		return
	}
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	if result == nil {
		result = map[string]bool{"ok": true}
	}
	respond(w, 200, map[string]any{"protocol": sdk.Protocol, "result": result})
}

// One daemon owns one registry. Existing ACP SDK/session/approval code consumes
// these descriptors exactly as it consumes the built-in CLI adapters.
var communityAdapters struct {
	sync.RWMutex
	runtime *plugins.Runtime
	dir     string
}

func communityAdapter(id string) (agentAdapter, bool) {
	communityAdapters.RLock()
	defer communityAdapters.RUnlock()
	if communityAdapters.runtime == nil {
		return nil, false
	}
	for _, entry := range communityAdapters.runtime.Capabilities() {
		if entry.ID == id && entry.ACP != 0 && entry.State == "ready" {
			for _, p := range communityAdapters.runtime.List() {
				if p.ID == entry.Plugin {
					return communityCLIAdapter{cliAgentAdapter{filepath.Join(communityAdapters.dir, p.ID, p.Manifest.Entrypoint), "acp", entry.ACP, []string{"--acp"}}, filepath.Join(communityAdapters.dir, p.ID), id, communityAdapters.runtime}, true
				}
			}
		}
	}
	return nil, false
}

type communityCLIAdapter struct {
	cliAgentAdapter
	dir     string
	id      string
	runtime *plugins.Runtime
}

func (a communityCLIAdapter) ConfigureCommand(cmd *exec.Cmd) error {
	// Revalidate availability at launch; same-user malicious mutation remains outside
	// this process boundary, as documented for all third-party executables.
	if err := a.runtime.VerifyCapability(a.id); err != nil {
		return err
	}
	if _, err := os.Stat(a.executable); err != nil {
		return err
	}
	if err := a.cliAgentAdapter.ConfigureCommand(cmd); err != nil {
		return err
	}
	cmd.Env = pluginEnvironment(a.dir)
	return nil
}
