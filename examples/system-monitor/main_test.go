// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	sdk "github.com/mobiledevloperlab/nimotehost/pkg/pluginsdk"
)

func TestManifestAndSnapshotContract(t *testing.T) {
	raw, err := os.ReadFile("manifest.template.json")
	if err != nil {
		t.Fatal(err)
	}
	var m sdk.Manifest
	if err = json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if err = m.Validate(); err != nil {
		t.Fatal(err)
	}
	var input, output jsonschema.Schema
	if err = json.Unmarshal(m.Capabilities[0].Input, &input); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(m.Capabilities[0].Output, &output); err != nil {
		t.Fatal(err)
	}
	in, err := input.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	out, err := output.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, payload := range []string{`{}`, `{"mode":"demo","limit":500}`, `{"mode":"demo","limit":21,"filter":"编译","sort":"memory"}`, `{"mode":"demo","sections":{"processes":false,"network":false}}`} {
		var value any
		json.Unmarshal([]byte(payload), &value)
		if err = in.Validate(value); err != nil {
			t.Fatal(err)
		}
		result, err := handle(context.Background(), "snapshot", json.RawMessage(payload), func(float64) {})
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		if len(raw) > sdk.MaxFrame-4096 {
			t.Fatal("snapshot exceeds wire budget")
		}
		json.Unmarshal(raw, &value)
		if err = out.Validate(value); err != nil {
			t.Fatal(err)
		}
	}
}
func TestDemoFilteringAndBounds(t *testing.T) {
	o := defaults()
	o.Mode = "demo"
	o.Limit = 500
	s := collect(context.Background(), o)
	if len(s.Processes) != 500 || len(s.History) != 60 || len(s.Alerts) == 0 || !s.HasWarnings {
		t.Fatal("missing pressure data")
	}
	o.Filter = "编译"
	o.Sort = "memory"
	o.Limit = 21
	s = collect(context.Background(), o)
	if len(s.Processes) != 21 || s.Summary["matched_processes"] != 100 {
		t.Fatal(s.Summary)
	}
	for i := 1; i < len(s.Processes); i++ {
		if s.Processes[i-1]["memory"].(float64) < s.Processes[i]["memory"].(float64) {
			t.Fatal("incorrect numeric sort")
		}
	}
	o.Sections.Processes = false
	o.Sections.Network = false
	s = collect(context.Background(), o)
	if len(s.Processes) != 0 || len(s.Network) != 0 || len(s.Alerts) != 0 {
		t.Fatal("disabled section collected")
	}
}
func TestRejectInvalidOptions(t *testing.T) {
	for _, raw := range []string{`null`, `[]`, `{"mode":"other"}`, `{"limit":501}`, `{"limit":0}`, `{"sort":"pid"}`, `{"thresholds":{"cpu":0}}`, `{"sections":{"unknown":true}}`, `{} {}`, `{"filter":123}`} {
		if _, err := decode(json.RawMessage(raw)); err == nil {
			t.Fatal("accepted", raw)
		}
	}
}
func TestProcessParser(t *testing.T) {
	rows := parseProcesses(" 12 10.2 1.5 2048 /System/Long Process Name\ninvalid\n13 x 1 2 nope\n14 0.0 0.0 0 idle\n")
	if len(rows) != 2 || rows[0]["name"] != "/System/Long Process Name" || rows[0]["rss_mib"] != 2.0 {
		t.Fatal(rows)
	}
}
func TestCanceledCollection(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := handle(ctx, "snapshot", json.RawMessage(`{"mode":"demo"}`), func(float64) {}); err == nil {
		t.Fatal("cancellation ignored")
	}
}
