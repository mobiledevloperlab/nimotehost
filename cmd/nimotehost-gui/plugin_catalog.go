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
	sdk "github.com/mobiledevloperlab/nimotehost/pkg/pluginsdk"
)

func (p *pluginsPanel) browseCatalog() {
	u := p.ui
	ctx, cancel := context.WithCancel(u.ctx)
	query := widget.NewEntry()
	query.SetPlaceHolder(u.t("catalog.query"))
	cards := column(12, wrappedLabel(u.t("action.loading")))
	search := widget.NewButtonWithIcon(u.t("catalog.search"), theme.SearchIcon(), nil)
	search.OnTapped = func() {
		if search.Disabled() {
			return
		}
		search.Disable()
		value := strings.TrimSpace(query.Text)
		go func() {
			c, done := context.WithTimeout(ctx, 8*time.Second)
			defer done()
			raw, err := u.manager.PluginCall(c, desktop.PluginRequest{Operation: "plugins.catalog.search", Query: value})
			var results sdk.CatalogSearch
			if err == nil {
				err = json.Unmarshal(raw, &results)
			}
			fyne.Do(func() {
				if ctx.Err() != nil {
					return
				}
				search.Enable()
				cards.Objects = nil
				if err != nil {
					cards.Add(wrappedLabel(u.t("plugins.unavailable")))
					cards.Add(readOnlyText(err.Error()))
					return
				}
				if results.Truncated {
					cards.Add(wrappedLabel(u.t("catalog.truncated")))
				}
				if len(results.Plugins) == 0 {
					cards.Add(wrappedLabel(u.t("catalog.empty")))
				}
				for _, failure := range results.Failures {
					cards.Add(wrappedLabel(u.t("catalog.sourceError", map[string]any{"Source": failure.Source})))
				}
				for _, match := range results.Plugins {
					entry := match
					review := widget.NewButton(u.t("plugins.review"), func() { p.reviewRemote(ctx, entry) })
					if !entry.Compatible {
						review.Disable()
					}
					description := entry.Description
					if !entry.Compatible {
						description += "\n" + u.t("catalog.incompatible")
					}
					cards.Add(widget.NewCard(entry.Name+" · "+entry.Version, entry.Repository+" · "+entry.Source, column(8, wrappedLabel(description), review)))
				}
				cards.Refresh()
			})
		}()
	}
	query.OnSubmitted = func(string) { search.OnTapped() }
	header := column(8, wrappedLabel(u.t("catalog.intro")), container.NewBorder(nil, nil, nil, search, query), container.NewHBox(widget.NewButton(u.t("catalog.sources"), p.catalogSources), widget.NewButton(u.t("catalog.downloads"), p.installJobs)))
	d := dialog.NewCustom(u.t("catalog.title"), u.t("action.close"), container.NewBorder(header, nil, nil, nil, container.NewVScroll(cards)), u.window)
	d.SetOnClosed(cancel)
	d.Resize(u.readerSize())
	d.Show()
	search.OnTapped()
}
func (p *pluginsPanel) reviewRemote(ctx context.Context, entry sdk.CatalogMatch) {
	u := p.ui
	go func() {
		c, cancel := context.WithTimeout(ctx, 8*time.Second)
		defer cancel()
		raw, err := u.manager.PluginCall(c, desktop.PluginRequest{Operation: "plugins.catalog.get", Source: entry.Source, ID: entry.ID})
		var offer sdk.CatalogOffer
		if err == nil {
			err = json.Unmarshal(raw, &offer)
		}
		var installed []desktop.PluginInfo
		if err == nil {
			raw, err = u.manager.PluginCall(c, desktop.PluginRequest{Operation: "plugins.list"})
			if err == nil {
				err = json.Unmarshal(raw, &installed)
			}
		}
		fyne.Do(func() {
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				u.showFailure("plugins.error", err)
				return
			}
			operation := "plugins.install_remote"
			title := u.t("catalog.downloadInstall")
			for _, plugin := range installed {
				if plugin.ID == offer.Manifest.ID {
					if plugin.State != "disabled" {
						u.showFailure("plugins.error", fmt.Errorf("%s", u.t("catalog.disableFirst")))
						return
					}
					operation = "plugins.update_remote"
					title = u.t("catalog.downloadUpdate")
				}
			}
			pretty, _ := json.MarshalIndent(offer, "", "  ")
			body := column(8, wrappedLabel(u.t("catalog.reviewWarning")), wrappedLabel(u.t("plugins.boundary")), readOnlyText(string(pretty)))
			d := dialog.NewCustomConfirm(title, title, u.t("action.cancel"), container.NewVScroll(body), func(ok bool) {
				if !ok {
					return
				}
				p.startRemoteInstall(desktop.PluginRequest{Operation: operation, Source: offer.Entry.Source, ID: offer.Entry.ID, SHA256: offer.SHA256, Confirmed: true})
			}, u.window)
			d.Resize(u.readerSize())
			d.Show()
		})
	}()
}
func (p *pluginsPanel) startRemoteInstall(req desktop.PluginRequest) {
	u := p.ui
	ctx := u.ctx
	go func() {
		c, cancel := context.WithTimeout(ctx, 8*time.Second)
		defer cancel()
		raw, err := u.manager.PluginCall(c, req)
		var job sdk.InstallStatus
		if err == nil {
			err = json.Unmarshal(raw, &job)
		}
		fyne.Do(func() {
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				u.showFailure("plugins.uncertain", err)
				return
			}
			p.watchInstall(job.ID)
		})
	}()
}
func (p *pluginsPanel) watchInstall(id string) {
	u := p.ui
	ctx, cancel := context.WithCancel(u.ctx)
	status := wrappedLabel(u.t("action.loading"))
	progress := widget.NewProgressBar()
	stop := widget.NewButton(u.t("catalog.cancelDownload"), func() { p.mutate(desktop.PluginRequest{Operation: "plugins.install.cancel", ID: id}) })
	d := dialog.NewCustom(u.t("catalog.downloads"), u.t("action.close"), column(8, readOnlyText(id), progress, stop, container.NewVScroll(status)), u.window)
	d.SetOnClosed(cancel)
	d.Resize(u.readerSize())
	d.Show()
	go func() {
		for ctx.Err() == nil {
			c, done := context.WithTimeout(ctx, 6*time.Second)
			raw, err := u.manager.PluginCall(c, desktop.PluginRequest{Operation: "plugins.install.status", ID: id})
			done()
			var job sdk.InstallStatus
			if err == nil {
				err = json.Unmarshal(raw, &job)
			}
			terminal := job.State == "succeeded" || job.State == "failed" || job.State == "canceled"
			fyne.Do(func() {
				if ctx.Err() != nil {
					return
				}
				if err != nil {
					status.SetText(err.Error())
					stop.Disable()
					return
				}
				progress.SetValue(job.Progress)
				status.SetText(job.Plugin + " · " + job.State + fmt.Sprintf("\n%d bytes", job.Bytes))
				if job.Error != nil {
					status.SetText(status.Text + "\n" + job.Error.Message)
				}
				if job.State == "installing" || terminal {
					stop.Disable()
				}
				if job.State == "succeeded" {
					status.SetText(status.Text + "\n" + u.t("catalog.installedDisabled"))
					p.refresh()
				}
			})
			if err != nil || terminal {
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
func (p *pluginsPanel) installJobs() {
	u := p.ui
	ctx := u.ctx
	go func() {
		c, cancel := context.WithTimeout(ctx, 6*time.Second)
		defer cancel()
		raw, err := u.manager.PluginCall(c, desktop.PluginRequest{Operation: "plugins.install.jobs"})
		var jobs []sdk.InstallStatus
		if err == nil {
			err = json.Unmarshal(raw, &jobs)
		}
		fyne.Do(func() {
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				u.showFailure("plugins.error", err)
				return
			}
			cards := column(8)
			if len(jobs) == 0 {
				cards.Add(wrappedLabel(u.t("catalog.noDownloads")))
			}
			for _, j := range jobs {
				job := j
				cards.Add(widget.NewButton(job.Plugin+" · "+job.State, func() { p.watchInstall(job.ID) }))
			}
			d := dialog.NewCustom(u.t("catalog.downloads"), u.t("action.close"), container.NewVScroll(cards), u.window)
			d.Resize(u.readerSize())
			d.Show()
		})
	}()
}
func (p *pluginsPanel) catalogSources() {
	u := p.ui
	ctx, cancel := context.WithCancel(u.ctx)
	cards := column(8)
	var refresh func()
	refresh = func() {
		go func() {
			c, done := context.WithTimeout(ctx, 6*time.Second)
			defer done()
			raw, err := u.manager.PluginCall(c, desktop.PluginRequest{Operation: "plugins.catalog.sources"})
			var sources []sdk.CatalogSource
			if err == nil {
				err = json.Unmarshal(raw, &sources)
			}
			fyne.Do(func() {
				if ctx.Err() != nil {
					return
				}
				cards.Objects = nil
				if err != nil {
					cards.Add(wrappedLabel(err.Error()))
					return
				}
				if len(sources) == 0 {
					cards.Add(wrappedLabel(u.t("catalog.noSources")))
				}
				for _, s := range sources {
					source := s
					remove := widget.NewButton(u.t("catalog.removeSource"), func() {
						confirmDialog(u.window, u.t("catalog.removeSource"), source.ID+"\n"+source.URL, u.t("catalog.removeSource"), u.t("action.cancel"), func() {
							p.catalogSourceAction(ctx, desktop.PluginRequest{Operation: "plugins.catalog.remove", ID: source.ID, Confirmed: true}, refresh)
						})
					})
					cards.Add(surface(column(8, wrappedLabel(source.ID), readOnlyText(source.URL), remove)))
				}
				cards.Refresh()
			})
		}()
	}
	add := widget.NewButton(u.t("catalog.addSource"), func() {
		id := widget.NewEntry()
		location := widget.NewEntry()
		location.SetPlaceHolder("https://raw.githubusercontent.com/owner/catalog/main/index.json")
		form := dialog.NewForm(u.t("catalog.addSource"), u.t("plugins.review"), u.t("action.cancel"), []*widget.FormItem{widget.NewFormItem(u.t("catalog.sourceID"), id), widget.NewFormItem("URL", location)}, func(ok bool) {
			if !ok {
				return
			}
			req := desktop.PluginRequest{Operation: "plugins.catalog.add", ID: strings.TrimSpace(id.Text), Source: strings.TrimSpace(location.Text), Confirmed: true}
			confirmDialog(u.window, u.t("catalog.addSource"), u.t("catalog.sourceWarning")+"\n"+req.ID+"\n"+req.Source, u.t("catalog.addSource"), u.t("action.cancel"), func() { p.catalogSourceAction(ctx, req, refresh) })
		}, u.window)
		form.Resize(u.readerSize())
		form.Show()
	})
	d := dialog.NewCustom(u.t("catalog.sources"), u.t("action.close"), container.NewBorder(column(8, wrappedLabel(u.t("catalog.sourceWarning")), add), nil, nil, nil, container.NewVScroll(cards)), u.window)
	d.SetOnClosed(cancel)
	d.Resize(u.readerSize())
	d.Show()
	refresh()
}
func (p *pluginsPanel) catalogSourceAction(ctx context.Context, req desktop.PluginRequest, refresh func()) {
	go func() {
		c, cancel := context.WithTimeout(ctx, 6*time.Second)
		defer cancel()
		_, err := p.ui.manager.PluginCall(c, req)
		fyne.Do(func() {
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				p.ui.showFailure("plugins.error", err)
			} else {
				refresh()
			}
		})
	}()
}
