package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/costaluu/taskthing/internal/js"
	"github.com/costaluu/taskthing/internal/mdwal"
)

func at(iso string) js.Date { d, _ := js.ParseISO(iso); return d }

// ── date-format.test.ts ──────────────────────────────────────────────────────

func TestFormatTaskDate(t *testing.T) {
	wed := at("2026-07-23T12:00:00Z")
	mon := at("2026-07-20T12:00:00Z")
	cases := []struct {
		date, now  js.Date
		completed  bool
		format     string
		text, role string
	}{
		{at("2026-07-20T09:00:00Z"), wed, false, "", "3 days ago", "date-overdue"},
		{at("2026-07-22T09:00:00Z"), wed, false, "", "yesterday", "date-yesterday"},
		{at("2026-07-23T09:00:00Z"), wed, false, "", "today", "date-today"},
		{at("2026-07-24T09:00:00Z"), wed, false, "", "tomorrow", "date-tomorrow"},
		{at("2026-07-25T09:00:00Z"), wed, false, "", "in 2 days", "text-secondary"},
		{at("2026-07-30T09:00:00Z"), wed, false, "", "in 7 days", "text-secondary"},
		{at("2026-07-27T09:00:00Z"), wed, false, "", "in 4 days", "text-secondary"},
		{at("2026-07-29T09:00:00Z"), mon, false, "", "next week", "text-secondary"},
		{at("2026-08-02T09:00:00Z"), mon, false, "", "next week", "text-secondary"},
		{at("2026-08-03T09:00:00Z"), mon, false, "", "3 aug", "text-secondary"},
		{at("2026-08-21T09:00:00Z"), wed, false, "", "21 aug", "text-secondary"},
		{at("2027-03-15T09:00:00Z"), wed, false, "", "mar 2027", "text-secondary"},
		{at("2028-02-01T09:00:00Z"), wed, false, "", "2028", "text-secondary"},
		{at("2026-07-23T09:30:00Z"), wed, true, "america", "2026/07/23 09:30", "text-secondary"},
		{at("2026-07-23T09:30:00Z"), wed, true, "europe", "23/07/2026 09:30", "text-secondary"},
	}
	for _, c := range cases {
		d := c.date
		got := formatTaskDate(&d, c.now, c.completed, c.format)
		if got == nil || got.text != c.text || got.role != c.role {
			t.Errorf("formatTaskDate(%s, %s) = %+v, want %s/%s", d.ToISOString(), c.now.ToISOString(), got, c.text, c.role)
		}
	}
	if formatTaskDate(nil, wed, false, "") != nil {
		t.Error("an undated task has no date segment")
	}
	if got := absoluteDateTime(at("2026-07-25T09:00:00Z"), "europe"); got != "25/07/2026 09:00" {
		t.Errorf("next occurrence: %s", got)
	}
}

// ── updater.test.ts ──────────────────────────────────────────────────────────

type fakeWorld struct {
	state       updateState
	release     release
	fetches     int
	swapped     []byte
	migrated    bool
	swept       bool
	order       []string
	downloadErr error
}

func (w *fakeWorld) updater(current string) *updater {
	return &updater{
		currentVersion: current, os: "linux", arch: "x64",
		now:        func() float64 { return 100 * twelveHours },
		readState:  func() (updateState, error) { return w.state, nil },
		writeState: func(s updateState) error { w.state = s; return nil },
		latest:     func() (release, error) { w.fetches++; return w.release, nil },
		download: func(url string, progress func(int64, *int64)) ([]byte, error) {
			if w.downloadErr != nil {
				return nil, w.downloadErr
			}
			total := int64(4)
			if progress != nil {
				progress(2, &total)
				progress(4, &total)
			}
			w.order = append(w.order, "download")
			return []byte("new!"), nil
		},
		swap:          func(b []byte) error { w.order = append(w.order, "swap"); w.swapped = b; return nil },
		cleanupStale:  func() { w.swept = true },
		runMigrations: func() error { w.order = append(w.order, "migrate"); w.migrated = true; return nil },
	}
}

func newer(version string) release {
	return release{version: version, assets: []asset{{"taskthing-linux-x64", "u1"}, {"taskthing-win32-x64.exe", "u2"}}}
}

