package app

import (
	"bufio"
	"context"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/costaluu/taskthing/internal/js"
	"github.com/costaluu/taskthing/internal/mdwal"
	"github.com/costaluu/taskthing/internal/nodefs"
)

// ── shell completion ─────────────────────────────────────────────────────────
//
// `taskthing __complete <words…> <current>` prints one candidate per line,
// `value\tdescription`. Everything shell-specific lives in the shims below;
// the shim only escapes, displays, and keeps the typed word when no
// candidate extends it (a title typed to find a task's number).

//go:embed completion.bash
var bashShim string

//go:embed completion.zsh
var zshShim string

type candidate struct{ value, desc string }

// completer offers the next word; it filters by cur itself, since a task
// matches by its title, not by the number it completes to.
type completer func(c *Ctx, cur string) []candidate

func printCandidates(cands []candidate) {
	for _, c := range cands {
		if c.desc == "" {
			fmt.Println(c.value)
		} else {
			fmt.Println(c.value + "\t" + c.desc)
		}
	}
}

func byPrefix(cur string, cands []candidate) []candidate {
	var out []candidate
	for _, c := range cands {
		if strings.HasPrefix(c.value, cur) {
			out = append(out, c)
		}
	}
	return out
}

func plain(values ...string) []candidate {
	out := make([]candidate, len(values))
	for i, v := range values {
		out[i] = candidate{value: v}
	}
	return out
}

// complete is the candidates for the last word of words, read from the same
// tree, global stripping and option parsing Run uses.
func (c *CLI) complete(words []string) []candidate {
	if len(words) == 0 {
		return nil
	}
	cur, prev := words[len(words)-1], words[:len(words)-1]
	help := len(prev) > 0 && prev[0] == "help"
	if help {
		prev = prev[1:]
	}
	lookup, _ := stripGlobals(prev)
	cmd, rest := c.findCommand(lookup)
	if cmd == nil {
		if len(lookup) > 0 {
			return nil
		}
		return byPrefix(cur, commandCandidates(c.top))
	}
	if help {
		if len(rest) > 0 {
			return nil
		}
		return byPrefix(cur, commandCandidates(cmd.Commands))
	}
	afterDash := slices.Contains(prev, "--")
	// A half-typed line: an option still missing or mistyped mustn't fail
	// it, so every value reads as a plain string.
	loose := slices.Clone(cmd.Options)
	for i := range loose {
		loose[i].Kind = String
	}
	flags, positional, err := parseArgs(rest, loose, cmd.Name, false)
	if err != nil {
		return nil
	}
	ctx := &Ctx{Flags: flags, Positional: positional}
	// An option's value: `--name=value`, or the word after `--name`.
	if strings.HasPrefix(cur, "--") && !afterDash {
		if name, value, ok := strings.Cut(cur[2:], "="); ok {
			o, found := findOption(cmd.Options, name)
			if !found || o.Flag {
				return nil
			}
			out := optionValues(o, ctx, value)
			for i := range out {
				out[i].value = "--" + name + "=" + out[i].value
			}
			return out
		}
	}
	if n := len(prev); n > 0 && !afterDash && strings.HasPrefix(prev[n-1], "--") {
		if o, ok := findOption(cmd.Options, prev[n-1][2:]); ok && !o.Flag {
			return optionValues(o, ctx, cur)
		}
	}
	if strings.HasPrefix(cur, "-") && !afterDash {
		var out []candidate
		for _, o := range cmd.Options {
			if !slices.Contains(prev, "--"+o.Name) {
				out = append(out, candidate{"--" + o.Name, o.Description})
			}
		}
		return byPrefix(cur, append(out, candidate{"--help", "Show help"}))
	}
	var out []candidate
	if len(positional) == 0 {
		out = byPrefix(cur, commandCandidates(cmd.Commands))
	}
	if cmd.Args != nil {
		out = append(out, cmd.Args(ctx, cur)...)
	}
	return out
}

func optionValues(o Option, c *Ctx, cur string) []candidate {
	if o.Complete != nil {
		return o.Complete(c, cur)
	}
	return byPrefix(cur, plain(o.Values...))
}

func commandCandidates(cmds []*Command) []candidate {
	out := make([]candidate, len(cmds))
	for i, cmd := range cmds {
		out[i] = candidate{cmd.Name, cmd.Description}
	}
	return out
}

// ── what the words name ──────────────────────────────────────────────────────

