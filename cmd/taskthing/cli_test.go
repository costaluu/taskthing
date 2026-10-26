package main

// The CLI seam is the binary: every test runs taskthing as its own process
// against a temporary data home, then asserts on the workspace files it
// produced, on the kv_store, and on the command's piped output and exit code.
// Nothing here reaches into the command tree, so the same suite judges any
// build that claims to be taskthing — `TASKTHING_BIN=<path> go test ./cmd/...`
// runs it against another binary (the TS one it was ported from, say).

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/costaluu/taskthing/internal/js"
	"github.com/costaluu/taskthing/internal/mdwal"
)

var binary string

func TestMain(m *testing.M) {
	binary = os.Getenv("TASKTHING_BIN")
	if binary == "" {
		dir, err := os.MkdirTemp("", "taskthing-bin-")
		if err != nil {
			panic(err)
		}
		binary = filepath.Join(dir, "taskthing")
		if out, err := exec.Command("go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
			panic(string(out))
		}
		code := m.Run()
		os.RemoveAll(dir)
		os.Exit(code)
	}
	os.Exit(m.Run())
}

// Natural-language dates are relative to now, so tests pin it.
const now = "2026-07-22T09:00:00.000Z"

type run struct {
	code           int
	stdout, stderr string
}

// taskthing runs one command piped. `path` is split on spaces so a test reads
// like the command line; the rest passes through, so a title stays one argument.
func taskthing(t *testing.T, home, path string, args ...string) run {
	t.Helper()
	argv := append(strings.Fields(path), args...)
	cmd := exec.Command(binary, argv...)
	cmd.Env = append(os.Environ(), "TASKTHING_HOME="+home, "TASKTHING_NOW="+now)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	code := 0
	if exit, ok := err.(*exec.ExitError); ok {
		code = exit.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return run{code, out.String(), errOut.String()}
}

func mustCode(t *testing.T, r run, want int) {
	t.Helper()
	if r.code != want {
		t.Fatalf("exit %d, want %d\nstdout: %s\nstderr: %s", r.code, want, r.stdout, r.stderr)
	}
}

// dataHome is a data home with one workspace, which config.md names current.
func dataHome(t *testing.T, workspace ...string) string {
	t.Helper()
	ws := "main"
	if len(workspace) > 0 {
		ws = workspace[0]
	}
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, "workspaces", ws), 0o755)
	os.WriteFile(filepath.Join(home, "config.md"), []byte("---\ncurrentWorkspace: "+js.Quote(ws)+"\n---\n"), 0o644)
	return home
}

// workspaceMdwal reads a workspace back through the engine, as any consumer would.
func workspaceMdwal(home string, workspace ...string) *mdwal.Mdwal {
	ws := "main"
	if len(workspace) > 0 {
		ws = workspace[0]
	}
	return mdwal.New(mdwal.Config{
		Root:    filepath.Join(home, "workspaces", ws),
		Schemas: []mdwal.SchemaEntry{{Type: "task", Schema: mdwal.TaskFields}, {Type: "board", Schema: mdwal.BoardFields}},
		Mode:    func() (string, error) { return "local", nil },
		Clock:   func() int64 { return 1 },
		Author:  func() string { return "test" },
	})
}

func readAll(t *testing.T, kind, home string, workspace ...string) []*js.Object {
	t.Helper()
	all, err := workspaceMdwal(home, workspace...).ReadAll(kind)
	if err != nil {
		t.Fatal(err)
	}
	return all
}

func tasks(t *testing.T, home string, workspace ...string) []*js.Object {
	t.Helper()
	return readAll(t, "task", home, workspace...)
}

func get(o *js.Object, k string) any { return o.Value(k) }
func title(o *js.Object) string      { s, _ := o.Value("title").(string); return s }
func id(o *js.Object) string         { s, _ := o.Value("id").(string); return s }
func flag(o *js.Object, k string) bool {
	b, _ := o.Value(k).(bool)
	return b
}

func seedTask(t *testing.T, home string, fields *js.Object, workspace ...string) {
	t.Helper()
	if _, err := workspaceMdwal(home, workspace...).CreateEntity("task", fields, nil); err != nil {
		t.Fatal(err)
	}
}

// kvStore is the number↔nanoid map a listing leaves behind.
func kvStore(t *testing.T, home, kind string, workspace ...string) map[string]string {
	t.Helper()
	ws := "main"
	if len(workspace) > 0 {
		ws = workspace[0]
	}
	return readJSON(t, filepath.Join(home, "workspaces", ws, ".taskthing", "kv", kind+".json"))
}

func readJSON(t *testing.T, path string) map[string]string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]string{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func keys(m map[string]string) []string {
	var k []string
	for key := range m {
		k = append(k, key)
	}
	sort.Strings(k)
	return k
}

func parseLog(t *testing.T, home string) []mdwal.Event {
	t.Helper()
	return workspaceMdwal(home).ParseLog()
}

// remote is an empty bare repository to associate with.
func remote(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git(t, "init", "--quiet", "--bare", "--initial-branch=master", dir)
	return dir
}

func git(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %s", args, out)
	}
	return string(out)
}

func associate(t *testing.T, home, remote string) {
	t.Helper()
	mustCode(t, taskthing(t, home, "workspace remote add", remote), 0)
}

func contains(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("%q does not contain %q", haystack, needle)
	}
}

func lacks(t *testing.T, haystack, needle string) {
	t.Helper()
	if strings.Contains(haystack, needle) {
		t.Errorf("%q contains %q", haystack, needle)
	}
}

func matches(t *testing.T, haystack, pattern string) {
	t.Helper()
	if !regexp.MustCompile(pattern).MatchString(haystack) {
		t.Errorf("%q does not match %s", haystack, pattern)
	}
}

