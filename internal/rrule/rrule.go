// Package rrule ports the parts of rrule.js 2.8.1 taskthing uses: parsing and
// serializing an RFC 5545 rule exactly as rrule.js does (option order
// included, since the string is stored and synced verbatim), the natural
// language fromText/toText, and `after` — whose iteration is delegated to
// rrule-go, a port of the same python-dateutil algorithm.
package rrule

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	rrulego "github.com/teambition/rrule-go"

	"github.com/costaluu/taskthing/internal/js"
)

// Frequencies, as rrule.js numbers them.
const (
	YEARLY = iota
	MONTHLY
	WEEKLY
	DAILY
	HOURLY
	MINUTELY
	SECONDLY
)

var frequencies = []string{"YEARLY", "MONTHLY", "WEEKLY", "DAILY", "HOURLY", "MINUTELY", "SECONDLY"}

var allWeekdays = []string{"MO", "TU", "WE", "TH", "FR", "SA", "SU"}

// Weekday is rrule.js's Weekday: 0 = Monday, n = nth occurrence (0 = none).
type Weekday struct {
	Weekday int
	N       int
}

func (w Weekday) String() string {
	s := ""
	if w.Weekday >= 0 && w.Weekday < 7 {
		s = allWeekdays[w.Weekday]
	}
	if w.N != 0 {
		sign := ""
		if w.N > 0 {
			sign = "+"
		}
		s = sign + strconv.Itoa(w.N) + s
	}
	return s
}

func (w Weekday) jsWeekday() int {
	if w.Weekday == 6 {
		return 0
	}
	return w.Weekday + 1
}

// undefined marks a key that was assigned `undefined` (e.g. FREQ=BOGUS): the
// key exists, but holds no value.
type undefined struct{}

// opt is one origOptions entry. Values: int, string (an unparsed number),
// []any of those, Weekday, []Weekday, js.Date, undefined.
type opt struct {
	key   string
	value any
}

// Options is an rrule.js origOptions object, in key order.
type Options []opt

func (o Options) get(key string) (any, bool) {
	for _, e := range o {
		if e.key == key {
			return e.value, true
		}
	}
	return nil, false
}

// present is rrule.js's isPresent(origOptions[key]).
func (o Options) present(key string) bool {
	v, ok := o.get(key)
	if !ok {
		return false
	}
	_, undef := v.(undefined)
	return !undef
}

func (o Options) set(key string, value any) Options {
	for i := range o {
		if o[i].key == key {
			o[i].value = value
			return o
		}
	}
	return append(o, opt{key, value})
}

// With is `{ ...o, [key]: value }`.
func (o Options) With(key string, value any) Options {
	return append(Options(nil), o...).set(key, value)
}

// Dtstart is the origOptions dtstart, if one was given.
func (o Options) Dtstart() (js.Date, bool) {
	v, ok := o.get("dtstart")
	if !ok {
		return js.Date{}, false
	}
	d, ok := v.(js.Date)
	return d, ok
}

// RRule is a constructed rule: its origOptions plus the parsed options.
type RRule struct {
	Orig    Options
	options parsed
}

// ── parsing (parsestring.js) ─────────────────────────────────────────────────

var (
	headerRe   = regexp.MustCompile(`^([A-Z]+?)[:;]`)
	dtstartRe  = regexp.MustCompile(`(?i)DTSTART(?:;TZID=([^:=]+?))?(?::|=)([^;\s]+)`)
	untilRe    = regexp.MustCompile(`^(\d{4})(\d{2})(\d{2})(T(\d{2})(\d{2})(\d{2})Z?)?$`)
	rrulePfx   = regexp.MustCompile(`(?i)^RRULE:`)
	rulePfx    = regexp.MustCompile(`(?i)^(?:RRULE|EXRULE):`)
	integerRe  = regexp.MustCompile(`^[+-]?\d+$`)
	nthWeekday = regexp.MustCompile(`^([+-]?\d{1,2})([A-Z]{2})$`)
	leadingWS  = regexp.MustCompile(`^\s+`)
	trailingWS = regexp.MustCompile(`\s+$`)
)