// readOnlyWorkspace reads entities without the git author and clock
// openWorkspace sets up for writing.
func readOnlyWorkspace(root string) *mdwal.Mdwal {
	return mdwal.New(mdwal.Config{Root: root,
		Schemas: []mdwal.SchemaEntry{{Type: "task", Schema: mdwal.TaskFields}, {Type: "board", Schema: mdwal.BoardFields}}})
}

// entityMatch: a number or name completes by prefix; anything else typed
// finds the entity by its description (a title, a board's name) instead.
func entityMatch(cur string, c candidate) bool {
	if strings.HasPrefix(c.value, cur) {
		return true
	}
	if _, err := strconv.Atoi(cur); err == nil {
		return false
	}
	return c.desc != "" && strings.Contains(strings.ToLower(c.desc), strings.ToLower(cur))
}

// taskRefs offers the numbers the last listing handed out — the workspace's
// own first, then a global listing's, as resolveTask reads them — for tasks
// that keep, described by their title. A repeating verb skips what's typed.
func taskRefs(keep func(task *js.Object) bool, repeat bool) completer {
	return func(c *Ctx, cur string) []candidate {
		if !repeat && len(c.Positional) > 0 {
			return nil
		}
		root, err := targetWorkspace(workspaceFlag(c))
		if err != nil {
			return nil
		}
		type ref struct{ root, id string }
		refs := map[int]ref{}
		global := globalKvStore(dataHome()).load()
		for _, k := range global.Keys() {
			v, _ := global.Value(k).(string)
			if ws, id, ok := strings.Cut(v, "/"); ok {
				n, _ := strconv.Atoi(k)
				refs[n] = ref{workspacePath(ws), id}
			}
		}
		own := workspaceKvStore(root, "tasks").load()
		for _, k := range own.Keys() {
			v, _ := own.Value(k).(string)
			n, _ := strconv.Atoi(k)
			refs[n] = ref{root, v}
		}
		var numbers []int
		for n := range refs {
			numbers = append(numbers, n)
		}
		sort.Ints(numbers)
		var out []candidate
		for _, n := range numbers {
			r := refs[n]
			value := strconv.Itoa(n)
			if slices.Contains(c.Positional, value) {
				continue
			}
			task, err := readOnlyWorkspace(r.root).Read("task", r.id)
			if err != nil || !keep(task) {
				continue
			}
			if cand := (candidate{value, str(task, "title")}); entityMatch(cur, cand) {
				out = append(out, cand)
			}
		}
		return out
	}
}

func is(field string, want bool) func(*js.Object) bool {
	return func(t *js.Object) bool { return !boolean(t, "deleted") && boolean(t, field) == want }
}

func isDeleted(t *js.Object) bool { return boolean(t, "deleted") }
func isLive(t *js.Object) bool    { return !boolean(t, "deleted") }

// boardCandidates are live boards by name — what resolveBoard reads — or,
// deleted, by id, since a deleted board's name no longer resolves.
func boardCandidates(c *Ctx, deleted, inbox bool) []candidate {
	root, err := targetWorkspace(workspaceFlag(c))
	if err != nil {
		return nil
	}
	boards, err := boardRepository(readOnlyWorkspace(root)).filter(func(b *js.Object) bool { return boolean(b, "deleted") == deleted })
	if err != nil {
		return nil
	}
	sort.Slice(boards, func(i, j int) bool { return str(boards[i], "name") < str(boards[j], "name") })
	var out []candidate
	if inbox {
		out = append(out, candidate{value: "inbox"})
	}
	for _, b := range boards {
		if deleted {
			out = append(out, candidate{str(b, "id"), str(b, "name")})
		} else {
			out = append(out, candidate{value: str(b, "name")})
		}
	}
	return out
}

// boardRefs is the board argument of board verbs; a repeating one skips what's typed.
func boardRefs(deleted, repeat bool) completer {
	return func(c *Ctx, cur string) []candidate {
		if !repeat && len(c.Positional) > 0 {
			return nil
		}
		var out []candidate
		for _, b := range boardCandidates(c, deleted, false) {
			if !slices.Contains(c.Positional, b.value) && entityMatch(cur, b) {
				out = append(out, b)
			}
		}
		return out
	}
}

func boardOrInbox(c *Ctx, cur string) []candidate {
	return byPrefix(cur, boardCandidates(c, false, true))
}

