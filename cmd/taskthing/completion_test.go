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
		{[]string{"board", "set", "color", "work", "gree"}, []string{"green", "greenBright"}},
		{[]string{"add", "milk", "b:[my"}, []string{"b:[my board]"}},
		{[]string{"use", ""}, []string{"side"}},
		{[]string{"check", "--workspace", "s"}, []string{"side"}},
		{[]string{"config", "set", "autoUpdate", ""}, []string{"confirm", "silent"}},
		{[]string{"install", "--date-format", ""}, []string{"america", "europe"}},
		{[]string{"help", "board", "s"}, []string{"set\tedit a board field"}},
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
		out := zsh(t, env, `zpty z zsh -i
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
