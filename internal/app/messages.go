package app

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/costaluu/taskthing/internal/js"
)

// ── glyphs: a nerdfont form and a plain-unicode fallback for every mark ──────

type glyphs struct {
	checkboxUnchecked, checkboxChecked, inbox, star, description, recurrence string
	selector, dotUnselected, dotSelected, success, failure, info, warn       string
}

func createGlyphs(nerdfont bool) glyphs {
	if nerdfont {
		return glyphs{
			checkboxUnchecked: "", checkboxChecked: "", inbox: "", star: "",
			description: "\U000f0188", recurrence: "", selector: "", dotUnselected: "",
			dotSelected: "", success: "", failure: "", info: "", warn: "",
		}
	}
	return glyphs{
		checkboxUnchecked: "[ ]", checkboxChecked: "[x]", inbox: "📬", star: "⭐",
		description: "📄", recurrence: "🔄", selector: "→", dotUnselected: "○",
		dotSelected: "●", success: "✓", failure: "𐄂", info: "ℹ️", warn: "⚠️",
	}
}

// ── theme: semantic roles → ANSI indexes 0-15 (ADR-0005) ─────────────────────

var roles = []string{
	"highlight", "error", "success", "warning", "text-secondary", "background-secondary",
	"inbox-accent", "date-overdue", "date-yesterday", "date-today", "date-tomorrow",
}

var defaultTheme = map[string]int{
	"highlight": 4, "error": 1, "success": 2, "warning": 3, "text-secondary": 8,
	"background-secondary": 0, "inbox-accent": 6, "date-overdue": 1, "date-yesterday": 9,
	"date-today": 11, "date-tomorrow": 12,
}

type theme map[string]int

func (t theme) color(role string) int { return t[role] }

// createTheme overrides only the roles a custom JSON names.
func createTheme(custom string) (theme, error) {
	t := theme{}
	for k, v := range defaultTheme {
		t[k] = v
	}
	if custom == "" {
		return t, nil
	}
	parsed, err := js.Parse(custom)
	if err != nil {
		return nil, err
	}
	o, ok := parsed.(*js.Object)
	if !ok {
		return nil, errors.New(js.StringifyIndent([]any{js.Obj(
			"expected", "record", "code", "invalid_type", "path", []any{},
			"message", "Invalid input: expected record, received "+jsType(parsed),
		)}, 2))
	}
	var issues []any
	for _, role := range o.Keys() {
		known := false
		for _, r := range roles {
			if r == role {
				known = true
			}
		}
		if !known {
			quoted := make([]string, len(roles))
			values := make([]any, len(roles))
			for i, r := range roles {
				quoted[i], values[i] = js.Quote(r), r
			}
			issues = append(issues, js.Obj("code", "invalid_key", "origin", "record", "issues", []any{js.Obj(
				"code", "invalid_value", "values", values, "path", []any{},
				"message", "Invalid option: expected one of "+strings.Join(quoted, "|"),
			)}, "path", []any{role}, "message", "Invalid key in record"))
			continue
		}
		n, isNum := o.Value(role).(float64)
		switch {
		case !isNum:
			issues = append(issues, js.Obj("expected", "number", "code", "invalid_type", "path", []any{role},
				"message", "Invalid input: expected number, received "+jsType(o.Value(role))))
		case !js.IsInteger(n):
			issues = append(issues, js.Obj("expected", "int", "format", "safeint", "code", "invalid_type", "path", []any{role},
				"message", "Invalid input: expected int, received number"))
		case n < 0:
			issues = append(issues, js.Obj("origin", "number", "code", "too_small", "minimum", 0.0, "inclusive", true, "path", []any{role},
				"message", "Too small: expected number to be >=0"))
		case n > 15:
			issues = append(issues, js.Obj("origin", "number", "code", "too_big", "maximum", 15.0, "inclusive", true, "path", []any{role},
				"message", "Too big: expected number to be <=15"))
		default:
			t[role] = int(n)
		}
	}
	if len(issues) > 0 {
		return nil, errors.New(js.StringifyIndent(issues, 2))
	}
	return t, nil
}

func jsType(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case string:
		return "string"
	case float64:
		return "number"
	case []any:
		return "array"
	}
	return "object"
}

// ── outcome copy (task-message.ts, board-message.ts, workspace-message.ts) ───

// segment is one run of an outcome line; variable spans are the values the
// user entered, drawn as a chip.
type segment struct {
	text     string
	variable bool
}

type valueChip struct{ connector, text string }

type taskOutcome struct {
	title, predicate string
	value            *valueChip
	nextOccurrence   *string
	recurrence       *string
}

func taskMessage(o taskOutcome) []segment {
	s := []segment{{"task ", false}, {o.title, true}, {" " + o.predicate, false}}
	if o.value != nil {
		s = append(s, segment{o.value.connector, false}, segment{o.value.text, true})
	}
	if o.nextOccurrence != nil {
		s = append(s, segment{". next occurrence: ", false}, segment{*o.nextOccurrence, true})
		if o.recurrence != nil {
			s = append(s, segment{" ", false}, segment{*o.recurrence, true})
		}
	}
	return s
}

type entityOutcome struct {
	name, predicate string
	value           *valueChip
	suffix          *string
}

func namedMessage(noun string, o entityOutcome) []segment {
	s := []segment{{noun + " ", false}, {o.name, true}, {" " + o.predicate, false}}
	if o.value != nil {
		s = append(s, segment{o.value.connector, false}, segment{o.value.text, true})
	}
	if o.suffix != nil {
		s = append(s, segment{*o.suffix, false})
	}
	return s
}

