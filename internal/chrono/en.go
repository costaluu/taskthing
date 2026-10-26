package chrono

import (
	"math"
	"regexp"
	"sort"
	"strings"

	"github.com/dlclark/regexp2"

	"github.com/costaluu/taskthing/internal/js"
)

// ── dictionaries (locales/en/constants.js), in declaration order ─────────────

type entry[V any] struct {
	key   string
	value V
}

type dict[V any] []entry[V]

func (d dict[V]) get(k string) (V, bool) {
	for _, e := range d {
		if e.key == k {
			return e.value, true
		}
	}
	var zero V
	return zero, false
}

func ints(kv ...any) dict[int] {
	var d dict[int]
	for i := 0; i < len(kv); i += 2 {
		d = append(d, entry[int]{kv[i].(string), kv[i+1].(int)})
	}
	return d
}

func strs(kv ...string) dict[string] {
	var d dict[string]
	for i := 0; i < len(kv); i += 2 {
		d = append(d, entry[string]{kv[i], kv[i+1]})
	}
	return d
}

// merge is `{ ...a, ...b }`: b's keys overwrite in place, new ones append.
func merge[V any](a, b dict[V]) dict[V] {
	out := append(dict[V](nil), a...)
	for _, e := range b {
		found := false
		for i := range out {
			if out[i].key == e.key {
				out[i].value = e.value
				found = true
			}
		}
		if !found {
			out = append(out, e)
		}
	}
	return out
}

var weekdayDictionary = ints(
	"sunday", 0, "sun", 0, "sun.", 0, "monday", 1, "mon", 1, "mon.", 1,
	"tuesday", 2, "tue", 2, "tue.", 2, "wednesday", 3, "wed", 3, "wed.", 3,
	"thursday", 4, "thurs", 4, "thurs.", 4, "thur", 4, "thur.", 4, "thu", 4, "thu.", 4,
	"friday", 5, "fri", 5, "fri.", 5, "saturday", 6, "sat", 6, "sat.", 6,
)

var fullMonthNameDictionary = ints(
	"january", 1, "february", 2, "march", 3, "april", 4, "may", 5, "june", 6,
	"july", 7, "august", 8, "september", 9, "october", 10, "november", 11, "december", 12,
)

var monthDictionary = merge(fullMonthNameDictionary, ints(
	"jan", 1, "jan.", 1, "feb", 2, "feb.", 2, "mar", 3, "mar.", 3, "apr", 4, "apr.", 4,
	"jun", 6, "jun.", 6, "jul", 7, "jul.", 7, "aug", 8, "aug.", 8, "sep", 9, "sep.", 9,
	"sept", 9, "sept.", 9, "oct", 10, "oct.", 10, "nov", 11, "nov.", 11, "dec", 12, "dec.", 12,
))

var integerWordDictionary = ints(
	"one", 1, "two", 2, "three", 3, "four", 4, "five", 5, "six", 6, "seven", 7,
	"eight", 8, "nine", 9, "ten", 10, "eleven", 11, "twelve", 12,
)

var ordinalWordDictionary = ints(
	"first", 1, "second", 2, "third", 3, "fourth", 4, "fifth", 5, "sixth", 6,
	"seventh", 7, "eighth", 8, "ninth", 9, "tenth", 10, "eleventh", 11, "twelfth", 12,
	"thirteenth", 13, "fourteenth", 14, "fifteenth", 15, "sixteenth", 16,
	"seventeenth", 17, "eighteenth", 18, "nineteenth", 19, "twentieth", 20,
	"twenty first", 21, "twenty-first", 21, "twenty second", 22, "twenty-second", 22,
	"twenty third", 23, "twenty-third", 23, "twenty fourth", 24, "twenty-fourth", 24,
	"twenty fifth", 25, "twenty-fifth", 25, "twenty sixth", 26, "twenty-sixth", 26,
	"twenty seventh", 27, "twenty-seventh", 27, "twenty eighth", 28, "twenty-eighth", 28,
	"twenty ninth", 29, "twenty-ninth", 29, "thirtieth", 30, "thirty first", 31, "thirty-first", 31,
)

var timeUnitDictionaryNoAbbr = strs(
	"second", "second", "seconds", "second", "minute", "minute", "minutes", "minute",
	"hour", "hour", "hours", "hour", "day", "day", "days", "day", "week", "week", "weeks", "week",
	"month", "month", "months", "month", "quarter", "quarter", "quarters", "quarter",
	"year", "year", "years", "year",
)

var timeUnitDictionary = merge(strs(
	"s", "second", "sec", "second", "second", "second", "seconds", "second",
	"m", "minute", "min", "minute", "mins", "minute", "minute", "minute", "minutes", "minute",
	"h", "hour", "hr", "hour", "hrs", "hour", "hour", "hour", "hours", "hour",
	"d", "day", "day", "day", "days", "day",
	"w", "week", "week", "week", "weeks", "week",
	"mo", "month", "mon", "month", "mos", "month", "month", "month", "months", "month",
	"qtr", "quarter", "quarter", "quarter", "quarters", "quarter",
	"y", "year", "yr", "year", "year", "year", "years", "year",
), timeUnitDictionaryNoAbbr)

