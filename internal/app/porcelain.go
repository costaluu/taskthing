package app

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/costaluu/taskthing/internal/chrono"
	"github.com/costaluu/taskthing/internal/js"
	"github.com/costaluu/taskthing/internal/mdwal"
	"github.com/costaluu/taskthing/internal/nodefs"
	"github.com/costaluu/taskthing/internal/rrule"
)

// ── where the data lives ─────────────────────────────────────────────────────

func dataHome() string {
	if home, ok := os.LookupEnv("TASKTHING_HOME"); ok {
		return home
	}
	dir, _ := os.UserHomeDir()
	return filepath.Join(dir, ".taskthing")
}

func configPath() string { return filepath.Join(dataHome(), "config.md") }

// readConfig is config.md through the frontmatter path and the config schema.
func readConfig() (*js.Object, error) {
	text, err := nodefs.ReadFile(configPath())
	if err != nil {
		return nil, err
	}
	raw, err := mdwal.ParseFrontmatter(text)
	if err != nil {
		return nil, err
	}
	return mdwal.ConfigSchema.Parse(raw)
}

func writeConfig(config *js.Object) error {
	return nodefs.WriteFile(configPath(), mdwal.SerializeEntity(config))
}

func cfgNerdfont(c *js.Object) bool { return boolean(c, "nerdfont") }
func cfgDateFormat(c *js.Object) string {
	if s := str(c, "dateFormat"); s != "" {
		return s
	}
	return "america"
}
func cfgTheme(c *js.Object) string { return str(c, "theme") }

func currentWorkspaceName() (string, error) {
	c, err := readConfig()
	if err != nil {
		return "", err
	}
	return str(c, "currentWorkspace"), nil
}

func workspacePath(name string) string { return filepath.Join(dataHome(), "workspaces", name) }

func targetWorkspaceName(named string) (string, error) {
	if named != "" {
		return named, nil
	}
	return currentWorkspaceName()
}

func targetWorkspace(named string) (string, error) {
	name, err := targetWorkspaceName(named)
	if err != nil {
		return "", err
	}
	return workspacePath(name), nil
}

// lastLoggedTimestamp seeds the clock from this peer's last log line.
func lastLoggedTimestamp(root string) (int64, error) {
	text, _ := nodefs.ReadFile(filepath.Join(root, "events.log"))
	var last string
	for _, l := range strings.Split(text, "\n") {
		if l != "" {
			last = l
		}
	}
	if last == "" {
		return 0, nil
	}
	ts, _, _ := strings.Cut(last, "::")
	return mdwal.BigInt(ts)
}

func nowNanos() int64 { return time.Now().UnixMilli() * 1_000_000 }

// openWorkspace opens a workspace's engine and manager: the workspace is
// remote exactly while its repository has a remote.
func openWorkspace(root string) (*mdwal.Mdwal, *manager, error) {
	author, err := resolveGitAuthor()
	if err != nil {
		return nil, nil, err
	}
	seed, err := lastLoggedTimestamp(root)
	if err != nil {
		return nil, nil, err
	}
	clock := monotonicClock(nowNanos, seed)
	var mgr *manager
	m := mdwal.New(mdwal.Config{
		Root:    root,
		Schemas: []mdwal.SchemaEntry{{Type: "task", Schema: mdwal.TaskFields}, {Type: "board", Schema: mdwal.BoardFields}},
		Mode: func() (string, error) {
			if mgr.isAssociated() {
				return "remote", nil
			}
			return "local", nil
		},
		Clock:      clock,
		Author:     func() string { return author },
		Version:    Version,
		Transforms: Transforms,
	})
	mgr = newManager(root, m, func() string { return author })
	return m, mgr, nil
}

// now is the reference instant, which tests pin through TASKTHING_NOW.
func now() js.Date {
	if pinned, ok := os.LookupEnv("TASKTHING_NOW"); ok {
		d, _ := js.ParseISO(pinned)
		return d
	}
	return js.Now()
}

func parseDate(input, dateFormat string) (js.Date, error) {
	d, ok := chrono.ParseNaturalDate(input, now(), dateFormat)
	if !ok {
		return js.Date{}, fmt.Errorf("not a date: %s", input)
	}
	return d, nil
}

// ── outcome reporting ────────────────────────────────────────────────────────

// batching is set while eachReference runs, so one bad reference doesn't stop
// the rest.
var batching = false

// report runs a mutation and draws its outcome line, or its failure line.
func report(failure func(error) string, work func(config *js.Object) ([]segment, error)) error {
	config, err := readConfig()
	if err != nil {
		return err
	}
	segments, err := work(config)
	if err == nil {
		err = present.outcome(segments, config)
	}
	if err != nil {
		var exit *exitError
		if errors.As(err, &exit) {
			return err
		}
		if !batching {
			return present.fail(failure(err), config)
		}
		present.failure(failure(err), config)
		exitCode = 1
	}
	return nil
}

