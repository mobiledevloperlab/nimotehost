// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Nimote contributors

package hostcli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/mobiledevloperlab/nimotehost/internal/host"
	"github.com/mobiledevloperlab/nimotehost/internal/hostinstall"
	"github.com/mobiledevloperlab/nimotehost/internal/hostservice"
)

func installationPaths() (home, target, name string, err error) {
	home, err = os.UserHomeDir()
	if err != nil {
		return
	}
	target = filepath.Join(home, ".local", "share", "nimotehost")
	name = "nimotehost"
	if runtime.GOOS == "windows" {
		base := os.Getenv("LOCALAPPDATA")
		if !filepath.IsAbs(base) {
			err = errors.New("LOCALAPPDATA must be an absolute user directory")
			return
		}
		target = filepath.Join(base, "NimoteHost")
		name += ".exe"
	}
	return
}

func setup(ctx context.Context, dir string, out io.Writer) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	return setupBundle(ctx, dir, filepath.Dir(executable), out, nil)
}

func setupBundle(ctx context.Context, dir, source string, out io.Writer, guard func() error) (result error) {
	if runtime.GOOS != "windows" && os.Geteuid() == 0 {
		return errors.New("run setup as your normal user, without sudo")
	}
	home, target, name, err := installationPaths()
	if err != nil {
		return err
	}
	service, err := hostservice.NewHostExecutable(filepath.Join(target, name))
	if err != nil {
		return err
	}
	if err := service.CheckAvailable(ctx); err != nil {
		return err
	}
	tx, err := hostinstall.Prepare(ctx, source, target)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, tx.Close()) }()
	if err := checkManagedEdition(target); err != nil {
		return err
	}
	if guard != nil {
		if err := guard(); err != nil {
			return err
		}
	}
	migration, err := prepareNameMigration(ctx, dir)
	if err != nil {
		return err
	}
	defer func() {
		if result != nil {
			result = errors.Join(result, migration.rollback(context.Background()))
		}
	}()
	control, err := host.ControlLock(dir)
	if err != nil {
		return err
	}
	defer control.Close()
	installed, err := service.Installed()
	if err != nil {
		return err
	}
	if installed {
		if guard != nil || host.Edition == "connect" {
			if err := service.CheckExecutable(); err != nil {
				return err
			}
		}
		info, err := os.Lstat(filepath.Join(target, name))
		if err != nil || !info.Mode().IsRegular() {
			return errors.New("existing service uses a different or incomplete installation; uninstall its service definition (retaining state), then retry setup")
		}
	}
	_, statusErr := host.Call(ctx, dir, host.RPCRequest{Method: "GET", Path: "/v1/status"})
	running := statusErr == nil
	if statusErr != nil && !hostUnavailable(statusErr) {
		return fmt.Errorf("cannot check existing Host: %w", statusErr)
	}
	if !running {
		if err := requireIdleState(dir); err != nil {
			return err
		}
	}
	unchanged := tx.Unchanged(ctx)
	if running && installed && unchanged {
		if err := checkSetupCapabilities(ctx, dir); err != nil {
			return err
		}
		migration.committed = true
		return completeSetup(migration, home, target, name, "", out)
	}
	if running && !installed {
		return errors.New("a foreground Host is running; stop it before setting up the background service")
	}
	// Reuse the verified distribution when only service startup is needed.
	if unchanged {
		if err := installSetupService(ctx, dir, filepath.Join(target, name)); err != nil {
			return errors.Join(err, rollbackSetup(tx, service, dir, name, installed, running, out))
		}
		if err := completeSetup(migration, home, target, name, "", out); err != nil {
			return errors.Join(err, rollbackSetup(tx, service, dir, name, installed, running, out))
		}
		migration.committed = true
		return nil
	}
	if running {
		data, err := host.Call(ctx, dir, host.RPCRequest{Method: "GET", Path: "/v1/capabilities"})
		if err != nil {
			return err
		}
		var caps host.Capabilities
		if err := json.Unmarshal(data, &caps); err != nil {
			return err
		}
		if caps.Features["github_issue_tasks_v1"] && !strings.Contains(caps.Version, "+connect.") {
			if err := requireIdleLegacyWorkflows(ctx, dir); err != nil {
				return err
			}
		}
		if !caps.Features["idle_shutdown"] {
			return errors.New("this older development Host requires a one-time stop before setup; let tasks finish, then run nimotehost stop and retry setup")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		// Finish a mutating request and service stop even if the SSH
		// caller disconnects before activation.
		stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 25*time.Second)
		if _, err := host.Call(stopCtx, dir, host.RPCRequest{Method: "POST", Path: "/v1/shutdown-if-idle"}); err != nil {
			cancel()
			return fmt.Errorf("setup could not confirm idle shutdown: %w", err)
		}
		stopErr := awaitState(stopCtx, dir, false, host.Call)
		if stopErr == nil {
			stopErr = service.Stop(stopCtx)
		}
		cancel()
		if stopErr != nil {
			return errors.Join(stopErr, restoreSetupService(dir, target, name))
		}
	}
	if err := tx.Activate(ctx); err != nil {
		// If activation restored the previous directory, also restore service availability.
		if running {
			return errors.Join(err, restoreSetupService(dir, target, name))
		}
		return err
	}
	if err := installSetupService(ctx, dir, filepath.Join(target, name)); err != nil {
		return errors.Join(err, rollbackSetup(tx, service, dir, name, installed, running, out))
	}
	if err := completeSetup(migration, home, target, name, tx.Backup, out); err != nil {
		return errors.Join(err, rollbackSetup(tx, service, dir, name, installed, running, out))
	}
	migration.committed = true
	return nil
}

