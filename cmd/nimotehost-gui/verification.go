// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/mobiledevloperlab/nimotehost/internal/host"
	"strings"
	"time"
)

func (u *managerWindow) showVerification(id string) {
	go func() {
		ctx, cancel := context.WithTimeout(u.ctx, 10*time.Second)
		defer cancel()
		raw, err := host.Call(ctx, u.manager.Dir, host.RPCRequest{Method: "GET", Path: "/v1/sessions/" + id + "/verification"})
		var r host.VerificationReport
		if err == nil {
			err = json.Unmarshal(raw, &r)
		}
		fyne.Do(func() {
			if u.ctx.Err() != nil {
				return
			}
			if err != nil {
				u.showFailure("verification.error", err)
				return
			}
			status := r.Status
			if r.Stale {
				status += " · " + u.t("verification.stale")
			}
			absent := func(s string) string {
				if s == "" {
					return u.t("verification.unknown")
				}
				return s
			}
			pr := u.t("verification.unknown")
			if r.PRURL != nil {
				pr = *r.PRURL
			}
			body := container.NewVBox(widget.NewLabelWithStyle(status, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), wrappedLabel(u.t("verification.goal")+": "+absent(r.Goal)), wrappedLabel(u.t("verification.result")+": "+r.Result+" ("+r.ResultSource+")"), wrappedLabel(r.Workspace), wrappedLabel(u.t("verification.version")+": "+absent(r.Version.Branch)+" / "+absent(r.Version.Head)), wrappedLabel(fmt.Sprintf("%s: %t", u.t("verification.dirty"), r.Version.Dirty)), wrappedLabel("PR: "+pr), wrappedLabel(r.Version.DiffStat), wrappedLabel(strings.Join(r.Version.Files, "\n")))
			if r.AgentStatement != nil {
				body.Add(wrappedLabel(u.t("verification.agentStatement")))
				body.Add(wrappedLabel(r.AgentStatement.Text))
			}
			if len(r.Tests) == 0 {
				body.Add(wrappedLabel(u.t("verification.notRun")))
			}
			for _, t := range r.Tests {
				code := u.t("verification.unknown")
				if t.ExitCode != nil {
					code = fmt.Sprint(*t.ExitCode)
				}
				line := t.Command + " " + strings.Join(t.Args, " ") + " · " + t.Status + " · exit=" + code
				if t.Started != nil {
					line += " · " + t.Started.Local().Format(time.RFC3339)
				}
				if t.Finished != nil && t.Started != nil {
					line += " · " + t.Finished.Sub(*t.Started).String()
				}
				body.Add(wrappedLabel(line))
				if t.OutputPath != "" {
					path := t.OutputPath
					body.Add(widget.NewButton(u.t("verification.evidence"), func() { u.showEvidence(path, false) }))
				}
			}
			body.Add(wrappedLabel(strings.Join(r.Unknown, "\n")))
			body.Add(widget.NewButton(u.t("verification.diff"), func() { u.showEvidence(r.DiffPath, true) }))
			body.Add(widget.NewButton(u.t("verification.evidence"), func() { u.showEvidence(r.OutputPath, false) }))
			body.Add(widget.NewButton(u.t("action.refresh"), func() { u.showVerification(id) }))
			body.Add(widget.NewButton(u.t("verification.run"), func() { u.runVerification(id) }))
			d := dialog.NewCustom(u.t("verification.title"), u.t("action.close"), container.NewVScroll(body), u.window)
			d.Resize(u.readerSize())
			d.Show()
		})
	}()
}
func (u *managerWindow) showEvidence(path string, diff bool) {
	go func() {
		ctx, cancel := context.WithTimeout(u.ctx, 30*time.Second)
		defer cancel()
		var data []byte
		offset := 0
		digest := ""
		gap := false
		var err error
		for len(data) < 16<<20 {
			var raw []byte
			raw, err = host.Call(ctx, u.manager.Dir, host.RPCRequest{Method: "GET", Path: fmt.Sprintf("%s?offset=%d", path, offset)})
			if err != nil {
				break
			}
			var page struct {
				Diff   string `json:"diff"`
				Data   []byte `json:"data"`
				Start  int    `json:"start_offset"`
				Next   int    `json:"next_offset"`
				Total  int    `json:"total_bytes"`
				Digest string `json:"diff_sha"`
				Resync bool   `json:"resync_required"`
			}
			if err = json.Unmarshal(raw, &page); err != nil {
				break
			}
			if diff && page.Digest == "" {
				data = []byte(page.Diff)
				break
			}
			if diff && (page.Start != offset || page.Next < offset || page.Next > page.Total || page.Total > 4<<20 || digest != "" && digest != page.Digest) {
				err = errors.New("diff snapshot changed")
				break
			}
			if diff {
				digest = page.Digest
			}
			gap = gap || page.Resync
			data = append(data, page.Data...)
			if len(page.Data) == 0 || diff && page.Next == page.Total {
				break
			}
			if page.Next <= offset {
				err = errors.New("invalid evidence cursor")
				break
			}
			offset = page.Next
		}
		if diff && digest != "" {
			sum := sha256.Sum256(data)
			if hex.EncodeToString(sum[:]) != digest {
				err = errors.New("diff integrity check failed")
			}
		}
		text := string(data)
		if gap {
			text = u.t("verification.retained") + "\n" + text
		}
		fyne.Do(func() {
			if u.ctx.Err() != nil {
				return
			}
			if err != nil {
				u.showFailure("verification.error", err)
				return
			}
			u.showReader(u.t("verification.evidence"), text)
		})
	}()
}
func (u *managerWindow) runVerification(id string) {
	command := widget.NewEntry()
	args := widget.NewEntry()
	args.SetText("[]")
	d := dialog.NewForm(u.t("verification.run"), u.t("action.start"), u.t("action.cancel"), []*widget.FormItem{widget.NewFormItem(u.t("verification.command"), command), widget.NewFormItem(u.t("verification.args"), args)}, func(ok bool) {
		if !ok {
			return
		}
		var argv []string
		if err := json.Unmarshal([]byte(args.Text), &argv); err != nil {
			u.showFailure("verification.error", err)
			return
		}
		executable := command.Text
		go func() {
			ctx, cancel := context.WithTimeout(u.ctx, 10*time.Second)
			defer cancel()
			key, err := u.manager.MutationRequestID(ctx)
			if err == nil {
				body, _ := json.Marshal(map[string]any{"request_id": key, "tests": []host.TestCommand{{Command: executable, Args: argv}}})
				_, err = host.Call(ctx, u.manager.Dir, host.RPCRequest{Method: "POST", Path: "/v1/sessions/" + id + "/verification", Body: body})
			}
			fyne.Do(func() {
				if err != nil {
					u.showFailure("verification.error", err)
				} else {
					u.showVerification(id)
				}
			})
		}()
	}, u.window)
	d.Show()
}
