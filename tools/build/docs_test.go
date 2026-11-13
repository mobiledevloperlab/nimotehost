// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Nimote contributors

package main

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func TestBilingualDocumentation(t *testing.T) {
	root := filepath.Join("..", "..")
	pairs := [][2]string{
		{"README.md", "README.zh-CN.md"},
		{"CONTRIBUTING.md", "CONTRIBUTING.zh-CN.md"},
		{"SECURITY.md", "SECURITY.zh-CN.md"},
		{"CHANGELOG.md", "CHANGELOG.zh-CN.md"},
		{"THIRD_PARTY_NOTICES.md", "THIRD_PARTY_NOTICES.zh-CN.md"},
		{"docs/host.en.md", "docs/host.md"},
		{"docs/deployment.md", "docs/deployment.zh-CN.md"},
		{"docs/maintenance.md", "docs/maintenance.zh-CN.md"},
		{"docs/agent-adapters.md", "docs/agent-adapters.zh-CN.md"},
	}
	link := regexp.MustCompile(`\]\(([^)]+)\)`)
	for _, pair := range pairs {
		for _, name := range pair {
			path := filepath.Join(root, filepath.FromSlash(name))
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			text := string(data)
			if !strings.Contains(text, "English") || !strings.Contains(text, "简体中文") {
				t.Errorf("%s has no language navigation", name)
			}
			for _, match := range link.FindAllStringSubmatch(text, -1) {
				target := strings.SplitN(match[1], "#", 2)[0]
				if target == "" || strings.Contains(target, "://") {
					continue
				}
				if _, err := os.Stat(filepath.Join(filepath.Dir(path), filepath.FromSlash(target))); err != nil {
					t.Errorf("broken link in %s: %s", name, target)
				}
			}
		}
	}
}

func TestTranslatedRPCListsMatch(t *testing.T) {
	endpoint := regexp.MustCompile(`(?m)^\| (GET|POST|DELETE) ([^|]+) \|`)
	read := func(name string) [][]string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join("..", "..", "docs", name))
		if err != nil {
			t.Fatal(err)
		}
		return endpoint.FindAllStringSubmatch(string(data), -1)
	}
	english, chinese := read("host.en.md"), read("host.md")
	if len(english) == 0 || !reflect.DeepEqual(english, chinese) {
		t.Fatal("English and Chinese RPC endpoint lists differ")
	}
}
