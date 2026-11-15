// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/mobiledevloperlab/nimotehost/internal/host"
	"time"
)

type inboxPanel struct {
	root    fyne.CanvasObject
	ui      *managerWindow
	loading bool
	filter  *widget.Select
	status  *widget.Label
	list    *widget.List
	page    host.InboxPage
	rows    []host.InboxItem
}

var inboxViews = []string{"approval", "failed", "review", "running", "handled", "archived"}

func newInboxPanel(u *managerWindow) *inboxPanel {
	p := &inboxPanel{ui: u}
	p.status = wrappedLabel("")
	labels := []string{}
	for _, v := range inboxViews {
		labels = append(labels, u.t("inbox."+v))
	}
	p.filter = widget.NewSelect(labels, nil)
	p.filter.SetSelectedIndex(0)
	p.list = widget.NewList(func() int { return len(p.rows) }, func() fyne.CanvasObject {
		title, detail := widget.NewLabel(""), widget.NewLabel("")
		title.Truncation = fyne.TextTruncateEllipsis
		detail.Truncation = fyne.TextTruncateEllipsis
		return container.NewVBox(title, detail)
	}, func(i widget.ListItemID, obj fyne.CanvasObject) {
		item := p.rows[i]
		box := obj.(*fyne.Container)
		box.Objects[0].(*widget.Label).SetText(sessionTitle(u.messages, item.Session) + " · " + agentName(item.Session.Agent) + " · " + u.t("inbox."+item.View))
		box.Objects[1].(*widget.Label).SetText(item.Session.Directory + " · " + item.Updated.Local().Format("01-02 15:04") + " · " + item.Summary)
	})
	p.filter.OnChanged = func(string) { p.render() }
	p.list.OnSelected = func(i widget.ListItemID) {
		if i >= 0 && i < len(p.rows) {
			p.open(p.rows[i])
			p.list.UnselectAll()
		}
	}
	p.root = container.NewBorder(container.NewVBox(p.filter, p.status), container.NewHBox(widget.NewButton(u.t("action.refresh"), p.refresh), widget.NewButton(u.t("conflicts.title"), func() { u.showConflicts() })), nil, nil, p.list)
	return p
}
func (p *inboxPanel) render() {
	v := "approval"
	if i := p.filter.SelectedIndex(); i >= 0 {
		v = inboxViews[i]
	}
	p.rows = nil
	for _, r := range p.page.Items {
		if v == "archived" && r.Archived || !r.Archived && r.View == v {
			p.rows = append(p.rows, r)
		}
	}
	text := ""
	for _, view := range inboxViews {
		text += fmt.Sprintf("%s %d   ", p.ui.t("inbox."+view), p.page.Counts[view])
	}
	p.status.SetText(text)
	p.list.Refresh()
}
func (p *inboxPanel) refresh() {
	if p.loading {
		return
	}
	p.loading = true
	go func() {
		ctx, cancel := context.WithTimeout(p.ui.ctx, 10*time.Second)
		defer cancel()
		raw, err := host.Call(ctx, p.ui.manager.Dir, host.RPCRequest{Method: "GET", Path: "/v1/inbox?view=all"})
		var page host.InboxPage
		if err == nil {
			err = json.Unmarshal(raw, &page)
		}
		fyne.Do(func() {
			p.loading = false
			if p.ui.ctx.Err() != nil {
				return
			}
			if err != nil {
				p.status.SetText(p.ui.t("inbox.unavailable"))
				return
			}
			p.page = page
			p.render()
		})
	}()
}
func (p *inboxPanel) open(item host.InboxItem) {
	u := p.ui
	s := item.Session
	body := container.NewVBox(wrappedLabel(item.Summary), wrappedLabel(s.Directory), widget.NewButton(u.t("inbox.session"), func() { u.showSession(s) }))
	if s.State == "running" && s.Interaction == "structured" {
		body.Add(widget.NewButton(u.t("approval.open"), func() { inspectAgent(u, s.ID) }))
	}
	body.Add(widget.NewButton(u.t("verification.title"), func() { u.showVerification(s.ID) }))
	for _, status := range []string{"handled", "archived", "open"} {
		status := status
		button := widget.NewButton(u.t("inbox.mark."+status), func() {
			go func() {
				ctx, cancel := context.WithTimeout(u.ctx, 10*time.Second)
				defer cancel()
				key, err := u.manager.MutationRequestID(ctx)
				if err == nil {
					data, _ := json.Marshal(map[string]string{"request_id": key, "revision": item.Revision, "status": status})
					_, err = host.Call(ctx, u.manager.Dir, host.RPCRequest{Method: "POST", Path: "/v1/inbox/" + s.ID, Body: data})
				}
				fyne.Do(func() {
					if err != nil {
						u.showFailure("inbox.updateError", err)
					}
					p.refresh()
				})
			}()
		})
		if item.View == "approval" && status != "open" {
			button.Disable()
		}
		body.Add(button)
	}
	d := dialog.NewCustom(sessionTitle(u.messages, s), u.t("action.close"), container.NewVScroll(body), u.window)
	d.Resize(u.readerSize())
	d.Show()
}
