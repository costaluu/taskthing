package main

// Completion is tested in two layers. `__complete` decides every candidate,
// so its tests are the bulk and need no shell. Each shim then gets one test
// in its real shell: bash through COMPREPLY, the contract readline reads;
// zsh through zpty, typing TAB into an interactive zsh and reading back the
// line, since compadd only runs inside a live completion widget.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// completionHome: two boards, three open tasks and a checked one, listed.
func completionHome(t *testing.T) string {
	t.Helper()
	home := dataHome(t)
	os.MkdirAll(filepath.Join(home, "workspaces", "side"), 0o755)
	mustCode(t, taskthing(t, home, "board add", "work"), 0)
	mustCode(t, taskthing(t, home, "board add", "my board"), 0)
	for _, title := range []string{"buy milk", "review the PR", "review docs", "pay the bill"} {
		mustCode(t, taskthing(t, home, "add", title), 0)
	}
	mustCode(t, taskthing(t, home, "list"), 0)
	mustCode(t, taskthing(t, home, "check", number(t, home, "pay the bill")), 0)
	mustCode(t, taskthing(t, home, "list", "--checked"), 0)
	return home
}

// number is the task's number in the last listing.
func number(t *testing.T, home, title string) string {
	t.Helper()
	for n, taskID := range kvStore(t, home, "tasks") {
		for _, task := range tasks(t, home) {
			if id(task) == taskID && task.Value("title") == title {
				return n
			}
		}
	}
	t.Fatalf("no listed task %q", title)
	return ""
}

func complete(t *testing.T, home string, words ...string) []string {
	t.Helper()
	r := taskthing(t, home, "__complete", words...)
	mustCode(t, r, 0)
	return strings.Split(strings.TrimSuffix(r.stdout, "\n"), "\n")
}

func TestComplete(t *testing.T) {
	home := completionHome(t)
	n := func(title string) string { return number(t, home, title) + "\t" + title }
	cases := []struct {
		words []string
		want  []string
	}{
		{[]string{"boa"}, []string{"board\tcreate, list and edit boards", "boards\tlist boards (shortcut for `board list`)"}},
		// a verb offers only the tasks it applies to, by number, titled
		{[]string{"uncheck", ""}, []string{n("pay the bill")}},
		// a title typed finds its number; a number typed completes as a number
		{[]string{"check", "MILK"}, []string{n("buy milk")}},
		{[]string{"check", "zzz"}, nil},
		{[]string{"recover", ""}, nil},
		{[]string{"set", "title", number(t, home, "buy milk"), ""}, []string{"buy milk"}},
		{[]string{"set", "board", number(t, home, "buy milk"), ""}, []string{"inbox", "my board", "work"}},
		{[]string{"list", "--in-board", "w"}, []string{"work"}},
		{[]string{"board", "delete", "work", ""}, []string{"my board"}},
		// switch: a board or the inbox, then another
		{[]string{"board", "switch", ""}, []string{"inbox", "my board", "work"}},
		{[]string{"board", "switch", "work", ""}, []string{"inbox", "my board"}},
		{[]string{"board", "switch", "work", "inbox", ""}, nil},
		{[]string{"board", "set", "color", "work", "gree"}, []string{"green", "greenBright"}},
		{[]string{"add", "milk", "b:[my"}, []string{"b:[my board]\t\tmy board"}},
		{[]string{"use", ""}, []string{"side"}},
		{[]string{"check", "--workspace", "s"}, []string{"side"}},
		{[]string{"config", "set", "autoUpdate", ""}, []string{"confirm", "silent"}},
		{[]string{"install", "--date-format", ""}, []string{"america", "europe"}},
		{[]string{"help", "board", "se"}, []string{"set\tedit a board field"}},
		{[]string{"check", "--"}, []string{"--workspace\tthe workspace to act on (default: the current one)", "--help\tShow help"}},
		{[]string{"completion", ""}, []string{"install\tenable completion in your shell's rc file (default: $SHELL's)", "bash", "zsh"}},
		{[]string{"check", "--workspace=s"}, []string{"--workspace=side"}},
		{[]string{"list", "--in-board=my"}, []string{"--in-board=my board"}},
		{[]string{"check", "--nope=x"}, nil},
	}
	for _, c := range cases {
		got := complete(t, home, c.words...)
		if c.want == nil {
			c.want = []string{""}
		}
		if strings.Join(got, "\n") != strings.Join(c.want, "\n") {
			t.Errorf("complete %q\n got %q\nwant %q", c.words, got, c.want)
		}
	}
	// A repeating verb skips what's already typed.
	got := complete(t, home, "check", number(t, home, "buy milk"), "")
	if len(got) != 2 {
		t.Errorf("check after one task: got %q, want the other two", got)
	}
}

