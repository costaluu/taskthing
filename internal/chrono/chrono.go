package chrono

import (
	"sort"
	"strings"
	"sync"

	"github.com/dlclark/regexp2"

	"github.com/costaluu/taskthing/internal/js"
)

// Options is chrono's ParsingOption, as far as taskthing uses it.
type Options struct {
	ForwardDate bool
}

type context struct {
	text      []rune
	option    Options
	reference *Reference
	refDate   js.Date
}

func (c *context) createParsingComponents() *Components { return newComponents(c.reference, nil) }

func (c *context) createParsingResult(index int, text string, start *Components) *Result {
	return newResult(c.reference, index, text, start, nil)
}

// match is a RegExp exec result: index into the searched text, groups with
// whether each participated (an unmatched group is `undefined` in JS).
type match struct {
	index  int
	groups []string
	ok     []bool
}

func (m *match) g(i int) string { return m.groups[i] }

// has is JS truthiness of match[i]: participated and non-empty.
func (m *match) has(i int) bool { return i < len(m.ok) && m.ok[i] && m.groups[i] != "" }

// defined is `match[i] != null`.
func (m *match) defined(i int) bool { return i < len(m.ok) && m.ok[i] }

var reCache sync.Map

// re compiles a JS regex source with JS semantics; flags is "" or "i".
func re(source, flags string) *regexp2.Regexp {
	key := flags + "/" + source
	if v, ok := reCache.Load(key); ok {
		return v.(*regexp2.Regexp)
	}
	opts := regexp2.RegexOptions(regexp2.ECMAScript)
	if strings.Contains(flags, "i") {
		opts |= regexp2.IgnoreCase
	}
	r := regexp2.MustCompile(source, opts)
	reCache.Store(key, r)
	return r
}

func execRe(r *regexp2.Regexp, text []rune) *match {
	m, err := r.FindRunesMatch(text)
	if err != nil || m == nil {
		return nil
	}
	groups := m.Groups()
	out := &match{index: m.Index, groups: make([]string, len(groups)), ok: make([]bool, len(groups))}
	for i, g := range groups {
		if len(g.Captures) > 0 {
			out.groups[i] = g.String()
			out.ok[i] = true
		}
	}
	return out
}

// test is `text.match(regex) != null` / `regex.test(text)`.
func test(source, flags, text string) bool {
	return execRe(re(source, flags), []rune(text)) != nil
}

type parser interface {
	pattern(ctx *context) *regexp2.Regexp
	// extract returns nil, *Components, *Result, or *plainComponents.
	extract(ctx *context, m *match) any
}

// plainComponents is a parser returning a bare `{ day, month, year }` object.
type plainComponents struct {
	values map[string]int
	order  []string
}

type refiner interface {
	refine(ctx *context, results []*Result) []*Result
}

// Chrono is a configured parser.
type Chrono struct {
	parsers  []parser
	refiners []refiner
}

// Parse is chrono.parse(text, ref, option).
func (ch *Chrono) Parse(text string, ref js.Date, option Options) []*Result {
	ctx := &context{text: []rune(text), option: option, reference: referenceFromDate(ref), refDate: ref}
	var results []*Result
	for _, p := range ch.parsers {
		results = append(results, executeParser(ctx, p)...)
	}
	sort.SliceStable(results, func(i, j int) bool { return results[i].Index < results[j].Index })
	for _, r := range ch.refiners {
		results = r.refine(ctx, results)
	}
	return results
}

func executeParser(ctx *context, p parser) []*Result {
	var results []*Result
	pattern := p.pattern(ctx)
	original := ctx.text
	remaining := ctx.text
	m := execRe(pattern, remaining)
	for m != nil {
		m.index = m.index + len(original) - len(remaining)
		res := p.extract(ctx, m)
		if res == nil {
			remaining = original[min(m.index+1, len(original)):]
			m = execRe(pattern, remaining)
			continue
		}
		var parsed *Result
		switch r := res.(type) {
		case *Result:
			if r == nil {
				remaining = original[min(m.index+1, len(original)):]
				m = execRe(pattern, remaining)
				continue
			}
			parsed = r
		case *Components:
			if r == nil {
				remaining = original[min(m.index+1, len(original)):]
				m = execRe(pattern, remaining)
				continue
			}
			parsed = ctx.createParsingResult(m.index, m.g(0), nil)
			parsed.Start = r
		case *plainComponents:
			parsed = ctx.createParsingResult(m.index, m.g(0), newComponents(ctx.reference, r.values, r.order...))
		}
		results = append(results, parsed)
		remaining = original[min(parsed.Index+runeLen(parsed.Text), len(original)):]
		m = execRe(pattern, remaining)
	}
	return results
}

