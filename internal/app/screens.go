package app

import (
	"strconv"
	"strings"

	"github.com/costaluu/taskthing/internal/js"
	"github.com/costaluu/taskthing/internal/mdwal"
)

// ── interactive screens (selection.tsx, confirmation.tsx, …) ─────────────────
//
// Each is a small state machine: draw, read a key, update, redraw — the way
// ink re-renders a component on every state change — until it yields a value.

type selectionItem struct{ label, value string }

// selection is a mono-select: the cursor is the choice.
type selection struct {
	title  string
	items  []selectionItem
	cursor int
}

func (s *selection) lines() []string {
	g := createGlyphs(false)
	lines := []string{paint(s.title, textStyle{bold: true})}
	blank := strings.Repeat(" ", len([]rune(g.selector)))
	for i, it := range s.items {
		dot, selector := g.dotUnselected, blank
		if i == s.cursor {
			dot, selector = g.dotSelected, g.selector
		}
		lines = append(lines, selector+" "+dot+" "+it.label)
	}
	return lines
}

// handle returns the chosen value once enter is pressed.
func (s *selection) handle(k key) (string, bool) {
	switch {
	case k.down:
		s.cursor = min(s.cursor+1, len(s.items)-1)
	case k.up:
		s.cursor = max(s.cursor-1, 0)
	case k.enter:
		return s.items[s.cursor].value, true
	}
	return "", false
}

type confirmation struct{ title, question string }

func (c *confirmation) lines() []string {
	g := createGlyphs(false)
	return []string{paint(c.title, textStyle{bold: true}), g.selector + " " + c.question + " (y/n) "}
}

func (c *confirmation) handle(k key) (bool, bool) {
	switch strings.ToLower(k.input) {
	case "y":
		return true, true
	case "n":
		return false, true
	}
	return false, false
}

// interact drives a screen to its value: draw, key, redraw.
func interact[T any](draw func() []string, handle func(key) (T, bool)) (T, error) {
	kb, err := openKeyboard()
	if err != nil {
		var zero T
		return zero, err
	}
	defer kb.close()
	s := &screen{}
	s.render(frame(draw()))
	for {
		v, done := handle(kb.next())
		s.render(frame(draw()))
		if done {
			s.unmount()
			return v, nil
		}
	}
}

var dateFormats = []selectionItem{
	{"american (yyyy/mm/dd)", "america"},
	{"europe (dd/mm/yyyy)", "europe"},
}

type installResult struct {
	dateFormat string
	nerdfont   bool
}

// runInstallForm picks a date format, then asks about nerdfonts.
func runInstallForm() (installResult, error) {
	sel := &selection{title: "Date format", items: dateFormats}
	var conf *confirmation
	var result installResult
	return interact(func() []string {
		if conf == nil {
			return sel.lines()
		}
		return conf.lines()
	}, func(k key) (installResult, bool) {
		if conf == nil {
			if v, ok := sel.handle(k); ok {
				result.dateFormat = v
				conf = &confirmation{"Nerdfont", "do you support nerdfonts?"}
			}
			return result, false
		}
		if v, ok := conf.handle(k); ok {
			result.nerdfont = v
			return result, true
		}
		return result, false
	})
}

// ── theme editor (theme-screen.tsx) ──────────────────────────────────────────

type themeScreen struct {
	theme  theme
	edit   map[string]int
	cursor int
}

func newThemeScreen(t theme) *themeScreen {
	edit := map[string]int{}
	for _, r := range roles {
		edit[r] = t.color(r)
	}
	return &themeScreen{theme: t, edit: edit}
}

func previewGroups(now js.Date) []taskGroup {
	const day = 24 * 60 * 60 * 1000.0
	task := func(kv ...any) *js.Object {
		t := js.Obj("id", "", "board", mdwal.INBOX, "completed", false, "title", "", "star", false,
			"rrule", nil, "description", nil, "deleted", false)
		for i := 0; i+1 < len(kv); i += 2 {
			t.Set(kv[i].(string), kv[i+1])
		}
		t.Set("id", t.Value("title"))
		return t
	}
	at := func(offset float64) *js.Date { d := js.DateFromMillis(now.Time() + offset); return &d }
	return []taskGroup{
		{board: boardDisplay{mdwal.INBOX, "inbox", "", ""}, rows: []taskRow{
			{1, task("title", "team standup", "star", true, "rrule", "RRULE:FREQ=WEEKLY"), at(0)},
			{2, task("title", "call the dentist"), at(day)},
			{3, task("title", "ship the release", "completed", true, "description", "notes"), at(-day)},
			{4, task("title", "water the plants"), at(-day)},
		}},
		{board: boardDisplay{"work", "work", "★", "magenta"}, rows: []taskRow{
			{5, task("title", "pay the invoice", "board", "work"), at(-3 * day)},
		}},
	}
}

