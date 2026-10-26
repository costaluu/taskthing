package rrule

import (
	"errors"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/costaluu/taskthing/internal/js"
)

// ── fromText (nlp/parsetext.js + i18n.js) ────────────────────────────────────

type token struct {
	name string
	re   *regexp.Regexp
}

// The ENGLISH token table, in declaration order: on equal match length the
// earlier token wins, as in the JS `for (name in rules)` scan.
var tokens = []token{
	{"SKIP", regexp.MustCompile(`^[ \r\n\t]+|^\.$`)},
	{"number", regexp.MustCompile(`^[1-9][0-9]*`)},
	{"numberAsText", regexp.MustCompile(`(?i)^(one|two|three)`)},
	{"every", regexp.MustCompile(`(?i)^every`)},
	{"day(s)", regexp.MustCompile(`(?i)^days?`)},
	{"weekday(s)", regexp.MustCompile(`(?i)^weekdays?`)},
	{"week(s)", regexp.MustCompile(`(?i)^weeks?`)},
	{"hour(s)", regexp.MustCompile(`(?i)^hours?`)},
	{"minute(s)", regexp.MustCompile(`(?i)^minutes?`)},
	{"month(s)", regexp.MustCompile(`(?i)^months?`)},
	{"year(s)", regexp.MustCompile(`(?i)^years?`)},
	{"on", regexp.MustCompile(`(?i)^(on|in)`)},
	{"at", regexp.MustCompile(`(?i)^(at)`)},
	{"the", regexp.MustCompile(`(?i)^the`)},
	{"first", regexp.MustCompile(`(?i)^first`)},
	{"second", regexp.MustCompile(`(?i)^second`)},
	{"third", regexp.MustCompile(`(?i)^third`)},
	{"nth", regexp.MustCompile(`(?i)^([1-9][0-9]*)(\.|th|nd|rd|st)`)},
	{"last", regexp.MustCompile(`(?i)^last`)},
	{"for", regexp.MustCompile(`(?i)^for`)},
	{"time(s)", regexp.MustCompile(`(?i)^times?`)},
	{"until", regexp.MustCompile(`(?i)^(un)?til`)},
	{"monday", regexp.MustCompile(`(?i)^mo(n(day)?)?`)},
	{"tuesday", regexp.MustCompile(`(?i)^tu(e(s(day)?)?)?`)},
	{"wednesday", regexp.MustCompile(`(?i)^we(d(n(esday)?)?)?`)},
	{"thursday", regexp.MustCompile(`(?i)^th(u(r(sday)?)?)?`)},
	{"friday", regexp.MustCompile(`(?i)^fr(i(day)?)?`)},
	{"saturday", regexp.MustCompile(`(?i)^sa(t(urday)?)?`)},
	{"sunday", regexp.MustCompile(`(?i)^su(n(day)?)?`)},
	{"january", regexp.MustCompile(`(?i)^jan(uary)?`)},
	{"february", regexp.MustCompile(`(?i)^feb(ruary)?`)},
	{"march", regexp.MustCompile(`(?i)^mar(ch)?`)},
	{"april", regexp.MustCompile(`(?i)^apr(il)?`)},
	{"may", regexp.MustCompile(`(?i)^may`)},
	{"june", regexp.MustCompile(`(?i)^june?`)},
	{"july", regexp.MustCompile(`(?i)^july?`)},
	{"august", regexp.MustCompile(`(?i)^aug(ust)?`)},
	{"september", regexp.MustCompile(`(?i)^sep(t(ember)?)?`)},
	{"october", regexp.MustCompile(`(?i)^oct(ober)?`)},
	{"november", regexp.MustCompile(`(?i)^nov(ember)?`)},
	{"december", regexp.MustCompile(`(?i)^dec(ember)?`)},
	{"comma", regexp.MustCompile(`(?i)^(,\s*|(and|or)\s*)+`)},
}

type tokenizer struct {
	text   string
	done   bool
	symbol string // "" is JS null
	value  []string
}

func (t *tokenizer) start(text string) bool {
	t.text = text
	t.done = false
	return t.nextSymbol()
}

func (t *tokenizer) isDone() bool { return t.done && t.symbol == "" }

