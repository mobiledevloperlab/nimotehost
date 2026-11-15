// SPDX-License-Identifier: Apache-2.0
package main

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Bound text layout work independently of the number of loaded history pages.
// All text remains available through page controls and the reader's Copy action.
const (
	textPageRunes = 8192
	textPageLines = 128
)

type textPager struct {
	label          *widget.Label
	root           *fyne.Container
	view           *widget.RichText
	scroll         *container.Scroll
	controls       *fyne.Container
	previous, next *widget.Button
	position       *widget.Label
	pages          [][]widget.RichTextSegment
	index          int
}

func newTextPager() *textPager {
	p := &textPager{view: widget.NewRichText()}
	p.view.Wrapping = fyne.TextWrapWord
	p.scroll = container.NewVScroll(inset(p.view, 8))
	p.previous = widget.NewButtonWithIcon("", theme.NavigateBackIcon(), func() { p.index--; p.show() })
	p.next = widget.NewButtonWithIcon("", theme.NavigateNextIcon(), func() { p.index++; p.show() })
	p.position = widget.NewLabel("")
	p.controls = container.NewHBox(p.previous, p.position, p.next)
	p.root = container.NewBorder(nil, p.controls, nil, nil, p.scroll)
	p.setPages(nil, true)
	return p
}

func newPlainTextPager(text string) *textPager {
	p := newTextPager()
	p.label = readOnlyText("")
	p.scroll.Content = inset(p.label, 8)
	p.scroll.Refresh()
	p.setPages(textPages([]widget.RichTextSegment{&widget.TextSegment{Text: text}}), true)
	return p
}

func (p *textPager) setPages(pages [][]widget.RichTextSegment, reset bool) {
	p.pages = pages
	if reset {
		p.index = 0
	}
	p.show()
}

func (p *textPager) show() {
	p.index = max(0, min(p.index, len(p.pages)-1))
	p.view.Segments = nil
	if len(p.pages) > 0 {
		p.view.Segments = p.pages[p.index]
	}
	if p.label != nil {
		var text strings.Builder
		for _, s := range p.view.Segments {
			text.WriteString(s.(*widget.TextSegment).Text)
		}
		p.label.SetText(text.String())
	} else {
		p.view.Refresh()
	}
	p.scroll.ScrollToTop()
	p.position.SetText(fmt.Sprintf("%d / %d", p.index+1, max(1, len(p.pages))))
	setEnabled(p.previous, p.index > 0)
	setEnabled(p.next, p.index+1 < len(p.pages))
	if len(p.pages) > 1 {
		p.controls.Show()
	} else {
		p.controls.Hide()
	}
}

// Text segments are copied so paging never mutates caller-owned history data.
func textPages(segments []widget.RichTextSegment) [][]widget.RichTextSegment {
	var pages [][]widget.RichTextSegment
	var page []widget.RichTextSegment
	used, lines := 0, 0
	for _, segment := range segments {
		text, ok := segment.(*widget.TextSegment)
		if !ok {
			continue
		} // This reader accepts only text segments.
		rest := text.Text
		for rest != "" {
			if used == textPageRunes || lines == textPageLines {
				pages = append(pages, page)
				page = nil
				used, lines = 0, 0
			}
			end, count := 0, 0
			for end < len(rest) && count < textPageRunes-used && lines < textPageLines {
				r, size := utf8.DecodeRuneInString(rest[end:])
				if r == '\n' {
					lines++
				}
				end += size
				count++
			}
			part := *text
			part.Text = rest[:end]
			page = append(page, &part)
			used += count
			rest = rest[end:]
		}
	}
	if len(page) > 0 {
		pages = append(pages, page)
	}
	return pages
}
