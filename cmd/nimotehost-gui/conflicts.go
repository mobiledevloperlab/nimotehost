// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"encoding/json"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/mobiledevloperlab/nimotehost/internal/host"
	"strings"
	"time"
)

func (u *managerWindow) showConflicts() {
	go func() {
		ctx, cancel := context.WithTimeout(u.ctx, 10*time.Second)
		defer cancel()
		raw, err := host.Call(ctx, u.manager.Dir, host.RPCRequest{Method: "GET", Path: "/v1/conflicts"})
		var p struct {
			Warnings []host.ConflictWarning `json:"warnings"`
		}
		if err == nil {
			err = json.Unmarshal(raw, &p)
		}
		fyne.Do(func() {
			if u.ctx.Err() != nil {
				return
			}
			if err != nil {
				u.showFailure("conflicts.error", err)
				return
			}
			body := container.NewVBox(wrappedLabel(u.t("conflicts.intro")))
			if len(p.Warnings) == 0 {
				body.Add(wrappedLabel(u.t("conflicts.none")))
			}
			for _, w := range p.Warnings {
				body.Add(widget.NewLabelWithStyle(u.t("conflicts."+w.Kind), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}))
				body.Add(wrappedLabel(strings.Join(w.Agents, " / ") + "\n" + strings.Join(w.Directories, "\n") + "\n" + strings.Join(w.Files, "\n") + "\n" + w.Attribution))
				for _, id := range w.Sessions {
					id := id
					body.Add(widget.NewButton(u.t("inbox.session"), func() {
						for _, s := range u.sessions.source {
							if s.ID == id {
								u.showSession(s)
								break
							}
						}
					}))
				}
			}
			d := dialog.NewCustom(u.t("conflicts.title"), u.t("action.close"), container.NewVScroll(body), u.window)
			d.Resize(u.readerSize())
			d.Show()
		})
	}()
}