func TestUpdateCheck(t *testing.T) {
	w := &fakeWorld{release: newer("1.10.0"), state: updateState{autoUpdate: "confirm"}}
	r, err := w.updater("1.9.0").check(false)
	if err != nil || !r.pending || r.target != "1.10.0" {
		t.Fatalf("semver, not string compare: %+v %v", r, err)
	}
	if w.state.target == nil || *w.state.target != "1.10.0" || w.state.lastCheck == nil {
		t.Fatalf("result persisted: %+v", w.state)
	}
	// Within 12h the cached answer stands, offline.
	if _, err := w.updater("1.9.0").check(false); err != nil || w.fetches != 1 {
		t.Fatalf("cache reused: fetches=%d %v", w.fetches, err)
	}
	// Forced refetches.
	if _, err := w.updater("1.9.0").check(true); err != nil || w.fetches != 2 {
		t.Fatalf("forced refetch: %d", w.fetches)
	}
	for _, v := range []string{"1.9.0", "1.8.0"} {
		w := &fakeWorld{release: newer(v)}
		if r, _ := w.updater("1.9.0").check(false); r.pending {
			t.Errorf("%s is not newer", v)
		}
	}
	w = &fakeWorld{release: release{version: "2.0.0", assets: []asset{{"taskthing-darwin-arm64", "u"}}}}
	if _, err := w.updater("1.0.0").check(false); err == nil || err.Error() != "no release asset for linux/x64 in 2.0.0" {
		t.Errorf("missing asset: %v", err)
	}
}

func TestUpdateApply(t *testing.T) {
	w := &fakeWorld{release: newer("2.0.0"), state: updateState{autoUpdate: "confirm"}}
	if r, _, _ := w.updater("1.0.0").apply(func() bool { return false }, nil, nil); r != declined || w.swapped != nil {
		t.Fatalf("declined applies nothing: %v", r)
	}
	var progress []int64
	applying := false
	r, target, err := w.updater("1.0.0").apply(func() bool { return true }, func(d int64, _ *int64) { progress = append(progress, d) }, func() {
		applying = true
		w.order = append(w.order, "applying")
	})
	if err != nil || r != updated || target != "2.0.0" || string(w.swapped) != "new!" || !w.migrated {
		t.Fatalf("apply: %v %s %v", r, target, err)
	}
	if strings.Join(w.order, ",") != "download,applying,swap,migrate" || !applying || len(progress) != 2 {
		t.Fatalf("order %v progress %v", w.order, progress)
	}
	if w.state.updateAvailable || w.state.target != nil {
		t.Fatalf("pending cleared: %+v", w.state)
	}

	silent := &fakeWorld{release: newer("2.0.0"), state: updateState{autoUpdate: "silent"}}
	if r, _, _ := silent.updater("1.0.0").apply(func() bool { t.Error("asked"); return false }, nil, nil); r != updated {
		t.Errorf("silent mode: %v", r)
	}

	failing := &fakeWorld{release: newer("2.0.0"), state: updateState{autoUpdate: "silent"}, downloadErr: errors.New("boom")}
	if _, _, err := failing.updater("1.0.0").apply(func() bool { return true }, nil, nil); err == nil || failing.swapped != nil {
		t.Error("a failed download never swaps")
	}

	current := &fakeWorld{release: newer("1.0.0")}
	if r, _, _ := current.updater("1.0.0").apply(func() bool { return true }, nil, nil); r != upToDate || !current.swept || current.order != nil {
		t.Errorf("up to date still sweeps, downloads nothing: %v %v", r, current.order)
	}
}

func TestPlatformTokens(t *testing.T) {
	osToken, arch := platformTokens()
	if osToken == "windows" || arch == "amd64" {
		t.Errorf("assets are named by process.platform/arch, got %s/%s", osToken, arch)
	}
}

func TestSemverOrder(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{{"1.10.0", "1.9.0", 1}, {"0.1.0", "0.0.5", 1}, {"1.0.0", "1.0.0", 0}, {"1.0.0-rc.1", "1.0.0", -1}, {"v2.0.0", "1.0.0", 1}} {
		if got := semverOrder(c.a, c.b); got != c.want {
			t.Errorf("order(%s, %s) = %d", c.a, c.b, got)
		}
	}
}

// ── migration-runner.test.ts ─────────────────────────────────────────────────

