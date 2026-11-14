// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Nimote contributors

// Command install builds the standalone distribution and runs its setup command.
// Unlike platform scripts, this source entrypoint also works under policies that
// disallow unsigned PowerShell scripts; it does not change those policies.
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := install(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func install(ctx context.Context) error {
	if runtime.GOOS != "windows" && os.Geteuid() == 0 {
		return fmt.Errorf("run installation as your normal user, without sudo")
	}
	fmt.Fprintln(os.Stdout, "Building NimoteHost...")
	if err := run(ctx, "go", "run", "./tools/build"); err != nil {
		return fmt.Errorf("build failed; installation was not started: %w", err)
	}
	name := "nimotehost"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary, err := filepath.Abs(filepath.Join("dist", runtime.GOOS+"-"+runtime.GOARCH, name))
	if err != nil {
		return err
	}
	return run(ctx, binary, "setup")
}

func run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
