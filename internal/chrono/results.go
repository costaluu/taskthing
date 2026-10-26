// Package chrono is a port of the parts of chrono-node 2.10.1 that taskthing
// calls: the English casual configuration (`chrono.parse` and `chrono.en.GB`).
// The parsers, refiners and their regexes are carried over one-to-one, so a
// natural-language date resolves exactly as the TS binary resolved it.
package chrono

import (
	"math"
	"slices"
	"strings"

	"github.com/costaluu/taskthing/internal/js"
)

// Meridiem values, as chrono's enum.
const (
	AM = 0
	PM = 1
)

// Reference is chrono's ReferenceWithTimezone, for a plain Date reference (the
// only kind taskthing passes): no timezone override.
type Reference struct {
	instant        js.Date
	timezoneOffset *int
}

func referenceFromDate(d js.Date) *Reference { return &Reference{instant: d} }

func (r *Reference) dateWithAdjustedTimezone() js.Date {
	date := r.instant
	if r.timezoneOffset != nil {
		date = date.SetMinutes(date.GetMinutes() - r.systemTimezoneAdjustmentMinute(r.instant))
	}
	return date
}

func (r *Reference) systemTimezoneAdjustmentMinute(date js.Date) int {
	current := -date.GetTimezoneOffset()
	target := current
	if r.timezoneOffset != nil {
		target = *r.timezoneOffset
	}
	return current - target
}

func (r *Reference) getTimezoneOffset() int {
	if r.timezoneOffset != nil {
		return *r.timezoneOffset
	}
	return -r.instant.GetTimezoneOffset()
}

// orderedInts is a JS object of numbers: lookups plus insertion order.
type orderedInts struct {
	keys []string
	m    map[string]int
}

func newOrderedInts() *orderedInts { return &orderedInts{m: map[string]int{}} }

func (o *orderedInts) has(k string) bool { _, ok := o.m[k]; return ok }
func (o *orderedInts) set(k string, v int) {
	if !o.has(k) {
		o.keys = append(o.keys, k)
	}
	o.m[k] = v
}
func (o *orderedInts) del(k string) {
	if !o.has(k) {
		return
	}
	delete(o.m, k)
	o.keys = slices.DeleteFunc(o.keys, func(s string) bool { return s == k })
}
func (o *orderedInts) clone() *orderedInts {
	c := newOrderedInts()
	for _, k := range o.keys {
		c.set(k, o.m[k])
	}
	return c
}

// Components is chrono's ParsingComponents.
type Components struct {
	known     *orderedInts
	implied   *orderedInts
	reference *Reference
	tags      []string
}

func newComponents(ref *Reference, known map[string]int, order ...string) *Components {
	c := &Components{known: newOrderedInts(), implied: newOrderedInts(), reference: ref}
	for _, k := range order {
		c.known.set(k, known[k])
	}
	date := ref.dateWithAdjustedTimezone()
	c.imply("day", date.GetDate())
	c.imply("month", date.GetMonth()+1)
	c.imply("year", date.GetFullYear())
	c.imply("hour", 12)
	c.imply("minute", 0)
	c.imply("second", 0)
	c.imply("millisecond", 0)
	return c
}

// duration is a chrono Duration: unit → amount, with presence mattering.
type duration map[string]float64

func emptyDuration() duration { return duration{"day": 0, "second": 0, "millisecond": 0} }

func createRelativeFromReference(ref *Reference, d duration) *Components {
	date := addDuration(ref.dateWithAdjustedTimezone(), d)
	c := newComponents(ref, nil)
	c.addTag("result/relativeDate")
	_, hasHour := d["hour"]
	_, hasMinute := d["minute"]
	_, hasSecond := d["second"]
	_, hasMs := d["millisecond"]
	if hasHour || hasMinute || hasSecond || hasMs {
		c.addTag("result/relativeDateAndTime")
		assignSimilarTime(c, date)
		assignSimilarDate(c, date)
		c.assign("timezoneOffset", ref.getTimezoneOffset())
		return c
	}
	implySimilarTime(c, date)
	c.imply("timezoneOffset", ref.getTimezoneOffset())
	if _, ok := d["day"]; ok {
		c.assign("day", date.GetDate())
		c.assign("month", date.GetMonth()+1)
		c.assign("year", date.GetFullYear())
		c.assign("weekday", date.GetDay())
	} else if _, ok := d["week"]; ok {
		c.assign("day", date.GetDate())
		c.assign("month", date.GetMonth()+1)
		c.assign("year", date.GetFullYear())
		c.imply("weekday", date.GetDay())
	} else {
		c.imply("day", date.GetDate())
		if _, ok := d["month"]; ok {
			c.assign("month", date.GetMonth()+1)
			c.assign("year", date.GetFullYear())
		} else {
			c.imply("month", date.GetMonth()+1)
			if _, ok := d["year"]; ok {
				c.assign("year", date.GetFullYear())
			} else {
				c.imply("year", date.GetFullYear())
			}
		}
	}
	return c
}

