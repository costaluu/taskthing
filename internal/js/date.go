package js

import (
	"fmt"
	"math"
	"time"
)

// Date is a JavaScript Date: milliseconds since the epoch (NaN when invalid),
// whose local-time accessors and setters read the process zone (TZ), exactly
// as Bun's do.
type Date struct{ ms float64 }

func DateFromMillis(ms float64) Date { return Date{ms: ms} }
func DateFromTime(t time.Time) Date  { return Date{ms: float64(t.UnixMilli())} }

// Now is `new Date()`.
func Now() Date { return DateFromTime(time.Now()) }

func (d Date) Time() float64    { return d.ms }
func (d Date) Valid() bool      { return !math.IsNaN(d.ms) && math.Abs(d.ms) <= 8.64e15 }
func (d Date) Go() time.Time    { return time.UnixMilli(int64(d.ms)).UTC() }
func (d Date) local() time.Time { return time.UnixMilli(int64(d.ms)).In(time.Local) }
func (d Date) utc() time.Time   { return time.UnixMilli(int64(d.ms)).UTC() }

func (d Date) GetFullYear() int     { return d.local().Year() }
func (d Date) GetMonth() int        { return int(d.local().Month()) - 1 }
func (d Date) GetDate() int         { return d.local().Day() }
func (d Date) GetDay() int          { return int(d.local().Weekday()) }
func (d Date) GetHours() int        { return d.local().Hour() }
func (d Date) GetMinutes() int      { return d.local().Minute() }
func (d Date) GetSeconds() int      { return d.local().Second() }
func (d Date) GetMilliseconds() int { return d.local().Nanosecond() / 1e6 }

func (d Date) GetUTCFullYear() int { return d.utc().Year() }
func (d Date) GetUTCMonth() int    { return int(d.utc().Month()) - 1 }
func (d Date) GetUTCDate() int     { return d.utc().Day() }
func (d Date) GetUTCDay() int      { return int(d.utc().Weekday()) }
func (d Date) GetUTCHours() int    { return d.utc().Hour() }
func (d Date) GetUTCMinutes() int  { return d.utc().Minute() }
func (d Date) GetUTCSeconds() int  { return d.utc().Second() }

// GetTimezoneOffset is minutes *behind* UTC, as JS reports it.
func (d Date) GetTimezoneOffset() int {
	_, off := d.local().Zone()
	return -off / 60
}

type fields struct{ y, mon, day, h, mi, s, ms int }

func (d Date) localFields() fields {
	t := d.local()
	return fields{t.Year(), int(t.Month()) - 1, t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond() / 1e6}
}

func (d Date) utcFields() fields {
	t := d.utc()
	return fields{t.Year(), int(t.Month()) - 1, t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond() / 1e6}
}

func fromLocal(f fields) Date {
	t := time.Date(f.y, time.Month(f.mon+1), f.day, f.h, f.mi, f.s, 0, time.Local)
	return Date{ms: float64(t.UnixMilli()) + float64(f.ms)}
}

func fromUTC(f fields) Date {
	t := time.Date(f.y, time.Month(f.mon+1), f.day, f.h, f.mi, f.s, 0, time.UTC)
	return Date{ms: float64(t.UnixMilli()) + float64(f.ms)}
}

// NewLocal is `new Date(y, mon, day, h, mi, s, ms)`, including JS's quirk that
// a year in 0-99 means 1900+year.
func NewLocal(y, mon, day, h, mi, s, ms int) Date {
	if y >= 0 && y <= 99 {
		y += 1900
	}
	return fromLocal(fields{y, mon, day, h, mi, s, ms})
}

// UTC is `new Date(Date.UTC(y, mon, day, h, mi, s, ms))`, with the same quirk.
func UTC(y, mon, day, h, mi, s, ms int) Date {
	if y >= 0 && y <= 99 {
		y += 1900
	}
	return fromUTC(fields{y, mon, day, h, mi, s, ms})
}

func (d Date) with(set func(*fields), utc bool) Date {
	if !d.Valid() {
		return d
	}
	if utc {
		f := d.utcFields()
		set(&f)
		return fromUTC(f)
	}
	f := d.localFields()
	set(&f)
	return fromLocal(f)
}

func (d Date) SetFullYear(y int) Date     { return d.with(func(f *fields) { f.y = y }, false) }
func (d Date) SetMonth(m int) Date        { return d.with(func(f *fields) { f.mon = m }, false) }
func (d Date) SetDate(n int) Date         { return d.with(func(f *fields) { f.day = n }, false) }
func (d Date) SetHours(n int) Date        { return d.with(func(f *fields) { f.h = n }, false) }
func (d Date) SetMinutes(n int) Date      { return d.with(func(f *fields) { f.mi = n }, false) }
func (d Date) SetSeconds(n int) Date      { return d.with(func(f *fields) { f.s = n }, false) }
func (d Date) SetMilliseconds(n int) Date { return d.with(func(f *fields) { f.ms = n }, false) }

func (d Date) SetUTCFullYear(y int) Date { return d.with(func(f *fields) { f.y = y }, true) }
func (d Date) SetUTCMonth(m int) Date    { return d.with(func(f *fields) { f.mon = m }, true) }
func (d Date) SetUTCDate(n int) Date     { return d.with(func(f *fields) { f.day = n }, true) }

// ToISOString is Date#toISOString.
func (d Date) ToISOString() string {
	t := d.utc()
	return fmt.Sprintf("%04d-%02d-%02dT%02d:%02d:%02d.%03dZ", t.Year(), int(t.Month()), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond()/1e6)
}

// ParseISO parses the ISO-8601 forms `new Date(string)` accepts for the
// timestamps taskthing pins (TASKTHING_NOW and the like).
func ParseISO(s string) (Date, bool) {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.000Z07:00", "2006-01-02T15:04:05Z07:00", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return DateFromTime(t), true
		}
	}
	for _, layout := range []string{"2006-01-02T15:04:05.000", "2006-01-02T15:04:05", "2006-01-02T15:04"} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return DateFromTime(t), true
		}
	}
	return Date{ms: math.NaN()}, false
}