func TestCompleteWritesNothing(t *testing.T) {
	home := completionHome(t)
	before := snapshot(t, home)
	complete(t, home, "check", "review")
	complete(t, home, "set", "title", "1", "")
	complete(t, home, "board", "recover", "")
	if after := snapshot(t, home); after != before {
		t.Errorf("completion changed the data home")
	}
}

func snapshot(t *testing.T, root string) string {
	t.Helper()
	var b strings.Builder
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			content, _ := os.ReadFile(path)
			b.WriteString(path + "\n" + string(content) + "\n")
		}
		return nil
	})
	return b.String()
}

// shellEnv puts this binary on PATH as `taskthing`, the name the shims call.
func shellEnv(t *testing.T, home string) []string {
	t.Helper()
	if filepath.Base(binary) != "taskthing" {
		t.Skip("the shims call `taskthing`; TASKTHING_BIN is named otherwise")
	}
	return append(os.Environ(), "PATH="+filepath.Dir(binary)+":"+os.Getenv("PATH"),
		"TASKTHING_HOME="+home, "TASKTHING_NOW="+now)
}

func shim(t *testing.T, home, shell string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "taskthing."+shell)
	r := taskthing(t, home, "completion", shell)
	mustCode(t, r, 0)
	os.WriteFile(path, []byte(r.stdout), 0o644)
	return path
}

func TestBashCompletion(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("no bash")
	}
	home := completionHome(t)
	mustCode(t, taskthing(t, home, "board add", "my bag"), 0)
	milk := number(t, home, "buy milk")
	reviews := number(t, home, "review the PR") + "  (review the PR)|" + number(t, home, "review docs") + "  (review docs)"
	if number(t, home, "review docs") < number(t, home, "review the PR") {
		reviews = number(t, home, "review docs") + "  (review docs)|" + number(t, home, "review the PR") + "  (review the PR)"
	}
	cases := []struct {
		line string // typed, TAB at its end
		want string // COMPREPLY, |-separated
	}{
		{"taskthing check milk", milk},
		{`taskthing check buy\ m`, milk},
		{"taskthing set board " + milk + " my\\ bo", `my\ board`},
		// several names with a blank: what readline inserts stays one word
		{"taskthing set board " + milk + " m", `my\ bag|my\ board`},
		// inside an open quote the name goes in as it is
		{"taskthing set board " + milk + " 'my bo", "my board"},
		// several titles: listed with their numbers, plus the blank entry
		// that keeps readline from replacing what was typed
		{"taskthing check review", reviews + "| "},
		// readline replaces only what follows the : or =
		{"taskthing add x b:[w", `\[work\]`},
		{"taskthing check --workspace=s", "side"},
		{"taskthing nope ", ""},
	}
	run := func(t *testing.T, prelude string) {
		path := shim(t, home, "bash")
		for _, c := range cases {
			words := compWords(c.line)
			quoted := make([]string, len(words))
			for i, w := range words {
				quoted[i] = shellQuote(w)
			}
			script := prelude + "source " + path + `
COMP_WORDS=(` + strings.Join(quoted, " ") + `) COMP_CWORD=` + strconv.Itoa(len(words)-1) + `
COMP_LINE=` + shellQuote(c.line) + ` COMP_POINT=${#COMP_LINE}
_taskthing; (IFS='|'; printf '%s' "${COMPREPLY[*]}")`
			cmd := exec.Command("bash", "--norc", "--noprofile", "-c", script)
			cmd.Env = shellEnv(t, home)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("%q: %v\n%s", c.line, err, out)
			}
			if string(out) != c.want {
				t.Errorf("%q\n got %q\nwant %q", c.line, out, c.want)
			}
		}
	}
	t.Run("plain bash", func(t *testing.T) { run(t, "") })
	// The same shim, with the bash-completion package loaded beside it.
	const bashCompletion = "/usr/share/bash-completion/bash_completion"
	if _, err := os.Stat(bashCompletion); err == nil {
		t.Run("with bash-completion", func(t *testing.T) { run(t, "source "+bashCompletion+"\n") })
	}
}

