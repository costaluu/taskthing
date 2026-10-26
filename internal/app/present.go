package app

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/costaluu/taskthing/internal/js"
	"github.com/costaluu/taskthing/internal/mdwal"
	"github.com/costaluu/taskthing/internal/rrule"
)

// ── presenter ────────────────────────────────────────────────────────────────
//
// How every command draws: styled on a terminal, greppable lines when piped.
// The fork is decided once, from stdout. Failure is the presenter's business
// too: `fail` draws the line and ends the process non-zero.

type boardRow struct {
	number int
	board  boardDisplay
}

type migrationRow struct {
	number  int
	version string
	applied bool
}

// downloadPhase is update apply's sub-step: downloading (bytes) or applying.
type downloadPhase struct {
	applying   bool
	downloaded int64
	total      *int64
}

type presenter interface {
	outcome(segments []segment, config *js.Object) error
	taskList(groups []taskGroup, now js.Date, config *js.Object) error
	boardList(rows []boardRow, config *js.Object) error
	workspaceList(workspaces []string, current string, config *js.Object) error
	migrationList(rows []migrationRow, config *js.Object) error
	progress(copy commandCopy, work func() error, config *js.Object) error
	progressDynamic(pending string, failure func(error) string, run func() (string, error), config *js.Object) error
	downloadProgress(pending string, failure func(error) string, run func(report func(downloadPhase)) (string, error), config *js.Object) error
	fail(message string, config *js.Object) error
	failure(message string, config *js.Object)
}

var present presenter = plainPresenter{}

func fail1() error { return &exitError{1} }

// ── plain ────────────────────────────────────────────────────────────────────

type plainPresenter struct{}

func (plainPresenter) outcome(segments []segment, c *js.Object) error {
	var b strings.Builder
	for _, s := range segments {
		b.WriteString(s.text)
	}
	consoleLog(createGlyphs(cfgNerdfont(c)).success + " " + b.String())
	return nil
}

func (plainPresenter) taskList(groups []taskGroup, _ js.Date, _ *js.Object) error {
	type flat struct {
		row       taskRow
		workspace *string
	}
	var rows []flat
	for _, g := range groups {
		for _, r := range g.rows {
			rows = append(rows, flat{r, g.workspace})
		}
	}
	slices.SortStableFunc(rows, func(a, b flat) int { return a.row.number - b.row.number })
	for _, r := range rows {
		line := strconv.Itoa(r.row.number) + " " + str(r.row.task, "title")
		if r.workspace != nil {
			line += " (" + *r.workspace + ")"
		}
		consoleLog(line)
	}
	return nil
}

func (plainPresenter) boardList(rows []boardRow, _ *js.Object) error {
	for _, r := range rows {
		consoleLog(strconv.Itoa(r.number) + " " + r.board.name)
	}
	return nil
}

func (plainPresenter) workspaceList(workspaces []string, current string, _ *js.Object) error {
	for _, w := range workspaces {
		if w == current {
			consoleLog("* " + w)
		} else {
			consoleLog("  " + w)
		}
	}
	return nil
}

func (plainPresenter) migrationList(rows []migrationRow, _ *js.Object) error {
	pending := false
	for _, r := range rows {
		applied := "no"
		if r.applied {
			applied = "yes"
		} else {
			pending = true
		}
		consoleLog(fmt.Sprintf("%d %s %s", r.number, r.version, applied))
	}
	if pending {
		consoleLog("there's pending migrations. verify your taskthing installation.")
	} else {
		consoleLog("this workspace has no migrations pending")
	}
	return nil
}

func (plainPresenter) progress(copy commandCopy, work func() error, _ *js.Object) error {
	if err := work(); err != nil {
		consoleError(copy.failure(err))
		return fail1()
	}
	consoleLog(copy.success)
	return nil
}

func (plainPresenter) progressDynamic(_ string, failure func(error) string, run func() (string, error), _ *js.Object) error {
	line, err := run()
	if err != nil {
		consoleError(failure(err))
		return fail1()
	}
	consoleLog(line)
	return nil
}

func (plainPresenter) downloadProgress(_ string, failure func(error) string, run func(func(downloadPhase)) (string, error), _ *js.Object) error {
	line, err := run(func(downloadPhase) {})
	if err != nil {
		consoleError(failure(err))
		return fail1()
	}
	consoleLog(line)
	return nil
}

func (plainPresenter) fail(message string, c *js.Object) error {
	consoleError(createGlyphs(cfgNerdfont(c)).failure + " " + message)
	return fail1()
}

func (plainPresenter) failure(message string, c *js.Object) {
	consoleError(createGlyphs(cfgNerdfont(c)).failure + " " + message)
}

// ── terminal ─────────────────────────────────────────────────────────────────

type inkPresenter struct{}