// ParseString is RRule.parseString.
func ParseString(s string) (Options, error) {
	var lines []Options
	for _, line := range strings.Split(s, "\n") {
		o, keep, err := parseLine(line)
		if err != nil {
			return nil, err
		}
		if keep {
			lines = append(lines, o)
		}
	}
	var out Options
	for i := 0; i < len(lines) && i < 2; i++ {
		for _, e := range lines[i] {
			out = out.set(e.key, e.value)
		}
	}
	return out, nil
}

func parseLine(line string) (Options, bool, error) {
	// `.replace(/^\s+|\s+$/, '')`: only the first match goes.
	if loc := leadingWS.FindStringIndex(line); loc != nil {
		line = line[loc[1]:]
	} else if loc := trailingWS.FindStringIndex(line); loc != nil {
		line = line[:loc[0]]
	}
	if line == "" {
		return nil, false, nil
	}
	header := headerRe.FindStringSubmatch(strings.ToUpper(line))
	if header == nil {
		o, err := parseRrule(line)
		return o, true, err
	}
	switch header[1] {
	case "RRULE", "EXRULE":
		o, err := parseRrule(line)
		return o, true, err
	case "DTSTART":
		o, err := parseDtstart(line)
		return o, true, err
	}
	return nil, false, fmt.Errorf("Unsupported RFC prop %s in %s", header[1], line)
}

func parseDtstart(line string) (Options, error) {
	var o Options
	m := dtstartRe.FindStringSubmatch(line)
	if m == nil {
		return o, nil
	}
	if m[1] != "" {
		o = o.set("tzid", m[1])
	}
	d, err := untilStringToDate(m[2])
	if err != nil {
		return nil, err
	}
	return o.set("dtstart", d), nil
}

func parseRrule(line string) (Options, error) {
	o, err := parseDtstart(rrulePfx.ReplaceAllString(line, ""))
	if err != nil {
		return nil, err
	}
	for _, attr := range strings.Split(rulePfx.ReplaceAllString(line, ""), ";") {
		parts := strings.Split(attr, "=")
		key := parts[0]
		value, hasValue := "", len(parts) > 1
		if hasValue {
			value = parts[1]
		}
		switch strings.ToUpper(key) {
		case "FREQ":
			if !hasValue {
				return nil, errors.New("undefined is not an object (evaluating 'value.toUpperCase')")
			}
			if i := slices.Index(frequencies, strings.ToUpper(value)); i >= 0 {
				o = o.set("freq", i)
			} else {
				o = o.set("freq", undefined{})
			}
		case "WKST":
			if !hasValue {
				return nil, errors.New("undefined is not an object (evaluating 'value.toUpperCase')")
			}
			if i := slices.Index(allWeekdays, strings.ToUpper(value)); i >= 0 {
				o = o.set("wkst", Weekday{Weekday: i})
			} else {
				o = o.set("wkst", undefined{})
			}
		case "COUNT", "INTERVAL", "BYSETPOS", "BYMONTH", "BYMONTHDAY", "BYYEARDAY", "BYWEEKNO", "BYHOUR", "BYMINUTE", "BYSECOND":
			if !hasValue {
				return nil, errors.New("undefined is not an object (evaluating 'value.indexOf')")
			}
			o = o.set(strings.ToLower(key), parseNumber(value))
		case "BYWEEKDAY", "BYDAY":
			if !hasValue {
				return nil, errors.New("undefined is not an object (evaluating 'value.split')")
			}
			days, err := parseWeekday(value)
			if err != nil {
				return nil, err
			}
			o = o.set("byweekday", days)
		case "DTSTART", "TZID":
			dt, err := parseDtstart(line)
			if err != nil {
				return nil, err
			}
			tzid, _ := dt.get("tzid")
			if tzid == nil {
				tzid = undefined{}
			}
			o = o.set("tzid", tzid)
			dtstart, ok := dt.get("dtstart")
			if !ok {
				dtstart = undefined{}
			}
			o = o.set("dtstart", dtstart)
		case "UNTIL":
			d, err := untilStringToDate(value)
			if err != nil {
				return nil, err
			}
			o = o.set("until", d)
		case "BYEASTER":
			o = o.set("byeaster", js.ToNumber(value))
		default:
			return nil, fmt.Errorf("Unknown RRULE property '%s'", key)
		}
	}
	return o, nil
}