// compWords splits a line as bash fills COMP_WORDS: raw text, cut at
// unescaped blanks, and with : and = (COMP_WORDBREAKS) as words of their own.
func compWords(line string) []string {
	words := []string{""}
	quoted := false
	for i := 0; i < len(line); i++ {
		ch, last := line[i], len(words)-1
		switch {
		case ch == '\\' && i+1 < len(line):
			words[last] += line[i : i+2]
			i++
		case ch == '\'':
			quoted = !quoted
			words[last] += "'"
		case quoted:
			words[last] += string(ch)
		case ch == ' ':
			if words[last] != "" {
				words = append(words, "")
			}
		case ch == ':' || ch == '=':
			if words[last] != "" {
				words = append(words, "")
			}
			words[len(words)-1] = string(ch)
			words = append(words, "")
		default:
			words[last] += string(ch)
		}
	}
	return words
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// zshEnv is a fresh ZDOTDIR and HOME, with $SHELL zsh: what
// `completion install` and the first-run offer write into.
func zshEnv(t *testing.T, home string) (string, []string) {
	t.Helper()
	if _, err := exec.LookPath("zsh"); err != nil {
		t.Skip("no zsh")
	}
	dir := t.TempDir()
	return dir, append(shellEnv(t, home), "HOME="+dir, "ZDOTDIR="+dir, "SHELL=/bin/zsh")
}

// zsh runs a script that drives a pseudo-terminal through zpty.
func zsh(t *testing.T, env []string, script string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "zsh", "-f", "-c", "zmodload zsh/zpty\n"+script)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	return string(out)
}

// TestZshCompletion sets completion up as a user would, then types into a
// live zsh and runs the edited line as `print LINE: …`, a marker the echoed
// input can't contain — printed after zsh reads it, so quoting is checked too.
func TestZshCompletion(t *testing.T) {
	home := completionHome(t)
	dir, env := zshEnv(t, home)
	cmd := exec.Command(binary, "completion", "install")
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	rc, _ := os.ReadFile(filepath.Join(dir, ".zshrc"))
	lacks(t, string(rc), "compinit") // the line itself must bring compinit up
	milk := number(t, home, "buy milk")
	cases := []struct{ typed, want string }{
		{"taskthing check milk\t", "taskthing check " + milk},
		// several titles: listed, and the typed word stays
		{"taskthing check review\t", "taskthing check review"},
		{"taskthing set title " + milk + " \t", "taskthing set title " + milk + " buy milk"},
		{"taskthing add x 'b:[my\t", "taskthing add x b:[my board]"},
		{"taskthing check --workspace=s\t", "taskthing check --workspace=side"},
	}
	for _, c := range cases {
		// -d: no global rc files — a distro's /etc/zshrc can swallow what is
		// typed before its prompt (Fedora's does), and the shim isn't theirs to test.
		out := zsh(t, env, `zpty z zsh -d -i
zpty -w z 'PROMPT='
zpty -w -n z `+shellQuote(c.typed)+`
sleep 0.5
zpty -w z $'\x01print -r -- ${:-LI}NE: '
zpty -r z out '*LINE: *'$'\n'
out=${out##*LINE: }
print -r -- "${out%%$'\r'*}"`)
		if got := strings.TrimSpace(out); got != c.want {
			t.Errorf("%q\n got %q\nwant %q", c.typed, got, c.want)
		}
	}
}

func TestCompletionInstall(t *testing.T) {
	home := dataHome(t)
	dir, env := zshEnv(t, home)
	for range 2 {
		cmd := exec.Command(binary, "completion", "install")
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
	}
	rc, _ := os.ReadFile(filepath.Join(dir, ".zshrc"))
	if n := strings.Count(string(rc), "taskthing completion zsh"); n != 1 {
		t.Errorf(".zshrc loads completion %d times, want once:\n%s", n, rc)
	}
	r := taskthing(t, home, "completion install fish")
	mustCode(t, r, 1)
}

