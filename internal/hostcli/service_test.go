// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Nimote contributors

package hostcli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/mobiledevloperlab/nimotehost/internal/host"
)

type fakeService struct {
	installed  bool
	operations []string
	err        error
}

func (s *fakeService) Installed() (bool, error) { return s.installed, nil }
func (s *fakeService) Install(context.Context) error {
	s.operations = append(s.operations, "install")
	return s.err
}
func (s *fakeService) Restart(context.Context) error {
	s.operations = append(s.operations, "restart")
	return s.err
}
func (s *fakeService) Uninstall(context.Context) error {
	s.operations = append(s.operations, "uninstall")
	return s.err
}
func (*fakeService) Diagnostics() string { return "Check service logs." }

func TestHelpWithoutConfiguration(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	for _, args := range [][]string{nil, {"help"}, {"--help"}, {"-h"}} {
		var out bytes.Buffer
		if err := runWithExtension(context.Background(), args, strings.NewReader(""), &out, host.Extension{}); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "install") || !strings.Contains(out.String(), "restart") {
			t.Fatal(out.String())
		}
	}
	var out bytes.Buffer
	if err := runWithExtension(context.Background(), []string{"--version"}, strings.NewReader(""), &out, host.Extension{}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.String(), "nimotehost ") {
		t.Fatal(out.String())
	}
}

func TestLifecycleGuards(t *testing.T) {
	for _, tc := range []struct {
		name, command string
		installed     bool
		callErr       error
		wantErr       bool
		wantCalls     int
	}{
		{"running reinstall", "install", true, nil, true, 1},
		{"foreground install", "install", false, nil, true, 1},
		{"missing restart", "restart", false, nil, true, 0},
		{"missing uninstall", "uninstall", false, nil, false, 0},
		{"install authentication failure", "install", false, errors.New("Host RPC 401"), true, 1},
		{"restart authentication failure", "restart", true, errors.New("Host RPC 401"), true, 1},
		{"uninstall permission failure", "uninstall", true, os.ErrPermission, true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &fakeService{installed: tc.installed}
			calls := 0
			call := func(context.Context, string, host.RPCRequest) ([]byte, error) { calls++; return nil, tc.callErr }
			var out bytes.Buffer
			err := manageService(context.Background(), tc.command, "", &out, s, call)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error: %v", err)
			}
			if calls != tc.wantCalls || len(s.operations) != 0 {
				t.Fatalf("calls=%d operations=%v", calls, s.operations)
			}
		})
	}
}

func TestLifecycleWaitsForReadiness(t *testing.T) {
	for _, command := range []string{"install", "restart", "uninstall"} {
		t.Run(command, func(t *testing.T) {
			s := &fakeService{installed: true}
			var paths []string
			call := func(_ context.Context, _ string, req host.RPCRequest) ([]byte, error) {
				paths = append(paths, req.Path)
				if len(s.operations) == 0 && req.Path == "/v1/status" {
					return nil, os.ErrNotExist
				}
				return []byte(`{"running":true}`), nil
			}
			var out bytes.Buffer
			if err := manageService(context.Background(), command, "", &out, s, call); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(s.operations, []string{command}) {
				t.Fatal(s.operations)
			}
			if command != "uninstall" && paths[len(paths)-1] != "/v1/status" {
				t.Fatal(paths)
			}
		})
	}
}

func TestStartupFailureNeverReportsSuccess(t *testing.T) {
	s := &fakeService{}
	count := 0
	call := func(context.Context, string, host.RPCRequest) ([]byte, error) {
		count++
		if count == 1 {
			return nil, os.ErrNotExist
		}
		return nil, errors.New("Host RPC 401")
	}
	var out bytes.Buffer
	err := manageService(context.Background(), "install", "", &out, s, call)
	if err == nil || !strings.Contains(err.Error(), "Check service logs") || out.Len() != 0 {
		t.Fatalf("err=%v output=%q", err, out.String())
	}
}

func TestAwaitState(t *testing.T) {
	refused := &net.OpError{Op: "dial", Err: connectionRefused}
	for _, err := range []error{os.ErrNotExist, refused} {
		if !hostUnavailable(err) {
			t.Fatal(err)
		}
	}
	for _, err := range []error{nil, os.ErrPermission, context.DeadlineExceeded, &net.OpError{Op: "read", Err: syscall.ECONNRESET}} {
		if hostUnavailable(err) {
			t.Fatal(err)
		}
	}
	for _, running := range []bool{true, false} {
		calls := 0
		call := func(context.Context, string, host.RPCRequest) ([]byte, error) {
			calls++
			if (calls == 1) == running {
				return nil, refused
			}
			return nil, nil
		}
		if err := awaitState(context.Background(), "", running, call); err != nil {
			t.Fatal(err)
		}
		if calls != 2 {
			t.Fatal(calls)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	call := func(context.Context, string, host.RPCRequest) ([]byte, error) { return nil, refused }
	if err := awaitState(ctx, "", true, call); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	// A status error while stopping is not evidence that shutdown completed.
	denied := errors.New("Host RPC 401")
	if err := awaitState(context.Background(), "", false, func(context.Context, string, host.RPCRequest) ([]byte, error) { return nil, denied }); !errors.Is(err, denied) {
		t.Fatal(err)
	}
}

func TestShutdownCompletesBeforeServiceMutation(t *testing.T) {
	s := &fakeService{installed: true}
	polls := 0
	call := func(_ context.Context, _ string, req host.RPCRequest) ([]byte, error) {
		if len(s.operations) > 0 {
			return nil, nil
		}
		if req.Path == "/v1/shutdown" {
			return nil, nil
		}
		polls++
		if polls < 2 {
			return nil, nil
		}
		return nil, os.ErrNotExist
	}
	if err := manageService(context.Background(), "restart", "", &bytes.Buffer{}, s, call); err != nil {
		t.Fatal(err)
	}
	if polls != 2 {
		t.Fatal(polls)
	}
}

func TestAwaitStateRetriesConnectionClosure(t *testing.T) {
	for _, running := range []bool{true, false} {
		for _, transient := range []error{io.EOF, io.ErrUnexpectedEOF, &net.OpError{Op: "read", Err: syscall.ECONNRESET}} {
			calls := 0
			call := func(context.Context, string, host.RPCRequest) ([]byte, error) {
				calls++
				if calls == 1 {
					return nil, transient
				}
				if running {
					return nil, nil
				}
				return nil, &net.OpError{Op: "dial", Err: connectionRefused}
			}
			if err := awaitState(context.Background(), "", running, call); err != nil {
				t.Fatal(err)
			}
			if calls != 2 {
				t.Fatal(calls)
			}
		}
	}
}