func equal[T comparable](t *testing.T, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func byTitle(ts []*js.Object, name string) *js.Object {
	for _, task := range ts {
		if title(task) == name {
			return task
		}
	}
	return nil
}

func numberOf(listing, name string) string {
	for _, line := range strings.Split(listing, "\n") {
		if strings.Contains(line, name) {
			return strings.Fields(line)[0]
		}
	}
	return ""
}

// ── add ──────────────────────────────────────────────────────────────────────

func TestAddReadsDateTag(t *testing.T) {
	t.Parallel()
	home := dataHome(t)
	mustCode(t, taskthing(t, home, "add", "walk the dog d:[tomorrow]"), 0)
	task := tasks(t, home)[0]
	// The tag and its brackets are removed wholesale.
	equal(t, title(task), "walk the dog")
	// A DTSTART on its own, resolved against now, stored as RFC 5545.
	equal(t, get(task, "rrule"), any("DTSTART:20260723T000000Z"))
}

func TestAddReadsRecurrenceTag(t *testing.T) {
	t.Parallel()
	home := dataHome(t)
	taskthing(t, home, "add", "walk the dog r:[every monday]")
	taskthing(t, home, "add", "water plants d:[in 3 days] recurrence:[every day]")
	ts := tasks(t, home)
	walk := get(byTitle(ts, "walk the dog"), "rrule").(string)
	contains(t, walk, "RRULE:FREQ=WEEKLY;BYDAY=MO")
	contains(t, walk, "DTSTART:20260722T090000Z")
	equal(t, get(byTitle(ts, "water plants"), "rrule"), any("DTSTART:20260725T000000Z\nRRULE:FREQ=DAILY"))
}

func TestAddTagNeedsBrackets(t *testing.T) {
	t.Parallel()
	home := dataHome(t)
	taskthing(t, home, "add", "remind Ed: buy milk")
	taskthing(t, home, "add", "read about r: recurrence")
	var titles []string
	for _, task := range tasks(t, home) {
		titles = append(titles, title(task))
		if get(task, "rrule") != nil {
			t.Error("a colon is not a tag")
		}
	}
	sort.Strings(titles)
	equal(t, strings.Join(titles, "|"), "read about r: recurrence|remind Ed: buy milk")
}

func TestAddCreatesFromTitleAlone(t *testing.T) {
	t.Parallel()
	home := dataHome(t)
	mustCode(t, taskthing(t, home, "add", "walk the dog"), 0)
	ts := tasks(t, home)
	if len(ts) != 1 {
		t.Fatalf("%d tasks", len(ts))
	}
	task := ts[0]
	equal(t, title(task), "walk the dog")
	equal(t, get(task, "board"), any("inbox"))
	equal(t, get(task, "rrule"), nil)
	equal(t, flag(task, "completed"), false)
	equal(t, flag(task, "deleted"), false)
	equal(t, len(id(task)), 21)
}

// ── events and the remote ────────────────────────────────────────────────────

func TestLogsEventsExactlyWhenRemote(t *testing.T) {
	t.Parallel()
	home := dataHome(t)
	taskthing(t, home, "add", "local only")
	equal(t, len(parseLog(t, home)), 0)

	associate(t, home, remote(t))
	taskthing(t, home, "add", "shared work")
	events := parseLog(t, home)
	if len(events) != 1 {
		t.Fatalf("%d events", len(events))
	}
	equal(t, events[0].Op, "CREATE")
	equal(t, title(events[0].Payload.Value("snapshot").(*js.Object)), "shared work")
}

func TestEventCarriesVersion(t *testing.T) {
	t.Parallel()
	home := dataHome(t)
	associate(t, home, remote(t))
	taskthing(t, home, "add", "shared work")
	version := strings.TrimPrefix(strings.TrimSpace(taskthing(t, home, "--format=toon --version").stdout), "taskthing v")
	equal(t, parseLog(t, home)[0].Payload.Value("version"), any(version))
}

func TestClockSeededFromLog(t *testing.T) {
	t.Parallel()
	home := dataHome(t)
	associate(t, home, remote(t))
	const future = int64(9_000_000_000_000_000_000)
	logPath := filepath.Join(home, "workspaces", "main", "events.log")
	prior, _ := os.ReadFile(logPath)
	seeded := fmt.Sprintf("%d::CREATE::task::seeded::{\"author\":\"alice\",\"snapshot\":{\"title\":\"seed\"},\"version\":\"0.1.0\"}\n", future)
	os.WriteFile(logPath, append(prior, seeded...), 0o644)

	taskthing(t, home, "add", "after the step-back")
	var mine []mdwal.Event
	for _, ev := range parseLog(t, home) {
		if s, ok := ev.Payload.Value("snapshot").(*js.Object); ok && title(s) == "after the step-back" {
			mine = append(mine, ev)
		}
	}
	if len(mine) != 1 || mine[0].Ts <= future {
		t.Fatalf("the clock must stay above the log's last line: %+v", mine)
	}
}

func TestPullRefusesNewerEvent(t *testing.T) {
	t.Parallel()
	home := dataHome(t)
	associate(t, home, remote(t))
	logPath := filepath.Join(home, "workspaces", "main", "events.log")
	prior, _ := os.ReadFile(logPath)
	ahead := "1800000000000000000::CREATE::task::ahead::{\"author\":\"alice\",\"snapshot\":{\"title\":\"future\"},\"version\":\"9.9.9\"}\n"
	os.WriteFile(logPath, append(prior, ahead...), 0o644)

	r := taskthing(t, home, "pull")
	mustCode(t, r, 1)
	lacks(t, r.stdout, "pulled")
	contains(t, r.stdout+r.stderr, "9.9.9")
}

func TestPipedGitFailureSurfacesReason(t *testing.T) {
	t.Parallel()
	home := dataHome(t)
	r := taskthing(t, home, "pull")
	mustCode(t, r, 1)
	contains(t, r.stdout+r.stderr, "something went wrong during pull")
}

// ── entity (plumber) ─────────────────────────────────────────────────────────

func entityEvents(t *testing.T, r run) []mdwal.Event {
	t.Helper()
	mustCode(t, r, 0)
	events, err := mdwal.ParseEvents(r.stdout)
	if err != nil || len(events) != 1 {
		t.Fatalf("one migration line, got %q (%v)", r.stdout, err)
	}
	return events
}

func TestEntityCreate(t *testing.T) {
	t.Parallel()
	home := dataHome(t)
	ev := entityEvents(t, taskthing(t, home, "entity create", "list", "--log"))[0]
	equal(t, ev.Op, "CREATE_FOLDER")
	equal(t, ev.EntityType, "list")
	if v, _ := ev.Payload.Value("version").(string); v == "" {
		t.Error("the authoring version is stamped")
	}
}

func TestEntityRename(t *testing.T) {
	t.Parallel()
	home := dataHome(t)
	ev := entityEvents(t, taskthing(t, home, "entity rename", "list", "collection", "--log"))[0]
	equal(t, ev.Op, "RENAME_FOLDER")
	equal(t, ev.EntityType, "list")
	equal(t, ev.Payload.Value("to"), any("collection"))
}

func TestEntityDelete(t *testing.T) {
	t.Parallel()
	home := dataHome(t)
	ev := entityEvents(t, taskthing(t, home, "entity delete", "list", "--log"))[0]
	equal(t, ev.Op, "DELETE_FOLDER")
	equal(t, ev.EntityType, "list")
}

func TestEntityNeedsLog(t *testing.T) {
	t.Parallel()
	home := dataHome(t)
	r := taskthing(t, home, "entity create", "list")
	mustCode(t, r, 1)
	contains(t, r.stderr, "--log")
}

// ── help and routing ─────────────────────────────────────────────────────────

type envelope struct {
	Ok    bool
	Data  map[string]any
	Error map[string]any
}

func parseEnvelope(t *testing.T, text string) envelope {
	t.Helper()
	var e envelope
	if err := json.Unmarshal([]byte(text), &e); err != nil {
		t.Fatalf("not an envelope: %q", text)
	}
	return e
}

func TestHelpSurface(t *testing.T) {
	t.Parallel()
	home := dataHome(t)
	for _, args := range [][]string{{"help"}, {}, {"--help"}, {"-h"}} {
		cmd := strings.Join(args, " ")
		r := taskthing(t, home, cmd)
		mustCode(t, r, 0)
		e := parseEnvelope(t, r.stdout)
		if !e.Ok || e.Data["type"] != "help" || e.Data["cliName"] != "taskthing" {
			t.Fatalf("%v: %+v", args, e)
		}
		for _, c := range []string{"add", "list", "board", "sync", "config", "update", "workspace"} {
			contains(t, e.Data["text"].(string), c)
		}
	}
}

func TestUnknownCommand(t *testing.T) {
	t.Parallel()
	home := dataHome(t)
	r := taskthing(t, home, "frobnicate")
	mustCode(t, r, 1)
	e := parseEnvelope(t, r.stderr)
	if e.Ok || e.Error["kind"] != "command-not-found" || e.Error["command"] != "frobnicate" {
		t.Fatalf("%+v", e)
	}
	available := fmt.Sprint(e.Error["available"])
	contains(t, available, "add")
	contains(t, available, "board")
}

// ── listing and numbers ──────────────────────────────────────────────────────

func TestListNumbersAndRecords(t *testing.T) {
	t.Parallel()
	home := dataHome(t)
	taskthing(t, home, "add", "walk the dog")
	taskthing(t, home, "add", "buy milk")
	r := taskthing(t, home, "list")
	mustCode(t, r, 0)
	for _, s := range []string{"1", "walk the dog", "2", "buy milk"} {
		contains(t, r.stdout, s)
	}
	lacks(t, r.stdout, "inbox")
	store := kvStore(t, home, "tasks")
	equal(t, strings.Join(keys(store), ","), "1,2")
	ids := map[string]bool{}
	for _, task := range tasks(t, home) {
		ids[id(task)] = true
	}
	if !ids[store["1"]] || !ids[store["2"]] || store["1"] == store["2"] {
		t.Errorf("store %v", store)
	}
}

func TestListingResetsNumbers(t *testing.T) {
	t.Parallel()
	home := dataHome(t)
	taskthing(t, home, "add", "first")
	taskthing(t, home, "add", "second")
	taskthing(t, home, "list")
	equal(t, len(kvStore(t, home, "tasks")), 2)
	taskthing(t, home, "list")
	equal(t, strings.Join(keys(kvStore(t, home, "tasks")), ","), "1,2")
	taskthing(t, home, "add", "third")
	taskthing(t, home, "list")
	equal(t, strings.Join(keys(kvStore(t, home, "tasks")), ","), "1,2,3")
}

func TestListedNumberResolves(t *testing.T) {
	t.Parallel()
	home := dataHome(t)
	taskthing(t, home, "add", "walk the dog")
	taskthing(t, home, "add", "buy milk")
	taskthing(t, home, "list")
	store := kvStore(t, home, "tasks")
	mustCode(t, taskthing(t, home, "star", "2"), 0)
	var starred []string
	for _, task := range tasks(t, home) {
		if flag(task, "star") {
			starred = append(starred, id(task))
		}
	}
	equal(t, strings.Join(starred, ","), store["2"])
}

func completedIDs(t *testing.T, home string) string {
	var ids []string
	for _, task := range tasks(t, home) {
		if flag(task, "completed") {
			ids = append(ids, id(task))
		}
	}
	sort.Strings(ids)
	return strings.Join(ids, ",")
}

func sorted(s ...string) string { sort.Strings(s); return strings.Join(s, ",") }

func TestVerbActsOnEveryNumber(t *testing.T) {
	t.Parallel()
	home := dataHome(t)
	for _, s := range []string{"walk the dog", "buy milk", "water plants"} {
		taskthing(t, home, "add", s)
	}
	taskthing(t, home, "list")
	store := kvStore(t, home, "tasks")
	mustCode(t, taskthing(t, home, "check", "1", "3"), 0)
	equal(t, completedIDs(t, home), sorted(store["1"], store["3"]))
}

func TestBadReferenceAmongSeveral(t *testing.T) {
	t.Parallel()
	home := dataHome(t)
	taskthing(t, home, "add", "walk the dog")
	taskthing(t, home, "add", "buy milk")
	taskthing(t, home, "list")
	store := kvStore(t, home, "tasks")
	r := taskthing(t, home, "check", "1", "9", "2")
	mustCode(t, r, 1)
	matches(t, r.stderr, `(?i)list`)
	equal(t, completedIDs(t, home), sorted(store["1"], store["2"]))
}

func TestEveryVerbRefusesUnknownReference(t *testing.T) {
	t.Parallel()
	home := dataHome(t)
	taskthing(t, home, "add", "walk the dog")
	for _, v := range [][]string{
		{"check", "9"}, {"uncheck", "9"}, {"star", "9"}, {"unstar", "9"}, {"delete", "9"},
		{"recover", "9"}, {"clear", "9"}, {"set title", "9", "new title"},
		{"set description", "9", "notes"}, {"set date-time", "9", "tomorrow"}, {"set board", "9", "work"},
	} {
		r := taskthing(t, home, v[0], v[1:]...)
		mustCode(t, r, 1)
		matches(t, r.stderr, `(?i)list`)
	}
	ts := tasks(t, home)
	if len(ts) != 1 || title(ts[0]) != "walk the dog" || flag(ts[0], "completed") || flag(ts[0], "deleted") {
		t.Errorf("untouched: %v", ts)
	}
}

func TestUnlistedNumberIsAnError(t *testing.T) {
	t.Parallel()
	home := dataHome(t)
	taskthing(t, home, "add", "walk the dog")
	r := taskthing(t, home, "star", "1")
	mustCode(t, r, 1)
	matches(t, r.stderr, `(?i)list`)
	for _, task := range tasks(t, home) {
		if flag(task, "star") {
			t.Error("acted on a guess")
		}
	}
}

func TestListHidesCompleted(t *testing.T) {
	t.Parallel()
	home := dataHome(t)
	seedTask(t, home, js.Obj("title", "still open"))
	seedTask(t, home, js.Obj("title", "already done", "completed", true))
	plain := taskthing(t, home, "list")
	contains(t, plain.stdout, "still open")
	lacks(t, plain.stdout, "already done")
	checked := taskthing(t, home, "list", "--checked")
	contains(t, checked.stdout, "still open")
	contains(t, checked.stdout, "already done")
	taskthing(t, home, "list")
	equal(t, len(kvStore(t, home, "tasks")), 1)
}

func lineCount(s string) int { return len(strings.Split(strings.TrimSpace(s), "\n")) }

func TestListFiltersCompose(t *testing.T) {
	t.Parallel()
	home := dataHome(t)
	seedTask(t, home, js.Obj("title", "plain"))
	seedTask(t, home, js.Obj("title", "favourite", "star", true))
	seedTask(t, home, js.Obj("title", "documented", "description", "with notes"))
	seedTask(t, home, js.Obj("title", "favourite and documented", "star", true, "description", "why"))
	seedTask(t, home, js.Obj("title", "in the bin", "deleted", true))

	lacks(t, taskthing(t, home, "list").stdout, "in the bin")
	bin := taskthing(t, home, "list", "--deleted")
	contains(t, bin.stdout, "in the bin")
	lacks(t, bin.stdout, "plain")
	equal(t, lineCount(bin.stdout), 1)
	starred := taskthing(t, home, "list", "--starred")
	contains(t, starred.stdout, "favourite")
	lacks(t, starred.stdout, "plain")
	documented := taskthing(t, home, "list", "--hasDescription")
	contains(t, documented.stdout, "documented")
	lacks(t, documented.stdout, "plain")
	both := taskthing(t, home, "list", "--starred", "--hasDescription")
	contains(t, both.stdout, "favourite and documented")
	equal(t, lineCount(both.stdout), 1)
}

func TestInBoardFilter(t *testing.T) {
	t.Parallel()
	home := dataHome(t)
	m := workspaceMdwal(home)
	work, _ := m.CreateEntity("board", js.Obj("name", "work"), nil)
	house, _ := m.CreateEntity("board", js.Obj("name", "home"), nil)
	seedTask(t, home, js.Obj("title", "unfiled"))
	seedTask(t, home, js.Obj("title", "at work", "board", id(work)))
	seedTask(t, home, js.Obj("title", "at home", "board", id(house)))

	inWork := taskthing(t, home, "list", "--in-board=work")
	contains(t, inWork.stdout, "at work")
	lacks(t, inWork.stdout, "at home")
	lacks(t, inWork.stdout, "unfiled")
	inbox := taskthing(t, home, "list", "--in-board=inbox")
	contains(t, inbox.stdout, "unfiled")
	lacks(t, inbox.stdout, "at work")
}

func TestGlobalListing(t *testing.T) {
	t.Parallel()
	home := dataHome(t)
	os.MkdirAll(filepath.Join(home, "workspaces", "home"), 0o755)
	seedTask(t, home, js.Obj("title", "ship the release"))
	seedTask(t, home, js.Obj("title", "fix the sink"), "home")

	local := taskthing(t, home, "list")
	contains(t, local.stdout, "ship the release")
	lacks(t, local.stdout, "fix the sink")
	global := taskthing(t, home, "list", "--global")
	for _, s := range []string{"ship the release", "fix the sink", "main", "home"} {
		contains(t, global.stdout, s)
	}
	numbers := readJSON(t, filepath.Join(home, ".kv", "global.json"))
	other := ""
	for n, v := range numbers {
		if strings.HasPrefix(v, "home/") {
			other = n
		}
	}
	mustCode(t, taskthing(t, home, "star", other), 0)
	equal(t, flag(tasks(t, home, "home")[0], "star"), true)
	equal(t, flag(tasks(t, home)[0], "star"), false)
}

// ── boards ───────────────────────────────────────────────────────────────────

func TestBoardAdd(t *testing.T) {
	t.Parallel()
	home := dataHome(t)
	mustCode(t, taskthing(t, home, "board add", "work"), 0)
	boards := readAll(t, "board", home)
	if len(boards) != 1 {
		t.Fatal(len(boards))
	}
	b := boards[0]
	equal(t, get(b, "name"), any("work"))
	equal(t, len(id(b)), 21)
	equal(t, get(b, "icon"), any(""))
	equal(t, get(b, "color"), any(""))
	equal(t, flag(b, "deleted"), false)
	equal(t, len(tasks(t, home)), 0)
}

func TestBoardListOwnStore(t *testing.T) {
	t.Parallel()
	home := dataHome(t)
	taskthing(t, home, "board add", "work")
	taskthing(t, home, "board add", "home")
	taskthing(t, home, "add", "a task")
	taskthing(t, home, "list")
	r := taskthing(t, home, "board list")
	mustCode(t, r, 0)
	contains(t, r.stdout, "work")
	contains(t, r.stdout, "home")
	lacks(t, r.stdout, "a task")
	boards := kvStore(t, home, "boards")
	equal(t, strings.Join(keys(boards), ","), "1,2")
	equal(t, len(kvStore(t, home, "tasks")), 1)
	ids := map[string]bool{}
	for _, b := range readAll(t, "board", home) {
		ids[id(b)] = true
	}
	if !ids[boards["1"]] || !ids[boards["2"]] {
		t.Errorf("%v", boards)
	}
}

func TestBoardSet(t *testing.T) {
	t.Parallel()
	home := dataHome(t)
	taskthing(t, home, "board add", "wrok")
	taskthing(t, home, "board list")
	mustCode(t, taskthing(t, home, "board set name", "1", "work"), 0)
	mustCode(t, taskthing(t, home, "board set icon", "1", "💼"), 0)
	mustCode(t, taskthing(t, home, "board set color", "1", "blue"), 0)
	b := readAll(t, "board", home)[0]
	equal(t, get(b, "name"), any("work"))
	equal(t, get(b, "icon"), any("💼"))
	equal(t, get(b, "color"), any("blue"))
	mustCode(t, taskthing(t, home, "board set", "size", "1", "large"), 1)
}

func TestBoardDeleteRecover(t *testing.T) {
	t.Parallel()
	home := dataHome(t)
	taskthing(t, home, "board add", "work")
	taskthing(t, home, "board list")
	boardID := kvStore(t, home, "boards")["1"]
	seedTask(t, home, js.Obj("title", "at work", "board", boardID))
	seedTask(t, home, js.Obj("title", "at work too", "board", boardID))
	seedTask(t, home, js.Obj("title", "unfiled"))

	mustCode(t, taskthing(t, home, "board delete", "1"), 0)
	for _, task := range tasks(t, home) {
		equal(t, get(task, "board"), any("inbox"))
	}
	b, _ := workspaceMdwal(home).Read("board", boardID)
	equal(t, flag(b, "deleted"), true)

	mustCode(t, taskthing(t, home, "board recover", boardID), 0)
	b, _ = workspaceMdwal(home).Read("board", boardID)
	equal(t, flag(b, "deleted"), false)
	for _, task := range tasks(t, home) {
		equal(t, get(task, "board"), any("inbox"))
	}
}

func TestBoardDeleteTasks(t *testing.T) {
	t.Parallel()
	home := dataHome(t)
	taskthing(t, home, "board add", "work")
	taskthing(t, home, "board list")
	boardID := kvStore(t, home, "boards")["1"]
	seedTask(t, home, js.Obj("title", "at work", "board", boardID))
	seedTask(t, home, js.Obj("title", "unfiled"))

	mustCode(t, taskthing(t, home, "board delete", "1", "--tasks"), 0)
	for _, task := range tasks(t, home) {
		equal(t, flag(task, "deleted"), get(task, "title") == "at work")
		if get(task, "title") == "at work" {
			equal(t, get(task, "board"), any(boardID))
		}
	}
	b, _ := workspaceMdwal(home).Read("board", boardID)
	equal(t, flag(b, "deleted"), true)
}

func TestBoardClear(t *testing.T) {
	t.Parallel()
	home := dataHome(t)
	taskthing(t, home, "board add", "work")
	taskthing(t, home, "board list")
	boardID := kvStore(t, home, "boards")["1"]
	seedTask(t, home, js.Obj("title", "at work", "board", boardID))
	seedTask(t, home, js.Obj("title", "at work too", "board", boardID))
	seedTask(t, home, js.Obj("title", "unfiled"))

	mustCode(t, taskthing(t, home, "board clear", "1"), 0)
	for _, task := range tasks(t, home) {
		equal(t, flag(task, "deleted"), get(task, "title") != "unfiled")
	}
	b, _ := workspaceMdwal(home).Read("board", boardID)
	equal(t, flag(b, "deleted"), false)
}

// ── task verbs ───────────────────────────────────────────────────────────────

func TestCheckOneOffMintsNothing(t *testing.T) {
	t.Parallel()
	home := dataHome(t)
	taskthing(t, home, "add", "walk the dog")
	taskthing(t, home, "add", "pay the bill d:[tomorrow]")
	taskthing(t, home, "list")
	mustCode(t, taskthing(t, home, "check", "1"), 0)
	mustCode(t, taskthing(t, home, "check", "2"), 0)
	ts := tasks(t, home)
	equal(t, len(ts), 2)
	for _, task := range ts {
		equal(t, flag(task, "completed"), true)
	}
	equal(t, strings.TrimSpace(taskthing(t, home, "list").stdout), "")
}

func TestCheckRecurringMintsNext(t *testing.T) {
	t.Parallel()
	home := dataHome(t)
	rule := "DTSTART:20260701T090000Z\nRRULE:FREQ=WEEKLY;BYDAY=MO"
	seedTask(t, home, js.Obj("title", "water plants", "rrule", rule))
	taskthing(t, home, "list")
	original := kvStore(t, home, "tasks")["1"]
	mustCode(t, taskthing(t, home, "check", "1"), 0)
	ts := tasks(t, home)
	equal(t, len(ts), 2)
	for _, task := range ts {
		if id(task) == original {
			equal(t, flag(task, "completed"), true)
			equal(t, get(task, "rrule"), any(rule))
		} else {
			equal(t, title(task), "water plants")
			equal(t, flag(task, "completed"), false)
			// One step on, still in the past: overdue occurrences catch up one at a time.
			equal(t, get(task, "rrule"), any("DTSTART:20260706T090000Z\nRRULE:FREQ=WEEKLY;BYDAY=MO"))
		}
	}
}

func TestExhaustedRecurrenceMintsNothing(t *testing.T) {
	t.Parallel()
	home := dataHome(t)
	seedTask(t, home, js.Obj("title", "last one", "rrule", "DTSTART:20260701T090000Z\nRRULE:FREQ=WEEKLY;COUNT=1"))
	seedTask(t, home, js.Obj("title", "ran out", "rrule", "DTSTART:20260701T090000Z\nRRULE:FREQ=WEEKLY;UNTIL=20260705T000000Z"))
	taskthing(t, home, "list")
	taskthing(t, home, "check", "1")
	taskthing(t, home, "check", "2")
	ts := tasks(t, home)
	equal(t, len(ts), 2)
	for _, task := range ts {
		equal(t, flag(task, "completed"), true)
	}
}

func TestDeleteRecoverAsEvents(t *testing.T) {
	t.Parallel()
	home := dataHome(t)
	taskthing(t, home, "add", "walk the dog")
	associate(t, home, remote(t))
	taskthing(t, home, "list")
	taskID := kvStore(t, home, "tasks")["1"]

	mustCode(t, taskthing(t, home, "delete", "1"), 0)
	task, _ := workspaceMdwal(home).Read("task", taskID)
	equal(t, flag(task, "deleted"), true)
	equal(t, strings.TrimSpace(taskthing(t, home, "list").stdout), "")

	taskthing(t, home, "list", "--deleted")
	mustCode(t, taskthing(t, home, "recover", "1"), 0)
	task, _ = workspaceMdwal(home).Read("task", taskID)
	equal(t, flag(task, "deleted"), false)

	var fields []string
	for _, ev := range parseLog(t, home) {
		if ev.Op == "UPDATE_FIELD" {
			fields = append(fields, ev.Payload.Value("field").(string))
		}
	}
	equal(t, strings.Join(fields, ","), "deleted,deleted")
}

func TestClear(t *testing.T) {
	t.Parallel()
	home := dataHome(t)
	taskthing(t, home, "add", "keep me")
	taskthing(t, home, "add", "bin me")
	associate(t, home, remote(t))
	taskthing(t, home, "list")
	listing := taskthing(t, home, "list").stdout
	live := kvStore(t, home, "tasks")[numberOf(listing, "keep me")]

	refused := taskthing(t, home, "clear", numberOf(listing, "keep me"))
	mustCode(t, refused, 1)
	matches(t, refused.stderr, `(?i)delete`)
	equal(t, len(tasks(t, home)), 2)

	taskthing(t, home, "delete", numberOf(listing, "bin me"))
	taskthing(t, home, "list", "--deleted")
	before := mdwal.SerializeLog(parseLog(t, home))
	mustCode(t, taskthing(t, home, "clear", "1"), 0)
	remaining := tasks(t, home)
	if len(remaining) != 1 || id(remaining[0]) != live {
		t.Fatalf("remaining %v", remaining)
	}
	equal(t, mdwal.SerializeLog(parseLog(t, home)), before)
}

func TestUnstarAndSet(t *testing.T) {
	t.Parallel()
	home := dataHome(t)
	taskthing(t, home, "add", "walk the dog")
	taskthing(t, home, "list")
	taskthing(t, home, "star", "1")
	equal(t, flag(tasks(t, home)[0], "star"), true)
	mustCode(t, taskthing(t, home, "unstar", "1"), 0)
	equal(t, flag(tasks(t, home)[0], "star"), false)

	mustCode(t, taskthing(t, home, "set description", "1", "walk him twice"), 0)
	equal(t, get(tasks(t, home)[0], "description"), any("walk him twice"))
	mustCode(t, taskthing(t, home, "set date-time", "1", "tomorrow"), 0)
	equal(t, get(tasks(t, home)[0], "rrule"), any("DTSTART:20260723T000000Z"))

	seedTask(t, home, js.Obj("title", "water plants", "rrule", "DTSTART:20260701T090000Z\nRRULE:FREQ=WEEKLY;BYDAY=MO"))
	taskthing(t, home, "list")
	n := numberOf(taskthing(t, home, "list").stdout, "water plants")
	taskthing(t, home, "set date-time", n, "2026-08-10 09:00 UTC")
	equal(t, get(byTitle(tasks(t, home), "water plants"), "rrule"), any("DTSTART:20260810T090000Z\nRRULE:FREQ=WEEKLY;BYDAY=MO"))
}

func TestSetTitleAndUncheck(t *testing.T) {
	t.Parallel()
	home := dataHome(t)
	taskthing(t, home, "add", "walk the dgo")
	taskthing(t, home, "list")
	mustCode(t, taskthing(t, home, "set title", "1", "walk the dog"), 0)
	equal(t, title(tasks(t, home)[0]), "walk the dog")
	taskthing(t, home, "check", "1")
	equal(t, flag(tasks(t, home)[0], "completed"), true)
	taskthing(t, home, "list", "--checked")
	mustCode(t, taskthing(t, home, "uncheck", "1"), 0)
	equal(t, flag(tasks(t, home)[0], "completed"), false)
	contains(t, taskthing(t, home, "list").stdout, "walk the dog")

	seedTask(t, home, js.Obj("title", "water plants", "completed", true, "rrule", "DTSTART:20260701T090000Z\nRRULE:FREQ=WEEKLY;BYDAY=MO"))
	taskthing(t, home, "list", "--checked")
	n := numberOf(taskthing(t, home, "list", "--checked").stdout, "water plants")
	taskthing(t, home, "uncheck", n)
	equal(t, len(tasks(t, home)), 2)
}

// ── workspaces ───────────────────────────────────────────────────────────────

func TestWorkspaceFlag(t *testing.T) {
	t.Parallel()
	home := dataHome(t)
	os.MkdirAll(filepath.Join(home, "workspaces", "home"), 0o755)
	taskthing(t, home, "add", "fix the sink", "--workspace=home")
	equal(t, len(tasks(t, home)), 0)
	equal(t, len(tasks(t, home, "home")), 1)
	contains(t, taskthing(t, home, "list", "--workspace=home").stdout, "fix the sink")
	taskthing(t, home, "star", "1", "--workspace=home")
	taskthing(t, home, "set description", "1", "--workspace=home", "under the sink")
	there := tasks(t, home, "home")[0]
	equal(t, flag(there, "star"), true)
	equal(t, get(there, "description"), any("under the sink"))
	taskthing(t, home, "board add", "plumbing", "--workspace=home")
	equal(t, len(readAll(t, "board", home, "home")), 1)
	equal(t, len(readAll(t, "board", home)), 0)
}

func TestPeriodFilter(t *testing.T) {
	t.Parallel()
	home := dataHome(t)
	seedTask(t, home, js.Obj("title", "tomorrow", "rrule", "DTSTART:20260723T090000Z"))
	seedTask(t, home, js.Obj("title", "in a week", "rrule", "DTSTART:20260729T090000Z"))
	seedTask(t, home, js.Obj("title", "in two months", "rrule", "DTSTART:20260922T090000Z"))
	seedTask(t, home, js.Obj("title", "undated"))

	twoDays := taskthing(t, home, "list", "--period=2d").stdout
	contains(t, twoDays, "tomorrow")
	lacks(t, twoDays, "in a week")
	oneMonth := taskthing(t, home, "list", "--period=1m").stdout
	contains(t, oneMonth, "in a week")
	lacks(t, oneMonth, "in two months")
	oneYear := taskthing(t, home, "list", "--period=1y").stdout
	contains(t, oneYear, "in two months")
	lacks(t, twoDays, "undated")
	lacks(t, oneYear, "undated")
	mustCode(t, taskthing(t, home, "list", "--period=soon"), 1)
}

func TestWorkspaceLifecycle(t *testing.T) {
	t.Parallel()
	home := dataHome(t)
	mustCode(t, taskthing(t, home, "workspace create", "work"), 0)
	listed := taskthing(t, home, "workspace list").stdout
	contains(t, listed, "main")
	contains(t, listed, "work")
	matches(t, listed, `\*\s*main|main\s*\*`)
	mustCode(t, taskthing(t, home, "use", "work"), 0)
	taskthing(t, home, "add", "in the new one")
	equal(t, len(tasks(t, home, "work")), 1)
	equal(t, len(tasks(t, home)), 0)
	mustCode(t, taskthing(t, home, "workspace rename", "work", "office"), 0)
	contains(t, taskthing(t, home, "workspace list").stdout, "office")
	equal(t, len(tasks(t, home, "office")), 1)
	taskthing(t, home, "add", "still following")
	equal(t, len(tasks(t, home, "office")), 2)
}

func TestWorkspaceDelete(t *testing.T) {
	t.Parallel()
	home := dataHome(t)
	taskthing(t, home, "workspace create", "scratch")
	active := taskthing(t, home, "workspace delete", "main")
	mustCode(t, active, 1)
	matches(t, active.stderr, `(?i)current|active`)
	unconfirmed := taskthing(t, home, "workspace delete", "scratch")
	mustCode(t, unconfirmed, 1)
	matches(t, unconfirmed.stderr, `(?i)confirm`)
	contains(t, taskthing(t, home, "workspace list").stdout, "scratch")
	mustCode(t, taskthing(t, home, "workspace delete", "scratch", "--confirm"), 0)
	lacks(t, taskthing(t, home, "workspace list").stdout, "scratch")
}

func branches(t *testing.T, remote string, prefix string) []string {
	t.Helper()
	out := strings.TrimSpace(git(t, "--git-dir="+remote, "for-each-ref", "--format=%(refname:short)", prefix))
	return strings.Split(out, "\n")
}

func TestRemoteAddRemove(t *testing.T) {
	t.Parallel()
	home := dataHome(t)
	repo := remote(t)
	taskthing(t, home, "add", "written before sharing")
	associate(t, home, repo)

	before := branches(t, repo, "refs/heads")
	if !slices.Contains(before, "master") || !slices.ContainsFunc(before, func(b string) bool { return strings.HasPrefix(b, "users/") }) {
		t.Fatalf("branch model: %v", before)
	}
	contains(t, git(t, "--git-dir="+repo, "ls-tree", "-r", "--name-only", "master"), "tasks/")

	taskthing(t, home, "add", "written after")
	events := parseLog(t, home)
	creates := 0
	for _, ev := range events {
		if ev.Op == "CREATE" {
			creates++
		}
	}
	equal(t, creates, 1)
	equal(t, title(events[0].Payload.Value("snapshot").(*js.Object)), "written after")

	mustCode(t, taskthing(t, home, "workspace remote remove"), 0)
	equal(t, strings.Join(branches(t, repo, "refs/heads"), ","), strings.Join(before, ","))
	equal(t, len(tasks(t, home)), 2)
	taskthing(t, home, "add", "written alone again")
	equal(t, len(parseLog(t, home)), 0)
}

func TestSyncAndPull(t *testing.T) {
	t.Parallel()
	home := dataHome(t)
	repo := remote(t)
	associate(t, home, repo)
	taskthing(t, home, "add", "walk the dog")
	mustCode(t, taskthing(t, home, "sync"), 0)

	branch := branches(t, repo, "refs/heads/users")[0]
	published := git(t, "--git-dir="+repo, "show", branch+":events.log")
	contains(t, published, "CREATE")
	contains(t, published, "walk the dog")

	// Another peer consolidates a task of their own into master.
	clone := t.TempDir()
	git(t, "clone", "--quiet", "--branch", "master", repo, clone)
	os.MkdirAll(filepath.Join(clone, "tasks"), 0o755)
	os.WriteFile(filepath.Join(clone, "tasks", "bob-1.md"), []byte("---\ncreatedAt: 500\ntitle: \"bob's task\"\ntitle_lastModified: 500\ntitle_lastModifiedBy: \"bob\"\n---\n"), 0o644)
	git(t, "-C", clone, "add", "-A")
	git(t, "-C", clone, "-c", "user.name=bob", "-c", "user.email=bob@taskthing.local", "commit", "-q", "-m", "taskthing: consolidated snapshot")
	git(t, "-C", clone, "push", "-q", "origin", "master")

	mustCode(t, taskthing(t, home, "pull"), 0)
	var titles []string
	for _, task := range tasks(t, home) {
		titles = append(titles, title(task))
	}
	equal(t, sorted(titles...), "bob's task,walk the dog")
}

func TestRebuildAndTruncate(t *testing.T) {
	t.Parallel()
	home := dataHome(t)
	repo := remote(t)
	associate(t, home, repo)
	for _, s := range []string{"one", "two", "three"} {
		taskthing(t, home, "add", s)
		taskthing(t, home, "sync")
	}
	mustCode(t, taskthing(t, home, "rebuild"), 0)

	onMaster := 0
	for _, f := range strings.Split(git(t, "--git-dir="+repo, "ls-tree", "-r", "--name-only", "master"), "\n") {
		if strings.HasPrefix(f, "tasks/") {
			onMaster++
		}
	}
	equal(t, onMaster, 3)
	equal(t, len(parseLog(t, home)), 0)

	branch := branches(t, repo, "refs/heads/users")[0]
	count := func() int {
		n, _ := strconv.Atoi(strings.TrimSpace(git(t, "--git-dir="+repo, "rev-list", "--count", branch)))
		return n
	}
	if count() <= 2 {
		t.Fatalf("history to bound: %d", count())
	}
	mustCode(t, taskthing(t, home, "truncate", "--keep=2"), 0)
	equal(t, count(), 2)
	equal(t, len(tasks(t, home)), 3)
	mustCode(t, taskthing(t, home, "truncate"), 1)
}

// ── config, install, migrations, update ──────────────────────────────────────

func TestConfigSet(t *testing.T) {
	t.Parallel()
	home := dataHome(t)
	mustCode(t, taskthing(t, home, "config set", "dateFormat", "europe"), 0)
	mustCode(t, taskthing(t, home, "config set", "nerdfont", "true"), 0)
	shown := taskthing(t, home, "config").stdout
	contains(t, shown, "europe")
	contains(t, shown, "nerdfont")
	mustCode(t, taskthing(t, home, "config set", "autoSync", "6"), 0)
	contains(t, taskthing(t, home, "config").stdout, "autoSync 6")
	for _, junk := range []string{"0", "-1", "nope"} {
		mustCode(t, taskthing(t, home, "config set", "autoRebuild", junk), 1)
	}
	bad := taskthing(t, home, "config set", "dateFormat", "klingon")
	mustCode(t, bad, 1)
	matches(t, bad.stderr, `(?i)america|europe`)
	ws := taskthing(t, home, "config set", "currentWorkspace", "other")
	mustCode(t, ws, 1)
	matches(t, ws.stderr, `(?i)use`)
	taskthing(t, home, "add", "still the same workspace")
	equal(t, len(tasks(t, home)), 1)
	contains(t, taskthing(t, home, "config").stdout, "europe")
}

func TestInstallDefaults(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	mustCode(t, taskthing(t, home, "install"), 0)
	if _, err := os.Stat(filepath.Join(home, "workspaces", "local")); err != nil {
		t.Fatal("the local workspace")
	}
	shown := taskthing(t, home, "config").stdout
	for _, s := range []string{"currentWorkspace local", "dateFormat america", "nerdfont false"} {
		contains(t, shown, s)
	}
}

func TestInstallFromFlags(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	mustCode(t, taskthing(t, home, "install", "--date-format=europe", "--nerdfont"), 0)
	shown := taskthing(t, home, "config").stdout
	contains(t, shown, "dateFormat europe")
	contains(t, shown, "nerdfont true")
}

func TestInstallRefusesClobber(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	taskthing(t, home, "install", "--date-format=europe")
	again := taskthing(t, home, "install", "--date-format=america")
	mustCode(t, again, 1)
	matches(t, again.stderr, `(?i)already installed`)
	contains(t, taskthing(t, home, "config").stdout, "dateFormat europe")
	mustCode(t, taskthing(t, home, "install", "--date-format=america", "--force"), 0)
	contains(t, taskthing(t, home, "config").stdout, "dateFormat america")
}

func TestMigrationsReport(t *testing.T) {
	t.Parallel()
	home := dataHome(t)
	dir := filepath.Join(home, "workspaces", "main", ".taskthing", "migrations")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "0.1.0.md"), []byte("0::CREATE_FOLDER::list::::{}"), 0o644)
	r := taskthing(t, home, "migrations")
	mustCode(t, r, 0)
	for _, s := range []string{"0.1.0", "yes", "no migrations pending"} {
		contains(t, r.stdout, s)
	}
}

