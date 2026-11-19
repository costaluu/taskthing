package app

import (
	"strings"
	"testing"
)

func typeInto(p *emojiPicker, s string) {
	for _, r := range s {
		p.handle(key{input: string(r)})
	}
}

func screenText(p *emojiPicker) string { return strings.Join(p.lines(), "\n") }

func TestPickerStartsOnTheCommonListWithNothingHighlighted(t *testing.T) {
	p := newEmojiPicker("")
	if got := screenText(p); !strings.Contains(got, "Search an emoji") || !strings.Contains(got, "🏠") {
		t.Fatalf("the starter list must show:\n%s", got)
	}
	if v, done := p.handle(key{enter: true}); !done || v != "" {
		t.Fatalf("enter with nothing chosen skips the icon, got (%q, %v)", v, done)
	}
}

func TestPickerArrowThenEnterPicksFromTheCommonList(t *testing.T) {
	p := newEmojiPicker("")
	p.handle(key{down: true})
	v, done := p.handle(key{enter: true})
	if !done || v == "" {
		t.Fatalf("down then enter must pick the first common emoji, got (%q, %v)", v, done)
	}
}

func TestPickerFiltersAsYouType(t *testing.T) {
	p := newEmojiPicker("")
	typeInto(p, "shopping cart")
	if got := screenText(p); !strings.Contains(got, "🛒") || strings.Contains(got, "🏠") {
		t.Fatalf("the list must narrow to the match:\n%s", got)
	}
	if v, done := p.handle(key{enter: true}); !done || v != "🛒" {
		t.Fatalf("enter picks the highlighted first match, got (%q, %v)", v, done)
	}
}

func TestPickerBackspaceRemovesWholeCharacters(t *testing.T) {
	p := newEmojiPicker("")
	typeInto(p, "café")
	p.handle(key{backspace: true})
	if !strings.Contains(screenText(p), "> caf") || strings.Contains(screenText(p), "caf\xc3") {
		t.Fatalf("backspace must drop the whole é:\n%s", screenText(p))
	}
}

func TestPickerEscapeSkips(t *testing.T) {
	p := newEmojiPicker("")
	typeInto(p, "shop")
	if v, done := p.handle(key{escape: true}); !done || v != "" {
		t.Fatalf("escape skips the icon, got (%q, %v)", v, done)
	}
}

func TestPickerNoMatchKeepsTheScreenOpen(t *testing.T) {
	p := newEmojiPicker("")
	typeInto(p, "zzzzqqq")
	if !strings.Contains(screenText(p), "no match") {
		t.Fatalf("an empty result must say so:\n%s", screenText(p))
	}
	if _, done := p.handle(key{enter: true}); done {
		t.Fatal("enter on a search with no result must not close the picker; escape skips")
	}
}

func TestPickerStartsFromTheGivenText(t *testing.T) {
	p := newEmojiPicker("shopping")
	if !strings.Contains(screenText(p), "> shopping") {
		t.Fatalf("the board name seeds the search:\n%s", screenText(p))
	}
	if v, done := p.handle(key{enter: true}); !done || v == "" {
		t.Fatalf("a seeded search highlights its first match, got (%q, %v)", v, done)
	}
}

func TestPickerCursorStaysInRangeAndScrolls(t *testing.T) {
	p := newEmojiPicker("")
	p.handle(key{up: true}) // above the first row: stays unhighlighted-or-first, never panics
	for range 40 {
		p.handle(key{down: true})
	}
	rows := 0
	for _, l := range p.lines() {
		if strings.HasPrefix(l, "→") {
			rows++
		}
	}
	if rows != 1 {
		t.Fatalf("exactly one highlighted row must be visible after scrolling past the end, got %d:\n%s", rows, screenText(p))
	}
	if n := len(p.lines()); n > 12 {
		t.Fatalf("the picker must stay compact, drew %d lines", n)
	}
	typeInto(p, "face")
	if !strings.Contains(screenText(p), "→") {
		t.Fatal("typing resets the highlight to the first result")
	}
}

func TestPickerIgnoresControlKeys(t *testing.T) {
	p := newEmojiPicker("")
	for _, k := range []key{{left: true}, {right: true}, {ctrl: true, input: "a"}, {delete: true}} {
		if _, done := p.handle(k); done {
			t.Fatalf("%+v must not close the picker", k)
		}
	}
	if !strings.Contains(screenText(p), "> ") || strings.Contains(screenText(p), "> a") {
		t.Fatalf("control keys must not type:\n%s", screenText(p))
	}
}

func TestPickerSeedWithNoMatchStartsEmpty(t *testing.T) {
	p := newEmojiPicker("groceries")
	got := screenText(p)
	if strings.Contains(got, "no match") || !strings.Contains(got, "🏠") {
		t.Fatalf("a seed that matches nothing must fall back to the starter list:\n%s", got)
	}
	if v, done := p.handle(key{enter: true}); !done || v != "" {
		t.Fatalf("enter then skips, got (%q, %v)", v, done)
	}
}

func TestPickerCollapsesToOneLineWhenDone(t *testing.T) {
	p := newEmojiPicker("")
	typeInto(p, "shopping cart")
	p.handle(key{enter: true})
	if got := p.lines(); len(got) != 1 || !strings.Contains(got[0], "🛒") {
		t.Fatalf("a finished picker leaves one summary line, got %q", got)
	}
	none := newEmojiPicker("")
	none.handle(key{enter: true})
	if got := none.lines(); len(got) != 1 || !strings.Contains(got[0], "none") {
		t.Fatalf("enter with nothing chosen says none, got %q", got)
	}
	esc := newEmojiPicker("")
	esc.handle(key{escape: true})
	if got := esc.lines(); len(got) != 1 || !strings.Contains(got[0], "skipped") {
		t.Fatalf("escape says skipped, got %q", got)
	}
}

// Escape and an empty enter both return no glyph, but only escape cancels:
// `board set icon` clears the icon on the one and leaves it alone on the other.
func TestPickerTellsCancelFromNone(t *testing.T) {
	esc := newEmojiPicker("")
	esc.handle(key{escape: true})
	if !esc.cancelled {
		t.Error("escape cancels")
	}
	none := newEmojiPicker("")
	none.handle(key{enter: true})
	if none.cancelled {
		t.Error("an empty enter chooses none; it does not cancel")
	}
	pick := newEmojiPicker("shopping")
	pick.handle(key{enter: true})
	if pick.cancelled {
		t.Error("picking is not cancelling")
	}
}