// ── word-boundary parsers (AbstractParserWithWordBoundaryChecking) ───────────

type wordBoundary struct {
	inner        func(ctx *context) (string, string)
	innerExtract func(ctx *context, m *match) any
}

func (w wordBoundary) pattern(ctx *context) *regexp2.Regexp {
	source, flags := w.inner(ctx)
	return re(`(\W|^)`+source, flags)
}

func (w wordBoundary) extract(ctx *context, m *match) any {
	header := ""
	if m.ok[1] {
		header = m.groups[1]
	}
	m.index += runeLen(header)
	m.groups[0] = string([]rune(m.groups[0])[runeLen(header):])
	m.ok[0] = true
	for i := 2; i < len(m.groups); i++ {
		m.groups[i-1] = m.groups[i]
		m.ok[i-1] = m.ok[i]
	}
	return w.innerExtract(ctx, m)
}

func fixed(source, flags string) func(*context) (string, string) {
	return func(*context) (string, string) { return source, flags }
}

// ── merging refiners (abstractRefiners.js) ───────────────────────────────────

type mergingRefiner struct {
	shouldMerge func(between string, cur, next *Result, ctx *context) bool
	merge       func(between string, cur, next *Result, ctx *context) *Result
}

func (mr mergingRefiner) refine(ctx *context, results []*Result) []*Result {
	if len(results) < 2 {
		return results
	}
	var merged []*Result
	cur := results[0]
	for i := 1; i < len(results); i++ {
		next := results[i]
		between := substring(ctx.text, cur.Index+runeLen(cur.Text), next.Index)
		if !mr.shouldMerge(between, cur, next, ctx) {
			merged = append(merged, cur)
			cur = next
		} else {
			cur = mr.merge(between, cur, next, ctx)
		}
	}
	if cur != nil {
		merged = append(merged, cur)
	}
	return merged
}

type filterRefiner func(ctx *context, r *Result) bool

func (f filterRefiner) refine(ctx *context, results []*Result) []*Result {
	var out []*Result
	for _, r := range results {
		if f(ctx, r) {
			out = append(out, r)
		}
	}
	return out
}

type refinerFunc func(ctx *context, results []*Result) []*Result

func (f refinerFunc) refine(ctx *context, results []*Result) []*Result { return f(ctx, results) }

// ── common refiners ──────────────────────────────────────────────────────────

var overlapRemoval = refinerFunc(func(ctx *context, results []*Result) []*Result {
	if len(results) < 2 {
		return results
	}
	var filtered []*Result
	prev := results[0]
	for i := 1; i < len(results); i++ {
		r := results[i]
		if r.Index >= prev.Index+runeLen(prev.Text) {
			filtered = append(filtered, prev)
			prev = r
			continue
		}
		if runeLen(r.Text) > runeLen(prev.Text) {
			prev = r
		}
	}
	if prev != nil {
		filtered = append(filtered, prev)
	}
	return filtered
})

var extractTimezoneOffset = refinerFunc(func(ctx *context, results []*Result) []*Result {
	pattern := re(`^\s*(?:\(?(?:GMT|UTC)\s?)?([+-])(\d{1,2})(?::?(\d{2}))?\)?`, "i")
	for _, result := range results {
		if result.Start.IsCertain("timezoneOffset") {
			continue
		}
		suffix := []rune(substring(ctx.text, result.Index+runeLen(result.Text), len(ctx.text)))
		m := execRe(pattern, suffix)
		if m == nil {
			continue
		}
		hourOffset := int(js.ParseInt(m.g(2)))
		minuteText := "0"
		if m.has(3) {
			minuteText = m.g(3)
		}
		minuteOffset := int(js.ParseInt(minuteText))
		offset := hourOffset*60 + minuteOffset
		if offset > 14*60 {
			continue
		}
		if m.g(1) == "-" {
			offset = -offset
		}
		if result.End != nil {
			result.End.assign("timezoneOffset", offset)
		}
		result.Start.assign("timezoneOffset", offset)
		result.Text += m.g(0)
	}
	return results
})