func themeOf(c *js.Object) (theme, error) { return createTheme(cfgTheme(c)) }

func (inkPresenter) outcome(segments []segment, c *js.Object) error {
	t, err := themeOf(c)
	if err != nil {
		return err
	}
	renderStatic([]string{outcomeLine(segments, cfgNerdfont(c), t)})
	return nil
}

func outcomeLine(segments []segment, nerdfont bool, t theme) string {
	g := createGlyphs(nerdfont)
	chip := textStyle{color: inkColor(t.color("text-secondary")), background: inkColor(t.color("background-secondary"))}
	line := paint(g.success, textStyle{color: inkColor(t.color("success"))}) + " "
	for _, s := range segments {
		if s.variable {
			line += paint(s.text, chip)
		} else {
			line += s.text
		}
	}
	return line
}

func (inkPresenter) taskList(groups []taskGroup, now js.Date, c *js.Object) error {
	t, err := themeOf(c)
	if err != nil {
		return err
	}
	renderStatic(taskListLines(groups, now, cfgDateFormat(c), cfgNerdfont(c), t))
	return nil
}

func taskListLines(groups []taskGroup, now js.Date, dateFormat string, nerdfont bool, t theme) []string {
	g := createGlyphs(nerdfont)
	lines := []string{paint(" TASKS ", textStyle{bold: true, background: inkColor(t.color("highlight"))})}
	if len(groups) == 0 {
		lines = append(lines, paint("no tasks here", textStyle{color: inkColor(t.color("text-secondary"))}))
	}
	for _, group := range groups {
		isInbox := group.board.id == mdwal.INBOX
		icon, color := group.board.icon, group.board.color
		if isInbox {
			icon, color = g.inbox, inkColor(t.color("inbox-accent"))
		}
		header := icon + " " + group.board.name
		if group.workspace != nil {
			header += " (" + *group.workspace + ")"
		}
		lines = append(lines, " ", paint(header, textStyle{bold: true, underline: true, color: color}))
		for _, row := range group.rows {
			task := row.task
			completed := boolean(task, "completed")
			check := g.checkboxUnchecked
			if completed {
				check = paint(g.checkboxChecked, textStyle{color: inkColor(t.color("success"))})
			}
			line := strconv.Itoa(row.number) + ". " + check + " "
			if d := formatTaskDate(row.date, now, completed, dateFormat); d != nil {
				line += paint(d.text, textStyle{underline: true, color: inkColor(t.color(d.role))})
			}
			line += " "
			if boolean(task, "star") {
				line += paint(g.star+" ", textStyle{color: inkColor(t.color("warning"))})
			}
			line += str(task, "title")
			if task.Value("description") != nil {
				line += paint(" "+g.description, textStyle{color: inkColor(t.color("text-secondary"))})
			}
			if r := strPtr(task, "rrule"); r != nil && rrule.Recurs(*r) {
				line += paint(" "+g.recurrence, textStyle{color: inkColor(t.color("highlight"))})
			}
			lines = append(lines, line)
		}
	}
	return lines
}

func (inkPresenter) boardList(rows []boardRow, c *js.Object) error {
	t, err := themeOf(c)
	if err != nil {
		return err
	}
	g := createGlyphs(cfgNerdfont(c))
	lines := []string{paint("BOARDS", textStyle{bold: true, color: inkColor(t.color("highlight"))})}
	if len(rows) == 0 {
		lines = append(lines, paint("no boards here", textStyle{color: inkColor(t.color("text-secondary"))}))
	}
	for _, r := range rows {
		icon, color := r.board.icon, r.board.color
		if r.board.id == mdwal.INBOX {
			icon, color = g.inbox, inkColor(t.color("inbox-accent"))
		}
		lines = append(lines, strconv.Itoa(r.number)+". "+paint(icon+" "+r.board.name, textStyle{bold: true, underline: true, color: color}))
	}
	renderStatic(lines)
	return nil
}

func (inkPresenter) workspaceList(workspaces []string, current string, c *js.Object) error {
	t, err := themeOf(c)
	if err != nil {
		return err
	}
	g := createGlyphs(cfgNerdfont(c))
	blank := strings.Repeat(" ", len([]rune(g.selector)))
	highlight := inkColor(t.color("highlight"))
	lines := []string{paint("Workspaces", textStyle{bold: true, color: highlight})}
	if len(workspaces) == 0 {
		lines = append(lines, paint("no workspaces here", textStyle{color: inkColor(t.color("text-secondary"))}))
	}
	for _, name := range workspaces {
		if name == current {
			lines = append(lines, paint(g.selector+" "+g.dotSelected+" "+name, textStyle{color: highlight}))
		} else {
			lines = append(lines, blank+" "+g.dotUnselected+" "+name)
		}
	}
	renderStatic(lines)
	return nil
}

