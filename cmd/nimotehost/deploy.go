// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/mobiledevloperlab/nimotehost/internal/host"
	"github.com/mobiledevloperlab/nimotehost/internal/hostinstall"
	"github.com/mobiledevloperlab/nimotehost/internal/hostservice"
	"github.com/mobiledevloperlab/nimotehost/pkg/hostclient"
)

const deployUsage = "nimotehost deploy <status|health|install|upgrade|start|stop|uninstall>\n" +
	"  install, upgrade: [--version vMAJOR.MINOR.PATCH] [--desktop] [--require-protocol N] [--sha256 HEX]\n" +
	"  uninstall: [--purge-data]\n"

var (
	errDeployUsage      = errors.New("invalid deployment arguments")
	errDeployStopped    = errors.New("Host is stopped")
	errDeployConflict   = errors.New("deployment conflict")
	errDeployProtocol   = errors.New("incompatible Host protocol")
	errDeployPermission = errors.New("user permission required")
)

type deploymentError struct {
	code string
	exit int
	err  error
}

func (e *deploymentError) Error() string { return e.err.Error() }
func (e *deploymentError) Unwrap() error { return e.err }

func deploymentExit(err error) int {
	var failure *deploymentError
	if errors.As(err, &failure) {
		return failure.exit
	}
	return 1
}

func classifyDeployError(err error) *deploymentError {
	e := &deploymentError{code: "operation_failed", exit: 1, err: err}
	var remote *hostclient.Error
	_ = errors.As(err, &remote)
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		e.code, e.exit = "interrupted", 10
	case errors.Is(err, errDeployUsage):
		e.code, e.exit = "invalid_arguments", 2
	case errors.Is(err, hostinstall.ErrUnsupported):
		e.code, e.exit = "unsupported_platform", 3
	case errors.Is(err, errDeployPermission), errors.Is(err, os.ErrPermission), remote != nil && (remote.Status == 401 || remote.Status == 403):
		e.code, e.exit = "permission_denied", 4
	case errors.Is(err, errDeployStopped):
		e.code, e.exit = "host_stopped", 5
	case errors.Is(err, errDeployConflict), errors.Is(err, hostinstall.ErrLocked), remote != nil && remote.Status == 409:
		e.code, e.exit = "conflict", 6
	case errors.Is(err, hostinstall.ErrRelease):
		e.code, e.exit = "release_unavailable", 7
	case errors.Is(err, hostinstall.ErrIntegrity):
		e.code, e.exit = "integrity_failure", 8
	case errors.Is(err, errDeployProtocol), remote != nil && remote.Status == 426:
		e.code, e.exit = "incompatible_protocol", 9
	}
	return e
}

type deployResult struct {
	Timestamp         time.Time    `json:"timestamp"`
	Schema            int          `json:"schema"`
	Action            string       `json:"action"`
	OK                bool         `json:"ok"`
	OS                string       `json:"os"`
	Arch              string       `json:"arch"`
	Scope             string       `json:"scope"`
	UID               *int         `json:"uid,omitempty"`
	Binary            string       `json:"binary,omitempty"`
	Installed         *bool        `json:"installed,omitempty"`
	ServiceRegistered *bool        `json:"service_registered,omitempty"`
	Running           *bool        `json:"running,omitempty"`
	Version           string       `json:"version,omitempty"`
	RunningVersion    string       `json:"running_version,omitempty"`
	Protocol          int          `json:"protocol,omitempty"`
	MinProtocol       int          `json:"min_protocol,omitempty"`
	Release           string       `json:"release,omitempty"`
	ArchiveSHA256     string       `json:"archive_sha256,omitempty"`
	Error             *deployIssue `json:"error,omitempty"`
}

type deployIssue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type deployOptions struct {
	tag, sha256    string
	desktop, purge bool
	protocol       int
}

