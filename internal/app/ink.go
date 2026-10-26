package app

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ── chalk, as ink's <Text> applies it ────────────────────────────────────────
//
// A <Text> wraps its content innermost-first: dim, colour, background, bold,
// underline. Each wrap re-opens its style after any nested close of the same
// kind, the way chalk does, so nested spans keep their outer style.

type sgr struct{ open, close string }

var foreground = map[string]int{
	"black": 30, "red": 31, "green": 32, "yellow": 33, "blue": 34, "magenta": 35, "cyan": 36, "white": 37,
	"gray": 90, "grey": 90, "blackBright": 90, "redBright": 91, "greenBright": 92, "yellowBright": 93,
	"blueBright": 94, "magentaBright": 95, "cyanBright": 96, "whiteBright": 97,
}

// inkColor is the chalk colour name for an ANSI index 0-15.
var inkColorNames = []string{
	"black", "red", "green", "yellow", "blue", "magenta", "cyan", "white",
	"gray", "redBright", "greenBright", "yellowBright", "blueBright", "magentaBright", "cyanBright", "whiteBright",
}

func inkColor(index int) string {
	if index < 0 || index >= len(inkColorNames) {
		panic(fmt.Sprintf("ANSI colour index out of range (0-15): %d", index))
	}
	return inkColorNames[index]
}

// colorLevel is supports-color's answer for this terminal.
func colorLevel() int {
	if !stdoutTTY {
		return 0
	}
	if os.Getenv("TERM") == "dumb" {
		return 0
	}
	if ct := os.Getenv("COLORTERM"); ct == "truecolor" || ct == "24bit" {
		return 3
	}
	if strings.Contains(os.Getenv("TERM"), "256") {
		return 2
	}
	return 1
}

// colorize is ink's colorize: a chalk name, #hex, rgb(…) or ansi256(…); an
// unknown name draws uncoloured.
func colorize(color string, background bool) *sgr {
	if color == "" {
		return nil
	}
	if code, ok := foreground[color]; ok {
		if background {
			return &sgr{"\x1b[" + strconv.Itoa(code+10) + "m", "\x1b[49m"}
		}
		return &sgr{"\x1b[" + strconv.Itoa(code) + "m", "\x1b[39m"}
	}
	var r, g, b int
	switch {
	case strings.HasPrefix(color, "#") && (len(color) == 7 || len(color) == 4):
		hex := color[1:]
		if len(hex) == 3 {
			hex = string([]byte{hex[0], hex[0], hex[1], hex[1], hex[2], hex[2]})
		}
		n, err := strconv.ParseUint(hex, 16, 32)
		if err != nil {
			return nil
		}
		r, g, b = int(n>>16)&255, int(n>>8)&255, int(n)&255
	case strings.HasPrefix(color, "rgb("):
		if _, err := fmt.Sscanf(strings.ReplaceAll(color, " ", ""), "rgb(%d,%d,%d)", &r, &g, &b); err != nil {
			return nil
		}
	case strings.HasPrefix(color, "ansi256("):
		var n int
		if _, err := fmt.Sscanf(color, "ansi256(%d)", &n); err != nil {
			return nil
		}
		return ansi256(n, background)
	default:
		return nil
	}
	closeCode := "\x1b[39m"
	base := 38
	if background {
		closeCode, base = "\x1b[49m", 48
	}
	switch colorLevel() {
	case 3:
		return &sgr{fmt.Sprintf("\x1b[%d;2;%d;%d;%dm", base, r, g, b), closeCode}
	case 2:
		return ansi256(rgbToAnsi256(r, g, b), background)
	}
	code := rgbToAnsi16(r, g, b)
	if background {
		code += 10
	}
	return &sgr{"\x1b[" + strconv.Itoa(code) + "m", closeCode}
}

func ansi256(n int, background bool) *sgr {
	if background {
		return &sgr{fmt.Sprintf("\x1b[48;5;%dm", n), "\x1b[49m"}
	}
	return &sgr{fmt.Sprintf("\x1b[38;5;%dm", n), "\x1b[39m"}
}

