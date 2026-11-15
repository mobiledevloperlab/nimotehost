// SPDX-License-Identifier: Apache-2.0
package plugins

import (
	"encoding/json"
	"errors"

	"github.com/google/jsonschema-go/jsonschema"
)

// External references are rejected before resolution; schema validation must never
// fetch URLs or open files supplied by an untrusted plugin.
func compile(raw json.RawMessage) (*jsonschema.Resolved, error) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	nodes := 0
	var walk func(any, int) error
	walk = func(v any, depth int) error {
		nodes++
		if nodes > 4096 || depth > 32 {
			return errors.New("schema complexity limit exceeded")
		}
		switch x := v.(type) {
		case map[string]any:
			for k, v := range x {
				if k == "$ref" || k == "$dynamicRef" {
					return errors.New("schema references forbidden; inline schemas required")
				}
				if err := walk(v, depth+1); err != nil {
					return err
				}
			}
		case []any:
			for _, v := range x {
				if err := walk(v, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(value, 0); err != nil {
		return nil, err
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(raw, &schema); err != nil {
		return nil, err
	}
	return schema.Resolve(nil)
}
func validate(raw, value json.RawMessage) error {
	schema, err := compile(raw)
	if err != nil {
		return err
	}
	var v any
	if err = json.Unmarshal(value, &v); err != nil {
		return err
	}
	return schema.Validate(v)
}
