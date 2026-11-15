// SPDX-License-Identifier: Apache-2.0
package pluginsdk

import (
	_ "embed"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
)

// PluginUISchema is the normative UI 1.0 contract; clients vendor its exact bytes.
//
//go:embed plugin_ui.schema.json
var PluginUISchema []byte

// ValidatePluginUI leaves legacy metadata untouched. Unknown contract versions
// are rejected at publication; old clients can still use the capability schemas.
func ValidatePluginUI(raw json.RawMessage, plugin string, capabilities map[string]bool) error {
	if len(raw) == 0 {
		return nil
	}
	var ui map[string]json.RawMessage
	if err := json.Unmarshal(raw, &ui); err != nil || ui == nil {
		return errors.New("ui_invalid: expected object")
	}
	contract, ok := ui["contract"]
	if !ok {
		return nil
	}
	if len(raw) > 32768 {
		return errors.New("ui_limit: metadata exceeds 32 KiB")
	}
	var value any
	if err := json.Unmarshal(contract, &value); err != nil {
		return errors.New("ui_invalid: malformed contract")
	}
	nodes := 0
	var walk func(any, int) error
	walk = func(v any, depth int) error {
		nodes++
		if nodes > 4096 || depth > 24 {
			return errors.New("ui_limit: contract complexity")
		}
		switch x := v.(type) {
		case map[string]any:
			for _, child := range x {
				if err := walk(child, depth+1); err != nil {
					return err
				}
			}
		case []any:
			for _, child := range x {
				if err := walk(child, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(value, 0); err != nil {
		return err
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(PluginUISchema, &schema); err != nil {
		return err
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		return err
	}
	if err = resolved.Validate(value); err != nil {
		return errors.New("ui_invalid: invalid UI 1.0 contract")
	}
	obj := value.(map[string]any)
	for key := range obj["state"].(map[string]any) {
		if !uiName(key) {
			return errors.New("ui_invalid: state key")
		}
	}
	actions := obj["actions"].(map[string]any)
	for key, rawAction := range actions {
		a := rawAction.(map[string]any)
		id := a["capability"].(string)
		if !uiName(key) || !strings.HasPrefix(id, plugin+"/") || !capabilities[strings.TrimPrefix(id, plugin+"/")] {
			return errors.New("ui_forbidden: undeclared capability")
		}
	}
	count := 0
	var tree func(map[string]any, int) error
	tree = func(n map[string]any, depth int) error {
		count++
		if count > 128 || depth > 12 {
			return errors.New("ui_limit: component tree")
		}
		if a, ok := n["action"].(string); ok {
			if _, found := actions[a]; !found {
				return errors.New("ui_invalid: unknown action")
			}
		}
		if n["type"] == "form" || n["type"] == "button" {
			if _, ok := n["action"]; !ok {
				return errors.New("ui_invalid: action required")
			}
		}
		if n["type"] == "table" {
			if c, ok := n["columns"].([]any); !ok || len(c) == 0 {
				return errors.New("ui_invalid: table columns required")
			}
		}
		if children, ok := n["children"].([]any); ok {
			for _, child := range children {
				if err := tree(child.(map[string]any), depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return tree(obj["root"].(map[string]any), 0)
}
func uiName(s string) bool {
	if len(s) == 0 || len(s) > 64 {
		return false
	}
	for i, c := range []byte(s) {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || i > 0 && (c >= '0' && c <= '9' || c == '_' || c == '-')) {
			return false
		}
	}
	return true
}
