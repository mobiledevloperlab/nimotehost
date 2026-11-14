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
	"fyne.io/fyne/v2/widget"
)

type approvalDetails struct {
	raw       json.RawMessage
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Command   string `json:"command"`
	Cwd       string `json:"cwd"`
	GrantRoot string `json:"grant_root"`
	Reason    string `json:"reason"`
	Changes   []struct {
		Path string `json:"path"`
		Diff string `json:"diff"`
	} `json:"changes"`
}

func inspectAgent(u *managerWindow, id string) {
	if id == "" {
		return
	}
	ctx := u.ctx
	go func() {
		c, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		raw, err := u.manager.AgentState(c, id)
		fyne.Do(func() {
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				u.showFailure("approval.loadError", err)
				return
			}
			var snapshot struct {
				Status    string            `json:"status"`
				Approvals []json.RawMessage `json:"approvals"`
			}
			if err := json.Unmarshal(raw, &snapshot); err != nil {
				u.showFailure("approval.loadError", err)
				return
			}
			content := container.NewVBox(wrappedLabel(u.t("approval.intro")))
			if len(snapshot.Approvals) == 0 {
				content.Add(wrappedLabel(u.t("approval.empty")))
			}
			for _, rawApproval := range snapshot.Approvals {
				var approval approvalDetails
				if json.Unmarshal(rawApproval, &approval) != nil || approval.ID == "" {
					continue
				}
				approval.raw = rawApproval
				body := u.approvalContent(approval)
				approve := widget.NewButton(u.t("approval.allow"), nil)
				decline := widget.NewButton(u.t("approval.decline"), nil)
				decide := func(decision string) {
					title := u.t("approval.allow")
					if decision == "decline" {
						title = u.t("approval.decline")
					}
					confirmation := container.NewVBox(wrappedLabel(u.t("approval.confirm")), u.approvalContent(approval))
					d := dialog.NewCustomConfirm(title, title, u.t("action.cancel"), container.NewVScroll(confirmation), func(ok bool) {
						if !ok {
							return
						}
						approve.Disable()
						decline.Disable()
						go func() {
							c, cancel := context.WithTimeout(ctx, 10*time.Second)
							defer cancel()
							requestID, err := u.manager.MutationRequestID(c)
							if err == nil {
								err = u.manager.Decide(c, id, approval.ID, decision, requestID)
							}
							fyne.Do(func() {
								if ctx.Err() != nil {
									return
								}
								if err != nil {
									u.showFailure("approval.uncertain", fmt.Errorf("request %s: %w", requestID, err))
								} else {
									u.notice(u.t("approval.sent"))
									u.requestRefresh()
								}
							})
						}()
					}, u.window)
					d.Resize(fyne.NewSize(700, 480))
					d.Show()
				}
				approve.OnTapped = func() { decide("accept") }
				decline.OnTapped = func() { decide("decline") }
				body.Add(container.NewHBox(approve, decline))
				content.Add(widget.NewCard(u.t("approval.request"), "", body))
			}
			pretty := json.RawMessage(raw)
			encoded, _ := json.MarshalIndent(pretty, "", "  ")
			content.Add(widget.NewAccordion(widget.NewAccordionItem(u.t("details.technical"), readOnlyText(string(encoded)))))
			d := dialog.NewCustom(u.t("approval.title"), u.t("action.close"), container.NewVScroll(content), u.window)
			d.Resize(fyne.NewSize(760, 560))
			d.Show()
		})
	}()
}

func (u *managerWindow) approvalContent(a approvalDetails) *fyne.Container {
	body := container.NewVBox()
	if a.Command != "" {
		body.Add(wrappedLabel(u.t("approval.command")))
		body.Add(readOnlyText(a.Command))
	}
	if a.Cwd != "" {
		body.Add(wrappedLabel(u.t("approval.workspace", map[string]any{"Path": a.Cwd})))
	}
	if a.GrantRoot != "" {
		body.Add(wrappedLabel(u.t("approval.root", map[string]any{"Path": a.GrantRoot})))
	}
	if strings.TrimSpace(a.Reason) != "" {
		body.Add(wrappedLabel(a.Reason))
	}
	for _, change := range a.Changes {
		body.Add(widget.NewLabelWithStyle(change.Path, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}))
		body.Add(readOnlyText(change.Diff))
	}
	// Unknown or newly added request fields remain inspectable before deciding.
	if a.Command == "" && len(a.Changes) == 0 {
		body.Add(wrappedLabel(u.t("approval.inspectHint")))
	}
	pretty, _ := json.MarshalIndent(a.raw, "", "  ")
	body.Add(widget.NewAccordion(widget.NewAccordionItem(u.t("details.technical"), readOnlyText(string(pretty)))))
	return body
}