func updateHome(t *testing.T, available bool, target string) string {
	t.Helper()
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, "workspaces", "main"), 0o755)
	targetLine := "updateTarget: null"
	if target != "" {
		targetLine = "updateTarget: " + js.Quote(target)
	}
	ms := fmt.Sprint(nowMillis())
	os.WriteFile(filepath.Join(home, "config.md"), []byte(strings.Join([]string{
		"---", `currentWorkspace: "main"`, "lastUpdateCheck: " + ms, "updateAvailable: " + strconv.FormatBool(available),
		targetLine, `autoUpdate: "confirm"`, "---", "",
	}, "\n")), 0o644)
	return home
}

func TestUpdateCheckFromCache(t *testing.T) {
	t.Parallel()
	home := updateHome(t, true, "9.9.9")
	r := taskthing(t, home, "update check")
	mustCode(t, r, 0)
	matches(t, r.stdout, `there's a pending update \S+ → 9\.9\.9!`)
}

func TestUpdateApplyUpToDate(t *testing.T) {
	t.Parallel()
	home := updateHome(t, false, "")
	r := taskthing(t, home, "update apply")
	mustCode(t, r, 0)
	contains(t, r.stdout, "you're on the latest version!")
}

func nowMillis() int64 { return time.Now().UnixMilli() }
