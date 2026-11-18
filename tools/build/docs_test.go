// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Nimote contributors

package main

import (
	"go/scanner"
	"go/token"
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
	}
	link := regexp.MustCompile(`\]\(([^)]+)\)`)
	for _, pair := range pairs {
		for i, name := range pair {
			path := filepath.Join(root, filepath.FromSlash(name))
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			text := string(data)
			other := pair[1-i]
			if !strings.Contains(text, "]("+other+")") {
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

func TestFeatureDocumentationLayout(t *testing.T) {
	directory := filepath.Join("..", "..", "docs")
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Name() == "nimotehost.png" || entry.Name() == "nimotehost-rounded.png" {
			continue
		} // Original brand artwork, not a feature document.
		if entry.IsDir() && (entry.Name() == "plugin-system" || entry.Name() == "open-source-audit") {
			continue
		}
		if entry.IsDir() {
			t.Fatalf("unexpected documentation directory: %s", entry.Name())
		}
		names = append(names, entry.Name())
		data, err := os.ReadFile(filepath.Join(directory, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if containsHan(string(data)) {
			t.Fatalf("%s must document the current state in English", entry.Name())
		}
	}
	if !reflect.DeepEqual(names, []string{"completed.md", "external-agent-sessions-tests.md", "external-agent-sessions.md", "host-hardening-tests.md", "host-hardening.md", "pending.md", "plugin-ui-architecture.md", "plugin-ui-audit.md", "plugin-ui-implementation.md", "plugin-ui-protocol.md"}) {
		t.Fatalf("docs must contain the maintained functionality and hardening documents: %v", names)
	}
}

func TestImplementedRPCReference(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "completed.md"))
	if err != nil {
		t.Fatal(err)
	}
	endpoint := regexp.MustCompile(`(?m)^\| (GET|POST|DELETE) ([^|]+) \|`)
	var actual []string
	for _, match := range endpoint.FindAllStringSubmatch(string(data), -1) {
		actual = append(actual, match[1]+" "+strings.TrimSpace(match[2]))
	}
	expected := []string{
		"GET /v1/capabilities", "GET /v1/status", "GET /v1/sessions?after=revision",
		"POST /v1/sessions", "GET /v1/sessions/{id}", "DELETE /v1/sessions/{id}",
		"POST /v1/sessions/{id}/cancel", "POST /v1/sessions/{id}/input", "POST /v1/sessions/{id}/input-stream", "POST /v1/sessions/{id}/resize",
		"GET /v1/sessions/{id}/output?offset=0", "GET /v1/sessions/{id}/agent-state",
		"POST /v1/sessions/{id}/agent-message", "GET /v1/sessions/{id}/activity?after=sequence",
		"GET /v1/events?after=revision", "GET /v1/agent-history?agent=...&directory=...&cursor=...",
		"GET /v1/agent-history/{conversation_id}?agent=...&directory=...&cursor=...",
		"POST /v1/agent-history",
		"GET /v1/inbox?view=...", "POST /v1/inbox/{id}",
		"GET /v1/sessions/{id}/verification", "POST /v1/sessions/{id}/verification", "GET /v1/sessions/{id}/verification-diff", "GET /v1/conflicts",
		"GET /v1/github-tasks", "POST /v1/github-tasks", "GET /v1/github-tasks/{id}", "POST /v1/github-tasks/{id}/{action}",
		"POST /v1/extensions", "POST /v1/shutdown-if-idle", "POST /v1/shutdown",
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("RPC documentation differs: %v", actual)
	}
}

func containsHan(text string) bool {
	return regexp.MustCompile(`\p{Han}`).MatchString(text)
}

func TestSubdirectoryDocumentationAndCodeCommentsUseEnglish(t *testing.T) {
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", ".nimote", "dist", "release", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		extension := filepath.Ext(path)
		if extension != ".md" && extension != ".go" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if extension == ".md" {
			if filepath.Dir(path) != root && containsHan(string(data)) {
				t.Errorf("subdirectory documentation must use English: %s", path)
			}
			return nil
		}
		fileSet := token.NewFileSet()
		file := fileSet.AddFile(path, fileSet.Base(), len(data))
		var source scanner.Scanner
		source.Init(file, data, nil, scanner.ScanComments)
		for {
			position, kind, literal := source.Scan()
			if kind == token.EOF {
				break
			}
			if kind == token.COMMENT && containsHan(literal) {
				t.Errorf("code comment must use English: %s", fileSet.Position(position))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