func reportTask(verb string, work func(*js.Object) (taskOutcome, error)) error {
	return report(func(err error) string { return taskFailure(verb, err) }, func(c *js.Object) ([]segment, error) {
		o, err := work(c)
		if err != nil {
			return nil, err
		}
		return taskMessage(o), nil
	})
}

func reportNamed(noun, action string, work func(*js.Object) (entityOutcome, error)) error {
	return report(func(err error) string { return actionFailure(action, err) }, func(c *js.Object) ([]segment, error) {
		o, err := work(c)
		if err != nil {
			return nil, err
		}
		return namedMessage(noun, o), nil
	})
}

func reportBoard(action string, work func(*js.Object) (entityOutcome, error)) error {
	return reportNamed("board", action, work)
}

func reportWorkspace(action string, work func(*js.Object) (entityOutcome, error)) error {
	return reportNamed("workspace", action, work)
}

// eachReference runs a verb on every reference the user typed; none given is
// one empty reference, refused as before.
func eachReference(positional []string, run func(reference string) error) error {
	batching = true
	defer func() { batching = false }()
	refs := positional
	if len(refs) == 0 {
		refs = []string{""}
	}
	for _, r := range refs {
		if err := run(r); err != nil {
			return err
		}
	}
	return nil
}

// datedOutcome: the next occurrence is the rule's DTSTART, and the recurrence
// shows only when the rule truly recurs.
func datedOutcome(title, predicate string, rule *string, config *js.Object) (taskOutcome, error) {
	o := taskOutcome{title: title, predicate: predicate}
	if rule == nil {
		return o, nil
	}
	r, err := rrule.FromString(*rule)
	if err != nil {
		return o, err
	}
	next := absoluteDateTime(r.DtstartOf(), cfgDateFormat(config))
	o.nextOccurrence = &next
	if rrule.Recurs(*rule) {
		text := r.ToText()
		o.recurrence = &text
	}
	return o, nil
}

var configLabels = map[string]string{"dateFormat": "date format", "nerdfont": "nerdfont", "theme": "theme"}

// reportConfig persists a TUI config change and reports it in plain text.
func reportConfig(config *js.Object, key string, write func() error) error {
	label := configLabels[key]
	if label == "" {
		label = key
	}
	if err := write(); err != nil {
		return present.fail(fmt.Sprintf("something went wrong updating %s. error: %s", label, summarize(err)), config)
	}
	consoleLog(createGlyphs(cfgNerdfont(config)).success + " " + label + " updated successfully!")
	return nil
}

// ── filters and references ───────────────────────────────────────────────────

type taskFilters struct {
	checked, deleted, starred, hasDescription bool
	period, inBoard                           *string
}

var periodRe = regexp.MustCompile(`^(\d+)([dmy])$`)

func periodEnd(period string, from js.Date) (js.Date, error) {
	m := periodRe.FindStringSubmatch(period)
	if m == nil {
		return js.Date{}, fmt.Errorf("not a period: %s — try 2d, 3m or 1y", period)
	}
	amount := int(js.ToNumber(m[1]))
	end := from
	switch m[2] {
	case "d":
		end = end.SetUTCDate(end.GetUTCDate() + amount)
	case "m":
		end = end.SetUTCMonth(end.GetUTCMonth() + amount)
	case "y":
		end = end.SetUTCFullYear(end.GetUTCFullYear() + amount)
	}
	return end, nil
}

func inPeriod(rule *string, period string) (bool, error) {
	if rule == nil {
		return false, nil
	}
	r, err := rrule.FromString(*rule)
	if err != nil {
		return false, err
	}
	end, err := periodEnd(period, now())
	if err != nil {
		return false, err
	}
	return r.DtstartOf().Time() <= end.Time(), nil
}

// allWorkspaces lists the data home's workspaces, in directory order.
func allWorkspaces() ([]string, error) {
	entries, err := nodefs.ReaddirTyped(filepath.Join(dataHome(), "workspaces"))
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir {
			names = append(names, e.Name)
		}
	}
	return names, nil
}

func assertWorkspaceExists(name string) error {
	all, err := allWorkspaces()
	if err != nil {
		return err
	}
	for _, w := range all {
		if w == name {
			return nil
		}
	}
	return fmt.Errorf("no such workspace: %s", name)
}

func setCurrentWorkspace(name string) error {
	c, err := readConfig()
	if err != nil {
		return err
	}
	return writeConfig(with(c, "currentWorkspace", name))
}