// TestCompletionOffer: whoever installed before completion existed is asked
// once, on a terminal, on their next run.
func TestCompletionOffer(t *testing.T) {
	home := dataHome(t)
	dir, env := zshEnv(t, home)
	out := zsh(t, env, `zpty z taskthing boards
zpty -r z out '*\[Y/n\] '
zpty -w z y
zpty -r z out '*enabled*'
print -r -- "$out"`)
	contains(t, out, "completion enabled")
	rc, _ := os.ReadFile(filepath.Join(dir, ".zshrc"))
	contains(t, string(rc), `eval "$(taskthing completion zsh)"`)
	again := zsh(t, env, `zpty z taskthing boards
while zpty -r z chunk; do out+=$chunk; done
print -r -- "$out"`)
	lacks(t, again, "Enable TAB completion")
	// Piped, nothing asks.
	piped := dataHome(t)
	r := taskthing(t, piped, "boards")
	lacks(t, r.stdout, "Enable TAB completion")
}

// TestCompletePeerAndBranch: --peer offers the peers on the remote typed
// before it; --branch, the branches truncate may bound.
func TestCompletePeerAndBranch(t *testing.T) {
	home := dataHome(t)
	bare, clone := remote(t), t.TempDir()
	git(t, "clone", "--quiet", bare, clone)
	git(t, "-C", clone, "commit", "--quiet", "--allow-empty", "-m", "log")
	git(t, "-C", clone, "push", "--quiet", "origin", "HEAD:refs/heads/users/alice")
	got := strings.Join(complete(t, home, "workspace", "remote", "add", bare, "--peer", "al"), "\n")
	if got != "alice\ta peer on the remote — rejoin as it" {
		t.Errorf("--peer: got %q", got)
	}
	author := strings.TrimSpace(git(t, "config", "--global", "user.name"))
	got = strings.Join(complete(t, home, "truncate", "--branch", ""), "\n")
	if want := "master\nusers/" + author + "\tyour peer branch"; got != want {
		t.Errorf("--branch: got %q, want %q", got, want)
	}
}

// TestCompleteAddTags: inside add's text, an open tag completes wherever it
// sits — the whole quoted title arrives as one word — and a blank after the
// title offers the tags it doesn't carry yet.
const (
	dateHint       = "a date: today, tomorrow, friday, in 3 days, 25/12…"
	recurrenceHint = "a recurrence: every day, every monday, every 2 weeks…"
)

func TestCompleteAddTags(t *testing.T) {
	home := completionHome(t)
	cases := []struct {
		words []string
		want  []string
	}{
		// the candidate is the whole word, shown as just the part it adds
		// the candidate is the whole word, shown as just the part it adds; a
		// blank follows and the shell adds nothing, so the next tag can be typed
		{[]string{"add", "milk b:[my"}, []string{"milk b:[my board] \t\tmy board", "\tnospace"}},
		{[]string{"add", "milk d:[tomorrow] B:[w"}, []string{"milk d:[tomorrow] B:[work] \t\twork", "\tnospace"}},
		// a closed tag is not an open one
		{[]string{"add", "milk b:[work]"}, nil},
		// a date tag offers phrases the parser reads, with the day each means
		// (now is Wednesday 2026-07-22)
		{[]string{"add", "d:[to"}, []string{"d:[today]\t2026/07/22 00:00\ttoday", "d:[tomorrow]\t2026/07/23 00:00\ttomorrow"}},
		{[]string{"add", "walk", "date:[FRI"}, []string{"date:[friday]\t2026/07/24 00:00\tfriday"}},
		// anything else the parser reads is offered as typed, closed (a word
		// holding a blank was quoted: see the first cases)
		{[]string{"add", "d:[in 3 days"}, []string{"d:[in 3 days] \t2026/07/25 00:00\tin 3 days", "\tnospace"}},
		{[]string{"add", "d:[nonsense"}, nil},
		// a recurrence tag offers rules, described when the reading adds something
		{[]string{"add", "walk r:[every w"}, []string{"walk r:[every weekday] \t\tevery weekday", "walk r:[every week] \t\tevery week",
			"walk r:[every wednesday] \tevery week on Wednesday\tevery wednesday", "\tnospace"}},
		{[]string{"add", "recurrence:[every 3 days"}, []string{"recurrence:[every 3 days] \t\tevery 3 days", "\tnospace"}},
		{[]string{"add", "r:[every monday and fri"}, []string{"r:[every monday and fri] \tevery week on Monday, Friday\tevery monday and fri", "\tnospace"}},
		{[]string{"add", "r:[daily"}, nil},
		// a blank after some text offers the tags it doesn't carry yet
		{[]string{"add", "walk the dog "}, []string{"walk the dog d:[\t" + dateHint + "\td:[", "walk the dog r:[\t" + recurrenceHint + "\tr:[",
			"walk the dog b:[\ta board\tb:[", "\tnospace"}},
		{[]string{"add", "walk", "the", "dog", ""}, []string{"d:[\t" + dateHint, "r:[\t" + recurrenceHint, "b:[\ta board", "\tnospace"}},
		{[]string{"add", "walk d:[tomorrow] "}, []string{"walk d:[tomorrow] r:[\t" + recurrenceHint + "\tr:[", "walk d:[tomorrow] b:[\ta board\tb:[", "\tnospace"}},
		{[]string{"add", "walk", "B:[work]", "date:[friday]", ""}, []string{"r:[\t" + recurrenceHint, "\tnospace"}},
		{[]string{"add", ""}, nil},
		{[]string{"add", "walk the dog"}, nil},
		// a tag's first letters complete to the tag
		{[]string{"add", "walk the dog d"}, []string{"walk the dog d:[\t" + dateHint + "\td:[", "\tnospace"}},
		{[]string{"add", "walk", "rec"}, []string{"recurrence:[\t" + recurrenceHint, "\tnospace"}},
		{[]string{"add", "walk d:[friday] d"}, nil},
		// unquoted, a tag's value spans words: the candidate is the current word's end
		{[]string{"add", "walk", "d:[next", "w"}, []string{"week]\t2026/07/29 00:00\tnext week"}},
		{[]string{"add", "walk", "r:[every", "2", ""}, []string{"weeks]\t\tevery 2 weeks"}},
		{[]string{"add", "walk", "d:[in", "3", "days"}, []string{"days]\t2026/07/25 00:00\tin 3 days"}},
		{[]string{"add", "walk", "b:[my", "b"}, []string{"board]\t\tmy board"}},
	}
	for _, c := range cases {
		got := complete(t, home, c.words...)
		if c.want == nil {
			c.want = []string{""}
		}
		if strings.Join(got, "\n") != strings.Join(c.want, "\n") {
			t.Errorf("complete %q\n got %q\nwant %q", c.words, got, c.want)
		}
	}
}

