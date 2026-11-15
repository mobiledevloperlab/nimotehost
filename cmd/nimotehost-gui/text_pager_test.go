// SPDX-License-Identifier: Apache-2.0
package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTextPagingPreservesUnicodeAndStyles(t *testing.T) {
	for _, text := range []string{"", "short\ntext", strings.Repeat("中文🙂é\n", 12000), strings.Repeat("x", textPageRunes*3)} {
		segment := &widget.TextSegment{Text: text, Style: widget.RichTextStyle{TextStyle: fyne.TextStyle{Bold: true}}}
		pages := textPages([]widget.RichTextSegment{segment})
		var recovered strings.Builder
		for _, page := range pages {
			n := 0
			for _, s := range page {
				part := s.(*widget.TextSegment)
				if !utf8.ValidString(part.Text) || !part.Style.TextStyle.Bold {
					t.Fatal("lost Unicode or style")
				}
				n += utf8.RuneCountInString(part.Text)
				recovered.WriteString(part.Text)
			}
			if n > textPageRunes {
				t.Fatal("unbounded layout")
			}
		}
		if recovered.String() != text || segment.Text != text {
			t.Fatal("text lost or mutated")
		}
	}
}

func TestTextPagerNavigationAndReplacement(t *testing.T) {
	a := newUITestApp()
	defer a.Quit()
	p := newTextPager()
	p.setPages(textPages([]widget.RichTextSegment{&widget.TextSegment{Text: strings.Repeat("x", textPageRunes+1)}}), true)
	if !p.controls.Visible() || !p.previous.Disabled() || p.next.Disabled() {
		t.Fatal("wrong first page controls")
	}
	test.Tap(p.next)
	if p.index != 1 || p.previous.Disabled() || !p.next.Disabled() {
		t.Fatal("wrong last page controls")
	}
	test.Tap(p.previous)
	if p.index != 0 {
		t.Fatal("cannot navigate back")
	}
	p.setPages(nil, true)
	if p.controls.Visible() || len(p.view.Segments) != 0 {
		t.Fatal("stale content after replacement")
	}
}

func BenchmarkLongTextLayout(b *testing.B) {
	a := newUITestApp()
	defer a.Quit()
	text := strings.Repeat("中文 diagnostic output line\n", 1500)
	b.Run("WholeLabel", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			v := readOnlyText(text)
			v.Resize(fyne.NewSize(600, 400))
			v.MinSize()
		}
	})
	pages := textPages([]widget.RichTextSegment{&widget.TextSegment{Text: text}})
	b.Run("BoundedPage", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			p := newTextPager()
			p.setPages(pages, true)
			p.root.Resize(fyne.NewSize(600, 400))
			p.root.MinSize()
		}
	})
}

func TestPlainTextPagingRemainsSelectable(t *testing.T) {
	a := newUITestApp()
	defer a.Quit()
	text := strings.Repeat("selectable 中文\n", 2000)
	p := newPlainTextPager(text)
	var recovered strings.Builder
	for i := range p.pages {
		p.index = i
		p.show()
		if !p.label.Selectable {
			t.Fatal("reader lost text selection")
		}
		recovered.WriteString(p.label.Text)
	}
	if recovered.String() != text {
		t.Fatal("reader lost content")
	}
}

func TestTextPagingBoundsManyEmptyLines(t *testing.T) {
	text := strings.Repeat("\n", 4000)
	pages := textPages([]widget.RichTextSegment{&widget.TextSegment{Text: text}})
	for _, page := range pages {
		lines := 0
		for _, s := range page {
			lines += strings.Count(s.(*widget.TextSegment).Text, "\n")
		}
		if lines > textPageLines {
			t.Fatal("too many lines in one layout")
		}
	}
}

func TestLongReaderCopyIncludesEveryPage(t *testing.T) {
	a := newUITestApp()
	defer a.Quit()
	applyAppearance(a, "Light")
	w := &clipboardTestWindow{Window: a.NewWindow("reader"), clipboard: test.NewClipboard()}
	defer w.Close()
	m, _ := newMessages("en")
	u := &managerWindow{app: a, window: w, messages: m}
	w.Resize(fyne.NewSize(960, 640))
	text := strings.Repeat("中文 output\n", 3000)
	u.showReader("Details", text)
	var copy *widget.Button
	visitObjects(w.Canvas().Overlays().Top(), func(obj fyne.CanvasObject) {
		if b, ok := obj.(*widget.Button); ok && b.Text == u.t("action.copy") {
			copy = b
		}
	})
	if copy == nil {
		t.Fatal("copy action missing")
	}
	test.Tap(copy)
	if w.Clipboard().Content() != text {
		t.Fatal("copy only included the visible page")
	}
}

// The stock test window returns a fresh clipboard for every call.
type clipboardTestWindow struct {
	fyne.Window
	clipboard fyne.Clipboard
}

func (w *clipboardTestWindow) Clipboard() fyne.Clipboard { return w.clipboard }
