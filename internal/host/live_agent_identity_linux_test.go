// SPDX-License-Identifier: Apache-2.0
//go:build linux

package host

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRootLiveAgentControllerApprovalAndResume(t *testing.T) {
	provider := os.Getenv("NIMOTE_TEST_ROOT_LIVE_AGENT")
	if provider == "" {
		t.Skip("explicit root-only live model/isolated UID acceptance")
	}
	if os.Geteuid() != 0 || !supportsStructuredAgent(provider) {
		t.Fatal("root and a supported provider are required")
	}
	if runRootAcceptanceHelper(t, "TestRootLiveAgentControllerApprovalAndResume") {
		return
	}
	profile := os.Getenv("NIMOTE_TEST_ROOT_AGENT_PROFILE")
	if !filepath.IsAbs(profile) {
		t.Fatal("private test profile directory required")
	}
	root := rootScopeFixture(t)
	base, err := os.MkdirTemp("/tmp", "nimotehost-live-identity-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(base)
	if err := os.Chmod(base, 0755); err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(base, "workspace")
	if err := os.Mkdir(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	var bytes int64
	err = filepath.WalkDir(profile, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(profile, path)
		if err != nil {
			return err
		}
		target := filepath.Join(workspace, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0700)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			t.Fatal("profile must contain only regular files and directories")
		}
		bytes += info.Size()
		if bytes > 1<<20 {
			t.Fatal("test profile exceeds 1 MiB")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0600)
	})
	if err != nil {
		t.Fatal(err)
	}
	limits := DefaultResourceLimits()
	limits.CgroupRoot = root
	limits.MemoryBytes = 512 << 20
	limits.MaxProcesses = 128
	limits.MaxRuntimeSeconds = 180
	m, err := NewManagerWithLimits(filepath.Join(base, "host", "sessions"), limits)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	uid := uint32(1400000000 + time.Now().UnixNano()%100000000)
	identities := 0
	identityForStart := func(conversation string) *ExecutionIdentity {
		if identities > 0 && conversation == "" {
			t.Fatal("resume silently became a new conversation")
		}
		uid++
		// Owner-driven transfer after the previous owned cgroup has stopped. A fresh
		// UID is mandatory: UID reservations must never be recycled across sessions.
		if err := filepath.WalkDir(workspace, func(path string, _ fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			return os.Lchown(path, int(uid), int(uid))
		}); err != nil {
			t.Fatal(err)
		}
		identities++
		return &ExecutionIdentity{UID: uid, GID: uid}
	}
	authorize := func(record Session) string {
		device, err := CreateControllerDevice(filepath.Dir(m.dir), "live isolated "+provider, []string{record.ID})
		if err != nil {
			t.Fatal(err)
		}
		token, _, err := deviceCredential(DeviceContext(context.Background(), device.ID), filepath.Dir(m.dir))
		if err != nil {
			t.Fatal(err)
		}
		return token
	}
	runLiveAgentAcceptance(t, provider, m, workspace, identityForStart, authorize)
	if identities != 2 {
		t.Fatal("expected independent original and resumed session identities", identities)
	}
}
