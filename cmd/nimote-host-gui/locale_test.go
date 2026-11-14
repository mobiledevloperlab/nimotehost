// SPDX-License-Identifier: Apache-2.0
package main

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func TestCatalogsCoverEveryLanguageKeyAndTemplate(t *testing.T) {
	var english map[string]string
	data, err := translationFiles.ReadFile("translations/en.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &english); err != nil {
		t.Fatal(err)
	}
	placeholder := regexp.MustCompile(`{{\s*\.([A-Za-z]+)\s*}}`)
	vars := func(text string) []string {
		var result []string
		for _, match := range placeholder.FindAllStringSubmatch(text, -1) {
			result = append(result, match[1])
		}
		sort.Strings(result)
		return result
	}
	for _, code := range languageCodes {
		data, err := translationFiles.ReadFile("translations/" + code + ".json")
		if err != nil {
			t.Fatal(err)
		}
		var translated map[string]string
		if err = json.Unmarshal(data, &translated); err != nil {
			t.Fatal(err)
		}
		if len(translated) != len(english) {
			t.Fatalf("%s has missing or extra keys", code)
		}
		m, err := newMessages(code)
		if err != nil {
			t.Fatal(err)
		}
		for key, fallback := range english {
			value, ok := translated[key]
			if !ok || strings.TrimSpace(value) == "" {
				t.Fatalf("%s: missing %s", code, key)
			}
			if !reflect.DeepEqual(vars(value), vars(fallback)) {
				t.Fatalf("%s/%s has different template fields", code, key)
			}
			data := map[string]any{}
			for _, name := range vars(value) {
				data[name] = "sample"
			}
			result := m.t(key, data)
			if result == key || strings.Contains(result, "{{") {
				t.Fatalf("%s/%s did not render", code, key)
			}
		}
	}
	// Literal keys in presentation code and generated status keys must all exist.
	files, _ := filepath.Glob("*.go")
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		tree, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(tree, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "t" {
				return true
			}
			literal, ok := call.Args[0].(*ast.BasicLit)
			if !ok {
				return true
			}
			key, err := strconv.Unquote(literal.Value)
			if err == nil && english[key] == "" {
				t.Errorf("%s uses untranslated key %s", file, key)
			}
			return true
		})
	}
	for _, state := range []string{"running", "stopped", "unknown", "needs_approval", "idle", "completed", "succeeded", "failed", "canceled", "interrupted", "disconnected"} {
		if english["state."+state] == "" {
			t.Error("untranslated state", state)
		}
	}
	fallback, err := newMessages("unsupported")
	if err != nil || fallback.code != "en" {
		t.Fatal("unknown language did not fall back to English", err)
	}
}
