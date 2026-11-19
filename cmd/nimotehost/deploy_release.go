// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mobiledevloperlab/nimotehost/internal/host"
	"github.com/mobiledevloperlab/nimotehost/internal/hostinstall"
	"github.com/mobiledevloperlab/nimotehost/internal/hostservice"
)

type deployBuild struct {
	Version, OS, Arch     string
	Protocol, MinProtocol int
}

func (b deployBuild) supports(protocol int) bool {
	return protocol >= b.MinProtocol && protocol <= b.Protocol
}

func readDeployBuild(target string) (deployBuild, error) {
	var build deployBuild
	file, err := os.Open(filepath.Join(target, "BUILD.json"))
	if err != nil {
		return build, err
	}
	defer file.Close()
	if err := json.NewDecoder(io.LimitReader(file, 1<<20)).Decode(&build); err != nil {
		return build, err
	}
	if build.Version == "" || build.OS == "" || build.Arch == "" {
		return build, errors.New("incomplete Host build metadata")
	}
	// The published v2.4.0 bundles predate the manifest protocol fields.
	if build.Version == "2.4.0" && build.Protocol == 0 && build.MinProtocol == 0 {
		build.Protocol, build.MinProtocol = 1, 1
	}
	return build, nil
}

func deployRelease(ctx context.Context, dir, target string, opts deployOptions, result *deployResult) error {
	if err := outsideInstall(target); err != nil {
		return err
	}
	service, err := hostservice.NewHostExecutable(result.Binary)
	if err != nil {
		return err
	}
	if err := service.CheckAvailable(ctx); err != nil {
		return err
	}
	desktop := opts.desktop || desktopBundle(target)
	tag, asset, err := hostinstall.ResolveRelease(ctx, opts.tag, result.OS, result.Arch, desktop)
	if err != nil {
		return err
	}
	result.Release, result.ArchiveSHA256 = tag, strings.TrimPrefix(asset.Digest, "sha256:")
	if opts.sha256 != "" && !strings.EqualFold(opts.sha256, result.ArchiveSHA256) {
		return fmt.Errorf("%w: release digest differs from the pinned SHA-256", hostinstall.ErrIntegrity)
	}
	version := strings.TrimPrefix(tag, "v")
	check := func() error {
		if !desktop && desktopBundle(target) {
			return fmt.Errorf("%w: desktop installation appeared during download; retry", errDeployConflict)
		}
		if installed, err := readDeployBuild(target); err == nil && newerStable(installed.Version, version) {
			return fmt.Errorf("%w: installed version %s is newer than %s", errDeployConflict, installed.Version, version)
		}
		return nil
	}
	if err := check(); err != nil {
		return err
	}

	temporary, err := os.MkdirTemp("", "nimotehost-release-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temporary)
	archive, source := filepath.Join(temporary, "archive"), filepath.Join(temporary, "bundle")
	if err := asset.Download(ctx, archive); err != nil {
		return err
	}
	if err := hostinstall.Extract(ctx, archive, source, result.OS == "windows"); err != nil {
		return errors.Join(hostinstall.ErrIntegrity, err)
	}
	if err := hostinstall.Verify(ctx, source); err != nil {
		return errors.Join(hostinstall.ErrIntegrity, err)
	}
	build, err := readDeployBuild(source)
	if err != nil {
		return errors.Join(hostinstall.ErrIntegrity, err)
	}
	if build.Version != version || build.OS != result.OS || build.Arch != result.Arch {
		return fmt.Errorf("%w: archive build does not match selected release", hostinstall.ErrIntegrity)
	}
	if build.MinProtocol < 1 || !build.supports(opts.protocol) || !build.supports(host.Protocol) {
		return fmt.Errorf("%w: client %d, installer %d; release supports %d through %d", errDeployProtocol, opts.protocol, host.Protocol, build.MinProtocol, build.Protocol)
	}
	return setupBundle(ctx, dir, source, io.Discard, check)
}

func desktopBundle(directory string) bool {
	for _, name := range []string{"nimotehost-gui", "nimotehost-gui.exe", "NimoteHost.app"} {
		if _, err := os.Lstat(filepath.Join(directory, name)); err == nil {
			return true
		}
	}
	return false
}

func newerStable(current, candidate string) bool {
	parts := strings.Split(current, ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" || strings.Trim(part, "0123456789") != "" || len(part) > 1 && part[0] == '0' {
			return false
		}
	}
	for i, part := range strings.Split(candidate, ".") {
		if len(parts[i]) != len(part) {
			return len(parts[i]) > len(part)
		}
		if parts[i] != part {
			return parts[i] > part
		}
	}
	return false
}