func parseNumber(value string) any {
	if strings.Contains(value, ",") {
		var out []any
		for _, v := range strings.Split(value, ",") {
			out = append(out, parseIndividualNumber(v))
		}
		return out
	}
	return parseIndividualNumber(value)
}

func parseIndividualNumber(v string) any {
	if integerRe.MatchString(v) {
		n, _ := strconv.Atoi(v)
		return n
	}
	return v
}

func parseWeekday(value string) ([]Weekday, error) {
	var out []Weekday
	for _, day := range strings.Split(value, ",") {
		if len(day) == 2 {
			// `Days[day]`: an unknown pair is undefined, which parseOptions then
			// trips over.
			w := slices.Index(allWeekdays, day)
			if w < 0 {
				return nil, errors.New("undefined is not an object (evaluating 'wday.n')")
			}
			out = append(out, Weekday{Weekday: w})
			continue
		}
		m := nthWeekday.FindStringSubmatch(day)
		if m == nil {
			return nil, fmt.Errorf("Invalid weekday string: %s", day)
		}
		n, _ := strconv.Atoi(m[1])
		w := slices.Index(allWeekdays, m[2])
		if w < 0 {
			return nil, fmt.Errorf("undefined is not an object (evaluating 'Days[wdaypart].weekday')")
		}
		if n == 0 {
			return nil, errors.New("Can't create weekday with n == 0")
		}
		out = append(out, Weekday{Weekday: w, N: n})
	}
	return out, nil
}

func untilStringToDate(until string) (js.Date, error) {
	m := untilRe.FindStringSubmatch(until)
	if m == nil {
		return js.Date{}, fmt.Errorf("Invalid UNTIL value: %s", until)
	}
	n := func(s string) int { v, _ := strconv.Atoi(s); return v }
	return js.UTC(n(m[1]), n(m[2])-1, n(m[3]), n(m[5]), n(m[6]), n(m[7]), 0).SetUTCFullYear(n(m[1])), nil
}

// ── construction (rrule.js + parseoptions.js) ────────────────────────────────

// FromString is RRule.fromString.
func FromString(s string) (*RRule, error) {
	o, err := ParseString(s)
	if err != nil {
		return nil, err
	}
	return New(o)
}

// Valid reports whether RRule.fromString accepts the string.
func Valid(s string) bool {
	_, err := FromString(s)
	return err == nil
}

// parsed is the subset of rrule.js's parsed `options` taskthing reads.
type parsed struct {
	freq       int
	dtstart    js.Date
	interval   int
	wkst       int
	count      *int
	until      *js.Date
	bysetpos   []int
	bymonth    []int
	bymonthday []int
	bynmonth   []int
	byyearday  []int
	byweekno   []int
	byweekday  []int
	bynweekday [][2]int
	byhour     []int
	byminute   []int
	bysecond   []int
	byeaster   *int
}

// New is `new RRule(origOptions)`.
func New(orig Options) (*RRule, error) {
	for _, e := range orig {
		if d, ok := e.value.(js.Date); ok && !d.Valid() {
			return nil, fmt.Errorf("Invalid options: %s", e.key)
		}
	}
	p, err := parseOptions(orig)
	if err != nil {
		return nil, err
	}
	return &RRule{Orig: orig, options: p}, nil
}

