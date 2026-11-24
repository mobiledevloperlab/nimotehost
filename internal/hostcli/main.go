// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Nimote contributors

// Package hostcli implements the nimotehost CLI for both distributions.
package hostcli

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/mobiledevloperlab/nimotehost/internal/host"
	"github.com/mobiledevloperlab/nimotehost/internal/hostservice"
)

func Main(extension host.Extension) {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := runWithExtension(ctx, os.Args[1:], os.Stdin, os.Stdout, extension); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(deploymentExit(err))
	}
}
func runWithExtension(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer, extension host.Extension) error {
	if len(args) > 0 && args[0] == "deploy" {
		return deploy(ctx, args[1:], stdout)
	}
	if len(args) == 2 && args[0] == "device-bridge" {
		return deviceBridge(ctx, args[1], stdin, stdout)
	}
	if len(args) >= 1 && args[0] == "repair" {
		return repairCommand(args[1:], stdout)
	}
	if len(args) >= 1 && args[0] == "devices" {
		return devicesCommand(args[1:], stdout)
	}
	if len(args) == 2 && args[0] == "tools" {
		path, method := "/v1/tools", "GET"
		if args[1] == "refresh" {
			path, method = "/v1/tools/refresh", "POST"
		} else if args[1] != "status" {
			return errors.New("usage: nimotehost tools <status|refresh>")
		}
		dir, err := host.DefaultDir()
		if err != nil {
			return err
		}
		data, err := host.Call(ctx, dir, host.RPCRequest{Method: method, Path: path})
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(stdout, string(data))
		return err
	}
	if len(args) == 2 && args[0] == "rpc-encoded" {
		data, err := base64.StdEncoding.DecodeString(args[1])
		if err != nil {
			return err
		}
		return runWithExtension(ctx, []string{"rpc"}, strings.NewReader(string(data)), stdout, extension)
	}
	if len(args) == 0 || (len(args) == 1 && (args[0] == "--help" || args[0] == "-h")) {
		args = []string{"help"}
	}
	if len(args) == 1 && args[0] == "--version" {
		args = []string{"version"}
	}
	if len(args) != 1 {
		return errors.New("usage: nimotehost <setup|install|uninstall|start|stop|restart|status|capabilities|rpc|rpc-stream|mcp|version|help>")
	}
	if args[0] == "version" {
		_, err := fmt.Fprintln(stdout, "nimotehost", host.Version)
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
	if args[0] != "setup" {
		if err := host.CheckStateName(dir); err != nil {
			return err
		}
	}
	switch args[0] {
	case "setup":
		return setup(ctx, dir, stdout)
	case "rpc-stream":
		return host.ServeRPCStream(ctx, dir, stdin, stdout)
	case "agents":
		data, err := host.Call(ctx, dir, host.RPCRequest{Method: "GET", Path: "/v1/agents"})
		if err == nil {
			_, err = fmt.Fprintln(stdout, string(data))
			return err
		}
		if !hostUnavailable(err) {
			return err
		}
		fallthrough
	case "agents-local":
		if err := host.InitializeToolEnvironment(ctx, dir); err != nil {
			return err
		}
		return json.NewEncoder(stdout).Encode(host.InspectAgents(ctx))
	case "mcp":
		return host.ServeMCP(ctx, dir, stdin, stdout, os.Getenv("NIMOTEHOST_MCP_WRITE") == "1")
	case "start":
		config, err := host.ReadConfig(dir)
		if err != nil {
			return err
		}
		log, err := host.OpenRunLog(dir)
		if err != nil {
			return err
		}
		defer log.Close()
		var level slog.Level
		if err = level.UnmarshalText([]byte(config.LogLevel)); err != nil {
			return err
		}
		slog.SetDefault(slog.New(slog.NewTextHandler(io.MultiWriter(os.Stderr, log), &slog.HandlerOptions{Level: level})))
		if err = host.InitializeToolEnvironment(ctx, dir); err != nil {
			return err
		}
		err = host.ServeWithExtension(ctx, dir, stdout, extension)
		if err != nil {
			slog.Error("Host failed", "error", err)
		} else {
			slog.Info("Host stopped")
		}
		return err
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
		return errors.New("unknown Host command; run nimotehost help")
	}
}
