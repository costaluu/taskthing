package rrule

import (
	"strings"

	"github.com/costaluu/taskthing/internal/js"
)

// ── recurrence (recurrence.ts) ───────────────────────────────────────────────
//
// Completing a recurring task mints the next occurrence as a new entity that
// inherits the rule, so these work on the stored string, never on a parsed
// rule held anywhere else.

// Recurs reports whether an rrule actually recurs: a DTSTART on its own is a
// dated, non-recurring task, which the library would otherwise read as yearly.
func Recurs(rrule string) bool { return strings.Contains(rrule, "RRULE:") }

// WithDtstart is the same rule starting on a new date (or a bare DTSTART).
func WithDtstart(rrule *string, dtstart js.Date) (string, error) {
	var options Options
	if rrule != nil {
		r, err := FromString(*rrule)
		if err != nil {
			return "", err
		}
		options = r.Orig
	}
	r, err := New(options.With("dtstart", dtstart))
	if err != nil {
		return "", err
	}
	return r.String(), nil
}

// NextOccurrence is the rule of the occurrence after this one, one step on even
// when that step is still in the past; false when the rule is exhausted.
func NextOccurrence(rrule string) (string, bool, error) {
	if !Recurs(rrule) {
		return "", false, nil
	}
	rule, err := FromString(rrule)
	if err != nil {
		return "", false, err
	}
	next, ok := rule.After(rule.options.dtstart)
	if !ok {
		return "", false, nil
	}
	r, err := New(rule.Orig.With("dtstart", next))
	if err != nil {
		return "", false, err
	}
	return r.String(), true, nil
}

// ToTextOf is recurrenceToText: the human phrasing of a recurring rule.
func ToTextOf(rrule string) (string, error) {
	r, err := FromString(rrule)
	if err != nil {
		return "", err
	}
	return r.ToText(), nil
}