func completeSetup(migration *nameMigration, home, target, name, backup string, out io.Writer) error {
	if err := ensureSSHCommand(home, target, name); err != nil {
		return fmt.Errorf("Host is running, but SSH command discovery failed: %w", err)
	}
	if err := migration.updateAlias(filepath.Join(target, name)); err != nil {
		return fmt.Errorf("Host is running, but the legacy SSH alias needs attention: %w", err)
	}
	return printSetupReady(out, target, name, backup)
}

func installSetupService(ctx context.Context, dir, binary string) error {
	if err := runSetupCommand(ctx, binary, "install"); err != nil {
		return err
	}
	return checkSetupCapabilities(ctx, dir)
}

func checkSetupCapabilities(ctx context.Context, dir string) error {
	if host.Edition != "connect" {
		return nil
	}
	data, err := host.Call(ctx, dir, host.RPCRequest{Method: "GET", Path: "/v1/capabilities"})
	if err != nil {
		return err
	}
	var caps host.Capabilities
	if err := json.Unmarshal(data, &caps); err != nil {
		return err
	}
	if caps.Version != host.Version || !caps.Features["github_issue_tasks_v1"] {
		return errors.New("installed Host did not register the Connect workflow")
	}
	return nil
}

func restoreSetupService(dir, target, name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if _, err := host.Call(ctx, dir, host.RPCRequest{Method: "GET", Path: "/v1/status"}); err == nil {
		return nil
	} else if !hostUnavailable(err) {
		return err
	}
	return runSetupCommand(ctx, filepath.Join(target, name), "install")
}

func runSetupCommand(ctx context.Context, binary, command string) error {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, command)
	cmd.Stdout = io.Discard
	// Capture errors so a failed install never produces a misleading ready message.
	var stderr limitedBuffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s failed: %w: %s", command, errors.Join(err, ctx.Err()), stderr.String())
	}
	return nil
}

// Bound diagnostic output from a child command without affecting its execution.
type limitedBuffer struct{ data []byte }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	left := (64 << 10) - len(b.data)
	if left > len(p) {
		left = len(p)
	}
	if left > 0 {
		b.data = append(b.data, p[:left]...)
	}
	return n, nil
}
func (b *limitedBuffer) String() string { return string(b.data) }

func rollbackSetup(tx *hostinstall.Transaction, service *hostservice.Service, dir, name string, installed, running bool, out io.Writer) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// A failed readiness check can leave the native service running or retrying.
	// Stop it through the manager before restoring executable files.
	registered, err := service.Installed()
	if err != nil {
		return err
	}
	if registered {
		if _, err := host.Call(ctx, dir, host.RPCRequest{Method: "POST", Path: "/v1/shutdown"}); err == nil {
			if err := awaitState(ctx, dir, false, host.Call); err != nil {
				return err
			}
		} else if !hostUnavailable(err) {
			return fmt.Errorf("rollback could not authenticate shutdown; previous bundle: %s: %w", tx.Backup, err)
		}
		if err := service.Stop(ctx); err != nil {
			return fmt.Errorf("rollback could not stop the new service; previous bundle: %s: %w", tx.Backup, err)
		}
		if err := service.Uninstall(ctx); err != nil {
			return fmt.Errorf("rollback could not remove the new service: %w", err)
		}
	}

	if err := tx.Rollback(ctx); err != nil {
		return err
	}
	if installed {
		if err := runSetupCommand(ctx, filepath.Join(tx.Target, name), "install"); err != nil {
			return fmt.Errorf("previous bundle restored, but service recovery failed: %w", err)
		}
		if err := awaitState(ctx, dir, true, host.Call); err != nil {
			return fmt.Errorf("previous bundle restored, but Host is not ready: %w", err)
		}
		if !running {
			_, err := host.Call(ctx, dir, host.RPCRequest{Method: "POST", Path: "/v1/shutdown"})
			if err != nil {
				return err
			}
			if err := awaitState(ctx, dir, false, host.Call); err != nil {
				return err
			}
			return service.Stop(ctx)
		}
	}
	_, err = fmt.Fprintln(out, "Setup failed; the previous installation was restored. Credentials and task records were retained.")
	return err
}

func printSetupReady(out io.Writer, target, name, backup string) error {
	_, err := fmt.Fprintf(out, "NimoteHost is ready.\nHost binary: %s\nConnect with your existing SSH account and this absolute binary path.\n", filepath.Join(target, name))
	if err != nil {
		return err
	}
	if backup != "" {
		_, err = fmt.Fprintf(out, "Previous bundle retained at: %s\n", backup)
	}
	return err
}

// Older Hosts did not include workflow workers in their idle shutdown check.
func requireIdleLegacyWorkflows(ctx context.Context, dir string) error {
	path := "/v1/github-tasks"
	for {
		data, err := host.Call(ctx, dir, host.RPCRequest{Method: "GET", Path: path})
		if err != nil {
			return fmt.Errorf("check legacy workflows before upgrading: %w", err)
		}
		var page struct {
			Tasks []struct{ Running bool }
			Next  string `json:"next_cursor"`
		}
		if err := json.Unmarshal(data, &page); err != nil {
			return err
		}
		for _, task := range page.Tasks {
			if task.Running {
				return errors.New("Host has active GitHub workflows; let them finish before upgrading")
			}
		}
		if page.Next == "" {
			return nil
		}
		if !host.ValidID(page.Next) || path == "/v1/github-tasks?cursor="+page.Next {
			return errors.New("invalid legacy workflow cursor")
		}
		path = "/v1/github-tasks?cursor=" + page.Next
	}
}
