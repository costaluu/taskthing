package app

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/costaluu/taskthing/internal/js"
	"github.com/costaluu/taskthing/internal/mdwal"
	"github.com/costaluu/taskthing/internal/nodefs"
)

// ── manager (git orchestration) ──────────────────────────────────────────────
//
// The only module allowed to invoke git. Its branch model:
//   master        — the consolidated snapshot: derived files committed,
//                   events.log empty, a threshold map of how far each peer
//                   has been consolidated.
//   users/<user>  — one branch per peer, tracking only that peer's events.log.

const (
	thresholdsFile = ".taskthing/thresholds.json"
	versionFile    = ".taskthing/version.json"
	seenMasterFile = ".taskthing/seen-master"
)

type manager struct {
	root   string
	m      *mdwal.Mdwal
	author func() string
}

func newManager(root string, m *mdwal.Mdwal, author func() string) *manager {
	return &manager{root: root, m: m, author: author}
}

// shellError is Bun's ShellError for a command that exited non-zero.
type shellError struct{ code int }

func (e *shellError) Error() string { return fmt.Sprintf("Failed with exit code %d", e.code) }

type gitRun struct {
	stdin string
	env   []string
}

// run is `$\`…\`.quiet()`: stdout, or a ShellError on a non-zero exit.
func (g *manager) run(opts gitRun, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Stdin = strings.NewReader(opts.stdin)
	if opts.env != nil {
		cmd.Env = append(os.Environ(), opts.env...)
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &bytes.Buffer{}
	err := cmd.Run()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			return out.String(), &shellError{exit.ExitCode()}
		}
		return out.String(), &shellError{1}
	}
	return out.String(), nil
}

func (g *manager) git(args ...string) (string, error) {
	return g.run(gitRun{}, "git", append([]string{"-C", g.root}, args...)...)
}

func (g *manager) userBranch() string { return "users/" + g.author() }

func (g *manager) fetchMaster() error {
	_, err := g.git("fetch", "origin", "+refs/heads/master:refs/remotes/origin/master")
	return err
}

func nonEmptyLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

func (g *manager) pull() error {
	if err := g.fetchMaster(); err != nil {
		return err
	}
	if err := g.assertCanReadMaster(); err != nil {
		return err
	}
	var derived []string
	for _, f := range g.m.Folders() {
		derived = append(derived, f+"/")
	}
	listed, err := g.git("ls-tree", "-r", "--name-only", "origin/master")
	if err != nil {
		return err
	}
	for _, path := range nonEmptyLines(listed) {
		if !slices.ContainsFunc(derived, func(d string) bool { return strings.HasPrefix(path, d) }) {
			continue
		}
		blob, err := g.git("show", "origin/master:"+path)
		if err != nil {
			return err
		}
		if err := nodefs.WriteFile(filepath.Join(g.root, path), blob); err != nil {
			return err
		}
	}
	base, err := g.m.Snapshot()
	if err != nil {
		return err
	}
	state, err := g.m.Replay(base, g.m.ParseLog())
	if err != nil {
		return err
	}
	return g.m.Materialize(state)
}

func (g *manager) writeGitignore() error {
	var ignored []string
	for _, f := range g.m.Folders() {
		ignored = append(ignored, f+"/")
	}
	return nodefs.WriteFile(filepath.Join(g.root, ".gitignore"), strings.Join(ignored, "\n")+"\n.taskthing/\n")
}

func (g *manager) publish() error {
	if _, err := g.git("add", "events.log"); err != nil {
		return err
	}
	staged, err := g.git("diff", "--cached", "--name-only")
	if err != nil {
		return err
	}
	if js.Trim(staged) == "" {
		return nil
	}
	if _, err := g.git("commit", "-m", "taskthing: publish events"); err != nil {
		return err
	}
	_, err = g.git("push", "origin", g.userBranch())
	return err
}

func (g *manager) thresholdsFromMaster() (*js.Object, error) {
	text, err := g.git("show", "origin/master:"+thresholdsFile)
	if err != nil {
		return nil, err
	}
	v, err := js.Parse(text)
	if err != nil {
		return nil, err
	}
	o, ok := v.(*js.Object)
	if !ok {
		o = js.NewObject()
	}
	return o, nil
}

