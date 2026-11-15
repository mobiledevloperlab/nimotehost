package pluginsdk

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestPluginUIContractVectors(t *testing.T) {
	b, err := os.ReadFile("testdata/plugin_ui_vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name     string
		Valid    bool
		Contract json.RawMessage
	}
	if err = json.Unmarshal(b, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			err := ValidatePluginUI(append(append([]byte(`{"contract":`), c.Contract...), '}'), "http-inspector", map[string]bool{"request": true})
			if (err == nil) != c.Valid {
				t.Fatalf("valid=%v error=%v", c.Valid, err)
			}
		})
	}
}
func TestPluginUIBudgetsAndLegacy(t *testing.T) {
	for _, legacy := range []string{`{}`, `{"layout":"form","result":"json"}`} {
		if err := ValidatePluginUI([]byte(legacy), "test", nil); err != nil {
			t.Fatal(err)
		}
	}
	for _, bad := range []string{`null`, `{"contract":null}`, `{"contract":"` + strings.Repeat("x", 32769) + `"}`, `{"contract":{"version":"1.0","state":{},"actions":{},"root":` + strings.Repeat(`{"type":"column","children":[`, 14) + `{"type":"text"}` + strings.Repeat(`]}`, 14) + `}}`} {
		if err := ValidatePluginUI([]byte(bad), "test", nil); err == nil {
			t.Fatalf("accepted malicious UI")
		}
	}
}