// taskFilter: a task shows only if it satisfies every active flag.
func taskFilter(f taskFilters, inBoardID *string) func(*js.Object) (bool, error) {
	return func(task *js.Object) (bool, error) {
		if !(f.checked || !boolean(task, "completed")) {
			return false, nil
		}
		if f.deleted != boolean(task, "deleted") {
			return false, nil
		}
		if f.starred && !boolean(task, "star") {
			return false, nil
		}
		if f.hasDescription && task.Value("description") == nil {
			return false, nil
		}
		if f.period != nil {
			ok, err := inPeriod(strPtr(task, "rrule"), *f.period)
			if err != nil || !ok {
				return false, err
			}
		}
		return inBoardID == nil || str(task, "board") == *inBoardID, nil
	}
}

func inBoardFilter(f taskFilters, boards repository, root string) (*string, error) {
	if f.inBoard == nil {
		return nil, nil
	}
	id, err := resolveBoard(boards, root, *f.inBoard)
	if err != nil {
		return nil, err
	}
	return &id, nil
}

// taskDate is the completion instant when done, else the DTSTART.
func taskDate(task *js.Object, m *mdwal.Mdwal) (*js.Date, error) {
	if boolean(task, "completed") {
		raw, err := m.Read("task", str(task, "id"))
		if err != nil {
			return nil, err
		}
		ns, ok := raw.Value("completed_lastModified").(int64)
		if !ok {
			return nil, nil
		}
		d := js.DateFromMillis(float64(ns / 1_000_000))
		return &d, nil
	}
	rule := strPtr(task, "rrule")
	if rule == nil {
		return nil, nil
	}
	r, err := rrule.FromString(*rule)
	if err != nil {
		return nil, err
	}
	d := r.DtstartOf()
	return &d, nil
}

type boardDisplay struct{ id, name, icon, color string }

type taskRow struct {
	number int
	task   *js.Object
	date   *js.Date
}

type taskGroup struct {
	board     boardDisplay
	rows      []taskRow
	workspace *string
}

// gatherTaskGroups numbers one workspace's shown tasks, grouped by board in
// first-seen order and, within a group, undated first then by date.
func gatherTaskGroups(name string, f taskFilters, store kvStore, global bool) ([]taskGroup, error) {
	root := workspacePath(name)
	m, _, err := openWorkspace(root)
	if err != nil {
		return nil, err
	}
	tasks, boards := taskRepository(m), boardRepository(m)
	inBoard, err := inBoardFilter(f, boards, root)
	if err != nil {
		return nil, err
	}
	all, err := tasks.findAll()
	if err != nil {
		return nil, err
	}
	keep := taskFilter(f, inBoard)
	var shown []*js.Object
	for _, t := range all {
		ok, err := keep(t)
		if err != nil {
			return nil, err
		}
		if ok {
			shown = append(shown, t)
		}
	}
	allBoards, err := boards.findAll()
	if err != nil {
		return nil, err
	}
	boardByID := map[string]*js.Object{}
	for _, b := range allBoards {
		boardByID[str(b, "id")] = b
	}
	display := func(id string) boardDisplay {
		if id == mdwal.INBOX {
			return boardDisplay{mdwal.INBOX, "inbox", "", ""}
		}
		b := boardByID[id]
		if b == nil {
			return boardDisplay{id, id, "", ""}
		}
		return boardDisplay{id, str(b, "name"), str(b, "icon"), str(b, "color")}
	}

	type entry struct {
		task *js.Object
		date *js.Date
	}
	var order []string
	buckets := map[string][]entry{}
	for _, t := range shown {
		d, err := taskDate(t, m)
		if err != nil {
			return nil, err
		}
		board := str(t, "board")
		if _, ok := buckets[board]; !ok {
			order = append(order, board)
		}
		buckets[board] = append(buckets[board], entry{t, d})
	}
	var groups []taskGroup
	for _, board := range order {
		entries := buckets[board]
		stableSort(entries, func(a, b entry) float64 {
			switch {
			case a.date == nil && b.date == nil:
				return 0
			case a.date == nil:
				return -1
			case b.date == nil:
				return 1
			}
			return a.date.Time() - b.date.Time()
		})
		g := taskGroup{board: display(board)}
		if global {
			n := name
			g.workspace = &n
		}
		for _, e := range entries {
			key := str(e.task, "id")
			if global {
				key = name + "/" + key
			}
			n, err := store.set(key)
			if err != nil {
				return nil, err
			}
			g.rows = append(g.rows, taskRow{n, e.task, e.date})
		}
		groups = append(groups, g)
	}
	return groups, nil
}

// stableSort is Array#sort with a numeric comparator (stable, as in JSC).
func stableSort[T any](items []T, cmp func(a, b T) float64) {
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && cmp(items[j-1], items[j]) > 0; j-- {
			items[j-1], items[j] = items[j], items[j-1]
		}
	}
}