// Get is components.get: the known value, else the implied one.
func (c *Components) Get(k string) (int, bool) {
	if v, ok := c.known.m[k]; ok {
		return v, true
	}
	if v, ok := c.implied.m[k]; ok {
		return v, true
	}
	return 0, false
}

func (c *Components) get(k string) int { v, _ := c.Get(k); return v }

func (c *Components) IsCertain(k string) bool { return c.known.has(k) }

func (c *Components) certainComponents() []string { return append([]string(nil), c.known.keys...) }

func (c *Components) imply(k string, v int) *Components {
	if c.known.has(k) {
		return c
	}
	c.implied.set(k, v)
	return c
}

func (c *Components) assign(k string, v int) *Components {
	c.known.set(k, v)
	c.implied.del(k)
	return c
}

func (c *Components) addDurationAsImplied(d duration) *Components {
	date := addDuration(c.dateWithoutTimezoneAdjustment(), d)
	_, hasDay := d["day"]
	_, hasWeek := d["week"]
	_, hasMonth := d["month"]
	_, hasYear := d["year"]
	if hasDay || hasWeek || hasMonth || hasYear {
		c.delete("day", "weekday", "month", "year")
		c.imply("day", date.GetDate())
		c.imply("weekday", date.GetDay())
		c.imply("month", date.GetMonth()+1)
		c.imply("year", date.GetFullYear())
	}
	_, hasSecond := d["second"]
	_, hasMinute := d["minute"]
	_, hasHour := d["hour"]
	if hasSecond || hasMinute || hasHour {
		c.delete("second", "minute", "hour")
		c.imply("second", date.GetSeconds())
		c.imply("minute", date.GetMinutes())
		c.imply("hour", date.GetHours())
	}
	return c
}

func (c *Components) delete(keys ...string) {
	for _, k := range keys {
		c.known.del(k)
		c.implied.del(k)
	}
}

func (c *Components) clone() *Components {
	n := newComponents(c.reference, nil)
	n.known = c.known.clone()
	n.implied = c.implied.clone()
	// The constructor's tag set is fresh: clone() never carried tags.
	return n
}

func (c *Components) isOnlyDate() bool {
	return !c.IsCertain("hour") && !c.IsCertain("minute") && !c.IsCertain("second")
}

func (c *Components) isOnlyTime() bool {
	return !c.IsCertain("weekday") && !c.IsCertain("day") && !c.IsCertain("month") && !c.IsCertain("year")
}

func (c *Components) isOnlyWeekdayComponent() bool {
	return c.IsCertain("weekday") && !c.IsCertain("day") && !c.IsCertain("month")
}

func (c *Components) isDateWithUnknownYear() bool {
	return c.IsCertain("month") && !c.IsCertain("year")
}

func (c *Components) isValidDate() bool {
	date := js.UTC(c.get("year"), c.get("month")-1, c.get("day"), c.get("hour"), c.get("minute"), c.get("second"), c.get("millisecond"))
	date = date.SetUTCFullYear(c.get("year"))
	if date.GetUTCFullYear() != c.get("year") {
		return false
	}
	if date.GetUTCMonth() != c.get("month")-1 {
		return false
	}
	if date.GetUTCDate() != c.get("day") {
		return false
	}
	if date.GetUTCHours() != c.get("hour") {
		return false
	}
	if date.GetUTCMinutes() != c.get("minute") {
		return false
	}
	return true
}

// Date is components.date().
func (c *Components) Date() js.Date {
	offset, ok := c.Get("timezoneOffset")
	if !ok && c.reference.timezoneOffset != nil {
		offset, ok = *c.reference.timezoneOffset, true
	}
	if !ok {
		return c.dateWithoutTimezoneAdjustment()
	}
	date := js.UTC(c.get("year"), c.get("month")-1, c.get("day"), c.get("hour"), c.get("minute"), c.get("second"), c.get("millisecond"))
	date = date.SetUTCFullYear(c.get("year"))
	return js.DateFromMillis(date.Time() - float64(offset)*60000)
}

func (c *Components) dateWithoutTimezoneAdjustment() js.Date {
	date := js.NewLocal(c.get("year"), c.get("month")-1, c.get("day"), c.get("hour"), c.get("minute"), c.get("second"), c.get("millisecond"))
	return date.SetFullYear(c.get("year"))
}

func (c *Components) addTag(tag string) *Components {
	if !slices.Contains(c.tags, tag) {
		c.tags = append(c.tags, tag)
	}
	return c
}

func (c *Components) addTags(tags []string) *Components {
	for _, t := range tags {
		c.addTag(t)
	}
	return c
}

// Result is chrono's ParsingResult.
type Result struct {
	reference *Reference
	Index     int
	Text      string
	Start     *Components
	End       *Components
}

