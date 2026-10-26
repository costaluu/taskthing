package rrule

import (
	"encoding/json"
	"os"
	"os/exec"
	"testing"

	"github.com/costaluu/taskthing/internal/js"
)

func TestRecurrenceCorpus(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/corpus/recurrence.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		Rrule *string
		Next  *string
		Moved string
		Text  *string
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatal(err)
	}
	moveTo, _ := js.ParseISO("2026-09-01T08:00:00.000Z")
	for _, row := range rows {
		moved, err := WithDtstart(row.Rrule, moveTo)
		if err != nil || moved != row.Moved {
			t.Errorf("withDtstart(%v) = %q, %v; want %q", row.Rrule, moved, err, row.Moved)
		}
		if row.Rrule == nil {
			continue
		}
		next, ok, err := NextOccurrence(*row.Rrule)
		if err != nil || ok != (row.Next != nil) || (ok && next != *row.Next) {
			t.Errorf("nextOccurrence(%q) = %q %v %v; want %v", *row.Rrule, next, ok, err, row.Next)
		}
		if row.Text != nil {
			text, err := ToTextOf(*row.Rrule)
			if err != nil || text != *row.Text {
				t.Errorf("toText(%q) = %q, %v; want %q", *row.Rrule, text, err, *row.Text)
			}
		}
	}
}

func TestValidityCorpus(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/corpus/rrule-validity.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		Rrule string
		Valid bool
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if got := Valid(row.Rrule); got != row.Valid {
			t.Errorf("Valid(%q) = %v, want %v", row.Rrule, got, row.Valid)
		}
	}
}

// Zones run in their own processes (Go reads TZ once), as the chrono corpus does.
func TestRecurrenceTextCorpus(t *testing.T) {
	zone := os.Getenv("CORPUS_ZONE")
	if zone == "" {
		for _, z := range []string{"UTC", "America/Recife", "Asia/Tokyo"} {
			cmd := exec.Command(os.Args[0], "-test.run", "^TestRecurrenceTextCorpus$")
			cmd.Env = append(os.Environ(), "CORPUS_ZONE="+z, "TZ="+z)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Errorf("zone %s:\n%s", z, out)
			}
		}
		return
	}
	raw, err := os.ReadFile("../../testdata/corpus/recurrence-text.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		Zone, Text string
		Ok         *struct{ Rrule, Text string }
		Error      *string
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatal(err)
	}
	dtstart, _ := js.ParseISO("2026-07-15T14:30:00.000Z")
	for _, row := range rows {
		if row.Zone != zone {
			continue
		}
		var gotRule, gotText, gotErr string
		o, err := ParseText(row.Text)
		if err == nil && o.present("freq") {
			var r *RRule
			if r, err = New(o.With("dtstart", dtstart)); err == nil {
				gotRule, gotText = r.String(), r.ToText()
			}
		}
		if err != nil {
			gotErr = err.Error()
		}
		switch {
		case row.Error != nil:
			if gotErr != *row.Error {
				t.Errorf("%s %q: error %q, want %q", zone, row.Text, gotErr, *row.Error)
			}
		case row.Ok == nil:
			if gotRule != "" || gotErr != "" {
				t.Errorf("%s %q: got %q/%q, want no rule", zone, row.Text, gotRule, gotErr)
			}
		default:
			if gotRule != row.Ok.Rrule || gotText != row.Ok.Text {
				t.Errorf("%s %q: got %q / %q (%s), want %q / %q", zone, row.Text, gotRule, gotText, gotErr, row.Ok.Rrule, row.Ok.Text)
			}
		}
	}
}