func (t *tokenizer) nextSymbol() bool {
	t.symbol, t.value = "", nil
	for {
		if t.done {
			return false
		}
		var best []string
		bestSymbol := ""
		for _, tk := range tokens {
			if m := tk.re.FindStringSubmatch(t.text); m != nil {
				if best == nil || len(m[0]) > len(best[0]) {
					best, bestSymbol = m, tk.name
				}
			}
		}
		if best == nil {
			t.done = true
			t.symbol, t.value = "", nil
			return false
		}
		t.text = t.text[len(best[0]):]
		if t.text == "" {
			t.done = true
		}
		if bestSymbol != "SKIP" {
			t.symbol, t.value = bestSymbol, best
			return true
		}
	}
}

// accept returns the match (or a non-nil marker) when the current symbol is name.
func (t *tokenizer) accept(name string) []string {
	if t.symbol != name {
		return nil
	}
	v := t.value
	t.nextSymbol()
	if v == nil {
		return []string{""}
	}
	return v
}

func (t *tokenizer) expect(name string) error {
	if t.accept(name) != nil {
		return nil
	}
	found := t.symbol
	if found == "" {
		found = "null"
	}
	return errors.New("expected " + name + " but found " + found)
}

type textParser struct {
	ttr *tokenizer
	o   Options
}

func (p *textParser) weekdays(o string) []Weekday {
	v, _ := p.o.get(o)
	w, _ := v.([]Weekday)
	return w
}

func (p *textParser) ints(key string) []any {
	v, _ := p.o.get(key)
	a, _ := v.([]any)
	return a
}

// ParseText is rrule.js's parseText: the options, or nil when nothing parsed.
func ParseText(text string) (Options, error) {
	p := &textParser{ttr: &tokenizer{}}
	if !p.ttr.start(text) {
		return nil, nil
	}
	if err := p.S(); err != nil {
		return nil, err
	}
	if p.o == nil {
		p.o = Options{}
	}
	return p.o, nil
}

func atoi(s string) int { n, _ := strconv.Atoi(s); return n }

var mondayToFriday = []Weekday{{Weekday: 0}, {Weekday: 1}, {Weekday: 2}, {Weekday: 3}, {Weekday: 4}}

func (p *textParser) S() error {
	ttr := p.ttr
	if err := ttr.expect("every"); err != nil {
		return err
	}
	if n := ttr.accept("number"); n != nil {
		p.o = p.o.set("interval", atoi(n[0]))
	}
	if ttr.isDone() {
		return errors.New("Unexpected end")
	}
	switch ttr.symbol {
	case "day(s)":
		p.o = p.o.set("freq", DAILY)
		if ttr.nextSymbol() {
			if err := p.AT(); err != nil {
				return err
			}
			return p.F()
		}
	case "weekday(s)":
		p.o = p.o.set("freq", WEEKLY)
		p.o = p.o.set("byweekday", append([]Weekday(nil), mondayToFriday...))
		ttr.nextSymbol()
		if err := p.AT(); err != nil {
			return err
		}
		return p.F()
	case "week(s)":
		p.o = p.o.set("freq", WEEKLY)
		if ttr.nextSymbol() {
			if err := p.ON(); err != nil {
				return err
			}
			if err := p.AT(); err != nil {
				return err
			}
			return p.F()
		}
	case "hour(s)", "minute(s)", "month(s)", "year(s)":
		freq := map[string]int{"hour(s)": HOURLY, "minute(s)": MINUTELY, "month(s)": MONTHLY, "year(s)": YEARLY}[ttr.symbol]
		p.o = p.o.set("freq", freq)
		if ttr.nextSymbol() {
			if err := p.ON(); err != nil {
				return err
			}
			return p.F()
		}
	case "monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday":
		p.o = p.o.set("freq", WEEKLY)
		p.o = p.o.set("byweekday", []Weekday{{Weekday: weekdayIndex(ttr.symbol)}})
		if !ttr.nextSymbol() {
			return nil
		}
		for ttr.accept("comma") != nil {
			if ttr.isDone() {
				return errors.New("Unexpected end")
			}
			wkd := p.decodeWKD()
			if wkd < 0 {
				return errors.New("Unexpected symbol " + nullable(ttr.symbol) + ", expected weekday")
			}
			p.o = p.o.set("byweekday", append(p.weekdays("byweekday"), Weekday{Weekday: wkd}))
			ttr.nextSymbol()
		}
		if err := p.AT(); err != nil {
			return err
		}
		if err := p.MDAYs(); err != nil {
			return err
		}
		return p.F()
	case "january", "february", "march", "april", "may", "june", "july", "august", "september", "october", "november", "december":
		p.o = p.o.set("freq", YEARLY)
		p.o = p.o.set("bymonth", []any{p.decodeM()})
		if !ttr.nextSymbol() {
			return nil
		}
		for ttr.accept("comma") != nil {
			if ttr.isDone() {
				return errors.New("Unexpected end")
			}
			m := p.decodeM()
			if m == 0 {
				return errors.New("Unexpected symbol " + nullable(ttr.symbol) + ", expected month")
			}
			p.o = p.o.set("bymonth", append(p.ints("bymonth"), m))
			ttr.nextSymbol()
		}
		if err := p.ON(); err != nil {
			return err
		}
		return p.F()
	default:
		return errors.New("Unknown symbol")
	}
	return nil
}