// taskThen and boardThen are `set`'s arguments: what to edit, then its new value.
func taskThen(value completer) completer  { return refThen(taskRefs(isLive, false), value) }
func boardThen(value completer) completer { return refThen(boardRefs(false, false), value) }

func refThen(ref, value completer) completer {
	return func(c *Ctx, cur string) []candidate {
		if len(c.Positional) == 0 {
			return ref(c, cur)
		}
		if value == nil || len(c.Positional) != 1 {
			return nil
		}
		return value(c, cur)
	}
}

// currentValue offers a field's value as it stands, to edit rather than retype.
func currentValue(field string, read func(c *Ctx) *js.Object) completer {
	return func(c *Ctx, cur string) []candidate {
		if e := read(c); e != nil && str(e, field) != "" {
			return byPrefix(cur, plain(str(e, field)))
		}
		return nil
	}
}

func referencedTask(c *Ctx) *js.Object {
	root, id, err := resolveTask(first(c.Positional), workspaceFlag(c))
	if err != nil {
		return nil
	}
	return taskRepository(readOnlyWorkspace(root)).findByID(id)
}

func referencedBoard(c *Ctx) *js.Object {
	root, err := targetWorkspace(workspaceFlag(c))
	if err != nil {
		return nil
	}
	m := readOnlyWorkspace(root)
	id, err := resolveBoard(boardRepository(m), root, first(c.Positional))
	if err != nil {
		return nil
	}
	return boardRepository(m).findByID(id)
}

func workspaces(cur string, skipCurrent bool) []candidate {
	all, err := allWorkspaces()
	if err != nil {
		return nil
	}
	current, _ := currentWorkspaceName()
	var out []candidate
	for _, w := range all {
		if !(skipCurrent && w == current) {
			out = append(out, candidate{value: w})
		}
	}
	return byPrefix(cur, out)
}

func workspaceValue(_ *Ctx, cur string) []candidate { return workspaces(cur, false) }

// workspaceArg is a workspace verb's first argument.
func workspaceArg(skipCurrent bool) completer {
	return func(c *Ctx, cur string) []candidate {
		if len(c.Positional) > 0 {
			return nil
		}
		return workspaces(cur, skipCurrent)
	}
}

// configArgs: a key, then the values its schema allows.
func configArgs(c *Ctx, cur string) []candidate {
	switch len(c.Positional) {
	case 0:
		return byPrefix(cur, plain(configKeys...))
	case 1:
		for _, f := range mdwal.ConfigSchema.Fields {
			if f.Name == c.Positional[0] {
				if f.Kind == "boolean" {
					return byPrefix(cur, plain("true", "false"))
				}
				return byPrefix(cur, plain(f.Values...))
			}
		}
	}
	return nil
}

// openBoardTag is an add's `b:[…` tag still being typed.
var openBoardTag = regexp.MustCompile(`(?i)^((?:b|board):\[)([^\]]*)$`)

func addArgs(c *Ctx, cur string) []candidate {
	m := openBoardTag.FindStringSubmatch(cur)
	if m == nil {
		return nil
	}
	var out []candidate
	for _, b := range boardCandidates(c, false, true) {
		if strings.HasPrefix(strings.ToLower(b.value), strings.ToLower(m[2])) {
			out = append(out, candidate{value: m[1] + b.value + "]"})
		}
	}
	return out
}

// peerValue: the peers already on the remote typed before — rejoining one is
// what --peer is for — and the git user.name a new peer defaults to.
func peerValue(c *Ctx, cur string) []candidate {
	var out []candidate
	root, _ := targetWorkspace(workspaceFlag(c))
	if name, err := resolveGitAuthor(root); err == nil {
		out = append(out, candidate{name, "your git user.name"})
	}
	if url := first(c.Positional); url != "" {
		// ponytail: a network call on TAB, bounded so a slow or
		// password-guarded remote costs a pause, never a hang.
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		ls := exec.CommandContext(ctx, "git", "ls-remote", "--heads", url, "users/*")
		ls.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_SSH_COMMAND=ssh -o BatchMode=yes -o ConnectTimeout=2")
		ls.WaitDelay = time.Second // killed git leaves ssh holding the pipes
		listed, _ := ls.Output()
		for _, line := range nonEmptyLines(string(listed)) {
			_, ref, _ := strings.Cut(line, "\t")
			name := strings.TrimPrefix(ref, "refs/heads/users/")
			if !slices.ContainsFunc(out, func(p candidate) bool { return p.value == name }) {
				out = append(out, candidate{name, "a peer on the remote — rejoin as it"})
			}
		}
	}
	return byPrefix(cur, out)
}

