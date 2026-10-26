package app

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/costaluu/taskthing/internal/chrono"
	"github.com/costaluu/taskthing/internal/js"
	"github.com/costaluu/taskthing/internal/rrule"
)

// ── the add string-input parser (add-input.ts) ───────────────────────────────
//
// One line in, a task out: `add "walk the dog d:[tomorrow] r:[every monday]
// b:[chores]"`. A tag is only recognised with its closing `[…]`, so free text
// like "remind Ed: buy milk" stays a title; tag and brackets go together.

var (
	dateTag       = regexp.MustCompile(`(?i)\b(?:d|date):\[([^\]]*)\]`)
	recurrenceTag = regexp.MustCompile(`(?i)\b(?:r|recurrence):\[([^\]]*)\]`)
	boardTag      = regexp.MustCompile(`(?i)\b(?:b|board):\[([^\]]*)\]`)
	jsSpaceRun    = regexp.MustCompile(`[\t\n\v\f\r \x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]{2,}`)
)

type parsedAddInput struct {
	title string
	rrule *string
	board *string
}

func parseAddInput(input string, now js.Date, dateFormat string) (parsedAddInput, error) {
	rest := input
	take := func(pattern *regexp.Regexp) *string {
		loc := pattern.FindStringSubmatchIndex(rest)
		if loc == nil {
			return nil
		}
		value := js.Trim(rest[loc[2]:loc[3]])
		rest = rest[:loc[0]] + rest[loc[1]:]
		return &value
	}
	date := take(dateTag)
	recurrence := take(recurrenceTag)
	board := take(boardTag)
	if board != nil && *board == "" {
		board = nil
	}

	var dtstart *js.Date
	if date != nil {
		d, ok := chrono.ParseNaturalDate(*date, now, dateFormat)
		if !ok {
			return parsedAddInput{}, fmt.Errorf("not a date: %s", *date)
		}
		dtstart = &d
	}
	title := jsSpaceRun.ReplaceAllString(js.Trim(rest), " ")
	if recurrence == nil {
		if dtstart == nil {
			return parsedAddInput{title: title, board: board}, nil
		}
		r, err := rrule.New(rrule.Options{}.With("dtstart", *dtstart))
		if err != nil {
			return parsedAddInput{}, err
		}
		s := r.String()
		return parsedAddInput{title: title, rrule: &s, board: board}, nil
	}
	rule, err := rrule.FromText(*recurrence)
	if err != nil {
		return parsedAddInput{}, err
	}
	if !rule.Orig.Present("freq") {
		return parsedAddInput{}, fmt.Errorf("not a recurrence: %s", *recurrence)
	}
	start := now
	if dtstart != nil {
		start = *dtstart
	}
	r, err := rrule.New(rule.Orig.With("dtstart", start))
	if err != nil {
		return parsedAddInput{}, err
	}
	s := r.String()
	return parsedAddInput{title: title, rrule: &s, board: board}, nil
}

var _ = strings.TrimSpace