func newResult(ref *Reference, index int, text string, start, end *Components) *Result {
	if start == nil {
		start = newComponents(ref, nil)
	}
	return &Result{reference: ref, Index: index, Text: text, Start: start, End: end}
}

func (r *Result) clone() *Result {
	n := newResult(r.reference, r.Index, r.Text, nil, nil)
	n.Start = nil
	if r.Start != nil {
		n.Start = r.Start.clone()
	}
	if r.End != nil {
		n.End = r.End.clone()
	}
	return n
}

func (r *Result) addTag(tag string) *Result {
	r.Start.addTag(tag)
	if r.End != nil {
		r.End.addTag(tag)
	}
	return r
}

// ── date helpers (utils/dates.js) ────────────────────────────────────────────

func assignSimilarDate(c *Components, t js.Date) {
	c.assign("day", t.GetDate())
	c.assign("month", t.GetMonth()+1)
	c.assign("year", t.GetFullYear())
}

func assignSimilarTime(c *Components, t js.Date) {
	c.assign("hour", t.GetHours())
	c.assign("minute", t.GetMinutes())
	c.assign("second", t.GetSeconds())
	c.assign("millisecond", t.GetMilliseconds())
	c.assign("meridiem", meridiemOf(t.GetHours()))
}

func implySimilarDate(c *Components, t js.Date) {
	c.imply("day", t.GetDate())
	c.imply("month", t.GetMonth()+1)
	c.imply("year", t.GetFullYear())
}

func implySimilarTime(c *Components, t js.Date) {
	c.imply("hour", t.GetHours())
	c.imply("minute", t.GetMinutes())
	c.imply("second", t.GetSeconds())
	c.imply("millisecond", t.GetMilliseconds())
	c.imply("meridiem", meridiemOf(t.GetHours()))
}

func meridiemOf(hour int) int {
	if hour < 12 {
		return AM
	}
	return PM
}

// ── calculation/duration.js ──────────────────────────────────────────────────

func addDuration(ref js.Date, d duration) js.Date {
	date := ref
	for short, long := range map[string]string{"y": "year", "mo": "month", "M": "month", "w": "week", "d": "day", "h": "hour", "m": "minute", "s": "second", "ms": "millisecond"} {
		if v, ok := d[short]; ok && v != 0 {
			d[long] = v
			delete(d, short)
		}
	}
	if v, ok := d["year"]; ok {
		floor := math.Floor(v)
		date = date.SetFullYear(date.GetFullYear() + int(floor))
		if rem := v - floor; rem > 0 {
			d["month"] += rem * 12
		}
	}
	if v, ok := d["quarter"]; ok {
		floor := math.Floor(v)
		date = date.SetMonth(date.GetMonth() + int(floor)*3)
	}
	if v, ok := d["month"]; ok {
		floor := math.Floor(v)
		date = date.SetMonth(date.GetMonth() + int(floor))
		if rem := v - floor; rem > 0 {
			d["week"] += rem * 4
		}
	}
	if v, ok := d["week"]; ok {
		floor := math.Floor(v)
		date = date.SetDate(date.GetDate() + int(floor)*7)
		if rem := v - floor; rem > 0 {
			d["day"] += jsRound(rem * 7)
		}
	}
	if v, ok := d["day"]; ok {
		floor := math.Floor(v)
		date = date.SetDate(date.GetDate() + int(floor))
		if rem := v - floor; rem > 0 {
			d["hour"] += jsRound(rem * 24)
		}
	}
	if v, ok := d["hour"]; ok {
		floor := math.Floor(v)
		date = date.SetHours(date.GetHours() + int(floor))
		if rem := v - floor; rem > 0 {
			d["minute"] += jsRound(rem * 60)
		}
	}
	if v, ok := d["minute"]; ok {
		floor := math.Floor(v)
		date = date.SetMinutes(date.GetMinutes() + int(floor))
		if rem := v - floor; rem > 0 {
			d["second"] += jsRound(rem * 60)
		}
	}
	if v, ok := d["second"]; ok {
		floor := math.Floor(v)
		date = date.SetSeconds(date.GetSeconds() + int(floor))
		if rem := v - floor; rem > 0 {
			d["millisecond"] += jsRound(rem * 1000)
		}
	}
	if v, ok := d["millisecond"]; ok {
		floor := math.Floor(v)
		date = date.SetMilliseconds(date.GetMilliseconds() + int(floor))
	}
	return date
}

// jsRound is Math.round: halves round towards +∞.
func jsRound(x float64) float64 { return math.Floor(x + 0.5) }

func reverseDuration(d duration) duration {
	r := duration{}
	for k, v := range d {
		r[k] = -v
	}
	return r
}

func runeLen(s string) int { return len([]rune(s)) }

func substring(r []rune, start, end int) string {
	if start < 0 {
		start = 0
	}
	if end > len(r) {
		end = len(r)
	}
	if start > end {
		start, end = end, start
	}
	return string(r[start:end])
}

func toLower(s string) string { return strings.ToLower(s) }