var extractTimezoneAbbr = refinerFunc(func(ctx *context, results []*Result) []*Result {
	pattern := re(`^\s*,?\s*\(?([A-Z]{2,4})\)?(?=\W|$)`, "i")
	for _, result := range results {
		suffix := []rune(substring(ctx.text, result.Index+runeLen(result.Text), len(ctx.text)))
		m := execRe(pattern, suffix)
		if m == nil {
			continue
		}
		abbr := strings.ToUpper(m.g(1))
		refDate := result.Start.Date()
		extracted, ok := toTimezoneOffset(abbr, refDate)
		if !ok {
			continue
		}
		if current, has := result.Start.Get("timezoneOffset"); has && extracted != current {
			if result.Start.IsCertain("timezoneOffset") {
				continue
			}
			if abbr != m.g(1) {
				continue
			}
		}
		if result.Start.isOnlyDate() && abbr != m.g(1) {
			continue
		}
		result.Text += m.g(0)
		if !result.Start.IsCertain("timezoneOffset") {
			result.Start.assign("timezoneOffset", extracted)
		}
		if result.End != nil && !result.End.IsCertain("timezoneOffset") {
			result.End.assign("timezoneOffset", extracted)
		}
	}
	return results
})

var mergeWeekdayComponent = mergingRefiner{
	shouldMerge: func(between string, cur, next *Result, _ *context) bool {
		weekdayThenNormalDate := cur.Start.isOnlyWeekdayComponent() && !cur.Start.IsCertain("hour") && next.Start.IsCertain("day")
		return weekdayThenNormalDate && test(`^,?\s*$`, "", between)
	},
	merge: func(between string, cur, next *Result, _ *context) *Result {
		n := next.clone()
		n.Index = cur.Index
		n.Text = cur.Text + between + n.Text
		n.Start.assign("weekday", cur.Start.get("weekday"))
		if n.End != nil {
			n.End.assign("weekday", cur.Start.get("weekday"))
		}
		return n
	},
}

var forwardDate = refinerFunc(func(ctx *context, results []*Result) []*Result {
	if !ctx.option.ForwardDate {
		return results
	}
	for _, result := range results {
		refDate := ctx.reference.dateWithAdjustedTimezone()
		if result.Start.isOnlyTime() && ctx.reference.instant.Time() > result.Start.Date().Time() {
			following := ctx.reference.dateWithAdjustedTimezone()
			following = following.SetDate(following.GetDate() + 1)
			implySimilarDate(result.Start, following)
			if result.End != nil && result.End.isOnlyTime() {
				implySimilarDate(result.End, following)
				if result.Start.Date().Time() > result.End.Date().Time() {
					following = following.SetDate(following.GetDate() + 1)
					implySimilarDate(result.End, following)
				}
			}
		}
		if result.Start.isOnlyWeekdayComponent() && refDate.Time() > result.Start.Date().Time() {
			days := getDaysForwardToWeekday(refDate, result.Start.get("weekday"))
			if days == 0 {
				days = 7
			}
			implySimilarDate(result.Start, addDuration(refDate, duration{"day": float64(days)}))
			if result.End != nil && result.Start.Date().Time() > result.End.Date().Time() {
				days := getDaysForwardToWeekday(refDate, result.Start.get("weekday"))
				if days == 0 {
					days = 7
				}
				implySimilarDate(result.End, addDuration(refDate, duration{"day": float64(days)}))
			}
		}
		if result.Start.isDateWithUnknownYear() && refDate.Time() > result.Start.Date().Time() {
			for i := 0; i < 3 && refDate.Time() > result.Start.Date().Time(); i++ {
				result.Start.imply("year", result.Start.get("year")+1)
				if result.End != nil && !result.End.IsCertain("year") {
					result.End.imply("year", result.End.get("year")+1)
				}
			}
		}
	}
	return results
})

