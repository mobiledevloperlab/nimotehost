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
	"github.com/mobiledevloperlab/nimote-host/internal/desktop"
	"time"
)

func inspectAgent(w fyne.Window, m *desktop.Manager, id string) {
	if id == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		raw, err := m.AgentState(ctx, id)
		fyne.Do(func() {
			if err != nil {
				dialog.ShowError(err, w)
				return
			}
			var snapshot struct {
				Status    string            `json:"status"`
				Approvals []json.RawMessage `json:"approvals"`
			}
			if err = json.Unmarshal(raw, &snapshot); err != nil {
				dialog.ShowError(err, w)
				return
			}
			var data any
			_ = json.Unmarshal(raw, &data)
			pretty, _ := json.MarshalIndent(data, "", "  ")
			content := container.NewVBox(widget.NewLabel("Session "+id), widget.NewLabel("Observed "+time.Now().Format(time.RFC3339)+" · "+snapshot.Status))
			for _, approval := range snapshot.Approvals {
				var detail struct {
					ID      string `json:"id"`
					Command string `json:"command"`
					Kind    string `json:"kind"`
				}
				if json.Unmarshal(approval, &detail) != nil || detail.ID == "" {
					continue
				}
				content.Add(widget.NewLabel(fmt.Sprintf("Pending %s · %s", detail.Kind, detail.Command)))
				choose := func(decision string) {
					text := widget.NewMultiLineEntry()
					text.SetText(string(approval))
					text.Disable()
					d := dialog.NewCustomConfirm("Confirm "+decision, "Send response", "Cancel", container.NewVScroll(text), func(ok bool) {
						if !ok {
							return
						}
						go func() {
							ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
							defer cancel()
							requestID, err := m.MutationRequestID(ctx)
							if err == nil {
								err = m.Decide(ctx, id, detail.ID, decision, requestID)
							}
							fyne.Do(func() {
								if err != nil {
									dialog.ShowError(fmt.Errorf("response %s: %w; inspect current state before any further response", requestID, err), w)
								} else {
									dialog.ShowInformation("Response queued", "Host accepted the response. Refresh Agent state to observe the provider outcome.", w)
								}
							})
						}()
					}, w)
					d.Resize(fyne.NewSize(650, 350))
					d.Show()
				}
				content.Add(container.NewHBox(widget.NewButton("Approve once…", func() { choose("accept") }), widget.NewButton("Decline…", func() { choose("decline") })))
			}
			text := widget.NewMultiLineEntry()
			text.SetText(string(pretty))
			text.Disable()
			d := dialog.NewCustom("Agent state and approvals", "Close", container.NewBorder(content, nil, nil, nil, text), w)
			d.Resize(fyne.NewSize(720, 460))
			d.Show()
		})
	}()
}