func numbers(v any) ([]int, bool) {
	switch x := v.(type) {
	case int:
		return []int{x}, true
	case []any:
		var out []int
		for _, e := range x {
			n, ok := e.(int)
			if !ok {
				return nil, false
			}
			out = append(out, n)
		}
		return out, true
	}
	return nil, false
}

func parseOptions(o Options) (parsed, error) {
	p := parsed{freq: YEARLY, interval: 1, wkst: 0}
	if v, ok := o.get("freq"); ok {
		f, isInt := v.(int)
		if !isInt || f < 0 || f >= len(frequencies) {
			return p, fmt.Errorf("Invalid frequency: %v %v", "undefined", "undefined")
		}
		p.freq = f
	}
	if o.present("byeaster") {
		p.freq = YEARLY
		if n, ok := o.Value("byeaster").(float64); ok {
			e := int(n)
			p.byeaster = &e
		}
	}
	if d, ok := o.Dtstart(); ok && d.Valid() {
		p.dtstart = d
	} else {
		p.dtstart = js.DateFromMillis(math.Floor(js.Now().Time()/1000) * 1000)
	}
	if v, ok := o.get("interval"); ok {
		if n, isInt := v.(int); isInt {
			p.interval = n
		}
	}
	if v, ok := o.get("wkst"); ok {
		switch w := v.(type) {
		case Weekday:
			p.wkst = w.Weekday
		case int:
			p.wkst = w
		}
	}
	if v, ok := o.get("count"); ok {
		if n, isInt := v.(int); isInt {
			p.count = &n
		}
	}
	if v, ok := o.get("until"); ok {
		if d, isDate := v.(js.Date); isDate {
			p.until = &d
		}
	}
	if v, ok := o.get("bysetpos"); ok && o.present("bysetpos") {
		ns, _ := numbers(v)
		for _, n := range ns {
			if n == 0 || !(n >= -366 && n <= 366) {
				return p, errors.New("bysetpos must be between 1 and 366, or between -366 and -1")
			}
		}
		p.bysetpos = ns
	}
	if v, ok := o.get("bymonth"); ok {
		p.bymonth, _ = numbers(v)
	}
	if v, ok := o.get("byyearday"); ok {
		p.byyearday, _ = numbers(v)
	}
	if v, ok := o.get("byweekno"); ok {
		p.byweekno, _ = numbers(v)
	}
	bymonthday, hasMonthday := []int(nil), false
	if v, ok := o.get("bymonthday"); ok && o.present("bymonthday") {
		bymonthday, hasMonthday = numbers(v)
	}
	if !(len(p.byweekno) > 0 || len(p.byyearday) > 0 || hasMonthday && len(bymonthday) > 0 || o.present("byweekday") || o.present("byeaster")) {
		switch p.freq {
		case YEARLY:
			if len(p.bymonth) == 0 {
				p.bymonth = []int{p.dtstart.GetUTCMonth() + 1}
			}
			bymonthday, hasMonthday = []int{p.dtstart.GetUTCDate()}, true
		case MONTHLY:
			bymonthday, hasMonthday = []int{p.dtstart.GetUTCDate()}, true
		case WEEKLY:
			p.byweekday = []int{(p.dtstart.GetUTCDay() + 6) % 7}
		}
	}
	for _, n := range bymonthday {
		if n > 0 {
			p.bymonthday = append(p.bymonthday, n)
		} else if n < 0 {
			p.bynmonth = append(p.bynmonth, n)
		}
	}
	if v, ok := o.get("byweekday"); ok && o.present("byweekday") {
		var days []Weekday
		switch w := v.(type) {
		case Weekday:
			days = []Weekday{w}
		case []Weekday:
			days = w
		case int:
			days = []Weekday{{Weekday: w}}
		}
		p.byweekday = nil
		for _, d := range days {
			if d.N == 0 || p.freq > MONTHLY {
				p.byweekday = append(p.byweekday, d.Weekday)
			} else {
				p.bynweekday = append(p.bynweekday, [2]int{d.Weekday, d.N})
			}
		}
	}
	if v, ok := o.get("byhour"); ok && o.present("byhour") {
		p.byhour, _ = numbers(v)
	} else if p.freq < HOURLY {
		p.byhour = []int{p.dtstart.GetUTCHours()}
	}
	if v, ok := o.get("byminute"); ok && o.present("byminute") {
		p.byminute, _ = numbers(v)
	} else if p.freq < MINUTELY {
		p.byminute = []int{p.dtstart.GetUTCMinutes()}
	}
	if v, ok := o.get("bysecond"); ok && o.present("bysecond") {
		p.bysecond, _ = numbers(v)
	} else if p.freq < SECONDLY {
		p.bysecond = []int{p.dtstart.GetUTCSeconds()}
	}
	return p, nil
}