func runnerFor(t *testing.T, roots []string, migrations []Migration) migrationRunner {
	return migrationRunner{
		migrations: migrations,
		workspaces: roots,
		engine: func(root string) *mdwal.Mdwal {
			return mdwal.New(mdwal.Config{Root: root, Schemas: []mdwal.SchemaEntry{{Type: "task", Schema: mdwal.TaskFields}, {Type: "board", Schema: mdwal.BoardFields}},
				Mode: func() (string, error) { return "remote", nil }, Clock: monotonicClock(nowNanos, 0), Author: func() string { return "t" }})
		},
	}
}

func TestMigrationRunner(t *testing.T) {
	dir := t.TempDir()
	remote, local := filepath.Join(dir, "remote"), filepath.Join(dir, "local")
	os.MkdirAll(remote, 0o755)
	os.MkdirAll(local, 0o755)
	os.WriteFile(filepath.Join(remote, ".gitignore"), []byte("tasks/\n"), 0o644)
	migration := Migration{"0.2.0", "1::CREATE_FOLDER::list::::{\"author\":\"dev\"}\n"}

	if err := runnerFor(t, []string{remote, local}, []Migration{migration}).run(); err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{remote, local} {
		if st, err := os.Stat(filepath.Join(root, "lists")); err != nil || !st.IsDir() {
			t.Errorf("%s: folder not created", root)
		}
		if _, err := os.Stat(filepath.Join(root, "events.log")); err == nil {
			t.Errorf("%s: a migration must not be logged", root)
		}
		if b, _ := os.ReadFile(filepath.Join(root, ".taskthing/migrations/0.2.0.md")); string(b) != migration.Content {
			t.Errorf("%s: not recorded", root)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(remote, ".gitignore")); string(b) != "tasks/\nboards/\n.taskthing/\n" {
		t.Errorf("gitignore regenerated from the schema folders: %q", b)
	}
	if _, err := os.Stat(filepath.Join(local, ".gitignore")); err == nil {
		t.Error("a local workspace does not gain a .gitignore")
	}

	// Recorded: never re-applied, even if its folder is gone.
	os.RemoveAll(filepath.Join(remote, "lists"))
	if err := runnerFor(t, []string{remote}, []Migration{migration}).run(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(remote, "lists")); err == nil {
		t.Error("re-applied a recorded migration")
	}

	// A failure on one workspace is surfaced, unrecorded, and does not block others.
	broken, fine := filepath.Join(dir, "broken"), filepath.Join(dir, "fine")
	os.MkdirAll(broken, 0o755)
	os.MkdirAll(fine, 0o755)
	os.WriteFile(filepath.Join(broken, "olds"), []byte("not a folder"), 0o644)
	rename := Migration{"0.3.0", "1::RENAME_FOLDER::old::::{\"author\":\"dev\",\"to\":\"new\"}\n"}
	os.MkdirAll(filepath.Join(fine, "olds"), 0o755)
	os.MkdirAll(filepath.Join(broken, "news", "x"), 0o755)
	err := runnerFor(t, []string{broken, fine}, []Migration{rename}).run()
	if err == nil || !strings.HasPrefix(err.Error(), "migration failed on 1 target(s): "+broken) {
		t.Errorf("failure surfaced: %v", err)
	}
	if _, err := os.Stat(filepath.Join(fine, "news")); err != nil {
		t.Error("the healthy workspace was still migrated")
	}
	if _, err := os.Stat(filepath.Join(broken, ".taskthing/migrations/0.3.0.md")); err == nil {
		t.Error("a failed migration must stay pending")
	}
}

func TestConfigMigration(t *testing.T) {
	dir := t.TempDir()
	config := js.Obj("currentWorkspace", "main", "dateFormat", "america")
	runner := migrationRunner{
		readConfig:  func() (*js.Object, error) { return config.Clone(), nil },
		writeConfig: func(c *js.Object) error { config = c; return nil },
		configHome:  dir,
		configMigrations: []ConfigMigration{{"0.2.0", func(c *js.Object) *js.Object {
			return with(c, "dateFormat", "europe")
		}}},
	}
	if err := runner.run(); err != nil {
		t.Fatal(err)
	}
	if str(config, "dateFormat") != "europe" {
		t.Error("config not rewritten")
	}
	config.Set("dateFormat", "america")
	if err := runner.run(); err != nil || str(config, "dateFormat") != "america" {
		t.Error("a config migration runs once per machine")
	}
}