func unlikelyFormatFilter(strict bool) filterRefiner {
	return func(ctx *context, r *Result) bool {
		if test(`^\d*(\.\d*)?$`, "", strings.Replace(r.Text, " ", "", 1)) {
			return false
		}
		if !r.Start.isValidDate() {
			return false
		}
		if r.End != nil && !r.End.isValidDate() {
			return false
		}
		if strict && r.Start.isOnlyWeekdayComponent() {
			return false
		}
		return true
	}
}

// ── merging calculations (calculation/mergingCalculation.js) ─────────────────

func mergeDateTimeResult(dateResult, timeResult *Result) *Result {
	result := dateResult.clone()
	result.Start = mergeDateTimeComponent(dateResult.Start, timeResult.Start)
	if dateResult.End != nil || timeResult.End != nil {
		endDate := dateResult.End
		if endDate == nil {
			endDate = dateResult.Start
		}
		endTime := timeResult.End
		if endTime == nil {
			endTime = timeResult.Start
		}
		endDateTime := mergeDateTimeComponent(endDate, endTime)
		if dateResult.End == nil && endDateTime.Date().Time() < result.Start.Date().Time() {
			nextDay := endDateTime.Date()
			nextDay = nextDay.SetDate(nextDay.GetDate() + 1)
			if endDateTime.IsCertain("day") {
				assignSimilarDate(endDateTime, nextDay)
			} else {
				implySimilarDate(endDateTime, nextDay)
			}
		}
		result.End = endDateTime
	}
	return result
}

func mergeDateTimeComponent(dateC, timeC *Components) *Components {
	dt := dateC.clone()
	if timeC.IsCertain("hour") {
		dt.assign("hour", timeC.get("hour"))
		dt.assign("minute", timeC.get("minute"))
		if timeC.IsCertain("second") {
			dt.assign("second", timeC.get("second"))
			if timeC.IsCertain("millisecond") {
				dt.assign("millisecond", timeC.get("millisecond"))
			} else {
				dt.imply("millisecond", timeC.get("millisecond"))
			}
		} else {
			dt.imply("second", timeC.get("second"))
			dt.imply("millisecond", timeC.get("millisecond"))
		}
	} else {
		dt.imply("hour", timeC.get("hour"))
		dt.imply("minute", timeC.get("minute"))
		dt.imply("second", timeC.get("second"))
		dt.imply("millisecond", timeC.get("millisecond"))
	}
	if timeC.IsCertain("timezoneOffset") {
		dt.assign("timezoneOffset", timeC.get("timezoneOffset"))
	}
	_, dateHasMeridiem := dateC.Get("meridiem")
	casual := false
	for _, t := range dateC.tags {
		if strings.HasPrefix(t, "casualReference/") {
			casual = true
		}
	}
	dateHasMeaningfulMeridiem := dateHasMeridiem && (dateC.IsCertain("meridiem") || casual)
	if timeC.IsCertain("meridiem") {
		dt.assign("meridiem", timeC.get("meridiem"))
	} else if _, ok := timeC.Get("meridiem"); ok && !dateHasMeaningfulMeridiem {
		dt.imply("meridiem", timeC.get("meridiem"))
	}
	if m, ok := dt.Get("meridiem"); ok && m == PM && dt.get("hour") < 12 {
		if timeC.IsCertain("hour") {
			dt.assign("hour", dt.get("hour")+12)
		} else {
			dt.imply("hour", dt.get("hour")+12)
		}
	}
	dt.addTags(dateC.tags)
	dt.addTags(timeC.tags)
	return dt
}

