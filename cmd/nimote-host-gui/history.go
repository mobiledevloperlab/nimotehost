// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/mobiledevloperlab/nimote-host/internal/desktop"
	"github.com/mobiledevloperlab/nimote-host/internal/host"
	"net/url"
	"time"
)

type historyItem struct {
	ID        string `json:"conversation_id"`
	Title     string `json:"title"`
	Directory string `json:"directory"`
	Agent     string `json:"agent"`
	Resumable bool   `json:"resumable"`
}

func historyPanel(w fyne.Window, m *desktop.Manager) fyne.CanvasObject {
	provider := widget.NewSelect([]string{"codex", "claude", "opencode"}, nil)
	provider.SetSelected("codex")
	directory := widget.NewEntry()
	directory.SetPlaceHolder("Absolute workspace directory")
	selected := widget.NewSelect(nil, nil)
	output := widget.NewMultiLineEntry()
	output.Disable()
	state := widget.NewLabel("")
	var items []historyItem
	var cursor string
	var loadedProvider, loadedDirectory string
	busy := false
	var load func(bool)
	load = func(next bool) {
		if busy {
			return
		}
		busy = true
		agent, dir := provider.Selected, directory.Text
		c := ""
		if next {
			if agent != loadedProvider || dir != loadedDirectory || cursor == "" {
				busy = false
				return
			}
			c = cursor
		}
		state.SetText("Reading native history…")
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
			defer cancel()
			q := url.Values{"agent": {agent}, "directory": {dir}, "cursor": {c}}
			raw, err := host.Call(ctx, m.Dir, host.RPCRequest{Method: "GET", Path: "/v1/agent-history?" + q.Encode()})
			var page struct {
				Items []historyItem `json:"items"`
				Next  *string       `json:"next_cursor"`
			}
			if err == nil {
				err = json.Unmarshal(raw, &page)
			}
			fyne.Do(func() {
				busy = false
				if err != nil {
					state.SetText(err.Error())
					return
				}
				if !next {
					items = nil
				}
				items = append(items, page.Items...)
				loadedProvider, loadedDirectory = agent, dir
				cursor = ""
				if page.Next != nil {
					cursor = *page.Next
				}
				ids := []string{}
				for _, i := range items {
					ids = append(ids, i.ID)
				}
				selected.SetOptions(ids)
				state.SetText("History loaded. Select a conversation; Next page loads more.")
			})
		}()
	}
	find := func() (historyItem, error) {
		for _, i := range items {
			if i.ID == selected.Selected {
				return i, nil
			}
		}
		return historyItem{}, errors.New("select a loaded conversation")
	}
	selected.OnChanged = func(id string) {
		i, err := find()
		if err != nil {
			return
		}
		b, _ := json.MarshalIndent(i, "", "  ")
		output.SetText(string(b))
	}
	read := widget.NewButton("Read transcript", func() {
		i, err := find()
		if err != nil {
			dialog.ShowError(err, w)
			return
		}
		agent, dir := loadedProvider, i.Directory
		if dir == "" {
			dir = loadedDirectory
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
			defer cancel()
			q := url.Values{"agent": {agent}, "directory": {dir}}
			raw, err := host.Call(ctx, m.Dir, host.RPCRequest{Method: "GET", Path: "/v1/agent-history/" + url.PathEscape(i.ID) + "?" + q.Encode()})
			fyne.Do(func() {
				if err != nil {
					dialog.ShowError(err, w)
					return
				}
				var data any
				if err = json.Unmarshal(raw, &data); err != nil {
					dialog.ShowError(err, w)
					return
				}
				pretty, _ := json.MarshalIndent(data, "", "  ")
				output.SetText(string(pretty))
			})
		}()
	})
	resume := widget.NewButton("Resume conversation…", func() {
		i, err := find()
		if err != nil {
			dialog.ShowError(err, w)
			return
		}
		if !i.Resumable {
			dialog.ShowError(errors.New("provider does not mark this conversation resumable"), w)
			return
		}
		agent, dir := loadedProvider, i.Directory
		if dir == "" {
			dir = loadedDirectory
		}
		dialog.ShowConfirm("Resume native conversation?", "This starts a new structured Agent process for the selected existing conversation. It does not replay prompts or fall back to a new conversation.", func(ok bool) {
			if !ok {
				return
			}
			var key [24]byte
			if _, err := rand.Read(key[:]); err != nil {
				dialog.ShowError(err, w)
				return
			}
			body, _ := json.Marshal(host.StartRequest{Kind: "agent", Agent: agent, Interaction: "structured", Directory: dir, Resume: &host.AgentResume{ConversationID: i.ID}, RequestID: hex.EncodeToString(key[:])})
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 70*time.Second)
				defer cancel()
				_, err := host.Call(ctx, m.Dir, host.RPCRequest{Method: "POST", Path: "/v1/sessions", Body: body})
				fyne.Do(func() {
					if err != nil {
						dialog.ShowError(err, w)
					} else {
						state.SetText("Resume request accepted. Inspect the new session in Sessions for provider readiness or failure.")
					}
				})
			}()
		}, w)
	})
	return container.NewBorder(container.NewVBox(widget.NewForm(widget.NewFormItem("Agent", provider), widget.NewFormItem("Workspace", directory)), container.NewHBox(widget.NewButton("Load history", func() { load(false) }), widget.NewButton("Next page", func() { load(true) })), selected, container.NewHBox(read, resume), state), nil, nil, nil, output)
}