func nullable(s string) string {
	if s == "" {
		return "null"
	}
	return s
}

func weekdayIndex(symbol string) int {
	for i, w := range allWeekdays {
		if strings.EqualFold(symbol[:2], w) {
			return i
		}
	}
	return -1
}

func (p *textParser) ON() error {
	ttr := p.ttr
	on := ttr.accept("on")
	the := ttr.accept("the")
	if on == nil && the == nil {
		return nil
	}
	for {
		nth, err := p.decodeNTH()
		if err != nil {
			return err
		}
		wkd := p.decodeWKD()
		m := p.decodeM()
		switch {
		case nth != 0:
			if wkd >= 0 {
				ttr.nextSymbol()
				p.o = p.o.set("byweekday", append(p.weekdays("byweekday"), Weekday{Weekday: wkd, N: nth}))
			} else {
				p.o = p.o.set("bymonthday", append(p.ints("bymonthday"), nth))
				ttr.accept("day(s)")
			}
		case wkd >= 0:
			ttr.nextSymbol()
			p.o = p.o.set("byweekday", append(p.weekdays("byweekday"), Weekday{Weekday: wkd}))
		case ttr.symbol == "weekday(s)":
			ttr.nextSymbol()
			if !p.o.present("byweekday") {
				p.o = p.o.set("byweekday", append([]Weekday(nil), mondayToFriday...))
			}
		case ttr.symbol == "week(s)":
			ttr.nextSymbol()
			n := ttr.accept("number")
			if n == nil {
				return errors.New("Unexpected symbol " + nullable(ttr.symbol) + ", expected week number")
			}
			p.o = p.o.set("byweekno", []any{atoi(n[0])})
			for ttr.accept("comma") != nil {
				n = ttr.accept("number")
				if n == nil {
					return errors.New("Unexpected symbol " + nullable(ttr.symbol) + "; expected monthday")
				}
				p.o = p.o.set("byweekno", append(p.ints("byweekno"), atoi(n[0])))
			}
		case m != 0:
			ttr.nextSymbol()
			p.o = p.o.set("bymonth", append(p.ints("bymonth"), m))
		default:
			return nil
		}
		if ttr.accept("comma") == nil && ttr.accept("the") == nil && ttr.accept("on") == nil {
			return nil
		}
	}
}

func (p *textParser) AT() error {
	ttr := p.ttr
	if ttr.accept("at") == nil {
		return nil
	}
	for {
		n := ttr.accept("number")
		if n == nil {
			return errors.New("Unexpected symbol " + nullable(ttr.symbol) + ", expected hour")
		}
		p.o = p.o.set("byhour", []any{atoi(n[0])})
		for ttr.accept("comma") != nil {
			n = ttr.accept("number")
			if n == nil {
				return errors.New("Unexpected symbol " + nullable(ttr.symbol) + "; expected hour")
			}
			p.o = p.o.set("byhour", append(p.ints("byhour"), atoi(n[0])))
		}
		if ttr.accept("comma") == nil && ttr.accept("at") == nil {
			return nil
		}
	}
}

func (p *textParser) decodeM() int {
	months := []string{"january", "february", "march", "april", "may", "june", "july", "august", "september", "october", "november", "december"}
	for i, m := range months {
		if p.ttr.symbol == m {
			return i + 1
		}
	}
	return 0
}

func (p *textParser) decodeWKD() int {
	switch p.ttr.symbol {
	case "monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday":
		return weekdayIndex(p.ttr.symbol)
	}
	return -1
}