func mergeDateTimeRefiner(between string) mergingRefiner {
	return mergingRefiner{
		shouldMerge: func(b string, cur, next *Result, _ *context) bool {
			return ((cur.Start.isOnlyDate() && next.Start.isOnlyTime()) || (next.Start.isOnlyDate() && cur.Start.isOnlyTime())) &&
				test(between, "", b)
		},
		merge: func(b string, cur, next *Result, _ *context) *Result {
			var r *Result
			if cur.Start.isOnlyDate() {
				r = mergeDateTimeResult(cur, next)
			} else {
				r = mergeDateTimeResult(next, cur)
			}
			r.Index = cur.Index
			r.Text = cur.Text + b + next.Text
			return r
		},
	}
}

func mergeDateRangeRefiner(between, flags string) mergingRefiner {
	return mergingRefiner{
		shouldMerge: func(b string, cur, next *Result, _ *context) bool {
			return cur.End == nil && next.End == nil && test(between, flags, b)
		},
		merge: func(b string, from, to *Result, _ *context) *Result {
			if !from.Start.isOnlyWeekdayComponent() && !to.Start.isOnlyWeekdayComponent() {
				for _, key := range to.Start.certainComponents() {
					if !from.Start.IsCertain(key) {
						from.Start.imply(key, to.Start.get(key))
					}
				}
				for _, key := range from.Start.certainComponents() {
					if !to.Start.IsCertain(key) {
						to.Start.imply(key, from.Start.get(key))
					}
				}
			}
			if from.Start.Date().Time() > to.Start.Date().Time() {
				fromDate := from.Start.Date()
				toDate := to.Start.Date()
				switch {
				case to.Start.isOnlyWeekdayComponent() && addDuration(toDate, duration{"day": 7}).Time() > fromDate.Time():
					toDate = addDuration(toDate, duration{"day": 7})
					to.Start.imply("day", toDate.GetDate())
					to.Start.imply("month", toDate.GetMonth()+1)
					to.Start.imply("year", toDate.GetFullYear())
				case from.Start.isOnlyWeekdayComponent() && addDuration(fromDate, duration{"day": -7}).Time() < toDate.Time():
					fromDate = addDuration(fromDate, duration{"day": -7})
					from.Start.imply("day", fromDate.GetDate())
					from.Start.imply("month", fromDate.GetMonth()+1)
					from.Start.imply("year", fromDate.GetFullYear())
				case to.Start.isDateWithUnknownYear() && addDuration(toDate, duration{"year": 1}).Time() > fromDate.Time():
					toDate = addDuration(toDate, duration{"year": 1})
					to.Start.imply("year", toDate.GetFullYear())
				case from.Start.isDateWithUnknownYear() && addDuration(fromDate, duration{"year": -1}).Time() < toDate.Time():
					fromDate = addDuration(fromDate, duration{"year": -1})
					from.Start.imply("year", fromDate.GetFullYear())
				default:
					from, to = to, from
				}
			}
			result := from.clone()
			result.Start = from.Start
			result.End = to.Start
			result.Index = min(from.Index, to.Index)
			if from.Index < to.Index {
				result.Text = from.Text + b + to.Text
			} else {
				result.Text = to.Text + b + from.Text
			}
			return result
		},
	}
}

// ── common/casualReferences.js ───────────────────────────────────────────────

func casualNow(ref *Reference) *Components {
	target := ref.dateWithAdjustedTimezone()
	c := newComponents(ref, nil)
	assignSimilarDate(c, target)
	assignSimilarTime(c, target)
	c.assign("timezoneOffset", ref.getTimezoneOffset())
	c.addTag("casualReference/now")
	return c
}

func casualToday(ref *Reference) *Components {
	target := ref.dateWithAdjustedTimezone()
	c := newComponents(ref, nil)
	assignSimilarDate(c, target)
	implySimilarTime(c, target)
	c.delete("meridiem")
	c.addTag("casualReference/today")
	return c
}

func theDayAfter(ref *Reference, n int) *Components {
	target := ref.dateWithAdjustedTimezone()
	c := newComponents(ref, nil)
	next := target.SetDate(target.GetDate() + n)
	assignSimilarDate(c, next)
	implySimilarTime(c, next)
	c.delete("meridiem")
	return c
}