func (inkPresenter) migrationList(rows []migrationRow, c *js.Object) error {
	t, err := themeOf(c)
	if err != nil {
		return err
	}
	g := createGlyphs(cfgNerdfont(c))
	lines := []string{paint("MIGRATIONS", textStyle{bold: true, color: inkColor(t.color("highlight"))})}
	pending := false
	for _, r := range rows {
		applied := "no"
		if r.applied {
			applied = "yes"
		} else {
			pending = true
		}
		lines = append(lines, fmt.Sprintf("%d. %s %s", r.number, r.version, applied))
	}
	if pending {
		lines = append(lines, paint(g.warn+" there's pending migrations. verify your taskthing installation.", textStyle{color: inkColor(t.color("warning"))}))
	} else {
		lines = append(lines, paint(g.info+" this workspace has no migrations pending", textStyle{color: inkColor(t.color("text-secondary"))}))
	}
	renderStatic(lines)
	return nil
}

var spinnerFrames = []string{"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"}

// spin animates `pending` beside a braille frame, plus an optional sub-line,
// until run settles; then the success or failure line replaces both.
func spin(pending string, nerdfont bool, sub func() string, run func() (string, error)) bool {
	s := &screen{}
	var mu sync.Mutex
	frameIndex := 0
	draw := func() {
		lines := []string{spinnerFrames[frameIndex] + " " + pending}
		if sub != nil {
			if extra := sub(); extra != "" {
				lines = append(lines, extra)
			}
		}
		s.render(frame(lines))
	}
	mu.Lock()
	draw()
	mu.Unlock()
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(80 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				mu.Lock()
				frameIndex = (frameIndex + 1) % len(spinnerFrames)
				draw()
				mu.Unlock()
			}
		}
	}()
	line, err := run()
	close(done)
	mu.Lock()
	defer mu.Unlock()
	g := createGlyphs(nerdfont)
	ok := err == nil
	if ok {
		s.render(frame([]string{g.success + " " + line}))
	} else {
		s.render(frame([]string{g.failure + " " + err.Error()}))
	}
	s.unmount()
	return ok
}

func (inkPresenter) progress(copy commandCopy, work func() error, c *js.Object) error {
	ok := spin(copy.pending, cfgNerdfont(c), nil, func() (string, error) {
		if err := work(); err != nil {
			return "", fmt.Errorf("%s", copy.failure(err))
		}
		return copy.success, nil
	})
	if !ok {
		return fail1()
	}
	return nil
}

func (inkPresenter) progressDynamic(pending string, _ func(error) string, run func() (string, error), c *js.Object) error {
	if !spin(pending, cfgNerdfont(c), nil, run) {
		return fail1()
	}
	return nil
}

func formatBytes(n int64) string {
	switch {
	case n < 1024:
		return strconv.FormatInt(n, 10) + "B"
	case n < 1024*1024:
		return strconv.FormatFloat(float64(n)/1024, 'f', 1, 64) + "KB"
	}
	return strconv.FormatFloat(float64(n)/(1024*1024), 'f', 1, 64) + "MB"
}

func (inkPresenter) downloadProgress(pending string, _ func(error) string, run func(func(downloadPhase)) (string, error), c *js.Object) error {
	t, err := themeOf(c)
	if err != nil {
		return err
	}
	var mu sync.Mutex
	var phase *downloadPhase
	sub := func() string {
		if phase == nil {
			return ""
		}
		if phase.applying {
			return "applying update..."
		}
		fill := textStyle{color: inkColor(t.color("success"))}
		track := textStyle{color: inkColor(t.color("text-secondary"))}
		if phase.total == nil {
			return "downloading new version... " + paint(formatBytes(phase.downloaded), fill)
		}
		fraction := 1.0
		if *phase.total != 0 {
			fraction = min(1, float64(phase.downloaded)/float64(*phase.total))
		}
		filled := int(jsRound(fraction * 24))
		return "downloading new version... " + paint(strings.Repeat("─", filled), fill) +
			paint(strings.Repeat("─", 24-filled), track) + " " + strconv.Itoa(int(jsRound(fraction*100))) + "% " +
			paint("("+formatBytes(phase.downloaded)+"/"+formatBytes(*phase.total)+")", track)
	}
	ok := spin(pending, cfgNerdfont(c), func() string { mu.Lock(); defer mu.Unlock(); return sub() }, func() (string, error) {
		return run(func(p downloadPhase) { mu.Lock(); phase = &p; mu.Unlock() })
	})
	if !ok {
		return fail1()
	}
	return nil
}

func (inkPresenter) fail(message string, c *js.Object) error {
	consoleError(createGlyphs(cfgNerdfont(c)).failure + " " + message)
	return fail1()
}

func (inkPresenter) failure(message string, c *js.Object) {
	consoleError(createGlyphs(cfgNerdfont(c)).failure + " " + message)
}
