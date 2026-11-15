// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/mobiledevloperlab/nimotehost/internal/desktop"
)

type pluginsPanel struct {
	ui      *managerWindow
	root    fyne.CanvasObject
	cards   *fyne.Container
	loading bool
}

func newPluginsPanel(u *managerWindow) *pluginsPanel {
	p := &pluginsPanel{ui: u, cards: column(12, wrappedLabel(u.t("plugins.empty")))}
	install := widget.NewButtonWithIcon(u.t("catalog.localInstall"), theme.ContentAddIcon(), func() { p.install("plugins.install", "") })
	install.Importance = widget.HighImportance
	header := column(12, pageHeading(u.t("nav.plugins"), u.t("plugins.intro")), wrappedLabel(u.t("plugins.boundary")), container.NewHBox(install, widget.NewButtonWithIcon(u.t("catalog.title"), theme.SearchIcon(), p.browseCatalog), widget.NewButtonWithIcon(u.t("action.refresh"), theme.ViewRefreshIcon(), p.refresh)))
	p.root = inset(container.NewBorder(header, nil, nil, nil, container.NewVScroll(p.cards)), pagePadding)
	return p
}
func (p *pluginsPanel) refresh() {
	if p.loading || p.ui.ctx.Err() != nil {
		return
	}
	p.loading = true
	ctx := p.ui.ctx
	go func() {
		c, cancel := context.WithTimeout(ctx, 8*time.Second)
		defer cancel()
		raw, err := p.ui.manager.PluginCall(c, desktop.PluginRequest{Operation: "plugins.list"})
		var plugins []desktop.PluginInfo
		var caps []desktop.PluginCapability
		if err == nil {
			err = json.Unmarshal(raw, &plugins)
		}
		if err == nil {
			raw, err = p.ui.manager.PluginCall(c, desktop.PluginRequest{Operation: "capabilities.list"})
			if err == nil {
				err = json.Unmarshal(raw, &caps)
			}
		}
		fyne.Do(func() {
			if ctx.Err() != nil {
				return
			}
			p.loading = false
			p.render(plugins, caps, err)
		})
	}()
}
func (p *pluginsPanel) render(plugins []desktop.PluginInfo, caps []desktop.PluginCapability, err error) {
	u := p.ui
	p.cards.Objects = nil
	if err != nil {
		p.cards.Add(wrappedLabel(u.t("plugins.unavailable")))
		p.cards.Add(widget.NewButton(u.t("details.technical"), func() { u.showReader(u.t("nav.plugins"), err.Error()) }))
		return
	}
	if len(plugins) == 0 {
		p.cards.Add(wrappedLabel(u.t("plugins.empty")))
	}
	for _, item := range plugins {
		plugin := item
		details := widget.NewButton(u.t("devices.details"), func() { raw, _ := json.MarshalIndent(plugin, "", "  "); u.showReader(plugin.ID, string(raw)) })
		actions := container.NewGridWithColumns(2, details)
		if plugin.State == "disabled" {
			actions.Add(widget.NewButton(u.t("plugins.enable"), func() {
				raw, _ := json.MarshalIndent(plugin.Manifest, "", "  ")
				p.confirm(u.t("plugins.enable"), u.t("plugins.boundary")+"\n\n"+string(raw), desktop.PluginRequest{Operation: "plugins.enable", ID: plugin.ID, Grants: plugin.Manifest.Permissions, Confirmed: true})
			}))
			actions.Add(widget.NewButton(u.t("plugins.update"), func() { p.install("plugins.update", plugin.ID) }))
			actions.Add(widget.NewButton(u.t("plugins.uninstall"), func() {
				p.confirm(u.t("plugins.uninstall"), plugin.ID, desktop.PluginRequest{Operation: "plugins.uninstall", ID: plugin.ID, Confirmed: true})
			}))
		} else {
			actions.Add(widget.NewButton(u.t("plugins.disable"), func() { p.mutate(desktop.PluginRequest{Operation: "plugins.disable", ID: plugin.ID}) }))
		}
		content := column(8, mutedLabel(plugin.Manifest.Publisher+" · "+plugin.State), actions)
		for _, entry := range caps {
			if entry.Plugin != plugin.ID {
				continue
			}
			capability := entry
			invoke := widget.NewButton(u.t("plugins.invoke"), func() { p.invoke(capability) })
			if capability.State != "ready" {
				invoke.Disable()
			}
			content.Add(container.NewBorder(nil, nil, nil, invoke, wrappedLabel(capability.Title+" · "+capability.Kind+" · "+capability.Version)))
		}
		p.cards.Add(widget.NewCard(plugin.ID+" · "+plugin.Manifest.Version, "", content))
	}
	p.cards.Refresh()
}
func (p *pluginsPanel) confirm(title, body string, req desktop.PluginRequest) {
	d := dialog.NewCustomConfirm(title, title, p.ui.t("action.cancel"), container.NewVScroll(readOnlyText(body)), func(ok bool) {
		if ok {
			p.mutate(req)
		}
	}, p.ui.window)
	d.Resize(p.ui.readerSize())
	d.Show()
}
func (p *pluginsPanel) mutate(req desktop.PluginRequest) {
	ctx := p.ui.ctx
	go func() {
		c, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		_, err := p.ui.manager.PluginCall(c, req)
		fyne.Do(func() {
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				p.ui.showFailure("plugins.error", err)
			} else {
				p.ui.notice(p.ui.t("action.done"))
				p.refresh()
			}
		})
	}()
}
func (p *pluginsPanel) install(operation, expectedID string) {
	u := p.ui
	path := widget.NewEntry()
	path.SetPlaceHolder(u.t("plugins.folder"))
	browse := widget.NewButton(u.t("plugins.browse"), func() {
		dialog.ShowFolderOpen(func(uri fyne.ListableURI, err error) {
			if err != nil {
				u.showFailure("plugins.error", err)
				return
			}
			if uri != nil {
				path.SetText(uri.Path())
			}
		}, u.window)
	})
	title := u.t("plugins.install")
	if operation == "plugins.update" {
		title = u.t("plugins.update")
	}
	form := dialog.NewCustomConfirm(title, u.t("plugins.review"), u.t("action.cancel"), column(8, wrappedLabel(u.t("plugins.folder")), container.NewBorder(nil, nil, nil, browse, path)), func(ok bool) {
		if !ok {
			return
		}
		dir := strings.TrimSpace(path.Text)
		ctx := u.ctx
		go func() {
			manifest, digest, err := desktop.ReviewPluginBundle(dir)
			if err == nil && expectedID != "" && manifest.ID != expectedID {
				err = fmt.Errorf("expected plugin %s", expectedID)
			}
			fyne.Do(func() {
				if ctx.Err() != nil {
					return
				}
				if err != nil {
					u.showFailure("plugins.error", err)
					return
				}
				raw, _ := json.MarshalIndent(manifest, "", "  ")
				p.confirm(title, u.t("plugins.boundary")+"\nSHA256: "+digest+"\n\n"+string(raw), desktop.PluginRequest{Operation: operation, Source: dir, SHA256: digest, Confirmed: true})
			})
		}()
	}, u.window)
	form.Resize(u.readerSize())
	form.Show()
}
func pluginInput(cap desktop.PluginCapability) string {
	if cap.ACP != 0 {
		return "{\n  \"directory\": \"\",\n  \"request_id\": \"\"\n}"
	}
	var schema struct {
		Properties map[string]struct {
			Type    string `json:"type"`
			Default any    `json:"default"`
			Enum    []any  `json:"enum"`
		} `json:"properties"`
		Required []string `json:"required"`
	}
	if json.Unmarshal(cap.Input, &schema) != nil {
		return "{}"
	}
	values := map[string]any{}
	for _, key := range schema.Required {
		prop := schema.Properties[key]
		switch {
		case prop.Default != nil:
			values[key] = prop.Default
		case len(prop.Enum) > 0:
			values[key] = prop.Enum[0]
		default:
			switch prop.Type {
			case "string":
				values[key] = ""
			case "integer", "number":
				values[key] = 0
			case "boolean":
				values[key] = false
			case "array":
				values[key] = []any{}
			case "object":
				values[key] = map[string]any{}
			}
		}
	}
	raw, _ := json.MarshalIndent(values, "", "  ")
	return string(raw)
}
func (p *pluginsPanel) invoke(cap desktop.PluginCapability) {
	u := p.ui
	ctx := u.ctx
	input := widget.NewMultiLineEntry()
	input.SetText(pluginInput(cap))
	input.SetMinRowsVisible(8)
	raw, _ := json.MarshalIndent(cap, "", "  ")
	body := column(8, wrappedLabel(cap.Description), wrappedLabel(u.t("plugins.input")), input, widget.NewAccordion(widget.NewAccordionItem(u.t("plugins.schema"), readOnlyText(string(raw)))))
	d := dialog.NewCustomConfirm(cap.Title, u.t("plugins.invoke"), u.t("action.cancel"), container.NewVScroll(body), func(ok bool) {
		if !ok {
			return
		}
		value := json.RawMessage(input.Text)
		if !json.Valid(value) {
			u.showFailure("plugins.error", fmt.Errorf("invalid JSON"))
			return
		}
		send := func() {
			go func() {
				c, cancel := context.WithTimeout(ctx, 8*time.Second)
				defer cancel()
				requestID, err := desktop.PluginRequestID()
				var result json.RawMessage
				var page struct {
					Epoch string `json:"epoch"`
				}
				if err == nil {
					result, err = u.manager.PluginCall(c, desktop.PluginRequest{Operation: "capabilities.subscribe"})
					if err == nil {
						err = json.Unmarshal(result, &page)
					}
				}
				if err == nil {
					result, err = u.manager.PluginCall(c, desktop.PluginRequest{Operation: "capabilities.invoke", ID: cap.ID, Input: value, Confirmed: true, RequestID: requestID, Epoch: page.Epoch})
				}
				fyne.Do(func() {
					if ctx.Err() != nil {
						return
					}
					if err != nil {
						u.showFailure("plugins.uncertain", fmt.Errorf("request %s: %w", requestID, err))
						return
					}
					if cap.ACP != 0 {
						u.showReader(cap.Title, string(result))
						u.requestRefresh()
						return
					}
					var run struct {
						ID string `json:"id"`
					}
					if json.Unmarshal(result, &run) != nil || run.ID == "" {
						u.showFailure("plugins.error", fmt.Errorf("invalid invocation response"))
						return
					}
					p.watchInvocation(run.ID, page.Epoch)
				})
			}()
		}
		if cap.Sensitive || cap.ACP != 0 {
			confirmDialog(u.window, cap.Title, u.t("plugins.confirmInvoke")+"\n"+string(value), u.t("plugins.invoke"), u.t("action.cancel"), send)
		} else {
			send()
		}
	}, u.window)
	d.Resize(u.readerSize())
	d.Show()
}
func (p *pluginsPanel) watchInvocation(id, epoch string) {
	u := p.ui
	view := wrappedLabel(u.t("action.loading"))
	progress := widget.NewProgressBar()
	ctx, cancel := context.WithCancel(u.ctx)
	cancelButton := widget.NewButton(u.t("plugins.cancelCall"), func() { p.mutate(desktop.PluginRequest{Operation: "capabilities.cancel", ID: id}) })
	d := dialog.NewCustom(u.t("plugins.result"), u.t("action.close"), column(8, readOnlyText(id), progress, cancelButton, container.NewVScroll(view)), u.window)
	d.SetOnClosed(cancel)
	d.Resize(u.readerSize())
	d.Show()
	go func() {
		var cursor uint64
		for ctx.Err() == nil {
			c, done := context.WithTimeout(ctx, 6*time.Second)
			raw, err := u.manager.PluginCall(c, desktop.PluginRequest{Operation: "capabilities.result", ID: id})
			var run struct {
				State string `json:"state"`
			}
			if err == nil {
				err = json.Unmarshal(raw, &run)
			}
			var page struct {
				Cursor uint64 `json:"cursor"`
				Resync bool   `json:"resync_required"`
				Events []struct {
					ID       string   `json:"id"`
					Progress *float64 `json:"progress"`
				} `json:"events"`
			}
			var fraction *float64
			if err == nil {
				events, eventErr := u.manager.PluginCall(c, desktop.PluginRequest{Operation: "capabilities.subscribe", Epoch: epoch, After: cursor})
				if eventErr == nil && json.Unmarshal(events, &page) == nil {
					cursor = page.Cursor
					for _, event := range page.Events {
						if event.ID == id && event.Progress != nil {
							v := *event.Progress
							fraction = &v
						}
					}
				}
			}
			done()
			pretty := string(raw)
			var data any
			if json.Unmarshal(raw, &data) == nil {
				encoded, _ := json.MarshalIndent(data, "", "  ")
				pretty = string(encoded)
			}
			fyne.Do(func() {
				if ctx.Err() != nil {
					return
				}
				if err != nil {
					view.SetText(err.Error())
					cancelButton.Disable()
				} else {
					view.SetText(pretty)
					if fraction != nil {
						progress.SetValue(*fraction)
					}
					if run.State != "running" {
						cancelButton.Disable()
						if run.State == "succeeded" {
							progress.SetValue(1)
						}
					}
				}
			})
			if err != nil || run.State != "running" {
				return
			}
			timer := time.NewTimer(time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()
}