func rgbToAnsi256(r, g, b int) int {
	if r == g && g == b {
		if r < 8 {
			return 16
		}
		if r > 248 {
			return 231
		}
		return int(float64(r-8)/247*24+0.5) + 232
	}
	q := func(c int) int { return int(float64(c)/255*5 + 0.5) }
	return 16 + 36*q(r) + 6*q(g) + q(b)
}

func rgbToAnsi16(r, g, b int) int {
	value := int(float64(max(r, g, b))/255*2 + 0.5)
	if value == 0 {
		return 30
	}
	result := 30 + ((int(float64(b)/255+0.5) << 2) | (int(float64(g)/255+0.5) << 1) | int(float64(r)/255+0.5))
	if value == 2 {
		result += 60
	}
	return result
}

func wrapStyle(s string, style *sgr) string {
	if style == nil {
		return s
	}
	return style.open + strings.ReplaceAll(s, style.close, style.close+style.open) + style.close
}

// textStyle is the subset of ink <Text> props taskthing draws with.
type textStyle struct {
	color, background string
	bold, underline   bool
	dim               bool
}

func paint(s string, st textStyle) string {
	if colorLevel() == 0 {
		return s
	}
	if st.dim {
		s = wrapStyle(s, &sgr{"\x1b[2m", "\x1b[22m"})
	}
	s = wrapStyle(s, colorize(st.color, false))
	s = wrapStyle(s, colorize(st.background, true))
	if st.bold {
		s = wrapStyle(s, &sgr{"\x1b[1m", "\x1b[22m"})
	}
	if st.underline {
		s = wrapStyle(s, &sgr{"\x1b[4m", "\x1b[24m"})
	}
	return s
}

// ── text width and wrapping (string-width / wrap-ansi) ───────────────────────

func runeWidth(r rune) int {
	switch {
	case r == 0 || r < 32 || (r >= 0x7f && r < 0xa0):
		return 0
	case r >= 0x300 && r <= 0x36f, r == 0x200d, r >= 0xfe00 && r <= 0xfe0f, r >= 0xe0100 && r <= 0xe01ef:
		return 0
	case r >= 0x1100 && r <= 0x115f, r >= 0x2e80 && r <= 0xa4cf, r >= 0xac00 && r <= 0xd7a3,
		r >= 0xf900 && r <= 0xfaff, r >= 0xfe30 && r <= 0xfe4f, r >= 0xff00 && r <= 0xff60,
		r >= 0xffe0 && r <= 0xffe6, r >= 0x1f300 && r <= 0x1f64f, r >= 0x1f900 && r <= 0x1f9ff,
		r >= 0x1f680 && r <= 0x1f6ff, r >= 0x2b50 && r <= 0x2b55, r >= 0x20000 && r <= 0x3fffd,
		r == 0x231a, r == 0x231b, r == 0x23f0, r == 0x23f3, r >= 0x25fd && r <= 0x25fe, r == 0x2b1b, r == 0x2b1c:
		return 2
	}
	return 1
}

// visibleWidth ignores ANSI escapes.
func visibleWidth(s string) int {
	w := 0
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			i = skipEscape(s, i)
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		w += runeWidth(r)
		i += size
	}
	return w
}

func skipEscape(s string, i int) int {
	j := i + 1
	if j < len(s) && s[j] == '[' {
		j++
		for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
			j++
		}
		return j + 1
	}
	return j + 1
}

// wrapLine breaks one styled line at spaces to fit width, hard-breaking words
// longer than it, as ink's wrap-ansi does.
func wrapLine(line string, width int) []string {
	if width <= 0 || visibleWidth(line) <= width {
		return []string{line}
	}
	words := strings.Split(line, " ")
	var out []string
	cur, curWidth := "", 0
	for i, word := range words {
		ww := visibleWidth(word)
		sep := 0
		if i > 0 && cur != "" {
			sep = 1
		}
		if curWidth+sep+ww <= width {
			if sep == 1 {
				cur += " "
			}
			cur += word
			curWidth += sep + ww
			continue
		}
		if cur != "" || i > 0 {
			out = append(out, cur)
		}
		cur, curWidth = "", 0
		for visibleWidth(word) > width {
			head, rest := splitAtWidth(word, width)
			out = append(out, head)
			word = rest
		}
		cur, curWidth = word, visibleWidth(word)
	}
	return append(out, cur)
}