func (p *textParser) decodeNTH() (int, error) {
	ttr := p.ttr
	switch ttr.symbol {
	case "last":
		ttr.nextSymbol()
		return -1, nil
	case "first":
		ttr.nextSymbol()
		return 1, nil
	case "second":
		ttr.nextSymbol()
		if ttr.accept("last") != nil {
			return -2, nil
		}
		return 2, nil
	case "third":
		ttr.nextSymbol()
		if ttr.accept("last") != nil {
			return -3, nil
		}
		return 3, nil
	case "nth":
		v := atoi(ttr.value[1])
		if v < -366 || v > 366 {
			return 0, errors.New("Nth out of range: " + strconv.Itoa(v))
		}
		ttr.nextSymbol()
		if ttr.accept("last") != nil {
			return -v, nil
		}
		return v, nil
	}
	return 0, nil
}

func (p *textParser) MDAYs() error {
	ttr := p.ttr
	ttr.accept("on")
	ttr.accept("the")
	nth, err := p.decodeNTH()
	if err != nil || nth == 0 {
		return err
	}
	p.o = p.o.set("bymonthday", []any{nth})
	ttr.nextSymbol()
	for ttr.accept("comma") != nil {
		nth, err = p.decodeNTH()
		if err != nil {
			return err
		}
		if nth == 0 {
			return errors.New("Unexpected symbol " + nullable(ttr.symbol) + "; expected monthday")
		}
		p.o = p.o.set("bymonthday", append(p.ints("bymonthday"), nth))
		ttr.nextSymbol()
	}
	return nil
}

func (p *textParser) F() error {
	ttr := p.ttr
	if ttr.symbol == "until" {
		ms := dateParse(ttr.text)
		if math.IsNaN(ms) || ms == 0 {
			return errors.New("Cannot parse until date:" + ttr.text)
		}
		p.o = p.o.set("until", js.DateFromMillis(ms))
	} else if ttr.accept("for") != nil {
		if ttr.value == nil {
			return errors.New("null is not an object (evaluating 'ttr.value[0]')")
		}
		p.o = p.o.set("count", atoi(ttr.value[0]))
		if err := ttr.expect("number"); err != nil {
			return err
		}
	}
	return nil
}

// FromText is RRule.fromText.
func FromText(text string) (*RRule, error) {
	o, err := ParseText(text)
	if err != nil {
		return nil, err
	}
	if o == nil {
		o = Options{}
	}
	return New(o)
}

// dateParse covers the shapes `Date.parse` accepts in an "until <date>": ISO
// (date-only is UTC), and month-name or slash dates read in local time.
func dateParse(s string) float64 {
	t := strings.TrimSpace(s)
	if d, ok := js.ParseISO(t); ok {
		return d.Time()
	}
	for _, layout := range []string{"January 2, 2006", "January 2 2006", "Jan 2, 2006", "Jan 2 2006", "2 January 2006", "2 Jan 2006", "1/2/2006", "01/02/2006", "January 2, 2006 15:04", "Jan 2, 2006 15:04"} {
		if tm, err := time.ParseInLocation(layout, t, time.Local); err == nil {
			return float64(tm.UnixMilli())
		}
	}
	return math.NaN()
}

// ── toText (nlp/totext.js) ───────────────────────────────────────────────────

var monthNames = []string{"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"}
var dayNames = []string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}

var common = []string{"count", "until", "interval", "byweekday", "bymonthday", "bymonth"}

var implemented = map[int][]string{
	HOURLY:   common,
	MINUTELY: common,
	DAILY:    append([]string{"byhour"}, common...),
	WEEKLY:   common,
	MONTHLY:  common,
	YEARLY:   append([]string{"byweekno", "byyearday"}, common...),
}

type byweekdayInfo struct {
	allWeeks, someWeeks []Weekday
	isWeekdays          bool
	isEveryDay          bool
}

type toText struct {
	r          *RRule
	text       []string
	bymonthday []int
	byweekday  *byweekdayInfo
}

