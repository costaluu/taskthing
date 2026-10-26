package app

import (
	"os"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/term"

	"github.com/costaluu/taskthing/internal/js"
)

// ── the process's streams ────────────────────────────────────────────────────

var (
	stdoutTTY = term.IsTerminal(int(os.Stdout.Fd()))
	stderrTTY = term.IsTerminal(int(os.Stderr.Fd()))
	stdinTTY  = term.IsTerminal(int(os.Stdin.Fd()))
)

func stdoutColumns() int {
	if !stdoutTTY {
		return 0
	}
	w, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		return 0
	}
	return w
}

// consoleLog is console.log.
func consoleLog(s string) { os.Stdout.WriteString(s + "\n") }

// consoleError is Bun's console.error: red whenever either stream is a
// terminal, unless NO_COLOR is set.
func consoleError(s string) {
	if _, noColor := os.LookupEnv("NO_COLOR"); (stdoutTTY || stderrTTY) && !noColor {
		s = "\x1b[0m\x1b[31m" + s + "\x1b[0m"
	}
	os.Stderr.WriteString(s + "\n")
}

// Bunli's own colours: only on a terminal stdout, and NO_COLOR (non-empty) off.
func bunliColor(code, s string) string {
	if !stdoutTTY || os.Getenv("NO_COLOR") != "" {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func bunliRed(s string) string { return bunliColor("31", s) }
func bunliDim(s string) string { return bunliColor("2", s) }

// ── YAML (the `yaml` package's stringify, for the envelopes) ─────────────────

var yamlNumberLike = regexp.MustCompile(`^(?:[-+]?(?:\d[\d_]*)?\.?\d+(?:[eE][-+]?\d+)?|0x[0-9a-fA-F]+|0o[0-7]+|[-+]?\.(?:inf|Inf|INF)|\.(?:nan|NaN|NAN))$`)

func yamlPlainOK(s string) bool {
	if s == "" || strings.TrimSpace(s) != s {
		return false
	}
	switch strings.ToLower(s) {
	case "true", "false", "null", "~", "yes", "no", "on", "off":
		return false
	}
	if yamlNumberLike.MatchString(s) {
		return false
	}
	if strings.ContainsAny(s[:1], "-?:,[]{}#&*!|>'\"%@`") {
		return false
	}
	if strings.Contains(s, ": ") || strings.Contains(s, " #") || strings.HasSuffix(s, ":") {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func yamlQuote(s string) string {
	if strings.Contains(s, `"`) && !strings.Contains(s, "'") {
		return "'" + s + "'"
	}
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\t':
			b.WriteString(`\t`)
		case '\n':
			b.WriteString(`\n`)
		default:
			if r < 0x20 {
				b.WriteString(`\x` + strconv.FormatInt(int64(r)+0x100, 16)[1:])
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

func yamlScalar(v any, indent int) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case bool:
		if x {
			return "true"
		}
		return "false"
	case float64:
		return js.NumberToString(x)
	case int:
		return strconv.Itoa(x)
	case string:
		if strings.Contains(x, "\n") {
			pad := strings.Repeat(" ", indent+2)
			chomp := "|-"
			body := x
			if strings.HasSuffix(x, "\n") {
				chomp, body = "|", strings.TrimSuffix(x, "\n")
			}
			lines := strings.Split(body, "\n")
			for i, l := range lines {
				if l != "" {
					lines[i] = pad + l
				}
			}
			return chomp + "\n" + strings.Join(lines, "\n")
		}
		if yamlPlainOK(x) {
			return x
		}
		return yamlQuote(x)
	}
	return ""
}

func yamlStringify(v any, indent int) string {
	pad := strings.Repeat(" ", indent)
	var b strings.Builder
	switch x := v.(type) {
	case *js.Object:
		for _, k := range x.Keys() {
			val := x.Value(k)
			switch inner := val.(type) {
			case *js.Object:
				if inner.Len() == 0 {
					b.WriteString(pad + k + ": {}\n")
				} else {
					b.WriteString(pad + k + ":\n" + yamlStringify(inner, indent+2))
				}
			case []any:
				if len(inner) == 0 {
					b.WriteString(pad + k + ": []\n")
				} else {
					b.WriteString(pad + k + ":\n" + yamlStringify(inner, indent+2))
				}
			default:
				b.WriteString(pad + k + ": " + yamlScalar(val, indent) + "\n")
			}
		}
	case []any:
		for _, e := range x {
			b.WriteString(pad + "- " + strings.TrimLeft(yamlStringify(e, indent+2), " "))
		}
	default:
		b.WriteString(yamlScalar(v, indent-2) + "\n")
	}
	return b.String()
}

// ── Markdown (bunli's formatMarkdown) ────────────────────────────────────────

func isScalar(v any) bool {
	switch v.(type) {
	case *js.Object, []any:
		return false
	}
	return true
}

func jsStringOf(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case bool:
		if x {
			return "true"
		}
		return "false"
	case float64:
		return js.NumberToString(x)
	case int:
		return strconv.Itoa(x)
	case string:
		return x
	case []any:
		parts := make([]string, len(x))
		for i, e := range x {
			if e != nil {
				parts[i] = jsStringOf(e)
			}
		}
		return strings.Join(parts, ",")
	case *js.Object:
		return "[object Object]"
	}
	return ""
}

func mdTable(headers []string, rows [][]string) string {
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len([]rune(h))
		for _, r := range rows {
			if i < len(r) {
				widths[i] = max(widths[i], len([]rune(r[i])))
			}
		}
	}
	line := func(cells []string) string {
		parts := make([]string, len(headers))
		for i := range headers {
			c := ""
			if i < len(cells) {
				c = cells[i]
			}
			parts[i] = padEnd(c, widths[i])
		}
		return "| " + strings.Join(parts, " | ") + " |"
	}
	seps := make([]string, len(widths))
	for i, w := range widths {
		seps[i] = strings.Repeat("-", w+2)
	}
	body := make([]string, len(rows))
	for i, r := range rows {
		body[i] = line(r)
	}
	return line(headers) + "\n|" + strings.Join(seps, "|") + "|\n" + strings.Join(body, "\n")
}

func kvTable(o *js.Object) string {
	var rows [][]string
	for _, k := range o.Keys() {
		rows = append(rows, []string{k, jsStringOf(o.Value(k))})
	}
	return mdTable([]string{"Key", "Value"}, rows)
}

func isFlat(o *js.Object) bool {
	for _, k := range o.Keys() {
		if !isScalar(o.Value(k)) {
			return false
		}
	}
	return true
}

func isArrayOfObjects(v any) bool {
	arr, ok := v.([]any)
	if !ok || len(arr) == 0 {
		return false
	}
	for _, e := range arr {
		if _, ok := e.(*js.Object); !ok {
			return false
		}
	}
	return true
}

func columnarTable(items []any) string {
	var keys []string
	seen := map[string]bool{}
	for _, it := range items {
		for _, k := range it.(*js.Object).Keys() {
			if !seen[k] {
				seen[k] = true
				keys = append(keys, k)
			}
		}
	}
	var rows [][]string
	for _, it := range items {
		o := it.(*js.Object)
		r := make([]string, len(keys))
		for i, k := range keys {
			if v, ok := o.Get(k); ok && v != nil {
				r[i] = jsStringOf(v)
			}
		}
		rows = append(rows, r)
	}
	return mdTable(keys, rows)
}

func formatMarkdown(v any, path []string) string {
	heading := func(p []string) string { return "## " + strings.Join(p, ".") + "\n\n" }
	if isScalar(v) {
		if len(path) == 0 {
			return jsStringOf(v)
		}
		return heading(path) + jsStringOf(v)
	}
	if arr, ok := v.([]any); ok {
		if isArrayOfObjects(arr) {
			if len(path) == 0 {
				return columnarTable(arr)
			}
			return heading(path) + columnarTable(arr)
		}
		return formatMarkdown(jsStringOf(arr), path)
	}
	o := v.(*js.Object)
	if len(path) == 0 && isFlat(o) {
		return kvTable(o)
	}
	keys := o.Keys()
	needsHeadings := len(path) > 0 || len(keys) > 1 || !isFlat(o)
	if !needsHeadings {
		return kvTable(o)
	}
	var sections []string
	for _, k := range keys {
		child := append(append([]string(nil), path...), k)
		val := o.Value(k)
		switch {
		case isScalar(val):
			sections = append(sections, heading(child)+jsStringOf(val))
		case isArrayOfObjects(val):
			sections = append(sections, heading(child)+columnarTable(val.([]any)))
		default:
			if nested, ok := val.(*js.Object); ok {
				if isFlat(nested) {
					sections = append(sections, heading(child)+kvTable(nested))
				} else {
					sections = append(sections, formatMarkdown(nested, child))
				}
			} else {
				sections = append(sections, heading(child)+jsStringOf(val))
			}
		}
	}
	return strings.Join(sections, "\n\n")
}
