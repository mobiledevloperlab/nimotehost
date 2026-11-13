// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Nimote contributors

// Command nimote-host runs the optional Go Host over existing authenticated SSH.
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/mobiledevloperlab/nimote-host/internal/host"
	"github.com/mobiledevloperlab/nimote-host/internal/hostservice"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, os.Args[1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) == 2 && args[0] == "rpc-encoded" {
		data, err := base64.StdEncoding.DecodeString(args[1])
		if err != nil {
			return err
		}
		return run(ctx, []string{"rpc"}, strings.NewReader(string(data)), stdout)
	}
	if len(args) == 0 || (len(args) == 1 && (args[0] == "--help" || args[0] == "-h")) {
		args = []string{"help"}
	}
	if len(args) == 1 && args[0] == "--version" {
		args = []string{"version"}
	}
	if len(args) != 1 {
		return errors.New("usage: nimote-host <setup|install|uninstall|start|stop|restart|status|capabilities|rpc|rpc-stream|mcp|version|help>")
	}
	if args[0] == "version" {
		_, err := fmt.Fprintln(stdout, "nimote-host", host.Version)
		return err
	}
	if args[0] == "help" {
		_, err := fmt.Fprint(stdout, helpText)
		return err
	}
	dir, err := host.DefaultDir()
	if err != nil {
		return err
	}
	switch args[0] {
	case "setup":
		return setup(ctx, dir, stdout)
	case "rpc-stream":
		return host.ServeRPCStream(ctx, dir, stdin, stdout)
	case "mcp":
		return host.ServeMCP(ctx, dir, stdin, stdout, os.Getenv("NIMOTE_HOST_MCP_WRITE") == "1")
	case "start":
		return host.Serve(ctx, dir, stdout)
	case "install", "uninstall", "restart":
		service, err := hostservice.NewHostDefault()
		if err != nil {
			return err
		}
		return manageService(ctx, args[0], dir, stdout, service, host.Call)

	case "stop", "status", "capabilities":
		req := host.RPCRequest{Method: "GET", Path: "/v1/" + args[0]}
		if args[0] == "stop" {
			req.Method = "POST"
			req.Path = "/v1/shutdown"
		}
		data, err := host.Call(ctx, dir, req)
		if err != nil {
			return err
		}
		if args[0] == "stop" {
			if err := awaitState(ctx, dir, false, host.Call); err != nil {
				return err
			}
		}
		_, err = stdout.Write(data)
		return err
	case "rpc":
		data, err := io.ReadAll(io.LimitReader(stdin, (1<<20)+1))
		if err != nil {
			return err
		}
		if len(data) > 1<<20 {
			return errors.New("RPC request too large")
		}
		var req host.RPCRequest
		if err := json.Unmarshal(data, &req); err != nil {
			return err
		}
		result, err := host.Call(ctx, dir, req)
		if err != nil {
			return err
		}
		_, err = stdout.Write(result)
		return err
	default:
		return errors.New("unknown Host command; run nimote-host help")
	}
}