func casualTonight(ref *Reference) *Components {
	target := ref.dateWithAdjustedTimezone()
	c := newComponents(ref, nil)
	assignSimilarDate(c, target)
	c.imply("hour", 22)
	c.imply("meridiem", PM)
	c.addTag("casualReference/tonight")
	return c
}

func casualEvening(ref *Reference) *Components {
	c := newComponents(ref, nil)
	c.imply("meridiem", PM)
	c.imply("hour", 20)
	c.addTag("casualReference/evening")
	return c
}

func casualMidnight(ref *Reference) *Components {
	c := newComponents(ref, nil)
	if ref.dateWithAdjustedTimezone().GetHours() > 2 {
		c.addDurationAsImplied(duration{"day": 1})
	}
	c.assign("hour", 0)
	c.imply("minute", 0)
	c.imply("second", 0)
	c.imply("millisecond", 0)
	c.addTag("casualReference/midnight")
	return c
}

func casualPartOfDay(ref *Reference, meridiem, hour int, tag string) *Components {
	c := newComponents(ref, nil)
	c.imply("meridiem", meridiem)
	c.imply("hour", hour)
	c.imply("minute", 0)
	c.imply("second", 0)
	c.imply("millisecond", 0)
	c.addTag(tag)
	return c
}

func casualNoon(ref *Reference) *Components {
	c := newComponents(ref, nil)
	c.imply("meridiem", AM)
	c.assign("hour", 12)
	c.imply("minute", 0)
	c.imply("second", 0)
	c.imply("millisecond", 0)
	c.addTag("casualReference/noon")
	return c
}

// ── calculation/weekdays.js and years.js ─────────────────────────────────────

func createParsingComponentsAtWeekday(ref *Reference, weekday int, modifier string) *Components {
	refDate := ref.dateWithAdjustedTimezone()
	days := getDaysToWeekday(refDate, weekday, modifier)
	c := newComponents(ref, nil)
	c = c.addDurationAsImplied(duration{"day": float64(days)})
	c.assign("weekday", weekday)
	return c
}

func getDaysToWeekday(refDate js.Date, weekday int, modifier string) int {
	refWeekday := refDate.GetDay()
	switch modifier {
	case "this":
		return getDaysForwardToWeekday(refDate, weekday)
	case "last":
		return getBackwardDaysToWeekday(refDate, weekday)
	case "next":
		if refWeekday == 0 {
			if weekday == 0 {
				return 7
			}
			return weekday
		}
		if refWeekday == 6 {
			if weekday == 6 {
				return 7
			}
			if weekday == 0 {
				return 8
			}
			return 1 + weekday
		}
		if weekday < refWeekday && weekday != 0 {
			return getDaysForwardToWeekday(refDate, weekday)
		}
		return getDaysForwardToWeekday(refDate, weekday) + 7
	}
	backward := getBackwardDaysToWeekday(refDate, weekday)
	forward := getDaysForwardToWeekday(refDate, weekday)
	if forward < -backward {
		return forward
	}
	return backward
}

func getDaysForwardToWeekday(refDate js.Date, weekday int) int {
	count := weekday - refDate.GetDay()
	if count < 0 {
		count += 7
	}
	return count
}

func getBackwardDaysToWeekday(refDate js.Date, weekday int) int {
	count := weekday - refDate.GetDay()
	if count >= 0 {
		count -= 7
	}
	return count
}

func findMostLikelyADYear(year int) int {
	if year < 100 {
		if year > 50 {
			return year + 1900
		}
		return year + 2000
	}
	return year
}

func findYearClosestToRef(refDate js.Date, day, month int) int {
	date := refDate.SetMonth(month - 1)
	date = date.SetDate(day)
	nextYear := addDuration(date, duration{"year": 1})
	lastYear := addDuration(date, duration{"year": -1})
	abs := func(x float64) float64 {
		if x < 0 {
			return -x
		}
		return x
	}
	if abs(nextYear.Time()-refDate.Time()) < abs(date.Time()-refDate.Time()) {
		date = nextYear
	} else if abs(lastYear.Time()-refDate.Time()) < abs(date.Time()-refDate.Time()) {
		date = lastYear
	}
	return date.GetFullYear()
}