// Value is Options.get without the flag.
func (o Options) Value(key string) any { v, _ := o.get(key); return v }

// DtstartOf is `rule.options.dtstart`.
func (r *RRule) DtstartOf() js.Date { return r.options.dtstart }

// ── serialization (optionstostring.js) ───────────────────────────────────────

var defaultKeys = []string{"freq", "dtstart", "interval", "wkst", "count", "until", "tzid", "bysetpos", "bymonth", "bymonthday", "bynmonthday", "byyearday", "byweekno", "byweekday", "bynweekday", "byhour", "byminute", "bysecond", "byeaster"}

// String is RRule#toString: the origOptions, in their own order.
func (r *RRule) String() string { return OptionsToString(r.Orig) }

func OptionsToString(o Options) string {
	var rules []string
	dtstart := ""
	tzid, _ := o.get("tzid")
	tzidStr, hasTzid := tzid.(string)
	hasTzid = hasTzid && tzidStr != ""
	for _, e := range o {
		if e.key == "tzid" || !slices.Contains(defaultKeys, e.key) {
			continue
		}
		key := strings.ToUpper(e.key)
		value := e.value
		if value == nil {
			continue
		}
		if _, undef := value.(undefined); undef {
			continue
		}
		if arr, ok := value.([]any); ok && len(arr) == 0 {
			continue
		}
		if arr, ok := value.([]Weekday); ok && len(arr) == 0 {
			continue
		}
		out := ""
		switch key {
		case "FREQ":
			if f, ok := value.(int); ok && f >= 0 && f < len(frequencies) {
				out = frequencies[f]
			}
		case "WKST":
			switch w := value.(type) {
			case int:
				out = Weekday{Weekday: w}.String()
			case Weekday:
				out = w.String()
			}
		case "BYWEEKDAY":
			key = "BYDAY"
			var days []string
			switch w := value.(type) {
			case Weekday:
				days = []string{w.String()}
			case []Weekday:
				for _, d := range w {
					days = append(days, d.String())
				}
			case int:
				days = []string{Weekday{Weekday: w}.String()}
			}
			out = strings.Join(days, ",")
		case "DTSTART":
			if d, ok := value.(js.Date); ok {
				if hasTzid && strings.ToUpper(tzidStr) != "UTC" {
					dtstart = "DTSTART;TZID=" + tzidStr + ":" + timeToUntilString(d, false)
				} else {
					dtstart = "DTSTART:" + timeToUntilString(d, true)
				}
			}
		case "UNTIL":
			if d, ok := value.(js.Date); ok {
				out = timeToUntilString(d, !hasTzid)
			}
		default:
			out = jsString(value)
		}
		if out != "" {
			rules = append(rules, key+"="+out)
		}
	}
	var parts []string
	if dtstart != "" {
		parts = append(parts, dtstart)
	}
	if len(rules) > 0 {
		parts = append(parts, "RRULE:"+strings.Join(rules, ";"))
	}
	return strings.Join(parts, "\n")
}

