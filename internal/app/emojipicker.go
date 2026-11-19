package app

import (
	"strings"
	"unicode/utf8"

	"github.com/costaluu/taskthing/internal/emoji"
)

// ── emoji picker ─────────────────────────────────────────────────────────────
//
// A search box over a short list: type to narrow, arrows to move, enter to
// pick, escape to skip. Before anything is typed it shows the common icons
// with nothing highlighted, so a bare enter skips instead of picking one by
// accident.

const (
	pickerRows    = 8  // visible results
	pickerResults = 50 // kept per search, scrolled through
)

type emojiPicker struct {
	query     string
	results   []emoji.Entry
	cursor    int // -1: nothing highlighted
	offset    int // first visible result
	done      bool
	cancelled bool   // escape: the caller leaves things as they were
	picked    string // the chosen glyph, "" for none
}

// newEmojiPicker starts on seed (the board's name) when it matches something,
// and on the common list when it does not.
func newEmojiPicker(seed string) *emojiPicker {
	p := &emojiPicker{}
	if len(emoji.Search(seed, 1)) > 0 {
		p.query = strings.TrimSpace(seed)
	}
	p.refilter()
	return p
}

func (p *emojiPicker) refilter() {
	p.offset = 0
	if strings.TrimSpace(p.query) == "" {
		p.results, p.cursor = emoji.Common(), -1
		return
	}
	p.results = emoji.Search(p.query, pickerResults)
	p.cursor = -1
	if len(p.results) > 0 {
		p.cursor = 0
	}
}

func (p *emojiPicker) lines() []string {
	if p.done { // leave one line behind instead of the whole screen
		summary := "icon: none"
		switch {
		case p.cancelled:
			summary = "icon: skipped"
		case p.picked != "":
			summary = "icon: " + p.picked
		}
		return []string{paint(summary, textStyle{dim: true})}
	}
	g := createGlyphs(false)
	blank := strings.Repeat(" ", utf8.RuneCountInString(g.selector))
	lines := []string{
		paint("Search an emoji", textStyle{bold: true}),
		"> " + p.query + "▏",
	}
	if len(p.results) == 0 {
		lines = append(lines, paint("no match", textStyle{dim: true}))
	}
	for i := p.offset; i < min(p.offset+pickerRows, len(p.results)); i++ {
		selector := blank
		if i == p.cursor {
			selector = g.selector
		}
		lines = append(lines, selector+" "+p.results[i].Glyph+"  "+p.results[i].Name)
	}
	return append(lines, paint("↑↓ move · enter pick · esc skip", textStyle{dim: true}))
}

// handle returns the chosen glyph ("" for none) once the picker is done.
func (p *emojiPicker) handle(k key) (glyph string, done bool) {
	defer func() {
		if done {
			p.done, p.picked = true, glyph
		}
	}()
	switch {
	case k.escape:
		p.cancelled = true
		return "", true
	case k.enter:
		if p.cursor >= 0 {
			return p.results[p.cursor].Glyph, true
		}
		return "", strings.TrimSpace(p.query) == ""
	case k.down:
		if len(p.results) > 0 {
			p.cursor = min(p.cursor+1, len(p.results)-1)
		}
	case k.up:
		p.cursor = max(p.cursor-1, 0)
		if len(p.results) == 0 {
			p.cursor = -1
		}
	case k.backspace:
		if _, size := utf8.DecodeLastRuneInString(p.query); size > 0 {
			p.query = p.query[:len(p.query)-size]
			p.refilter()
		}
		return "", false
	case k.ctrl || k.left || k.right || k.delete:
	case k.input != "":
		p.query += printable(k.input)
		p.refilter()
	}
	switch { // keep the highlighted row inside the window
	case p.cursor < p.offset:
		p.offset = max(p.cursor, 0)
	case p.cursor >= p.offset+pickerRows:
		p.offset = p.cursor - pickerRows + 1
	}
	return "", false
}

// printable drops control characters from a (possibly pasted) chunk.
func printable(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 32 || r == 0x7f {
			return -1
		}
		return r
	}, s)
}

// pickEmoji runs the picker on a terminal and returns the glyph ("" for none)
// and whether the user cancelled with escape.
func pickEmoji(seed string) (glyph string, cancelled bool, err error) {
	p := newEmojiPicker(seed)
	glyph, err = interact(p.lines, p.handle)
	return glyph, p.cancelled, err
}

// askIcon opens the picker when a person is at the terminal; scripts and
// pipes get ("", false) and nothing changes for them.
func askIcon(seed string) (icon string, cancelled bool) {
	if !stdinTTY || !stdoutTTY {
		return "", false
	}
	icon, cancelled, err := pickEmoji(seed)
	if err != nil {
		return "", false
	}
	return icon, cancelled
}
