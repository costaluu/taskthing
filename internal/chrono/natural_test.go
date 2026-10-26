package chrono

import (
	"encoding/json"
	"os"
	"os/exec"
	"testing"

	"github.com/costaluu/taskthing/internal/js"
)

// The corpus is what chrono-node answered, per zone; each zone runs in its own
// process because Go reads TZ once, at startup.
func TestNaturalDatesCorpus(t *testing.T) {
	zone := os.Getenv("CORPUS_ZONE")
	if zone == "" {
		for _, z := range []string{"UTC", "America/Recife", "Asia/Tokyo"} {
			cmd := exec.Command(os.Args[0], "-test.run", "^TestNaturalDatesCorpus$")
			cmd.Env = append(os.Environ(), "CORPUS_ZONE="+z, "TZ="+z)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Errorf("zone %s:\n%s", z, out)
			}
		}
		return
	}
	raw, err := os.ReadFile("../../testdata/corpus/dates.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		Zone, Now, DateFormat, Input string
		Result                       *string
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatal(err)
	}
	checked := 0
	defer func() {
		if checked == 0 {
			t.Errorf("no corpus rows for zone %s", zone)
		}
	}()
	for _, row := range rows {
		if row.Zone != zone {
			continue
		}
		checked++
		now, _ := js.ParseISO(row.Now)
		got, ok := ParseNaturalDate(row.Input, now, row.DateFormat)
		var gotS *string
		if ok {
			s := got.ToISOString()
			gotS = &s
		}
		if (gotS == nil) != (row.Result == nil) || (gotS != nil && *gotS != *row.Result) {
			t.Errorf("%s %s %s %q: got %v want %v", row.Zone, row.Now, row.DateFormat, row.Input, str(gotS), str(row.Result))
		}
	}
}

func str(s *string) string {
	if s == nil {
		return "null"
	}
	return *s
}