// matchAnyPattern is utils/pattern.js's: longest terms first, dots escaped.
func matchAnyPattern[V any](d dict[V]) string {
	keys := make([]string, len(d))
	for i, e := range d {
		keys[i] = e.key
	}
	sort.SliceStable(keys, func(i, j int) bool { return len([]rune(keys[i])) > len([]rune(keys[j])) })
	return "(?:" + strings.ReplaceAll(strings.Join(keys, "|"), ".", `\.`) + ")"
}

var openParen = regexp.MustCompile(`\((?:[^?]|$)`)

func repeatedTimeunitPattern(prefix, single, connector string) string {
	noCapture := replaceOpenParens(single)
	return prefix + noCapture + "(?:" + connector + noCapture + "){0,10}"
}

// replaceOpenParens is `.replace(/\((?!\?)/g, "(?:")`.
func replaceOpenParens(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '(' && (i+1 >= len(s) || s[i+1] != '?') {
			b.WriteString("(?:")
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

var (
	numberPattern = `(?:` + matchAnyPattern(integerWordDictionary) + `|[0-9]+|[0-9]+\.[0-9]+|half(?:\s{0,2}an?)?|an?\b(?:\s{0,2}few)?|few|several|the|a?\s{0,2}couple\s{0,2}(?:of)?)`

	ordinalNumberPattern = `(?:` + matchAnyPattern(ordinalWordDictionary) + `|[0-9]{1,2}(?:st|nd|rd|th)?)`

	yearPattern = `(?:[1-9][0-9]{0,3}\s{0,2}(?:BE|AD|BC|BCE|CE)|[1-9][0-9]{3}|[0-9]{2}(?!\w|:\d|\s+(?:am|pm|o\s*clock|jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec)))`

	singleTimeUnitPattern       = `(` + numberPattern + `)\s{0,3}(` + matchAnyPattern(timeUnitDictionary) + `)`
	singleTimeUnitNoAbbrPattern = `(` + numberPattern + `)\s{0,3}(` + matchAnyPattern(timeUnitDictionaryNoAbbr) + `)`
	timeUnitConnectorPattern    = `\s{0,5},?(?:\s*and)?\s{0,5}`

	timeUnitsPattern       = repeatedTimeunitPattern(`(?:(?:about|around)\s{0,3})?`, singleTimeUnitPattern, timeUnitConnectorPattern)
	timeUnitsNoAbbrPattern = repeatedTimeunitPattern(`(?:(?:about|around)\s{0,3})?`, singleTimeUnitNoAbbrPattern, timeUnitConnectorPattern)
)

func parseNumberPattern(s string) float64 {
	num := strings.ToLower(s)
	if v, ok := integerWordDictionary.get(num); ok {
		return float64(v)
	}
	switch {
	case num == "a" || num == "an" || num == "the":
		return 1
	case strings.Contains(num, "few"):
		return 3
	case strings.Contains(num, "half"):
		return 0.5
	case strings.Contains(num, "couple"):
		return 2
	case strings.Contains(num, "several"):
		return 7
	}
	return js.ParseFloat(num)
}

var ordinalSuffix = regexp.MustCompile(`(?i)(?:st|nd|rd|th)$`)

func parseOrdinalNumberPattern(s string) int {
	num := strings.ToLower(s)
	if v, ok := ordinalWordDictionary.get(num); ok {
		return v
	}
	num = ordinalSuffix.ReplaceAllString(num, "")
	return int(js.ParseInt(num))
}

var (
	reBE   = regexp.MustCompile(`(?i)BE`)
	reBCE  = regexp.MustCompile(`(?i)BCE?`)
	reADCE = regexp.MustCompile(`(?i)(AD|CE)`)
)

func replaceFirst(r *regexp.Regexp, s string) string {
	loc := r.FindStringIndex(s)
	if loc == nil {
		return s
	}
	return s[:loc[0]] + s[loc[1]:]
}

func parseYear(s string) int {
	switch {
	case reBE.MatchString(s):
		return int(js.ParseInt(replaceFirst(reBE, s))) - 543
	case reBCE.MatchString(s):
		return -int(js.ParseInt(replaceFirst(reBCE, s)))
	case reADCE.MatchString(s):
		return int(js.ParseInt(replaceFirst(reADCE, s)))
	}
	return findMostLikelyADYear(int(js.ParseInt(s)))
}

// parseDuration collects every `<number> <unit>` fragment, or nil.
func parseDuration(text string) duration {
	fragments := duration{}
	single := re(singleTimeUnitPattern, "i")
	remaining := text
	m := execRe(single, []rune(remaining))
	for m != nil {
		if !test(`^[a-zA-Z]+$`, "", m.g(0)) {
			unit, _ := timeUnitDictionary.get(strings.ToLower(m.g(2)))
			fragments[unit] = parseNumberPattern(m.g(1))
		}
		remaining = js.Trim(string([]rune(remaining)[runeLen(m.g(0)):]))
		m = execRe(single, []rune(remaining))
	}
	if len(fragments) == 0 {
		return nil
	}
	return fragments
}

// ── parsers ──────────────────────────────────────────────────────────────────

func isoFormatParser() parser {
	pattern := `([0-9]{4})\-([0-9]{1,2})\-([0-9]{1,2})(?:T([0-9]{1,2}):([0-9]{1,2})(?::([0-9]{1,2})(?:\.(\d{1,4}))?)?(Z|([+-]\d{2}):?(\d{2})?)?)?(?=\W|$)`
	return wordBoundary{inner: fixed(pattern, "i"), innerExtract: func(ctx *context, m *match) any {
		c := newComponents(ctx.reference, map[string]int{
			"year":  int(js.ParseInt(m.g(1))),
			"month": int(js.ParseInt(m.g(2))),
			"day":   int(js.ParseInt(m.g(3))),
		}, "year", "month", "day")
		if m.defined(4) {
			c.assign("hour", int(js.ParseInt(m.g(4))))
			c.assign("minute", int(js.ParseInt(m.g(5))))
			if m.defined(6) {
				c.assign("second", int(js.ParseInt(m.g(6))))
			}
			if m.defined(7) {
				c.assign("millisecond", int(js.ParseInt(m.g(7))))
			}
			if m.defined(8) {
				offset := 0
				if m.has(9) {
					hourOffset := int(js.ParseInt(m.g(9)))
					minuteOffset := 0
					if m.defined(10) {
						minuteOffset = int(js.ParseInt(m.g(10)))
					}
					offset = hourOffset * 60
					if offset < 0 {
						offset -= minuteOffset
					} else {
						offset += minuteOffset
					}
				}
				c.assign("timezoneOffset", offset)
			}
		}
		return c.addTag("parser/ISOFormatParser")
	}}
}

type slashDateFormatParser struct{ monthGroup, dayGroup int }

func newSlashDateFormatParser(littleEndian bool) parser {
	if littleEndian {
		return slashDateFormatParser{monthGroup: 3, dayGroup: 2}
	}
	return slashDateFormatParser{monthGroup: 2, dayGroup: 3}
}

func (slashDateFormatParser) pattern(*context) *regexp2.Regexp {
	return re(`([^\d]|^)([0-3]{0,1}[0-9]{1})[\/\.\-]([0-3]{0,1}[0-9]{1})(?:[\/\.\-]([0-9]{4}|[0-9]{2}))?(\W|$)`, "i")
}

func (p slashDateFormatParser) extract(ctx *context, m *match) any {
	index := m.index + runeLen(m.g(1))
	indexEnd := m.index + runeLen(m.g(0)) - runeLen(m.g(5))
	if index > 0 {
		if test(`\d/?$`, "", substring(ctx.text, 0, index)) {
			return nil
		}
	}
	if indexEnd < len(ctx.text) {
		if test(`^/?\d`, "", substring(ctx.text, indexEnd, len(ctx.text))) {
			return nil
		}
	}
	text := substring(ctx.text, index, indexEnd)
	if test(`^\d\.\d$`, "", text) || test(`^\d\.\d{1,2}\.\d{1,2}\s*$`, "", text) {
		return nil
	}
	if !m.has(4) && !strings.Contains(text, "/") {
		return nil
	}
	result := ctx.createParsingResult(index, text, nil)
	month := int(js.ParseInt(m.g(p.monthGroup)))
	day := int(js.ParseInt(m.g(p.dayGroup)))
	if month < 1 || month > 12 {
		if month > 12 {
			if day >= 1 && day <= 12 && month <= 31 {
				day, month = month, day
			} else {
				return nil
			}
		}
	}
	if day < 1 || day > 31 {
		return nil
	}
	result.Start.assign("day", day)
	result.Start.assign("month", month)
	if m.has(4) {
		result.Start.assign("year", findMostLikelyADYear(int(js.ParseInt(m.g(4)))))
	} else {
		result.Start.imply("year", findYearClosestToRef(ctx.refDate, day, month))
	}
	return result.addTag("parser/SlashDateFormatParser")
}

func timeUnitWithinFormatParser() parser {
	optional := `(?:(?:within|in|for)\s*)?(?:(?:about|around|roughly|approximately|just)\s*(?:~\s*)?)?(` + timeUnitsPattern + `)(?=\W|$)`
	prefixed := `(?:within|in|for)\s*(?:(?:about|around|roughly|approximately|just)\s*(?:~\s*)?)?(` + timeUnitsPattern + `)(?=\W|$)`
	return wordBoundary{
		inner: func(ctx *context) (string, string) {
			if ctx.option.ForwardDate {
				return optional, "i"
			}
			return prefixed, "i"
		},
		innerExtract: func(ctx *context, m *match) any {
			if test(`^for\s*the\s*\w+`, "", m.g(0)) {
				return nil
			}
			units := parseDuration(m.g(1))
			if units == nil {
				return nil
			}
			return createRelativeFromReference(ctx.reference, units)
		},
	}
}

func monthNameLittleEndianParser() parser {
	pattern := `(?:on\s{0,3})?(` + ordinalNumberPattern + `)(?:\s{0,3}(?:to|\-|\–|until|through|till)\s{0,3}(` + ordinalNumberPattern + `))?(?:-|/|\s{0,3}(?:of)?\s{0,3})(` + matchAnyPattern(monthDictionary) + `)(?:(?:-|/|,?\s{0,3})(` + yearPattern + `(?!\w)))?(?=\W|$)`
	return wordBoundary{inner: fixed(pattern, "i"), innerExtract: func(ctx *context, m *match) any {
		result := ctx.createParsingResult(m.index, m.g(0), nil)
		month, _ := monthDictionary.get(strings.ToLower(m.g(3)))
		day := parseOrdinalNumberPattern(m.g(1))
		if day > 31 {
			m.index = m.index + runeLen(m.g(1))
			return nil
		}
		result.Start.assign("month", month)
		result.Start.assign("day", day)
		if m.has(4) {
			result.Start.assign("year", parseYear(m.g(4)))
		} else {
			result.Start.imply("year", findYearClosestToRef(ctx.refDate, day, month))
		}
		if m.has(2) {
			end := parseOrdinalNumberPattern(m.g(2))
			result.End = result.Start.clone()
			result.End.assign("day", end)
		}
		return result
	}}
}

func monthNameMiddleEndianParser(shouldSkipYearLikeDate bool) parser {
	pattern := `(` + matchAnyPattern(monthDictionary) + `)(?:-|/|\s*,?\s*)(` + ordinalNumberPattern + `)(?!\s*(?:am|pm))\s*(?:(?:to|\-)\s*(` + ordinalNumberPattern + `)\s*)?(?:(?:-|/|\s*,\s*|\s+)(` + yearPattern + `))?(?=\W|$)(?!\:\d)`
	return wordBoundary{inner: fixed(pattern, "i"), innerExtract: func(ctx *context, m *match) any {
		month, _ := monthDictionary.get(strings.ToLower(m.g(1)))
		day := parseOrdinalNumberPattern(m.g(2))
		if day > 31 {
			return nil
		}
		if shouldSkipYearLikeDate {
			if !m.has(3) && !m.has(4) && test(`^\d{2}$`, "", m.g(2)) {
				return nil
			}
		}
		c := newComponents(ctx.reference, map[string]int{"day": day, "month": month}, "day", "month").
			addTag("parser/ENMonthNameMiddleEndianParser")
		if m.has(4) {
			c.assign("year", parseYear(m.g(4)))
		} else {
			c.imply("year", findYearClosestToRef(ctx.refDate, day, month))
		}
		if !m.has(3) {
			return c
		}
		end := parseOrdinalNumberPattern(m.g(3))
		result := ctx.createParsingResult(m.index, m.g(0), nil)
		result.Start = c
		result.End = c.clone()
		result.End.assign("day", end)
		return result
	}}
}

func weekdayParser() parser {
	pattern := `(?:(?:\,|\(|\（)\s*)?(?:on\s*?)?(?:(this|last|past|next)\s*)?(` + matchAnyPattern(weekdayDictionary) + `|weekend|weekday)(?:\s*(?:\,|\)|\）))?(?:\s*(?:of\s*)?(this|last|past|next)\s*week)?(?=\W|$)`
	return wordBoundary{inner: fixed(pattern, "i"), innerExtract: func(ctx *context, m *match) any {
		modifierWord := ""
		if m.has(1) {
			modifierWord = m.g(1)
		} else if m.has(3) {
			modifierWord = m.g(3)
		}
		modifierWord = strings.ToLower(modifierWord)
		modifier := ""
		switch modifierWord {
		case "last", "past":
			modifier = "last"
		case "next":
			modifier = "next"
		case "this":
			modifier = "this"
		}
		word := strings.ToLower(m.g(2))
		var weekday int
		if v, ok := weekdayDictionary.get(word); ok {
			weekday = v
		} else if word == "weekend" {
			weekday = 6
			if modifier == "last" {
				weekday = 0
			}
		} else if word == "weekday" {
			refWeekday := ctx.reference.dateWithAdjustedTimezone().GetDay()
			if refWeekday == 0 || refWeekday == 6 {
				weekday = 1
				if modifier == "last" {
					weekday = 5
				}
			} else {
				weekday = refWeekday - 1
				if modifier == "last" {
					weekday--
				} else {
					weekday++
				}
				weekday = weekday%5 + 1
			}
		} else {
			return nil
		}
		return createParsingComponentsAtWeekday(ctx.reference, weekday, modifier)
	}}
}

func slashMonthFormatParser() parser {
	return wordBoundary{inner: fixed(`([0-9]|0[1-9]|1[012])/([0-9]{4})`, "i"), innerExtract: func(ctx *context, m *match) any {
		year := int(js.ParseInt(m.g(2)))
		month := int(js.ParseInt(m.g(1)))
		return ctx.createParsingComponents().imply("day", 1).assign("month", month).assign("year", year)
	}}
}

func timeUnitAgoFormatParser() parser {
	pattern := `(` + timeUnitsPattern + `)\s{0,5}(?:ago|before|earlier)(?=\W|$)`
	return wordBoundary{inner: fixed(pattern, "i"), innerExtract: func(ctx *context, m *match) any {
		d := parseDuration(m.g(1))
		if d == nil {
			return nil
		}
		return createRelativeFromReference(ctx.reference, reverseDuration(d))
	}}
}

func timeUnitLaterFormatParser() parser {
	pattern := `(` + timeUnitsPattern + `)\s{0,5}(?:later|after|from now|henceforth|forward|out)(?=(?:\W|$))`
	return wordBoundary{inner: fixed(pattern, "i"), innerExtract: func(ctx *context, m *match) any {
		d := parseDuration(m.g(1))
		if d == nil {
			return nil
		}
		return createRelativeFromReference(ctx.reference, d)
	}}
}

func yearMonthNameParser() parser {
	year := `(?:[1-9][0-9]{0,3}\s{0,2}(?:BE|AD|BC|BCE|CE)|[1-9][0-9]{3})`
	pattern := `(` + year + `)(?:\s*[-.\/,]?\s*|\s+of\s+)(` + matchAnyPattern(monthDictionary) + `)(?=[^\s\w]|\s+[^0-9]|\s+$|$)`
	return wordBoundary{inner: fixed(pattern, "i"), innerExtract: func(ctx *context, m *match) any {
		y := parseYear(m.g(1))
		month, _ := monthDictionary.get(strings.ToLower(m.g(2)))
		result := ctx.createParsingResult(m.index, m.g(0), nil)
		result.Start.imply("day", 1)
		result.Start.assign("month", month)
		result.Start.assign("year", y)
		result.Start.addTag("parser/ENYearMonthNameParser")
		return result
	}}
}

func yearMonthDayParser(strictMonthDateOrder bool) parser {
	pattern := `([0-9]{4})[-\.\/\s](?:(` + matchAnyPattern(monthDictionary) + `)|([0-9]{1,2}))[-\.\/\s]([0-9]{1,2})(?=\W|$)`
	return wordBoundary{inner: fixed(pattern, "i"), innerExtract: func(ctx *context, m *match) any {
		year := int(js.ParseInt(m.g(1)))
		day := int(js.ParseInt(m.g(4)))
		var month int
		if m.has(3) {
			month = int(js.ParseInt(m.g(3)))
		} else {
			month, _ = monthDictionary.get(strings.ToLower(m.g(2)))
		}
		if month < 1 || month > 12 {
			if strictMonthDateOrder {
				return nil
			}
			if day >= 1 && day <= 12 {
				month, day = day, month
			}
		}
		if day < 1 || day > 31 {
			return nil
		}
		return &plainComponents{values: map[string]int{"day": day, "month": month, "year": year}, order: []string{"day", "month", "year"}}
	}}
}

func casualDateParser() parser {
	return wordBoundary{inner: fixed(`(now|today|tonight|tomorrow|overmorrow|tmr|tmrw|yesterday|last\s*night)(?=\W|$)`, "i"), innerExtract: func(ctx *context, m *match) any {
		target := ctx.refDate
		lower := strings.ToLower(m.g(0))
		c := ctx.createParsingComponents()
		switch lower {
		case "now":
			c = casualNow(ctx.reference)
		case "today":
			c = casualToday(ctx.reference)
		case "yesterday":
			c = theDayAfter(ctx.reference, -1).addTag("casualReference/yesterday")
		case "tomorrow", "tmr", "tmrw":
			c = theDayAfter(ctx.reference, 1).addTag("casualReference/tomorrow")
		case "tonight":
			c = casualTonight(ctx.reference)
		case "overmorrow":
			c = theDayAfter(ctx.reference, 2)
		default:
			if test(`last\s*night`, "", lower) {
				if target.GetHours() > 6 {
					target = target.SetDate(target.GetDate() - 1)
				}
				assignSimilarDate(c, target)
				c.imply("hour", 0)
			}
		}
		c.addTag("parser/ENCasualDateParser")
		return c
	}}
}

func casualTimeParser() parser {
	return wordBoundary{inner: fixed(`(?:this)?\s{0,3}(morning|afternoon|evening|night|midnight|midday|noon)(?=\W|$)`, "i"), innerExtract: func(ctx *context, m *match) any {
		var c *Components
		switch strings.ToLower(m.g(1)) {
		case "afternoon":
			c = casualPartOfDay(ctx.reference, PM, 15, "casualReference/afternoon")
		case "evening", "night":
			c = casualEvening(ctx.reference)
		case "midnight":
			c = casualMidnight(ctx.reference)
		case "morning":
			c = casualPartOfDay(ctx.reference, AM, 6, "casualReference/morning")
		case "noon", "midday":
			c = casualNoon(ctx.reference)
		}
		if c == nil {
			return nil
		}
		return c.addTag("parser/ENCasualTimeParser")
	}}
}

func monthNameParser() parser {
	pattern := `((?:in)\s*)?(` + matchAnyPattern(monthDictionary) + `)\s*(?:(?:,|-|of)?\s*(` + yearPattern + `)?)?(?=[^\s\w]|\s+[^0-9]|\s+$|$)`
	return wordBoundary{inner: fixed(pattern, "i"), innerExtract: func(ctx *context, m *match) any {
		monthName := strings.ToLower(m.g(2))
		if _, full := fullMonthNameDictionary.get(monthName); runeLen(m.g(0)) <= 3 && !full {
			return nil
		}
		prefix := ""
		if m.has(1) {
			prefix = m.g(1)
		}
		start := m.index + runeLen(prefix)
		result := ctx.createParsingResult(start, substring(ctx.text, start, m.index+runeLen(m.g(0))), nil)
		result.Start.imply("day", 1)
		result.Start.addTag("parser/ENMonthNameParser")
		month, _ := monthDictionary.get(monthName)
		result.Start.assign("month", month)
		if m.has(3) {
			result.Start.assign("year", parseYear(m.g(3)))
		} else {
			result.Start.imply("year", findYearClosestToRef(ctx.refDate, 1, month))
		}
		return result
	}}
}

func relativeDateFormatParser() parser {
	pattern := `(this|last|past|next|after\s*this)\s*(` + matchAnyPattern(timeUnitDictionary) + `)(?=\s*)(?=\W|$)`
	return wordBoundary{inner: fixed(pattern, "i"), innerExtract: func(ctx *context, m *match) any {
		modifier := strings.ToLower(m.g(1))
		unitWord := strings.ToLower(m.g(2))
		unit, _ := timeUnitDictionary.get(unitWord)
		if modifier == "next" || strings.HasPrefix(modifier, "after") {
			return createRelativeFromReference(ctx.reference, duration{unit: 1})
		}
		if modifier == "last" || modifier == "past" {
			return createRelativeFromReference(ctx.reference, duration{unit: -1})
		}
		c := ctx.createParsingComponents()
		date := ctx.reference.instant
		switch {
		case strings.Contains(unitWord, "week"):
			date = date.SetDate(date.GetDate() - date.GetDay())
			c.imply("day", date.GetDate())
			c.imply("month", date.GetMonth()+1)
			c.imply("year", date.GetFullYear())
		case strings.Contains(unitWord, "month"):
			date = date.SetDate(1)
			c.imply("day", date.GetDate())
			c.assign("year", date.GetFullYear())
			c.assign("month", date.GetMonth()+1)
		case strings.Contains(unitWord, "year"):
			date = date.SetDate(1)
			date = date.SetMonth(0)
			c.imply("day", date.GetDate())
			c.imply("month", date.GetMonth()+1)
			c.assign("year", date.GetFullYear())
		}
		return c
	}}
}

func timeUnitCasualRelativeFormatParser() parser {
	pattern := `(this|last|past|next|after|\+|-)\s*(` + timeUnitsPattern + `)(?=\W|$)`
	return wordBoundary{inner: fixed(pattern, "i"), innerExtract: func(ctx *context, m *match) any {
		prefix := strings.ToLower(m.g(1))
		d := parseDuration(m.g(2))
		if d == nil {
			return nil
		}
		switch prefix {
		case "last", "past", "-":
			d = reverseDuration(d)
		}
		return createRelativeFromReference(ctx.reference, d)
	}}
}

// ── time expressions (AbstractTimeExpressionParser + ENTimeExpressionParser) ─

const (
	hourGroup        = 2
	minuteGroup      = 3
	secondGroup      = 4
	millisecondGroup = 5
	ampmGroup        = 6
)

type timeExpressionParser struct{ strict bool }

func (timeExpressionParser) primaryPattern() *regexp2.Regexp {
	prefix := `(?:(?:at|from)\s*)??`
	suffix := `(?:\s*(?:o\W*clock|at\s*night|in\s*the\s*(?:morning|afternoon)))?(?!/)(?=\W|$)`
	return re(`(^|\s|T|\b)`+prefix+`(\d{1,4})(?:(?:\.|:|：)(\d{1,2})(?:(?::|：)(\d{2})(?:\.(\d{1,6}))?)?)?(?:\s*(a\.m\.|p\.m\.|am?|pm?))?`+suffix, "i")
}

func (timeExpressionParser) followingPattern() *regexp2.Regexp {
	phase := `\s*(?:\-|\–|\~|\〜|to|until|through|till|\?)\s*`
	return re(`^(`+phase+`)(\d{1,4})(?:(?:\.|\:|\：)(\d{1,2})(?:(?:\.|\:|\：)(\d{1,2})(?:\.(\d{1,6}))?)?)?(?:\s*(a\.m\.|p\.m\.|am?|pm?))?(?!/)(?=\W|$)`, "i")
}

func (p timeExpressionParser) pattern(*context) *regexp2.Regexp { return p.primaryPattern() }

func (p timeExpressionParser) extract(ctx *context, m *match) any {
	start := p.extractPrimary(ctx, m)
	if start == nil {
		if test(`^\d{4}`, "", m.g(0)) {
			m.index += 4
			return nil
		}
		m.index += runeLen(m.g(0))
		return nil
	}
	index := m.index + runeLen(m.g(1))
	text := string([]rune(m.g(0))[runeLen(m.g(1)):])
	result := ctx.createParsingResult(index, text, start)
	m.index += runeLen(m.g(0))
	remaining := []rune(substring(ctx.text, m.index, len(ctx.text)))
	following := execRe(p.followingPattern(), remaining)
	if test(`^\d{3,4}`, "", text) && following != nil {
		if test(`^\s*([+-])\s*\d{2,4}$`, "", following.g(0)) {
			return nil
		}
		if test(`^\s*([+-])\s*\d{2}\W\d{2}`, "", following.g(0)) {
			return nil
		}
	}
	if following == nil || test(`^\s*([+-])\s*\d{3,4}$`, "", following.g(0)) {
		return p.checkWithoutFollowing(result)
	}
	result.End = p.extractFollowing(ctx, following, result)
	if result.End != nil {
		result.Text += following.g(0)
	}
	return p.checkWithFollowing(result)
}

func (p timeExpressionParser) extractPrimary(ctx *context, m *match) *Components {
	c := ctx.createParsingComponents()
	minute := 0
	meridiem := -1
	hour := int(js.ParseInt(m.g(hourGroup)))
	if hour > 100 {
		if runeLen(m.g(hourGroup)) == 4 && !m.defined(minuteGroup) && !m.has(ampmGroup) {
			return nil
		}
		if p.strict || m.defined(minuteGroup) {
			return nil
		}
		minute = hour % 100
		hour = hour / 100
	}
	if hour > 24 {
		return nil
	}
	if m.defined(minuteGroup) {
		if runeLen(m.g(minuteGroup)) == 1 && !m.has(ampmGroup) {
			return nil
		}
		minute = int(js.ParseInt(m.g(minuteGroup)))
	}
	if minute >= 60 {
		return nil
	}
	if hour > 12 {
		meridiem = PM
	}
	if m.defined(ampmGroup) {
		if hour > 12 {
			return nil
		}
		ampm := strings.ToLower(m.g(ampmGroup)[:1])
		if ampm == "a" {
			meridiem = AM
			if hour == 12 {
				hour = 0
			}
		}
		if ampm == "p" {
			meridiem = PM
			if hour != 12 {
				hour += 12
			}
		}
	}
	c.assign("hour", hour)
	c.assign("minute", minute)
	if meridiem != -1 {
		c.assign("meridiem", meridiem)
	} else if hour < 12 {
		c.imply("meridiem", AM)
	} else {
		c.imply("meridiem", PM)
	}
	if m.defined(millisecondGroup) {
		ms := int(js.ParseInt(string([]rune(m.g(millisecondGroup))[:min(3, runeLen(m.g(millisecondGroup)))])))
		if ms >= 1000 {
			return nil
		}
		c.assign("millisecond", ms)
	}
	if m.defined(secondGroup) {
		second := int(js.ParseInt(m.g(secondGroup)))
		if second >= 60 {
			return nil
		}
		c.assign("second", second)
	}

	// ENTimeExpressionParser's adjustments for "at night" / "in the morning|afternoon".
	whole := m.g(0)
	if strings.HasSuffix(whole, "night") {
		h := c.get("hour")
		if h >= 6 && h < 12 {
			c.assign("hour", h+12)
			c.assign("meridiem", PM)
		} else if h < 6 {
			c.assign("meridiem", AM)
		}
	}
	if strings.HasSuffix(whole, "afternoon") {
		c.assign("meridiem", PM)
		if h := c.get("hour"); h >= 0 && h <= 6 {
			c.assign("hour", h+12)
		}
	}
	if strings.HasSuffix(whole, "morning") {
		c.assign("meridiem", AM)
	}
	return c.addTag("parser/ENTimeExpressionParser")
}

func (p timeExpressionParser) extractFollowing(ctx *context, m *match, result *Result) *Components {
	c := ctx.createParsingComponents()
	if m.defined(millisecondGroup) {
		ms := int(js.ParseInt(string([]rune(m.g(millisecondGroup))[:min(3, runeLen(m.g(millisecondGroup)))])))
		if ms >= 1000 {
			return nil
		}
		c.assign("millisecond", ms)
	}
	if m.defined(secondGroup) {
		second := int(js.ParseInt(m.g(secondGroup)))
		if second >= 60 {
			return nil
		}
		c.assign("second", second)
	}
	hour := int(js.ParseInt(m.g(hourGroup)))
	minute := 0
	meridiem := -1
	if m.defined(minuteGroup) {
		minute = int(js.ParseInt(m.g(minuteGroup)))
	} else if hour > 100 {
		minute = hour % 100
		hour = hour / 100
	}
	if minute >= 60 || hour > 24 {
		return nil
	}
	if hour >= 12 {
		meridiem = PM
	}
	if m.defined(ampmGroup) {
		if hour > 12 {
			return nil
		}
		ampm := strings.ToLower(m.g(ampmGroup)[:1])
		if ampm == "a" {
			meridiem = AM
			if hour == 12 {
				hour = 0
				if !c.IsCertain("day") {
					c.imply("day", c.get("day")+1)
				}
			}
		}
		if ampm == "p" {
			meridiem = PM
			if hour != 12 {
				hour += 12
			}
		}
		if !result.Start.IsCertain("meridiem") {
			if meridiem == AM {
				result.Start.imply("meridiem", AM)
				if result.Start.get("hour") == 12 {
					result.Start.assign("hour", 0)
				}
			} else {
				result.Start.imply("meridiem", PM)
				if result.Start.get("hour") != 12 {
					result.Start.assign("hour", result.Start.get("hour")+12)
				}
			}
		}
	}
	c.assign("hour", hour)
	c.assign("minute", minute)
	if meridiem >= 0 {
		c.assign("meridiem", meridiem)
	} else {
		startAtPM := result.Start.IsCertain("meridiem") && result.Start.get("hour") > 12
		if startAtPM {
			if result.Start.get("hour")-12 > hour {
				c.imply("meridiem", AM)
			} else if hour <= 12 {
				c.assign("hour", hour+12)
				c.assign("meridiem", PM)
			}
		} else if hour > 12 {
			c.imply("meridiem", PM)
		} else {
			c.imply("meridiem", AM)
		}
	}
	if c.Date().Time() < result.Start.Date().Time() {
		c.imply("day", c.get("day")+1)
	}
	return c.addTag("parser/ENTimeExpressionParser")
}

func (p timeExpressionParser) checkWithoutFollowing(result *Result) any {
	if test(`^\d$`, "", result.Text) || test(`^\d\d\d+$`, "", result.Text) || test(`\d[apAP]$`, "", result.Text) {
		return nil
	}
	if m := execRe(re(`[^\d:.](\d[\d.]+)$`, ""), []rune(result.Text)); m != nil {
		ending := m.g(1)
		if p.strict {
			return nil
		}
		if strings.Contains(ending, ".") && !test(`\d(\.\d{2})+$`, "", ending) {
			return nil
		}
		if js.ParseInt(ending) > 24 {
			return nil
		}
	}
	return result
}

func (p timeExpressionParser) checkWithFollowing(result *Result) any {
	if test(`^\d+-\d+$`, "", result.Text) {
		return nil
	}
	if m := execRe(re(`[^\d:.](\d[\d.]+)\s*-\s*(\d[\d.]+)$`, ""), []rune(result.Text)); m != nil {
		if p.strict {
			return nil
		}
		starting, ending := m.g(1), m.g(2)
		if strings.Contains(ending, ".") && !test(`\d(\.\d{2})+$`, "", ending) {
			return nil
		}
		if js.ParseInt(ending) > 24 || js.ParseInt(starting) > 24 {
			return nil
		}
	}
	return result
}

// ── English refiners ─────────────────────────────────────────────────────────

var mergeRelativeAfterDate = mergingRefiner{
	shouldMerge: func(between string, cur, next *Result, _ *context) bool {
		if !test(`^\s*$`, "i", between) {
			return false
		}
		return test(`^[+-]`, "i", next.Text) || test(`^-`, "i", next.Text)
	},
	merge: func(between string, cur, next *Result, _ *context) *Result {
		units := parseDuration(next.Text)
		if test(`^-`, "i", next.Text) {
			units = reverseDuration(units)
		}
		c := createRelativeFromReference(referenceFromDate(cur.Start.Date()), units)
		return newResult(cur.reference, cur.Index, cur.Text+between+next.Text, c, nil)
	},
}

var mergeRelativeFollowByDate = mergingRefiner{
	shouldMerge: func(between string, cur, next *Result, _ *context) bool {
		if !test(`^\s*$`, "i", between) {
			return false
		}
		earlier := test(`\s+(before|from)$`, "i", cur.Text)
		later := test(`\s+(after|since)$`, "i", cur.Text)
		if !earlier && !later {
			return false
		}
		return next.Start.get("day") != 0 && next.Start.get("month") != 0 && next.Start.get("year") != 0
	},
	merge: func(between string, cur, next *Result, _ *context) *Result {
		d := parseDuration(cur.Text)
		if test(`\s+(before|from)$`, "i", cur.Text) {
			d = reverseDuration(d)
		}
		c := createRelativeFromReference(referenceFromDate(next.Start.Date()), d)
		return newResult(next.reference, cur.Index, cur.Text+between+next.Text, c, nil)
	},
}

var extractYearSuffix = refinerFunc(func(ctx *context, results []*Result) []*Result {
	pattern := re(`^\s*(`+yearPattern+`)`, "i")
	for _, result := range results {
		if !result.Start.isDateWithUnknownYear() {
			continue
		}
		suffix := []rune(substring(ctx.text, result.Index+runeLen(result.Text), len(ctx.text)))
		m := execRe(pattern, suffix)
		if m == nil {
			continue
		}
		if runeLen(js.Trim(m.g(0))) <= 3 {
			continue
		}
		year := parseYear(m.g(1))
		if result.End != nil {
			result.End.assign("year", year)
		}
		result.Start.assign("year", year)
		result.Text += m.g(0)
	}
	return results
})

var enUnlikelyFormatFilter = filterRefiner(func(ctx *context, r *Result) bool {
	text := js.Trim(r.Text)
	if text == js.Trim(string(ctx.text)) {
		return true
	}
	if strings.ToLower(text) == "may" {
		before := js.Trim(substring(ctx.text, 0, r.Index))
		if !test(`\b(in)$`, "i", before) {
			return false
		}
	}
	if strings.HasSuffix(strings.ToLower(text), "the second") {
		return false
	}
	return true
})

// ── configurations ───────────────────────────────────────────────────────────

func casual(littleEndian bool) *Chrono {
	return &Chrono{
		parsers: []parser{
			yearMonthDayParser(false),
			isoFormatParser(),
			newSlashDateFormatParser(littleEndian),
			timeUnitWithinFormatParser(),
			monthNameLittleEndianParser(),
			monthNameMiddleEndianParser(littleEndian),
			weekdayParser(),
			slashMonthFormatParser(),
			timeExpressionParser{strict: false},
			timeUnitAgoFormatParser(),
			timeUnitLaterFormatParser(),
			yearMonthNameParser(),
			casualDateParser(),
			casualTimeParser(),
			monthNameParser(),
			relativeDateFormatParser(),
			timeUnitCasualRelativeFormatParser(),
		},
		refiners: []refiner{
			overlapRemoval,
			mergeRelativeAfterDate,
			mergeRelativeFollowByDate,
			overlapRemoval,
			extractTimezoneOffset,
			mergeWeekdayComponent,
			mergeDateTimeRefiner(`^\s*(T|at|after|before|on|of|,|-|\.|∙|:)?\s*$`),
			extractTimezoneAbbr,
			overlapRemoval,
			forwardDate,
			unlikelyFormatFilter(false),
			mergeDateTimeRefiner(`^\s*(T|at|after|before|on|of|,|-|\.|∙|:)?\s*$`),
			extractYearSuffix,
			mergeDateRangeRefiner(`^\s*(to|-|–|until|through|till)\s*$`, "i"),
			enUnlikelyFormatFilter,
		},
	}
}

// Casual is `chrono` (chrono.casual): month-first slash dates.
var Casual = casual(false)

// GB is `chrono.en.GB`: day-first slash dates.
var GB = casual(true)

var _ = math.Floor