// resolveTask turns what the user typed into a task: a listed number (this
// workspace's, else a global listing's), or a raw id.
func resolveTask(reference, workspace string) (string, string, error) {
	root, err := targetWorkspace(workspace)
	if err != nil {
		return "", "", err
	}
	number := js.ToNumber(reference)
	if !js.IsInteger(number) {
		return root, reference, nil
	}
	if own, ok := workspaceKvStore(root, "tasks").get(number); ok {
		return root, own, nil
	}
	global, ok := globalKvStore(dataHome()).get(number)
	if !ok {
		return "", "", fmt.Errorf("no task numbered %s — run `taskthing list` first", js.NumberToString(number))
	}
	// `slice(0, i)` / `slice(i + 1)`, whose -1 (no slash) the TS read as
	// "all but the last character" / "the whole string".
	sep := strings.Index(global, "/")
	if sep == -1 {
		return workspacePath(global[:max(0, len(global)-1)]), global, nil
	}
	return workspacePath(global[:sep]), global[sep+1:], nil
}

// resolveBoard: `inbox`, a listed number, a live board's name, or a raw id.
func resolveBoard(boards repository, root, reference string) (string, error) {
	if strings.ToLower(js.Trim(reference)) == "inbox" {
		return mdwal.INBOX, nil
	}
	number := js.ToNumber(reference)
	if js.Trim(reference) != "" && js.IsInteger(number) {
		id, ok := workspaceKvStore(root, "boards").get(number)
		if !ok {
			return "", fmt.Errorf("no board numbered %s — run `taskthing board list` first", js.NumberToString(number))
		}
		return id, nil
	}
	named, err := boards.filter(func(b *js.Object) bool { return !boolean(b, "deleted") && str(b, "name") == reference })
	if err != nil {
		return "", err
	}
	if len(named) > 0 {
		return str(named[0], "id"), nil
	}
	if boards.findByID(reference) != nil {
		return reference, nil
	}
	return "", fmt.Errorf("no such board: %s", reference)
}

// ── migrations and self-update ───────────────────────────────────────────────

// recordedMigrations are the versions recorded as applied here, in readdir order.
func recordedMigrations(root string) ([]string, map[string]bool) {
	set := map[string]bool{}
	names, err := nodefs.Readdir(filepath.Join(root, ".taskthing", "migrations"))
	if err != nil {
		return nil, set
	}
	var order []string
	for _, n := range names {
		v := strings.TrimSuffix(n, ".md")
		if !set[v] {
			order = append(order, v)
		}
		set[v] = true
	}
	return order, set
}

func runAllMigrations() error {
	author, err := resolveGitAuthor()
	if err != nil {
		return err
	}
	clock := monotonicClock(nowNanos, 0)
	names, err := allWorkspaces()
	if err != nil {
		return err
	}
	var roots []string
	for _, n := range names {
		roots = append(roots, workspacePath(n))
	}
	runner := migrationRunner{
		migrations: Migrations,
		workspaces: roots,
		engine: func(root string) *mdwal.Mdwal {
			return mdwal.New(mdwal.Config{
				Root:    root,
				Schemas: []mdwal.SchemaEntry{{Type: "task", Schema: mdwal.TaskFields}, {Type: "board", Schema: mdwal.BoardFields}},
				Mode:    func() (string, error) { return "local", nil },
				Clock:   clock,
				Author:  func() string { return author },
			})
		},
		readConfig:       readConfig,
		writeConfig:      writeConfig,
		configHome:       dataHome(),
		configMigrations: ConfigMigrations,
	}
	return runner.run()
}

// authorEntityLog runs a folder op against a throwaway workspace in logging
// mode and returns the log line(s) it wrote — a dev-time authoring tool.
func authorEntityLog(op, name string, newName *string) (string, error) {
	author, err := resolveGitAuthor()
	if err != nil {
		return "", err
	}
	scratch, err := os.MkdirTemp("", "taskthing-authoring-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(scratch)
	m := mdwal.New(mdwal.Config{
		Root:    scratch,
		Mode:    func() (string, error) { return "remote", nil },
		Clock:   monotonicClock(nowNanos, 0),
		Author:  func() string { return author },
		Version: Version,
	})
	switch op {
	case "create":
		err = m.CreateFolder(name, nil)
	case "rename":
		if newName == nil {
			return "", errors.New("entity rename needs a <new-name>")
		}
		if err = m.CreateFolder(name, &mdwal.LogOption{NoLog: true}); err == nil {
			err = m.RenameFolder(name, *newName, nil)
		}
	case "delete":
		err = m.DeleteFolder(name, nil)
	}
	if err != nil {
		return "", err
	}
	return nodefs.ReadFile(filepath.Join(scratch, "events.log"))
}

var _ = math.NaN