func (g *manager) thresholdFromMaster() (*int64, error) {
	t, err := g.thresholdsFromMaster()
	if err != nil {
		return nil, err
	}
	v, ok := t.Get(g.author())
	if !ok {
		return nil, nil
	}
	s, _ := v.(string)
	n, err := mdwal.BigInt(s)
	if err != nil {
		return nil, err
	}
	return &n, nil
}

func (g *manager) sync() error {
	if err := g.fetchMaster(); err != nil {
		return err
	}
	tipOut, err := g.git("rev-parse", "origin/master")
	if err != nil {
		return err
	}
	tip := strings.TrimSpace(tipOut)
	seen, _ := nodefs.ReadFile(filepath.Join(g.root, seenMasterFile))
	if tip != strings.TrimSpace(seen) {
		threshold, err := g.thresholdFromMaster()
		if err != nil {
			return err
		}
		if threshold != nil {
			if err := g.m.Purge(*threshold); err != nil {
				return err
			}
		}
		if err := g.pull(); err != nil {
			return err
		}
		if err := nodefs.WriteFile(filepath.Join(g.root, seenMasterFile), tip); err != nil {
			return err
		}
	}
	return g.publish()
}

// addBlob hashes content (with the trailing newline `echo` adds) into the
// object database and stages it at path in the scratch index.
func (g *manager) addBlob(env []string, content, path string) error {
	blob, err := g.run(gitRun{stdin: content + "\n"}, "git", "-C", g.root, "hash-object", "-w", "--stdin")
	if err != nil {
		return err
	}
	_, err = g.run(gitRun{env: env}, "git", "-C", g.root, "update-index", "--add", "--cacheinfo", "100644,"+strings.TrimSpace(blob)+","+path)
	return err
}

func (g *manager) assertCanReadMaster() error {
	ours := g.m.Version()
	if ours == "" {
		return nil
	}
	text, _ := g.git("show", "origin/master:"+versionFile)
	if js.Trim(text) == "" {
		return nil
	}
	v, err := js.Parse(text)
	if err != nil {
		return err
	}
	o, _ := v.(*js.Object)
	theirs := ""
	if o != nil {
		theirs, _ = o.Value("version").(string)
	}
	if mdwal.CompareSemver(theirs, ours) > 0 {
		return fmt.Errorf("master was consolidated by version %s; update taskthing to >= %s", theirs, theirs)
	}
	return nil
}

func (g *manager) remoteBranches(url string) ([]string, error) {
	listed, err := g.run(gitRun{}, "git", "ls-remote", "--heads", url)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, l := range nonEmptyLines(listed) {
		_, after, _ := strings.Cut(l, "refs/heads/")
		out = append(out, after)
	}
	return out, nil
}

type peerLog struct {
	user   string
	events []mdwal.Event
}

// collectPeerLogs reads every peer's log out of git, in ref order.
func (g *manager) collectPeerLogs() ([]peerLog, error) {
	if _, err := g.git("fetch", "origin", "+refs/heads/users/*:refs/remotes/origin/users/*"); err != nil {
		return nil, err
	}
	refs, err := g.git("for-each-ref", "--format=%(refname:short)", "refs/remotes/origin/users")
	if err != nil {
		return nil, err
	}
	var logs []peerLog
	for _, ref := range nonEmptyLines(refs) {
		user := strings.TrimPrefix(ref, "origin/users/")
		text, err := g.git("show", ref+":events.log")
		if err != nil {
			return nil, err
		}
		events, err := mdwal.ParseEvents(text)
		if err != nil {
			return nil, err
		}
		logs = append(logs, peerLog{user, events})
	}
	return logs, nil
}

func (g *manager) assertAboveConsolidationFloor(branch, oldestKept string) error {
	if err := g.fetchMaster(); err != nil {
		return err
	}
	consolidated := int64(0)
	if t, err := g.thresholdFromMaster(); err != nil {
		return err
	} else if t != nil {
		consolidated = *t
	}
	dropped, err := g.git("rev-list", oldestKept+"^@")
	if err != nil {
		return err
	}
	for _, commit := range nonEmptyLines(dropped) {
		text, _ := g.git("show", commit+":events.log")
		events, err := mdwal.ParseEvents(text)
		if err != nil {
			return err
		}
		for _, ev := range events {
			if ev.Ts > consolidated {
				return fmt.Errorf("refusing to truncate %s: it would drop events not yet consolidated into master", branch)
			}
		}
	}
	return nil
}

