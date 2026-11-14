// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"encoding/json"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/mobiledevloperlab/nimote-host/internal/host"
)

type historyItem struct {
	ID        string `json:"conversation_id"`
	Title     string `json:"title"`
	Directory string `json:"directory"`
	Resumable bool   `json:"resumable"`
}
type historyMessage struct {
	Role      string         `json:"role"`
	Kind      string         `json:"kind"`
	Text      string         `json:"text"`
	Tool      map[string]any `json:"tool"`
	Truncated bool           `json:"truncated"`
}
type transcriptPage struct {
	Items     []historyMessage `json:"items"`
	Next      *string          `json:"next_cursor"`
	Truncated bool             `json:"truncated"`
}

func historyPanel(u *managerWindow) fyne.CanvasObject {
	ctx := u.ctx
	provider := widget.NewSelect([]string{"Codex", "Claude Code", "OpenCode"}, nil)
	provider.SetSelectedIndex(0)
	fitSelect(provider)
	agents := []string{"codex", "claude", "opencode"}
	directory := widget.NewEntry()
	directory.SetPlaceHolder(u.t("history.workspace"))
	browse := widget.NewButtonWithIcon("", theme.FolderOpenIcon(), func() {
		d := dialog.NewFolderOpen(func(uri fyne.ListableURI, err error) {
			if err != nil {
				u.showFailure("history.loadError", err)
				return
			}
			if uri != nil {
				directory.SetText(uri.Path())
			}
		}, u.window)
		d.SetConfirmText(u.t("action.choose"))
		d.SetDismissText(u.t("action.cancel"))
		d.Show()
	})
	status := wrappedLabel(u.t("history.empty"))
	title := widget.NewLabelWithStyle(u.t("history.select"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	title.Truncation = fyne.TextTruncateEllipsis
	transcript := widget.NewRichTextWithText(u.t("history.selectHint"))
	transcript.Wrapping = fyne.TextWrapWord
	var items []historyItem
	var selected *historyItem
	var listCursor, loadedAgent, loadedDirectory, readCursor string
	var messages []historyMessage
	var generation uint64
	var cancelRead context.CancelFunc
	loading := false
	resume := widget.NewButton(u.t("history.resume"), nil)
	resume.Disable()
	moreText := widget.NewButton(u.t("history.moreText"), nil)
	moreText.Hide()
	details := widget.NewButton(u.t("details.technical"), nil)
	details.Disable()
	var read func(bool)
	list := widget.NewList(func() int { return len(items) }, func() fyne.CanvasObject { return container.NewVBox(widget.NewLabel(""), widget.NewLabel("")) }, func(i widget.ListItemID, obj fyne.CanvasObject) {
		row := obj.(*fyne.Container)
		item := items[i]
		name := strings.TrimSpace(item.Title)
		if name == "" {
			name = u.t("history.untitled")
		}
		first := row.Objects[0].(*widget.Label)
		first.TextStyle = fyne.TextStyle{Bold: true}
		first.Truncation = fyne.TextTruncateEllipsis
		first.SetText(name)
		second := row.Objects[1].(*widget.Label)
		second.Truncation = fyne.TextTruncateEllipsis
		second.SetText(item.Directory)
	})
	read = func(next bool) {
		if selected == nil {
			return
		}
		item := *selected
		agent, dir := loadedAgent, item.Directory
		if dir == "" {
			dir = loadedDirectory
		}
		if agent != "codex" && !filepath.IsAbs(dir) {
			status.SetText(u.t("history.workspaceRequired"))
			u.window.Canvas().Focus(directory)
			return
		}
		if cancelRead != nil {
			cancelRead()
		}
		c, cancel := context.WithTimeout(ctx, 35*time.Second)
		cancelRead = cancel
		generation++
		version := generation
		cursor := ""
		if next {
			cursor = readCursor
		}
		moreText.Disable()
		status.SetText(u.t("action.loading"))
		go func() {
			defer cancel()
			q := url.Values{"agent": {agent}, "directory": {dir}, "cursor": {cursor}}
			raw, err := host.Call(c, u.manager.Dir, host.RPCRequest{Method: "GET", Path: "/v1/agent-history/" + url.PathEscape(item.ID) + "?" + q.Encode()})
			var page transcriptPage
			if err == nil {
				err = json.Unmarshal(raw, &page)
			}
			fyne.Do(func() {
				if ctx.Err() != nil || generation != version {
					return
				}
				moreText.Enable()
				if err != nil {
					status.SetText(u.t("history.loadError"))
					return
				}
				if !next {
					messages = nil
				}
				messages = append(messages, page.Items...)
				readCursor = ""
				if page.Next != nil {
					readCursor = *page.Next
				}
				transcript.Segments = transcriptSegments(u.messages, messages)
				transcript.Refresh()
				details.Enable()
				if readCursor != "" {
					moreText.Show()
				} else {
					moreText.Hide()
				}
				if page.Truncated {
					status.SetText(u.t("history.truncated"))
				} else {
					status.SetText(u.t("history.loaded"))
				}
			})
		}()
	}
	list.OnSelected = func(i widget.ListItemID) {
		if i < 0 || i >= len(items) {
			return
		}
		item := items[i]
		selected = &item
		readCursor = ""
		messages = nil
		title.SetText(item.Title)
		if strings.TrimSpace(item.Title) == "" {
			title.SetText(u.t("history.untitled"))
		}
		resume.Disable()
		if item.Resumable {
			resume.Enable()
		}
		details.Disable()
		moreText.Hide()
		read(false)
	}
	moreText.OnTapped = func() {
		if readCursor != "" {
			read(true)
		}
	}
	details.OnTapped = func() {
		raw, _ := json.MarshalIndent(messages, "", "  ")
		d := dialog.NewCustom(u.t("details.technical"), u.t("action.close"), readOnlyText(string(raw)), u.window)
		d.Resize(fyne.NewSize(700, 460))
		d.Show()
	}
	load := widget.NewButtonWithIcon(u.t("history.load"), theme.ViewRefreshIcon(), nil)
	load.Importance = widget.HighImportance
	more := widget.NewButton(u.t("history.more"), nil)
	more.Hide()
	loadPage := func(next bool) {
		if loading {
			return
		}
		loading = true
		agent, dir := agents[provider.SelectedIndex()], directory.Text
		cursor := ""
		if next {
			if agent != loadedAgent || dir != loadedDirectory || listCursor == "" {
				loading = false
				return
			}
			cursor = listCursor
		}
		load.Disable()
		more.Disable()
		provider.Disable()
		directory.Disable()
		browse.Disable()
		status.SetText(u.t("action.loading"))
		go func() {
			c, cancel := context.WithTimeout(ctx, 35*time.Second)
			defer cancel()
			q := url.Values{"agent": {agent}, "directory": {dir}, "cursor": {cursor}}
			raw, err := host.Call(c, u.manager.Dir, host.RPCRequest{Method: "GET", Path: "/v1/agent-history?" + q.Encode()})
			var page struct {
				Items []historyItem `json:"items"`
				Next  *string       `json:"next_cursor"`
			}
			if err == nil {
				err = json.Unmarshal(raw, &page)
			}
			fyne.Do(func() {
				if ctx.Err() != nil {
					return
				}
				loading = false
				load.Enable()
				more.Enable()
				provider.Enable()
				directory.Enable()
				browse.Enable()
				if err != nil {
					status.SetText(u.t("history.loadError"))
					return
				}
				if !next {
					items = nil
					selected = nil
					generation++
					if cancelRead != nil {
						cancelRead()
					}
					messages = nil
					resume.Disable()
					details.Disable()
					moreText.Hide()
					title.SetText(u.t("history.select"))
					transcript.Segments = []widget.RichTextSegment{&widget.TextSegment{Text: u.t("history.selectHint")}}
					transcript.Refresh()
					list.UnselectAll()
				}
				items = append(items, page.Items...)
				loadedAgent, loadedDirectory = agent, dir
				listCursor = ""
				if page.Next != nil {
					listCursor = *page.Next
				}
				if listCursor != "" {
					more.Show()
				} else {
					more.Hide()
				}
				list.Refresh()
				if len(items) == 0 {
					status.SetText(u.t("history.none"))
				} else {
					status.SetText(u.t("history.selectHint"))
				}
			})
		}()
	}
	load.OnTapped = func() { loadPage(false) }
	more.OnTapped = func() { loadPage(true) }
	resume.OnTapped = func() {
		if selected == nil || !selected.Resumable {
			return
		}
		item := *selected
		agent, dir := loadedAgent, item.Directory
		if dir == "" {
			dir = loadedDirectory
		}
		if !filepath.IsAbs(dir) {
			status.SetText(u.t("history.workspaceRequired"))
			return
		}
		confirmDialog(u.window, u.t("history.resume"), u.t("history.resumeConfirm"), u.t("history.resume"), u.t("action.cancel"), func() {
			resume.Disable()
			go func() {
				c, cancel := context.WithTimeout(ctx, 70*time.Second)
				defer cancel()
				key, err := u.manager.MutationRequestID(c)
				if err == nil {
					body, _ := json.Marshal(host.StartRequest{Kind: "agent", Agent: agent, Interaction: "structured", Directory: dir, Resume: &host.AgentResume{ConversationID: item.ID}, RequestID: key})
					_, err = host.Call(c, u.manager.Dir, host.RPCRequest{Method: "POST", Path: "/v1/sessions", Body: body})
				}
				fyne.Do(func() {
					if ctx.Err() != nil {
						return
					}
					if err != nil {
						u.showFailure("history.resumeError", err)
					} else {
						u.notice(u.t("history.resuming"))
						u.sessions.tabs.SelectIndex(0)
						u.requestRefresh()
					}
				})
			}()
		})
	}
	left := container.NewBorder(nil, more, nil, nil, list)
	right := container.NewBorder(title, container.NewHBox(moreText, details), nil, nil, container.NewVScroll(transcript))
	split := container.NewHSplit(left, right)
	split.Offset = .35
	search := container.NewBorder(nil, nil, provider, container.NewHBox(browse, load), directory)
	return container.NewBorder(container.NewVBox(search, status, container.NewHBox(resume)), nil, nil, nil, split)
}

func transcriptSegments(m *messages, items []historyMessage) []widget.RichTextSegment {
	segments := make([]widget.RichTextSegment, 0, len(items)*2)
	for _, item := range items {
		role := "history.tool"
		if item.Role == "user" {
			role = "history.you"
		} else if item.Role == "assistant" {
			role = "history.assistant"
		}
		segments = append(segments, &widget.TextSegment{Text: m.t(role), Style: widget.RichTextStyle{TextStyle: fyne.TextStyle{Bold: true}}})
		text := historyDisplayText(item)
		if item.Truncated {
			text += "\n" + m.t("history.itemTruncated")
		}
		segments = append(segments, &widget.TextSegment{Text: text, Style: widget.RichTextStyle{TextStyle: fyne.TextStyle{Monospace: item.Role == "tool"}}})
	}
	return segments
}

func historyDisplayText(item historyMessage) string {
	parts := []string{}
	seen := map[string]bool{}
	appendText := func(text string) {
		if strings.TrimSpace(text) != "" && !seen[text] {
			parts = append(parts, text)
			seen[text] = true
		}
	}
	appendText(item.Text)
	if item.Role == "tool" {
		for _, key := range []string{"command", "name", "tool", "output", "aggregatedOutput", "result", "error"} {
			value := item.Tool[key]
			if text, ok := value.(string); ok {
				appendText(text)
			} else if value != nil {
				encoded, _ := json.MarshalIndent(value, "", "  ")
				appendText(string(encoded))
			}
		}
		if changes, ok := item.Tool["changes"].([]any); ok {
			for _, value := range changes {
				if change, ok := value.(map[string]any); ok {
					path, _ := change["path"].(string)
					diff, _ := change["diff"].(string)
					appendText(path + "\n" + diff)
				}
			}
		}
	}
	return strings.Join(parts, "\n")
}