func deploy(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 1 && (args[0] == "help" || args[0] == "--help") {
		_, err := io.WriteString(out, deployUsage)
		return err
	}
	result := deployResult{Schema: 1, OS: runtime.GOOS, Arch: platformArch(), Scope: "user"}
	if runtime.GOOS != "windows" {
		uid := os.Geteuid()
		result.UID = &uid
	}
	if len(args) > 0 {
		result.Action = args[0]
	}
	options, err := parseDeployOptions(result.Action, args)
	if err == nil {
		err = runDeployment(ctx, options, &result)
	}
	var failure *deploymentError
	if err != nil {
		failure = classifyDeployError(err)
		result.Error = &deployIssue{failure.code, failure.Error()}
	}
	result.OK = failure == nil
	result.Timestamp = time.Now().UTC()
	if err := json.NewEncoder(out).Encode(result); err != nil {
		return err
	}
	if failure != nil {
		return failure
	}
	return nil
}

func parseDeployOptions(action string, args []string) (deployOptions, error) {
	var opts deployOptions
	flags := flag.NewFlagSet("deploy", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	switch action {
	case "status", "health", "start", "stop":
	case "install", "upgrade":
		flags.StringVar(&opts.tag, "version", "", "")
		flags.StringVar(&opts.sha256, "sha256", "", "")
		flags.BoolVar(&opts.desktop, "desktop", false, "")
		flags.IntVar(&opts.protocol, "require-protocol", host.Protocol, "")
	case "uninstall":
		flags.BoolVar(&opts.purge, "purge-data", false, "")
	default:
		return opts, errDeployUsage
	}
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || (action == "install" || action == "upgrade") && opts.protocol < 1 {
		return opts, errDeployUsage
	}
	return opts, nil
}

func runDeployment(ctx context.Context, opts deployOptions, result *deployResult) (operationErr error) {
	if !hostinstall.Supported(result.OS, result.Arch) {
		return hostinstall.ErrUnsupported
	}
	home, target, name, err := installationPaths()
	if err != nil {
		return err
	}
	result.Binary = filepath.Join(target, name)
	dir, err := host.DefaultDir()
	if err != nil {
		return err
	}
	service, err := hostservice.NewHostExecutable(result.Binary)
	if err != nil {
		return err
	}
	if result.Action == "status" || result.Action == "health" {
		return deploymentStatus(ctx, dir, target, service, result)
	}
	if result.UID != nil && *result.UID == 0 {
		return fmt.Errorf("%w: run as the intended non-root user", errDeployPermission)
	}
	if result.Action == "install" || result.Action == "upgrade" {
		if err := deployRelease(ctx, dir, target, opts, result); err != nil {
			return err
		}
		return deploymentStatus(ctx, dir, target, service, result)
	}
	if err := host.CheckStateName(dir); err != nil {
		return err
	}
	tx, err := hostinstall.Lock(target)
	if err != nil {
		return err
	}
	defer func() { operationErr = errors.Join(operationErr, tx.Close()) }()
	if err := manageDeployment(ctx, opts, result, dir, home, target, name, service, tx); err != nil {
		return err
	}
	return deploymentStatus(ctx, dir, target, service, result)
}

func deploymentStatus(ctx context.Context, dir, target string, service *hostservice.Service, result *deployResult) error {
	inst, err := os.Lstat(result.Binary)
	installed := err == nil && inst.Mode().IsRegular()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	result.Installed = &installed
	if installed {
		build, err := readDeployBuild(target)
		if err != nil {
			return err
		}
		result.Version, result.Protocol, result.MinProtocol = build.Version, build.Protocol, build.MinProtocol
	}
	registered, err := service.Installed()
	if err != nil {
		return err
	}
	result.ServiceRegistered = &registered
	if err := host.CheckStateName(dir); err != nil {
		return err
	}
	status, err := runningDeployStatus(ctx, dir)
	if err != nil {
		return err
	}
	running := status != nil
	result.Running = &running
	if running {
		result.RunningVersion = status.Version
	}
	if result.Action == "health" && !running {
		return errDeployStopped
	}
	return nil
}

type deploymentRuntime struct {
	Running bool
	Version string
}

func runningDeployStatus(ctx context.Context, dir string) (*deploymentRuntime, error) {
	// Call creates the private directory when absent. Probe the token first.
	if _, err := os.Lstat(filepath.Join(dir, "token")); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	data, err := host.Call(ctx, dir, host.RPCRequest{Method: "GET", Path: "/v1/status"})
	if hostUnavailable(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var status deploymentRuntime
	if err := json.Unmarshal(data, &status); err != nil || !status.Running || status.Version == "" {
		return nil, errors.New("invalid Host status response")
	}
	return &status, nil
}

func manageDeployment(ctx context.Context, opts deployOptions, result *deployResult, dir, home, target, name string, service *hostservice.Service, tx *hostinstall.Transaction) error {
	registered, err := service.Installed()
	if err != nil {
		return err
	}
	_, statErr := os.Lstat(result.Binary)
	if result.Action == "uninstall" && !registered && errors.Is(statErr, os.ErrNotExist) && !opts.purge {
		return nil
	}
	if !errors.Is(statErr, os.ErrNotExist) || registered {
		if err := hostinstall.Verify(ctx, target); err != nil {
			return fmt.Errorf("%w: fixed installation could not be verified: %v", errDeployConflict, err)
		}
		if registered {
			if err := service.CheckExecutable(); err != nil {
				return fmt.Errorf("%w: %v", errDeployConflict, err)
			}
		}
	}
	if info, err := os.Lstat(dir); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errDeployConflict
		}
		lock, err := host.ControlLock(dir)
		if err != nil {
			return err
		}
		defer lock.Close()
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	status, err := runningDeployStatus(ctx, dir)
	if err != nil {
		return err
	}
	if status == nil {
		if err := requireIdleState(dir); err != nil {
			return err
		}
	}
	if result.Action == "start" {
		if !registered {
			return fmt.Errorf("%w: install or repair the managed service first", errDeployConflict)
		}
		if status != nil {
			return nil
		}
		if err := service.Restart(ctx); err != nil {
			return err
		}
		return awaitState(ctx, dir, true, host.Call)
	}
	if status != nil && !registered {
		return fmt.Errorf("%w: foreground Host is running", errDeployConflict)
	}
	if result.Action == "uninstall" {
		if err := outsideInstall(target); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// Complete a confirmed stop even after caller interruption.
	stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if status != nil {
		if _, err := host.Call(stopCtx, dir, host.RPCRequest{Method: "POST", Path: "/v1/shutdown-if-idle"}); err != nil {
			return err
		}
	}
	if status != nil {
		if err := awaitState(stopCtx, dir, false, host.Call); err != nil {
			return err
		}
	}
	if result.Action == "stop" {
		if registered {
			return service.Stop(stopCtx)
		}
		return nil
	}
	if registered {
		if err := service.Uninstall(stopCtx); err != nil {
			return err
		}
	}
	if err := requireIdleState(dir); err != nil {
		return err
	}
	if statErr == nil {
		if err := tx.Remove(stopCtx); err != nil {
			return err
		}
	}
	if runtime.GOOS != "windows" {
		link := filepath.Join(home, ".local", "bin", name)
		if path, err := os.Readlink(link); err == nil && path == result.Binary {
			if err := os.Remove(link); err != nil {
				return err
			}
		}
	}
	if opts.purge {
		return os.RemoveAll(dir)
	}
	return nil
}

// An unavailable local credential cannot establish that the Host has exited.
func requireIdleState(dir string) error {
	if _, err := os.Lstat(dir); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	idle, err := host.StateIdle(dir)
	if err != nil {
		return err
	}
	if !idle {
		return fmt.Errorf("%w: Host state is in use; verify service health and retry", errDeployConflict)
	}
	return nil
}

func outsideInstall(target string) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	resolved, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return err
	}
	if canonical, err := filepath.EvalSymlinks(target); err == nil {
		target = canonical
	}
	rel, err := filepath.Rel(target, resolved)
	if err == nil && filepath.IsLocal(rel) {
		return fmt.Errorf("%w: run the deployment executable from outside the installed bundle", errDeployConflict)
	}
	return nil
}