func splitAtWidth(s string, width int) (string, string) {
	w := 0
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			i = skipEscape(s, i)
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if w+runeWidth(r) > width {
			return s[:i], s[i:]
		}
		w += runeWidth(r)
		i += size
	}
	return s, ""
}

// frame lays out the lines of a <Box flexDirection="column"> on the terminal:
// wrapped to its width, trailing spaces trimmed.
func frame(lines []string) string {
	width := stdoutColumns()
	var out []string
	for _, l := range lines {
		for _, w := range wrapLine(l, width) {
			out = append(out, strings.TrimRight(w, " "))
		}
	}
	return strings.Join(out, "\n")
}

// ── the screen (ink's log-update) ────────────────────────────────────────────

type screen struct {
	lines   int
	started bool
	last    string
}

func eraseLines(n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteString("\x1b[2K")
		if i < n-1 {
			b.WriteString("\x1b[1A")
		}
	}
	if n > 0 {
		b.WriteString("\x1b[G")
	}
	return b.String()
}

// inkMounted: some screen was drawn, so ink's exit hook is registered.
var inkMounted bool

func (s *screen) render(f string) {
	inkMounted = true
	// log-update writes nothing when the frame is unchanged.
	if s.started && f == s.last {
		return
	}
	s.last = f
	out := "\x1b[?2026h"
	if s.started {
		out += eraseLines(s.lines)
	} else {
		out += "\x1b[?25l"
		s.started = true
	}
	out += f + "\n\x1b[?2026l"
	os.Stdout.WriteString(out)
	s.lines = strings.Count(f, "\n") + 2
}

func (s *screen) unmount() { os.Stdout.WriteString("\x1b[?25h\x1b[?25h") }

// renderStatic paints a screen once and leaves it on the terminal.
func renderStatic(lines []string) {
	s := &screen{}
	s.render(frame(lines))
	s.unmount()
}

// ── keyboard (ink's useInput, in raw mode) ───────────────────────────────────

type key struct {
	input                                string
	up, down, left, right, enter, escape bool
	backspace, delete, ctrl              bool
}

type keyboard struct {
	state *rawState
	buf   []byte
}

func openKeyboard() (*keyboard, error) {
	state, err := makeRaw()
	if err != nil {
		return nil, err
	}
	return &keyboard{state: state, buf: make([]byte, 64)}, nil
}

func (k *keyboard) close() { k.state.restore() }

// next reads one keypress; ctrl+c ends the process the way ink's exitOnCtrlC
// does, leaving the screen as it is.
func (k *keyboard) next() key {
	for {
		n, err := os.Stdin.Read(k.buf)
		if err != nil || n == 0 {
			k.close()
			os.Exit(0)
		}
		chunk := string(k.buf[:n])
		switch chunk {
		case "\x03":
			k.close()
			os.Stdout.WriteString("\x1b[?25h")
			os.Exit(0)
		case "\x1b[A", "\x1bOA":
			return key{up: true}
		case "\x1b[B", "\x1bOB":
			return key{down: true}
		case "\x1b[C", "\x1bOC":
			return key{right: true}
		case "\x1b[D", "\x1bOD":
			return key{left: true}
		case "\r", "\n":
			return key{enter: true, input: "\r"}
		case "\x1b":
			return key{escape: true}
		case "\x7f", "\b":
			return key{backspace: true}
		case "\x1b[3~":
			return key{delete: true}
		}
		if len(chunk) == 1 && chunk[0] < 32 {
			return key{ctrl: true, input: string(rune(chunk[0] + 'a' - 1))}
		}
		if strings.HasPrefix(chunk, "\x1b") {
			continue
		}
		return key{input: chunk}
	}
}