func jsString(v any) string {
	switch x := v.(type) {
	case int:
		return strconv.Itoa(x)
	case float64:
		return js.NumberToString(x)
	case string:
		return x
	case []any:
		parts := make([]string, len(x))
		for i, e := range x {
			parts[i] = jsString(e)
		}
		return strings.Join(parts, ",")
	}
	return fmt.Sprint(v)
}

func timeToUntilString(d js.Date, utc bool) string {
	s := fmt.Sprintf("%04d%02d%02dT%02d%02d%02d", d.GetUTCFullYear(), d.GetUTCMonth()+1, d.GetUTCDate(), d.GetUTCHours(), d.GetUTCMinutes(), d.GetUTCSeconds())
	if utc {
		s += "Z"
	}
	return s
}

// ── iteration ────────────────────────────────────────────────────────────────

// After is rule.after(dt, false): the first occurrence strictly after dt.
func (r *RRule) After(dt js.Date) (js.Date, bool) {
	p := r.options
	opt := rrulego.ROption{
		Freq:     rrulego.Frequency(p.freq),
		Dtstart:  p.dtstart.Go(),
		Interval: p.interval,
		Wkst:     goWeekday(p.wkst, 0),
	}
	if p.count != nil {
		opt.Count = *p.count
	}
	if p.until != nil {
		opt.Until = p.until.Go()
	}
	opt.Bysetpos = p.bysetpos
	opt.Bymonth = p.bymonth
	opt.Bymonthday = append(append([]int(nil), p.bymonthday...), p.bynmonth...)
	opt.Byyearday = p.byyearday
	opt.Byweekno = p.byweekno
	for _, w := range p.byweekday {
		opt.Byweekday = append(opt.Byweekday, goWeekday(w, 0))
	}
	for _, w := range p.bynweekday {
		opt.Byweekday = append(opt.Byweekday, goWeekday(w[0], w[1]))
	}
	opt.Byhour = p.byhour
	opt.Byminute = p.byminute
	opt.Bysecond = p.bysecond
	if p.byeaster != nil {
		opt.Byeaster = []int{*p.byeaster}
	}
	rule, err := rrulego.NewRRule(opt)
	if err != nil {
		return js.Date{}, false
	}
	tzid, _ := r.Orig.Value("tzid").(string)
	if tzid == "" || strings.ToUpper(tzid) == "UTC" {
		next := rule.After(dt.Go(), false)
		if next.IsZero() {
			return js.Date{}, false
		}
		return js.DateFromTime(next), true
	}
	// A zoned rule iterates naive wall-clock dates and rezones each one before
	// comparing it (DateWithZone#rezonedDate), so the answer shifts with both
	// the TZID and the process zone — rrule.js's behaviour, kept as is.
	loc, err := time.LoadLocation(tzid)
	if err != nil {
		return js.Date{}, false
	}
	next := rule.Iterator()
	for {
		occurrence, ok := next()
		if !ok {
			return js.Date{}, false
		}
		rezoned := rezone(occurrence, loc)
		if rezoned.After(dt.Go()) {
			return js.DateFromTime(rezoned), true
		}
	}
}

func rezone(t time.Time, loc *time.Location) time.Time {
	wall := func(in *time.Location) time.Time {
		w := t.In(in)
		return time.Date(w.Year(), w.Month(), w.Day(), w.Hour(), w.Minute(), w.Second(), 0, time.UTC)
	}
	return t.Add(-wall(loc).Sub(wall(time.Local)))
}

func goWeekday(w, n int) rrulego.Weekday {
	days := []rrulego.Weekday{rrulego.MO, rrulego.TU, rrulego.WE, rrulego.TH, rrulego.FR, rrulego.SA, rrulego.SU}
	if w < 0 || w > 6 {
		w = 0
	}
	if n != 0 {
		return days[w].Nth(n)
	}
	return days[w]
}

var _ = time.UTC

// Present is rrule.js's isPresent(origOptions[key]).
func (o Options) Present(key string) bool { return o.present(key) }