// summarize is an error's first line.
func summarize(err error) string {
	msg := err.Error()
	first, _, _ := strings.Cut(msg, "\n")
	return js.Trim(first)
}

func taskFailure(verb string, err error) string {
	return fmt.Sprintf("taskthing couldn't %s this task. error: %s", verb, summarize(err))
}

func actionFailure(action string, err error) string {
	return fmt.Sprintf("taskthing couldn't %s. error: %s", action, summarize(err))
}

// ── spinner copy (spinner-messages.ts) ───────────────────────────────────────

type commandCopy struct {
	pending, success string
	failure          func(error) string
}

func spinnerCopy(pending, success, during string) commandCopy {
	return commandCopy{pending, success, func(err error) string {
		return fmt.Sprintf("something went wrong during %s. error: %s", during, summarize(err))
	}}
}

func syncCopy(ws string) commandCopy {
	return spinnerCopy("syncing "+ws+"...", ws+" synchronized!", "syncing")
}
func rebuildCopy(ws string) commandCopy {
	return spinnerCopy("rebuilding "+ws+"...", ws+" rebuilt!", "rebuild")
}
func pullCopy(ws string) commandCopy {
	return spinnerCopy("pulling "+ws+"...", ws+" pulled!", "pull")
}
func truncateCopy(ws, branch string) commandCopy {
	return spinnerCopy("truncating branch "+branch+" from "+ws+"...", ws+" truncated!", "truncating")
}

const (
	updateCheckPending = "looking for updates..."
	updateCheckLatest  = "you're on the latest version!"
	updateApplyPending = "updating taskthing..."
)

func updateCheckUpdate(current, target string) string {
	return fmt.Sprintf("there's a pending update %s → %s!", current, target)
}
func updateCheckFailure(err error) string {
	return "something went wrong during update check. error: " + summarize(err)
}
func updateApplySuccess(target string) string { return "taskthing updated to version " + target }
func updateApplyFailure(err error) string {
	return "something went wrong during taskthing update. error: " + summarize(err)
}

// ── date formatting (date-format.ts) ─────────────────────────────────────────

var monthAbbrs = []string{"jan", "feb", "mar", "apr", "may", "jun", "jul", "aug", "sep", "oct", "nov", "dec"}

func pad2(n int) string { return fmt.Sprintf("%02d", n) }

// absoluteDateTime is a UTC date + time in the configured order.
func absoluteDateTime(d js.Date, dateFormat string) string {
	y := strconv.Itoa(d.GetUTCFullYear())
	m, day := pad2(d.GetUTCMonth()+1), pad2(d.GetUTCDate())
	t := pad2(d.GetUTCHours()) + ":" + pad2(d.GetUTCMinutes())
	if dateFormat == "europe" {
		return day + "/" + m + "/" + y + " " + t
	}
	return y + "/" + m + "/" + day + " " + t
}

func utcMidnight(d js.Date) float64 {
	return js.UTC(d.GetUTCFullYear(), d.GetUTCMonth(), d.GetUTCDate(), 0, 0, 0, 0).SetUTCFullYear(d.GetUTCFullYear()).Time()
}

type formattedDate struct{ text, role string }

// formatTaskDate buckets a date by calendar day relative to now.
func formatTaskDate(date *js.Date, now js.Date, completed bool, dateFormat string) *formattedDate {
	if date == nil {
		return nil
	}
	d := *date
	if completed {
		return &formattedDate{absoluteDateTime(d, dateFormat), "text-secondary"}
	}
	const day = 86_400_000.0
	days := int(jsRound((utcMidnight(d) - utcMidnight(now)) / day))
	switch {
	case days == 0:
		return &formattedDate{"today", "date-today"}
	case days == 1:
		return &formattedDate{"tomorrow", "date-tomorrow"}
	case days == -1:
		return &formattedDate{"yesterday", "date-yesterday"}
	case days <= -2:
		return &formattedDate{strconv.Itoa(-days) + " days ago", "date-overdue"}
	case days >= 2 && days <= 7:
		return &formattedDate{"in " + strconv.Itoa(days) + " days", "text-secondary"}
	}
	nowDay := now.GetUTCDay()
	daysToNextMon := 8 - nowDay
	if nowDay == 0 {
		daysToNextMon = 1
	}
	nextWeekStart := utcMidnight(now) + float64(daysToNextMon)*day
	nextWeekEnd := nextWeekStart + 7*day
	if mid := utcMidnight(d); mid >= nextWeekStart && mid < nextWeekEnd {
		return &formattedDate{"next week", "text-secondary"}
	}
	month := monthAbbrs[d.GetUTCMonth()]
	if d.GetUTCFullYear() == now.GetUTCFullYear() {
		return &formattedDate{strconv.Itoa(d.GetUTCDate()) + " " + month, "text-secondary"}
	}
	if days <= 365 {
		return &formattedDate{month + " " + strconv.Itoa(d.GetUTCFullYear()), "text-secondary"}
	}
	return &formattedDate{strconv.Itoa(d.GetUTCFullYear()), "text-secondary"}
}

func jsRound(x float64) float64 {
	f := float64(int64(x))
	if x < 0 && f != x {
		f--
	}
	if x-f >= 0.5 {
		return f + 1
	}
	return f
}
