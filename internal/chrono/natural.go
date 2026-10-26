package chrono

import (
	"regexp"

	"github.com/costaluu/taskthing/internal/js"
)

// ── natural-language date parsing (date-parse.ts) ────────────────────────────
//
// Wraps chrono with two corrections it doesn't make on its own: the "europe"
// dateFormat reads slash dates day-first (chrono's en.GB), and a date given
// without a time is pinned to that calendar date at UTC midnight rather than
// inheriting the reference instant's time of day.

var (
	pastIntent = regexp.MustCompile(`(?i)\b(last|yesterday|ago|previous)\b`)
	hasSlash   = regexp.MustCompile(`\d{1,2}/\d{1,2}`)
)

// ParseNaturalDate resolves `input` against `now`, or reports no date.
func ParseNaturalDate(input string, now js.Date, dateFormat string) (js.Date, bool) {
	engine := Casual
	if dateFormat == "europe" && hasSlash.MatchString(input) {
		engine = GB
	}
	results := engine.Parse(input, now, Options{ForwardDate: !pastIntent.MatchString(input)})
	if len(results) == 0 {
		return js.Date{}, false
	}
	start := results[0].Start
	if start.IsCertain("hour") {
		return start.Date(), true
	}
	return js.UTC(start.get("year"), start.get("month")-1, start.get("day"), 0, 0, 0, 0), true
}