func (g *manager) buildMasterCommit(thresholds *js.Object) (string, error) {
	index := filepath.Join(g.root, ".taskthing", "rebuild-index")
	if err := nodefs.MkdirAll(filepath.Join(g.root, ".taskthing")); err != nil {
		return "", err
	}
	os.Remove(index)
	env := []string{"GIT_INDEX_FILE=" + index}
	withIndex := func(args ...string) (string, error) {
		return g.run(gitRun{env: env}, "git", append([]string{"-C", g.root}, args...)...)
	}
	if _, err := withIndex("read-tree", "--empty"); err != nil {
		return "", err
	}
	for _, folder := range g.m.Folders() {
		withIndex("add", "-f", folder)
	}
	empty, err := g.run(gitRun{}, "git", "-C", g.root, "hash-object", "-w", "--stdin")
	if err != nil {
		return "", err
	}
	if _, err := withIndex("update-index", "--add", "--cacheinfo", "100644,"+strings.TrimSpace(empty)+",events.log"); err != nil {
		return "", err
	}
	if err := g.addBlob(env, js.Stringify(thresholds), thresholdsFile); err != nil {
		return "", err
	}
	if v := g.m.Version(); v != "" {
		if err := g.addBlob(env, js.Stringify(js.Obj("version", v)), versionFile); err != nil {
			return "", err
		}
	}
	tree, err := withIndex("write-tree")
	if err != nil {
		return "", err
	}
	commit, err := g.git("commit-tree", strings.TrimSpace(tree), "-m", "taskthing: consolidated snapshot")
	if err != nil {
		return "", err
	}
	os.Remove(index)
	return strings.TrimSpace(commit), nil
}

func (g *manager) pushMaster(commit, expected string) bool {
	_, err := g.git("push", "--force-with-lease=refs/heads/master:"+expected, "origin", commit+":refs/heads/master")
	return err == nil
}

// isAssociated: the workspace is remote exactly while git has an origin.
func (g *manager) isAssociated() bool {
	_, err := g.git("remote", "get-url", "origin")
	return err == nil
}

func (g *manager) disassociate() error {
	if _, err := g.git("remote", "remove", "origin"); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(g.root, "events.log")); err != nil && !os.IsNotExist(err) {
		return nodefs.Wrap("rm", filepath.Join(g.root, "events.log"), err)
	}
	return nil
}

// truncateHistory: an absent branch means this peer's own; an empty one is
// just a branch name nobody owns.
func (g *manager) truncateHistory(named *string, keepN int) error {
	branch := g.userBranch()
	if named != nil {
		branch = *named
	}
	if branch != g.userBranch() && branch != "master" {
		return fmt.Errorf("refusing to truncate %s: not a branch you own", branch)
	}
	if _, err := g.git("fetch", "origin", "+refs/heads/"+branch+":refs/remotes/origin/"+branch); err != nil {
		return err
	}
	listed, err := g.git("rev-list", "--max-count="+strconv.Itoa(keepN), "origin/"+branch)
	if err != nil {
		return err
	}
	keep := nonEmptyLines(listed)
	slices.Reverse(keep)
	oldest := ""
	if len(keep) > 0 {
		oldest = keep[0]
	}
	if err := g.assertAboveConsolidationFloor(branch, oldest); err != nil {
		return err
	}
	parent := ""
	for _, commit := range keep {
		tree, err := g.git("rev-parse", commit+"^{tree}")
		if err != nil {
			return err
		}
		message, err := g.git("log", "-1", "--format=%s", commit)
		if err != nil {
			return err
		}
		args := []string{"commit-tree", strings.TrimSpace(tree)}
		if parent != "" {
			args = append(args, "-p", parent)
		}
		args = append(args, "-m", strings.TrimSpace(message))
		out, err := g.git(args...)
		if err != nil {
			return err
		}
		parent = strings.TrimSpace(out)
	}
	if _, err := g.git("push", "--force", "origin", parent+":refs/heads/"+branch); err != nil {
		return err
	}
	if branch == g.userBranch() {
		_, err = g.git("reset", "--hard", parent)
	}
	return err
}