func (ts *themeScreen) lines() []string {
	g := createGlyphs(false)
	secondary := inkColor(ts.theme.color("text-secondary"))
	width := 0
	for _, r := range roles {
		width = max(width, len(r))
	}
	lines := []string{paint("Theme", textStyle{bold: true}), paint("↑/↓ role   ←/→ colour   enter save", textStyle{color: secondary}), " "}
	blank := strings.Repeat(" ", len([]rune(g.selector)))
	for i, role := range roles {
		selector := blank
		if i == ts.cursor {
			selector = g.selector
		}
		swatch := paint(" ", textStyle{background: inkColor(ts.edit[role])})
		lines = append(lines, selector+" "+swatch+" "+padEnd(role, width)+" < ansi "+itoa(ts.edit[role])+" >")
	}
	t := theme(ts.edit)
	lines = append(lines, " ", paint("Theme Preview", textStyle{bold: true}), " ",
		outcomeLine([]segment{{"added ", false}, {"call the dentist", true}}, false, t),
		paint(g.failure+" something went wrong", textStyle{color: inkColor(t.color("error"))}), " ")
	now := js.Now()
	return append(lines, taskListLines(previewGroups(now), now, "america", false, t)...)
}

func itoa(n int) string { return strconv.Itoa(n) }

// handle returns the config `theme` value on enter: "" when back at the
// default, else the JSON of the roles that differ from it.
func (ts *themeScreen) handle(k key) (string, bool) {
	role := roles[ts.cursor]
	switch {
	case k.down:
		ts.cursor = min(ts.cursor+1, len(roles)-1)
	case k.up:
		ts.cursor = max(ts.cursor-1, 0)
	case k.left:
		ts.edit[role] = (ts.edit[role] + 15) % 16
	case k.right:
		ts.edit[role] = (ts.edit[role] + 1) % 16
	case k.enter:
		overrides := js.NewObject()
		for _, r := range roles {
			if ts.edit[r] != defaultTheme[r] {
				overrides.Set(r, float64(ts.edit[r]))
			}
		}
		if overrides.Len() == 0 {
			return "", true
		}
		return js.Stringify(overrides), true
	}
	return "", false
}

func runThemeScreen(t theme) (string, error) {
	ts := newThemeScreen(t)
	return interact(ts.lines, ts.handle)
}

// ── config menu (config-screen.tsx) ──────────────────────────────────────────

var configHelp = map[string]string{
	"":           "↑/↓ move · enter select · q quit",
	"dateFormat": "↑/↓ move · enter save · esc back · q quit",
	"nerdfont":   "y/n select · esc back · q quit",
	"theme":      "esc back · q quit",
}

// configUpdate is the one setting chosen (key "" when the user left).
type configUpdate struct {
	key   string
	value any
}

func runConfigScreen(t theme) (configUpdate, error) {
	level := ""
	menu := &selection{title: "Config", items: []selectionItem{{"date format", "dateFormat"}, {"nerdfont", "nerdfont"}, {"theme", "theme"}}}
	var dates *selection
	var nerd *confirmation
	var themes *themeScreen
	enter := func(next string) {
		level = next
		switch next {
		case "dateFormat":
			dates = &selection{title: "Date format", items: dateFormats}
		case "nerdfont":
			nerd = &confirmation{"Nerdfont", "enable nerdfont support?"}
		case "theme":
			themes = newThemeScreen(t)
		case "":
			menu = &selection{title: "Config", items: menu.items}
		}
	}
	return interact(func() []string {
		var lines []string
		switch level {
		case "":
			lines = menu.lines()
		case "dateFormat":
			lines = dates.lines()
		case "nerdfont":
			lines = nerd.lines()
		case "theme":
			lines = themes.lines()
		}
		return append(lines, " ", paint(configHelp[level], textStyle{color: inkColor(t.color("text-secondary"))}))
	}, func(k key) (configUpdate, bool) {
		// The child editor sees the key first, then the menu's back/exit keys.
		var result *configUpdate
		switch level {
		case "":
			if v, ok := menu.handle(k); ok {
				defer enter(v)
			}
		case "dateFormat":
			if v, ok := dates.handle(k); ok {
				result = &configUpdate{"dateFormat", v}
			}
		case "nerdfont":
			if v, ok := nerd.handle(k); ok {
				result = &configUpdate{"nerdfont", v}
			}
		case "theme":
			if v, ok := themes.handle(k); ok {
				result = &configUpdate{"theme", v}
			}
		}
		if result != nil {
			return *result, true
		}
		if k.escape {
			if level == "" {
				return configUpdate{}, true
			}
			enter("")
		} else if k.input == "q" {
			return configUpdate{}, true
		}
		return configUpdate{}, false
	})
}