// An empty date tag lists the phrases in the order the days come.
func TestCompleteAddDateVocabulary(t *testing.T) {
	home := completionHome(t)
	var shown []string
	for _, line := range complete(t, home, "add", "d:[") {
		parts := strings.Split(line, "\t")
		shown = append(shown, parts[2]+"="+parts[1])
	}
	equal(t, strings.Join(shown, "|"), "today=2026/07/22 00:00|tomorrow=2026/07/23 00:00|"+
		"thursday=2026/07/23 00:00|friday=2026/07/24 00:00|saturday=2026/07/25 00:00|sunday=2026/07/26 00:00|"+
		"monday=2026/07/27 00:00|tuesday=2026/07/28 00:00|wednesday=2026/07/22 00:00|"+
		"next week=2026/07/29 00:00|next month=2026/08/22 00:00")
}

// editedLine types keys into a live interactive shell and returns the line as
// the shell's editor holds it, printed by a key bound for that: add's text is
// completed inside an open quote, so the line can't be run to read it back.
func editedLine(t *testing.T, shell string, env []string, keys string) string {
	t.Helper()
	spawn := "zsh -d -i"
	if shell == "bash" {
		spawn = "bash --noprofile --rcfile $HOME/rc.bash -i"
	}
	out := zsh(t, env, `zpty z `+spawn+`
sleep 0.4
zpty -w -n z `+shellQuote(keys)+`
sleep 0.7
zpty -w -n z $'\x18\x04'
sleep 0.3
out=; while zpty -r -t z chunk; do out+=$chunk; done
out=${out##*LINE<<}
print -r -- "${out%%>>END*}"`)
	return strings.TrimSuffix(out, "\n")
}