func (g *manager) rebuild() error {
	if err := g.sync(); err != nil {
		return err
	}
	for {
		if err := g.fetchMaster(); err != nil {
			return err
		}
		againstOut, err := g.git("rev-parse", "origin/master")
		if err != nil {
			return err
		}
		against := strings.TrimSpace(againstOut)
		logs, err := g.collectPeerLogs()
		if err != nil {
			return err
		}
		own := g.m.ParseLog()
		if i := slices.IndexFunc(logs, func(l peerLog) bool { return l.user == g.author() }); i >= 0 {
			logs[i].events = own
		} else {
			logs = append(logs, peerLog{g.author(), own})
		}
		thresholds, err := g.thresholdsFromMaster()
		if err != nil {
			return err
		}
		var all []mdwal.Event
		for _, l := range logs {
			all = append(all, l.events...)
			for _, ev := range l.events {
				seenV, ok := thresholds.Get(l.user)
				seen, _ := seenV.(string)
				seenTs, _ := mdwal.BigInt(seen)
				if !ok || ev.Ts > seenTs {
					thresholds.Set(l.user, strconv.FormatInt(ev.Ts, 10))
				}
			}
		}
		sort.SliceStable(all, func(i, j int) bool { return all[i].Ts < all[j].Ts })
		if err := g.pull(); err != nil {
			return err
		}
		base, err := g.m.Snapshot()
		if err != nil {
			return err
		}
		state, err := g.m.Replay(base, all)
		if err != nil {
			return err
		}
		if err := g.m.Materialize(state); err != nil {
			return err
		}
		commit, err := g.buildMasterCommit(thresholds)
		if err != nil {
			return err
		}
		if g.pushMaster(commit, against) {
			break
		}
	}
	return g.sync()
}

func (g *manager) init(url string, confirmDestroy bool) error {
	author := g.author()
	branches, err := g.remoteBranches(url)
	if err != nil {
		return err
	}
	hasLocalWork := false
	for _, t := range g.m.EntityTypes() {
		entities, err := g.m.ReadAll(t)
		if err != nil {
			return err
		}
		if len(entities) > 0 {
			hasLocalWork = true
		}
	}
	rejoining := slices.Contains(branches, g.userBranch())
	if rejoining && hasLocalWork && !confirmDestroy {
		return fmt.Errorf("%s already exists on the remote and this workspace has local work; confirm that the local tasks may be destroyed to continue", g.userBranch())
	}
	for _, args := range [][]string{
		{"init", "--initial-branch=master"},
		{"config", "user.name", author},
		{"config", "user.email", author + "@taskthing.local"},
		{"remote", "add", "origin", url},
	} {
		if _, err := g.git(args...); err != nil {
			return err
		}
	}
	hasMaster := slices.Contains(branches, "master")
	if !hasMaster {
		master, err := g.buildMasterCommit(js.NewObject())
		if err != nil {
			return err
		}
		if _, err := g.git("push", "origin", master+":refs/heads/master"); err != nil {
			return err
		}
	}
	if err := g.writeGitignore(); err != nil {
		return err
	}
	if rejoining {
		for _, folder := range g.m.Folders() {
			if err := nodefs.RemoveAll(filepath.Join(g.root, folder)); err != nil {
				return err
			}
		}
		os.Remove(filepath.Join(g.root, "events.log"))
		if _, err := g.git("fetch", "origin", "+refs/heads/"+g.userBranch()+":refs/remotes/origin/"+g.userBranch()); err != nil {
			return err
		}
		if _, err := g.git("checkout", "-b", g.userBranch(), "origin/"+g.userBranch()); err != nil {
			return err
		}
	} else {
		logPath := filepath.Join(g.root, "events.log")
		if !nodefs.Exists(logPath) {
			if err := nodefs.WriteFile(logPath, ""); err != nil {
				return err
			}
		}
		for _, args := range [][]string{
			{"checkout", "-b", g.userBranch()},
			{"add", ".gitignore", "events.log"},
			{"commit", "-m", "taskthing: associate workspace"},
		} {
			if _, err := g.git(args...); err != nil {
				return err
			}
		}
	}
	if hasMaster && !rejoining {
		for _, t := range g.m.EntityTypes() {
			entities, err := g.m.ReadAll(t)
			if err != nil {
				return err
			}
			for _, e := range entities {
				if err := g.m.LogExisting(t, str(e, "id")); err != nil {
					return err
				}
			}
		}
	}
	if _, err := g.git("push", "origin", g.userBranch()); err != nil {
		return err
	}
	return g.sync()
}
