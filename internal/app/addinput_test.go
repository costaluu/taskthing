package app

import (
	"encoding/json"
	"os"
	"os/exec"
	"testing"

	"github.com/costaluu/taskthing/internal/js"
)

// Each zone runs in its own process: Go reads TZ once.
func TestAddInputCorpus(t *testing.T) {
	zone := os.Getenv("CORPUS_ZONE")
	if zone == "" {
		for _, z := range []string{"UTC", "America/Recife", "Asia/Tokyo"} {
			cmd := exec.Command(os.Args[0], "-test.run", "^TestAddInputCorpus$")
			cmd.Env = append(os.Environ(), "CORPUS_ZONE="+z, "TZ="+z)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Errorf("zone %s:\n%s", z, out)
			}
		}
		return
	}
	raw, err := os.ReadFile("../../testdata/corpus/add-input.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		Zone, Now, DateFormat, Input string
		Ok                           *struct {
			Title string
			Rrule *string
			Board *string
		}
		Error *string
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.Zone != zone {
			continue
		}
		now, _ := js.ParseISO(row.Now)
		got, err := parseAddInput(row.Input, now, row.DateFormat)
		if row.Error != nil {
			if err == nil || err.Error() != *row.Error {
				t.Errorf("%s %q: error %v, want %q", zone, row.Input, err, *row.Error)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s %q: %v", zone, row.Input, err)
			continue
		}
		if got.title != row.Ok.Title || str2(got.rrule) != str2(row.Ok.Rrule) || str2(got.board) != str2(row.Ok.Board) {
			t.Errorf("%s %s %q: got %q %s %s, want %q %s %s", zone, row.DateFormat, row.Input, got.title, str2(got.rrule), str2(got.board), row.Ok.Title, str2(row.Ok.Rrule), str2(row.Ok.Board))
		}
	}
}

func str2(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}