func (r *RRule) isFullyConvertible() bool {
	keys, ok := implemented[r.options.freq]
	if !ok {
		return false
	}
	if r.Orig.present("until") && r.Orig.present("count") {
		if c, _ := r.Orig.Value("count").(int); c != 0 {
			return false
		}
	}
	for _, e := range r.Orig {
		switch e.key {
		case "dtstart", "tzid", "wkst", "freq":
			return true
		}
		found := false
		for _, k := range keys {
			if k == e.key {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// ToText is RRule#toText.
func (r *RRule) ToText() string {
	t := &toText{r: r}
	if r.Orig.present("bymonthday") {
		pos := append([]int(nil), r.options.bymonthday...)
		neg := append([]int(nil), r.options.bynmonth...)
		sort.Ints(pos)
		sort.Sort(sort.Reverse(sort.IntSlice(neg)))
		t.bymonthday = append(pos, neg...)
	}
	if r.Orig.present("byweekday") {
		var days []Weekday
		switch w := r.Orig.Value("byweekday").(type) {
		case Weekday:
			days = []Weekday{w}
		case []Weekday:
			days = w
		}
		var parts []string
		for _, d := range days {
			parts = append(parts, d.String())
		}
		joined := strings.Join(parts, ",")
		has := func(s string) bool { return strings.Contains(joined, s) }
		info := &byweekdayInfo{
			isWeekdays: has("MO") && has("TU") && has("WE") && has("TH") && has("FR") && !has("SA") && !has("SU"),
			isEveryDay: has("MO") && has("TU") && has("WE") && has("TH") && has("FR") && has("SA") && has("SU"),
		}
		for _, d := range days {
			if d.N == 0 {
				info.allWeeks = append(info.allWeeks, d)
			} else {
				info.someWeeks = append(info.someWeeks, d)
			}
		}
		sort.SliceStable(info.allWeeks, func(i, j int) bool { return info.allWeeks[i].Weekday < info.allWeeks[j].Weekday })
		sort.SliceStable(info.someWeeks, func(i, j int) bool { return info.someWeeks[i].Weekday < info.someWeeks[j].Weekday })
		t.byweekday = info
	}
	return t.String()
}

func (t *toText) add(s string) *toText {
	t.text = append(t.text, " ", s)
	return t
}

func plural(n int) bool { return n%100 != 1 }

func (t *toText) String() string {
	o := t.r.options
	if _, ok := implemented[o.freq]; !ok {
		return "RRule error: Unable to fully convert this rrule to text"
	}
	t.text = []string{"every"}
	switch o.freq {
	case HOURLY:
		t.unit("hour", "hours")
	case MINUTELY:
		t.unit("minute", "minutes")
	case DAILY:
		t.daily()
	case WEEKLY:
		t.weekly()
	case MONTHLY:
		t.monthly()
	case YEARLY:
		t.yearly()
	}
	if o.until != nil {
		u := *o.until
		t.add("until")
		t.add(monthNames[u.GetUTCMonth()] + " " + strconv.Itoa(u.GetUTCDate()) + ", " + strconv.Itoa(u.GetUTCFullYear()))
	} else if o.count != nil && *o.count != 0 {
		t.add("for").add(strconv.Itoa(*o.count))
		if plural(*o.count) {
			t.add("times")
		} else {
			t.add("time")
		}
	}
	if !t.r.isFullyConvertible() {
		t.add("(~ approximate)")
	}
	return strings.Join(t.text, "")
}

func (t *toText) unit(one, many string) {
	o := t.r.options
	if o.interval != 1 {
		t.add(strconv.Itoa(o.interval))
	}
	if plural(o.interval) {
		t.add(many)
	} else {
		t.add(one)
	}
}

func (t *toText) daily() {
	o := t.r.options
	if o.interval != 1 {
		t.add(strconv.Itoa(o.interval))
	}
	if t.byweekday != nil && t.byweekday.isWeekdays {
		t.pick("weekday", "weekdays")
	} else {
		t.pick("day", "days")
	}
	if t.r.Orig.present("bymonth") {
		t.add("in")
		t.bymonth()
	}
	if t.bymonthday != nil {
		t.bymonthdayText()
	} else if t.byweekday != nil {
		t.byweekdayText()
	} else if t.r.Orig.present("byhour") {
		t.byhour()
	}
}

func (t *toText) pick(one, many string) {
	if plural(t.r.options.interval) {
		t.add(many)
	} else {
		t.add(one)
	}
}

func (t *toText) weekly() {
	o := t.r.options
	if o.interval != 1 {
		t.add(strconv.Itoa(o.interval))
		t.pick("week", "weeks")
	}
	switch {
	case t.byweekday != nil && t.byweekday.isWeekdays:
		if o.interval == 1 {
			t.pick("weekday", "weekdays")
		} else {
			t.add("on").add("weekdays")
		}
	case t.byweekday != nil && t.byweekday.isEveryDay:
		t.pick("day", "days")
	default:
		if o.interval == 1 {
			t.add("week")
		}
		if t.r.Orig.present("bymonth") {
			t.add("in")
			t.bymonth()
		}
		if t.bymonthday != nil {
			t.bymonthdayText()
		} else if t.byweekday != nil {
			t.byweekdayText()
		}
		if t.r.Orig.present("byhour") {
			t.byhour()
		}
	}
}

func (t *toText) monthly() {
	o := t.r.options
	if t.r.Orig.present("bymonth") {
		if o.interval != 1 {
			t.add(strconv.Itoa(o.interval)).add("months")
			if plural(o.interval) {
				t.add("in")
			}
		}
		t.bymonth()
	} else {
		if o.interval != 1 {
			t.add(strconv.Itoa(o.interval))
		}
		t.pick("month", "months")
	}
	switch {
	case t.bymonthday != nil:
		t.bymonthdayText()
	case t.byweekday != nil && t.byweekday.isWeekdays:
		t.add("on").add("weekdays")
	case t.byweekday != nil:
		t.byweekdayText()
	}
}

func (t *toText) yearly() {
	o := t.r.options
	if t.r.Orig.present("bymonth") {
		if o.interval != 1 {
			t.add(strconv.Itoa(o.interval))
			t.add("years")
		}
		t.bymonth()
	} else {
		if o.interval != 1 {
			t.add(strconv.Itoa(o.interval))
		}
		t.pick("year", "years")
	}
	if t.bymonthday != nil {
		t.bymonthdayText()
	} else if t.byweekday != nil {
		t.byweekdayText()
	}
	if len(o.byyearday) > 0 {
		t.add("on the").add(list(intStrings(o.byyearday, nth), "and")).add("day")
	}
	if len(o.byweekno) > 0 {
		t.add("in")
		if plural(len(o.byweekno)) {
			t.add("weeks")
		} else {
			t.add("week")
		}
		t.add(list(intStrings(o.byweekno, strconv.Itoa), "and"))
	}
}

func (t *toText) bymonthdayText() {
	if t.byweekday != nil && len(t.byweekday.allWeeks) > 0 {
		t.add("on").add(list(weekdayStrings(t.byweekday.allWeeks), "or")).add("the").add(list(intStrings(t.bymonthday, nth), "or"))
	} else {
		t.add("on the").add(list(intStrings(t.bymonthday, nth), "and"))
	}
}

func (t *toText) byweekdayText() {
	if len(t.byweekday.allWeeks) > 0 && !t.byweekday.isWeekdays {
		t.add("on").add(strings.Join(weekdayStrings(t.byweekday.allWeeks), ", "))
	}
	if len(t.byweekday.someWeeks) > 0 {
		if len(t.byweekday.allWeeks) > 0 {
			t.add("and")
		}
		t.add("on the").add(list(weekdayStrings(t.byweekday.someWeeks), "and"))
	}
}

func (t *toText) byhour() {
	var hours []string
	if ns, ok := numbers(t.r.Orig.Value("byhour")); ok {
		for _, n := range ns {
			hours = append(hours, strconv.Itoa(n))
		}
	}
	t.add("at").add(list(hours, "and"))
}

func (t *toText) bymonth() {
	var names []string
	for _, m := range t.r.options.bymonth {
		names = append(names, monthNames[m-1])
	}
	t.add(list(names, "and"))
}

func nth(n int) string {
	if n == -1 {
		return "last"
	}
	npos := n
	if npos < 0 {
		npos = -npos
	}
	var s string
	switch npos {
	case 1, 21, 31:
		s = strconv.Itoa(npos) + "st"
	case 2, 22:
		s = strconv.Itoa(npos) + "nd"
	case 3, 23:
		s = strconv.Itoa(npos) + "rd"
	default:
		s = strconv.Itoa(npos) + "th"
	}
	if n < 0 {
		return s + " last"
	}
	return s
}

func weekdayStrings(days []Weekday) []string {
	out := make([]string, len(days))
	for i, d := range days {
		prefix := ""
		if d.N != 0 {
			prefix = nth(d.N) + " "
		}
		out[i] = prefix + dayNames[d.jsWeekday()]
	}
	return out
}

func intStrings(ns []int, f func(int) string) []string {
	out := make([]string, len(ns))
	for i, n := range ns {
		out[i] = f(n)
	}
	return out
}

// list is ToText#list with a final delimiter.
func list(items []string, final string) string {
	var b strings.Builder
	for i, s := range items {
		if i != 0 {
			if i == len(items)-1 {
				b.WriteString(" " + final + " ")
			} else {
				b.WriteString(", ")
			}
		}
		b.WriteString(s)
	}
	return b.String()
}