// branchValue is what truncate may bound: master, or this peer's own branch.
func branchValue(c *Ctx, cur string) []candidate {
	out := plain("master")
	if root, err := targetWorkspace(workspaceFlag(c)); err == nil {
		if name, err := resolveGitAuthor(root); err == nil {
			out = append(out, candidate{"users/" + name, "your peer branch"})
		}
	}
	return byPrefix(cur, out)
}

func shellArgs(c *Ctx, cur string) []candidate {
	if len(c.Positional) > 0 {
		return nil
	}
	return byPrefix(cur, plain("bash", "zsh"))
}

func completionCommand() *Command {
	return &Command{Name: "completion", Description: "print the shell completion script (bash|zsh)", Args: shellArgs,
		Handler: func(c *Ctx) error {
			switch first(c.Positional) {
			case "bash":
				fmt.Print(bashShim)
			case "zsh":
				fmt.Print(zshShim)
			default:
				return fmt.Errorf("no completion for %s — try bash or zsh", js.Quote(first(c.Positional)))
			}
			return nil
		},
		Commands: []*Command{{Name: "install", Description: "enable completion in your shell's rc file (default: $SHELL's)", Args: shellArgs,
			Handler: func(c *Ctx) error {
				shell := first(c.Positional)
				if shell == "" {
					shell = userShell()
				}
				rc, already, err := installCompletion(shell)
				if err != nil {
					return err
				}
				if already {
					consoleLog("completion is already enabled in " + rc)
				} else {
					consoleLog("completion enabled in " + rc + " — open a new terminal to use it")
				}
				return nil
			}}},
	}
}

// ── turning completion on ────────────────────────────────────────────────────

func userShell() string { return filepath.Base(os.Getenv("SHELL")) }

// completionRC is the file a shell reads at startup and the line there that
// loads completion — from the binary, so it never goes stale across updates.
// "" for a shell taskthing has no completion for.
func completionRC(shell string) (rc, line string) {
	home, _ := os.UserHomeDir()
	switch shell {
	case "bash":
		rc = filepath.Join(home, ".bashrc")
		if runtime.GOOS == "darwin" { // macOS terminals open login shells
			rc = filepath.Join(home, ".bash_profile")
		}
	case "zsh":
		dir := os.Getenv("ZDOTDIR")
		if dir == "" {
			dir = home
		}
		rc = filepath.Join(dir, ".zshrc")
	default:
		return "", ""
	}
	return rc, `command -v taskthing >/dev/null && eval "$(taskthing completion ` + shell + `)"`
}

func completionEnabled(rc, shell string) bool {
	text, _ := nodefs.ReadFile(rc)
	return strings.Contains(text, "taskthing completion "+shell)
}

// installCompletion appends the loading line once; already says it was there.
func installCompletion(shell string) (rc string, already bool, err error) {
	rc, line := completionRC(shell)
	if rc == "" {
		return "", false, fmt.Errorf("no completion for %s — try bash or zsh", js.Quote(shell))
	}
	if completionEnabled(rc, shell) {
		return rc, true, nil
	}
	f, err := os.OpenFile(rc, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return "", false, err
	}
	defer f.Close()
	_, err = f.WriteString("\n# taskthing: TAB completion\n" + line + "\n")
	return rc, false, err
}

// offerCompletion asks once per data home, on a terminal, whether to enable
// completion — so whoever installed before it existed is asked too.
func offerCompletion() {
	marker := filepath.Join(dataHome(), ".completion-offered")
	if !stdinTTY || !stdoutTTY || nodefs.Exists(marker) || !nodefs.Exists(configPath()) {
		return
	}
	if err := nodefs.WriteFile(marker, ""); err != nil {
		return // unrecorded, it would ask on every run
	}
	shell := userShell()
	rc, _ := completionRC(shell)
	if rc == "" || completionEnabled(rc, shell) {
		return
	}
	fmt.Printf("Enable TAB completion for taskthing? It adds one line to %s [Y/n] ", rc)
	answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	if a := strings.ToLower(strings.TrimSpace(answer)); a != "" && a != "y" && a != "yes" {
		consoleLog("skipped — `taskthing completion install` enables it later")
		return
	}
	if _, _, err := installCompletion(shell); err != nil {
		consoleLog("could not enable completion: " + err.Error())
		return
	}
	consoleLog("completion enabled — open a new terminal to use it")
}
