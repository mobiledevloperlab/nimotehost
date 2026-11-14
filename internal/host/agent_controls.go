// SPDX-License-Identifier: Apache-2.0
package host

import (
	"encoding/json"
	"errors"
)

// A provider-owned selection, projected without exposing arbitrary RPC access.
type agentChoice struct {
	Value string `json:"value"`
	Label string `json:"label"`
}
type agentOption struct {
	ID       string        `json:"id"`
	Label    string        `json:"label"`
	Category string        `json:"category,omitempty"`
	Value    string        `json:"value"`
	Values   []agentChoice `json:"values"`
}
type agentCommand struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}
type agentControls struct {
	Steer    bool           `json:"steer,omitempty"`
	Codex    bool           `json:"codex,omitempty"`
	Commands []agentCommand `json:"commands"`
	Options  []agentOption  `json:"options"`
}
type codexModel struct {
	Model         string `json:"model"`
	DisplayName   string `json:"displayName"`
	Default       bool   `json:"isDefault"`
	DefaultEffort string `json:"defaultReasoningEffort"`
	Efforts       []struct {
		Effort string `json:"reasoningEffort"`
	} `json:"supportedReasoningEfforts"`
}

func (a *structuredAgent) publishControls() {
	a.controls.Steer = a.acp == nil
	a.publishExecutionPolicy()
	a.emit("controls", a.controls)
}
func (a *structuredAgent) receiveModels(raw json.RawMessage) {
	var result struct {
		Data       []codexModel `json:"data"`
		NextCursor string       `json:"nextCursor"`
	}
	if json.Unmarshal(raw, &result) != nil {
		a.loadingModels = false
		a.modelPage = nil
		a.emit("error", "Invalid model catalog")
		return
	}
	a.modelPage = append(a.modelPage, result.Data...)
	if result.NextCursor != "" && len(a.modelPage) < 500 {
		if err := a.send(map[string]any{"id": "host-models", "method": "model/list", "params": map[string]any{"limit": 100, "cursor": result.NextCursor}}); err != nil {
			a.loadingModels = false
			a.modelPage = nil
			a.emit("error", err.Error())
		}
		return
	}
	a.models = a.modelPage
	a.modelPage = nil
	a.loadingModels = false
	a.codexControls()
}
func (a *structuredAgent) codexControls() {
	a.controls = agentControls{Codex: true, Commands: []agentCommand{{Name: "compact"}}, Options: []agentOption{}}
	choices := []agentChoice{}
	var selected *codexModel
	for i := range a.models {
		m := &a.models[i]
		if m.Model == "" {
			continue
		}
		label := m.DisplayName
		if label == "" {
			label = m.Model
		}
		choices = append(choices, agentChoice{m.Model, label})
		if a.model == "" && m.Default {
			a.model = m.Model
		}
		if m.Model == a.model {
			selected = m
		}
	}
	if a.model != "" && selected == nil {
		choices = append(choices, agentChoice{a.model, a.model})
	}
	if len(choices) > 0 {
		a.controls.Options = append(a.controls.Options, agentOption{ID: "model", Label: "Model", Category: "model", Value: a.model, Values: choices})
	}
	if selected != nil && len(selected.Efforts) > 0 {
		efforts := []agentChoice{}
		for _, e := range selected.Efforts {
			efforts = append(efforts, agentChoice{e.Effort, e.Effort})
		}
		a.controls.Options = append(a.controls.Options, agentOption{ID: "reasoning", Label: "Reasoning effort", Category: "thought_level", Value: a.effort, Values: efforts})
	}
	a.publishControls()
}
func (a *structuredAgent) configureCodex(id, value string) error {
	if len(a.models) == 0 {
		return errors.New("model catalog is not ready")
	}
	for _, opt := range a.controls.Options {
		if opt.ID != id {
			continue
		}
		for _, choice := range opt.Values {
			if choice.Value != value {
				continue
			}
			if id == "model" {
				a.model = value
				// An effort valid for one model need not be valid for the next.
				a.effort = ""
				for _, m := range a.models {
					if m.Model == value {
						a.effort = m.DefaultEffort
					}
				}
			} else if id == "reasoning" {
				a.effort = value
			} else {
				return errors.New("unknown configuration option")
			}
			a.codexControls()
			return nil
		}
	}
	return errors.New("unsupported configuration value")
}

// ACP agents advertise commands and configuration instead of sharing CLI/TUI
// command registries. Keep their IDs and supported values intact.
func (a *structuredAgent) configACP(raw json.RawMessage) {
	var result struct {
		ConfigOptions []struct {
			ID           string `json:"id"`
			Name         string `json:"name"`
			Category     string `json:"category"`
			CurrentValue string `json:"currentValue"`
			Options      []struct {
				Value   string `json:"value"`
				Name    string `json:"name"`
				Options []struct {
					Value string `json:"value"`
					Name  string `json:"name"`
				} `json:"options"`
			} `json:"options"`
		} `json:"configOptions"`
		Models *struct {
			Current   string `json:"currentModelId"`
			Available []struct {
				ID   string `json:"modelId"`
				Name string `json:"name"`
			} `json:"availableModels"`
		} `json:"models"`
	}
	if json.Unmarshal(raw, &result) != nil {
		return
	}
	if result.ConfigOptions != nil {
		a.controls.Options = []agentOption{}
		for _, c := range result.ConfigOptions {
			opt := agentOption{ID: c.ID, Label: c.Name, Category: c.Category, Value: c.CurrentValue, Values: []agentChoice{}}
			for _, v := range c.Options {
				if v.Value != "" {
					opt.Values = append(opt.Values, agentChoice{v.Value, v.Name})
				}
				for _, g := range v.Options {
					opt.Values = append(opt.Values, agentChoice{g.Value, g.Name})
				}
			}
			if len(opt.Values) > 0 {
				a.controls.Options = append(a.controls.Options, opt)
			}
		}
	} else if result.Models != nil {
		opt := agentOption{ID: "__model", Category: "model", Label: "Model", Value: result.Models.Current, Values: []agentChoice{}}
		for _, m := range result.Models.Available {
			opt.Values = append(opt.Values, agentChoice{m.ID, m.Name})
		}
		a.controls.Options = []agentOption{opt}
	}
	a.publishControls()
}