// TestShellsCompleteAddText: the shims carry add's text completion into the
// line — only the new part listed, the quote left open, no blank added.
func TestShellsCompleteAddText(t *testing.T) {
	home := completionHome(t)
	dir, env := zshEnv(t, home)
	os.WriteFile(filepath.Join(dir, ".zshrc"), []byte(`eval "$(taskthing completion zsh)"
alias tt=taskthing
dump() { print -r -- ""; print -r -- "LINE<<$BUFFER>>END" }
zle -N dump; bindkey '^X^D' dump
PROMPT=
`), 0o644)
	os.WriteFile(filepath.Join(dir, "rc.bash"), []byte(`alias tt=taskthing
eval "$(taskthing completion bash)"
bind -x '"\C-x\C-d": echo "LINE<<$READLINE_LINE>>END"'
PS1=
`), 0o644)
	cases := []struct{ shells, typed, want string }{
		// several tags: listed, the text untouched
		{"bash zsh", "taskthing add \"walk the dog \t", `taskthing add "walk the dog `},
		{"bash zsh", "taskthing add \"walk the dog d\t", `taskthing add "walk the dog d:[`},
		{"bash zsh", "taskthing add \"walk the dog d:[tom\t", `taskthing add "walk the dog d:[tomorrow] `},
		{"bash zsh", "taskthing add \"walk the dog d:[to\t", `taskthing add "walk the dog d:[to`},
		{"bash zsh", "taskthing add \"walk the dog b:[my\t", `taskthing add "walk the dog b:[my board] `},
		// the whole flow, one TAB after another
		{"bash zsh", "taskthing add \"walk \td\ttom\tr\tevery d\t", `taskthing add "walk d:[tomorrow] r:[every day] `},
		// unquoted, word by word (zsh reads an unquoted [ as a glob)
		{"bash", "taskthing add walk d\t", `taskthing add walk d:\[`},
		{"bash", "taskthing add walk d:[next w\t", `taskthing add walk d:[next week\] `},
		// a date outside add, over two words
		{"bash zsh", "taskthing set date-time " + number(t, home, "buy milk") + " next w\t", "taskthing set date-time " + number(t, home, "buy milk") + " next week "},
		{"bash zsh", "taskthing list --period 2d\t", "taskthing list --period 2d "},
		// an alias for taskthing completes like it
		{"bash zsh", "tt check milk\t", "tt check " + number(t, home, "buy milk") + " "},
		// what completed before still does
		{"bash zsh", "taskthing check milk\t", "taskthing check " + number(t, home, "buy milk") + " "},
		{"bash zsh", "taskthing check review\t", "taskthing check review"},
	}
	for _, shell := range []string{"bash", "zsh"} {
		if _, err := exec.LookPath(shell); err != nil {
			continue
		}
		for _, c := range cases {
			if !strings.Contains(c.shells, shell) {
				continue
			}
			t.Run(shell+" "+strings.ReplaceAll(c.typed, "\t", "<TAB>"), func(t *testing.T) {
				t.Parallel()
				if got := editedLine(t, shell, env, c.typed); got != c.want {
					t.Errorf("got %q\nwant %q", got, c.want)
				}
			})
		}
	}
}

// TestCompleteDatesAndPeriods: the other places a date is typed get the same
// help as add's date tag.
func TestCompleteDatesAndPeriods(t *testing.T) {
	home := completionHome(t)
	milk := number(t, home, "buy milk")
	cases := []struct {
		words []string
		want  []string
	}{
		{[]string{"set", "date-time", milk, "to"}, []string{"today\t2026/07/22 00:00", "tomorrow\t2026/07/23 00:00"}},
		// the value runs over several words: the candidate is the current word's end
		{[]string{"set", "date-time", milk, "next", "w"}, []string{"week\t2026/07/29 00:00\tnext week"}},
		{[]string{"set", "date-time", milk, "in 3 days"}, []string{"in 3 days\t2026/07/25 00:00"}},
		{[]string{"set", "date-time", milk, "zzz"}, nil},
		// a period: the usual windows, or the number typed in each unit, with where it ends
		{[]string{"list", "--period", ""}, []string{"1d\tuntil 2026/07/23", "7d\tuntil 2026/07/29", "1m\tuntil 2026/08/22", "1y\tuntil 2027/07/22"}},
		{[]string{"list", "--period", "3"}, []string{"3d\tuntil 2026/07/25", "3m\tuntil 2026/10/22", "3y\tuntil 2029/07/22"}},
		{[]string{"list", "--period=2"}, []string{"--period=2d\tuntil 2026/07/24", "--period=2m\tuntil 2026/09/22", "--period=2y\tuntil 2028/07/22"}},
		{[]string{"list", "--period", "2d"}, []string{"2d\tuntil 2026/07/24"}},
		{[]string{"list", "--period", "soon"}, nil},
	}
	for _, c := range cases {
		got := complete(t, home, c.words...)
		if c.want == nil {
			c.want = []string{""}
		}
		if strings.Join(got, "\n") != strings.Join(c.want, "\n") {
			t.Errorf("complete %q\n got %q\nwant %q", c.words, got, c.want)
		}
	}
}
