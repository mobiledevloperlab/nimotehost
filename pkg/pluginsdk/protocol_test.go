// SPDX-License-Identifier: Apache-2.0
package pluginsdk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestSDKWireCompatibilityAndRedaction(t *testing.T) {
	input := `{"id":"0","method":"invoke","capability":"x","input":{}}
{"id":"1","method":"initialize","protocol":"0.9"}
{"id":"2","method":"initialize","protocol":"1.0"}
{"id":"3","method":"ping"}
{"id":"4","method":"invoke","capability":"x","input":{}}
{"id":"5","method":"unknown"}
`
	var out bytes.Buffer
	if err := Serve(context.Background(), strings.NewReader(input), &out, func(context.Context, string, json.RawMessage, func(float64)) (any, error) {
		return nil, errors.New("credential=SECRET")
	}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "SECRET") {
		t.Fatal("error credential leak")
	}
	scan := json.NewDecoder(&out)
	for i := 0; i < 6; i++ {
		var res Response
		if err := scan.Decode(&res); err != nil {
			t.Fatal(err)
		}
		if i == 2 && res.Protocol != Protocol {
			t.Fatal("no negotiation")
		}
		if i == 0 && res.Error.Code != "not_initialized" {
			t.Fatal(res)
		}
		if i == 1 && res.Error.Code != "incompatible_protocol" {
			t.Fatal(res)
		}
		if i == 4 && res.Error.Code != "plugin_error" {
			t.Fatal(res)
		}
	}
}
func TestSDKFrameLimit(t *testing.T) {
	if Serve(context.Background(), strings.NewReader(strings.Repeat("x", MaxFrame+1)), &bytes.Buffer{}, nil) == nil {
		t.Fatal("oversized frame accepted")
	}
}
