package app

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/costaluu/taskthing/internal/js"
	"github.com/costaluu/taskthing/internal/mdwal"
	"github.com/costaluu/taskthing/internal/nodefs"
)

// ── migration runner (structural — ADR-0007) ─────────────────────────────────

type migrationRunner struct {
	migrations       []Migration
	workspaces       []string
	engine           func(root string) *mdwal.Mdwal
	readConfig       func() (*js.Object, error)
	writeConfig      func(*js.Object) error
	configHome       string
	configMigrations []ConfigMigration
}

func recordDir(root string) string { return filepath.Join(root, ".taskthing", "migrations") }

func appliedIn(dir string) map[string]bool {
	out := map[string]bool{}
	names, err := nodefs.Readdir(dir)
	if err != nil {
		return out
	}
	for _, n := range names {
		out[strings.TrimSuffix(n, ".md")] = true
	}
	return out
}

func applyFolderEvent(m *mdwal.Mdwal, ev mdwal.Event) error {
	off := &mdwal.LogOption{NoLog: true}
	switch ev.Op {
	case "CREATE_FOLDER":
		return m.CreateFolder(ev.EntityType, off)
	case "RENAME_FOLDER":
		to, _ := ev.Payload.Value("to").(string)
		return m.RenameFolder(ev.EntityType, to, off)
	case "TRUNCATE_FOLDER":
		return m.TruncateFolder(ev.EntityType, off)
	case "DELETE_FOLDER":
		return m.DeleteFolder(ev.EntityType, off)
	}
	return fmt.Errorf("structural migration carries a non-folder op: %s", ev.Op)
}

func pendingMigrations[T any](all []T, version func(T) string, done map[string]bool) []T {
	var pending []T
	for _, m := range all {
		if !done[version(m)] {
			pending = append(pending, m)
		}
	}
	sort.SliceStable(pending, func(i, j int) bool { return semverOrder(version(pending[i]), version(pending[j])) < 0 })
	return pending
}

func (r migrationRunner) migrateWorkspace(root string) error {
	pending := pendingMigrations(r.migrations, func(m Migration) string { return m.Version }, appliedIn(recordDir(root)))
	m := r.engine(root)
	for _, migration := range pending {
		events, err := mdwal.ParseEvents(migration.Content)
		if err != nil {
			return err
		}
		for _, ev := range events {
			if err := applyFolderEvent(m, ev); err != nil {
				return err
			}
		}
		if err := nodefs.MkdirAll(recordDir(root)); err != nil {
			return err
		}
		if err := nodefs.WriteFile(filepath.Join(recordDir(root), migration.Version+".md"), migration.Content); err != nil {
			return err
		}
	}
	if len(pending) > 0 {
		path := filepath.Join(root, ".gitignore")
		if nodefs.Exists(path) {
			var ignored []string
			for _, f := range m.Folders() {
				ignored = append(ignored, f+"/")
			}
			return nodefs.WriteFile(path, strings.Join(ignored, "\n")+"\n.taskthing/\n")
		}
	}
	return nil
}

func (r migrationRunner) migrateConfig() error {
	if len(r.configMigrations) == 0 || r.readConfig == nil || r.configHome == "" {
		return nil
	}
	dir := filepath.Join(r.configHome, "migrations")
	pending := pendingMigrations(r.configMigrations, func(m ConfigMigration) string { return m.Version }, appliedIn(dir))
	if len(pending) == 0 {
		return nil
	}
	config, err := r.readConfig()
	if err != nil {
		return err
	}
	for _, m := range pending {
		config = m.Apply(config)
	}
	if err := r.writeConfig(config); err != nil {
		return err
	}
	if err := nodefs.MkdirAll(dir); err != nil {
		return err
	}
	for _, m := range pending {
		if err := nodefs.WriteFile(filepath.Join(dir, m.Version+".md"), m.Version); err != nil {
			return err
		}
	}
	return nil
}

// run attempts every workspace and the config, then reports all failures.
func (r migrationRunner) run() error {
	var failures []string
	for _, root := range r.workspaces {
		if err := r.migrateWorkspace(root); err != nil {
			failures = append(failures, root+": "+err.Error())
		}
	}
	if err := r.migrateConfig(); err != nil {
		failures = append(failures, "config.md: "+err.Error())
	}
	if len(failures) > 0 {
		return fmt.Errorf("migration failed on %d target(s): %s", len(failures), strings.Join(failures, "; "))
	}
	return nil
}

// ── semver (Bun.semver.order) ────────────────────────────────────────────────

type semver struct {
	major, minor, patch int
	pre                 []string
}

func parseSemver(v string) semver {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	v, _, _ = strings.Cut(v, "+")
	core, pre, hasPre := strings.Cut(v, "-")
	parts := strings.Split(core, ".")
	n := func(i int) int {
		if i >= len(parts) {
			return 0
		}
		x, _ := strconv.Atoi(parts[i])
		return x
	}
	s := semver{major: n(0), minor: n(1), patch: n(2)}
	if hasPre {
		s.pre = strings.Split(pre, ".")
	}
	return s
}

// semverOrder is -1, 0 or 1.
func semverOrder(a, b string) int {
	x, y := parseSemver(a), parseSemver(b)
	for _, d := range []int{x.major - y.major, x.minor - y.minor, x.patch - y.patch} {
		if d != 0 {
			if d < 0 {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(x.pre) == 0 && len(y.pre) == 0:
		return 0
	case len(x.pre) == 0:
		return 1
	case len(y.pre) == 0:
		return -1
	}
	for i := 0; i < len(x.pre) && i < len(y.pre); i++ {
		if x.pre[i] == y.pre[i] {
			continue
		}
		xi, xErr := strconv.Atoi(x.pre[i])
		yi, yErr := strconv.Atoi(y.pre[i])
		switch {
		case xErr == nil && yErr == nil:
			if xi < yi {
				return -1
			}
			return 1
		case xErr == nil:
			return -1
		case yErr == nil:
			return 1
		case x.pre[i] < y.pre[i]:
			return -1
		default:
			return 1
		}
	}
	switch {
	case len(x.pre) < len(y.pre):
		return -1
	case len(x.pre) > len(y.pre):
		return 1
	}
	return 0
}

// ── update orchestrator (updater.ts) ─────────────────────────────────────────

type release struct {
	version string
	assets  []asset
}

type asset struct{ name, url string }

type updateState struct {
	lastCheck       *float64
	updateAvailable bool
	target          *string
	autoUpdate      string
}

type updater struct {
	currentVersion string
	os, arch       string
	latest         func() (release, error)
	now            func() float64
	readState      func() (updateState, error)
	writeState     func(updateState) error
	download       func(url string, progress func(done int64, total *int64)) ([]byte, error)
	swap           func([]byte) error
	cleanupStale   func()
	runMigrations  func() error
}

const twelveHours = 12 * 60 * 60 * 1000

// selectAsset: the asset whose name carries both the OS and the arch tokens.
func selectAsset(r release, osToken, arch string) (asset, error) {
	for _, a := range r.assets {
		if strings.Contains(a.name, osToken) && strings.Contains(a.name, arch) {
			return a, nil
		}
	}
	return asset{}, fmt.Errorf("no release asset for %s/%s in %s", osToken, arch, r.version)
}

type checkResult struct {
	pending         bool
	current, target string
}

func (u *updater) check(force bool) (checkResult, error) {
	state, err := u.readState()
	if err != nil {
		return checkResult{}, err
	}
	fresh := state.lastCheck != nil && u.now()-*state.lastCheck < twelveHours
	if !force && fresh {
		if state.updateAvailable && state.target != nil {
			return checkResult{true, u.currentVersion, *state.target}, nil
		}
		return checkResult{}, nil
	}
	r, err := u.latest()
	if err != nil {
		return checkResult{}, err
	}
	pending := semverOrder(r.version, u.currentVersion) > 0
	if pending {
		if _, err := selectAsset(r, u.os, u.arch); err != nil {
			return checkResult{}, err
		}
	}
	nowMs := u.now()
	state.lastCheck = &nowMs
	state.updateAvailable = pending
	state.target = nil
	if pending {
		v := r.version
		state.target = &v
	}
	if err := u.writeState(state); err != nil {
		return checkResult{}, err
	}
	if pending {
		return checkResult{true, u.currentVersion, r.version}, nil
	}
	return checkResult{}, nil
}

type applyResult int

const (
	upToDate applyResult = iota
	declined
	updated
)

func (u *updater) apply(confirm func() bool, progress func(done int64, total *int64), applying func()) (applyResult, string, error) {
	if u.cleanupStale != nil {
		u.cleanupStale()
	}
	result, err := u.check(false)
	if err != nil {
		return 0, "", err
	}
	if !result.pending {
		return upToDate, "", nil
	}
	state, err := u.readState()
	if err != nil {
		return 0, "", err
	}
	if state.autoUpdate == "confirm" && !confirm() {
		return declined, "", nil
	}
	r, err := u.latest()
	if err != nil {
		return 0, "", err
	}
	a, err := selectAsset(r, u.os, u.arch)
	if err != nil {
		return 0, "", err
	}
	bytes, err := u.download(a.url, progress)
	if err != nil {
		return 0, "", err
	}
	if applying != nil {
		applying()
	}
	if err := u.swap(bytes); err != nil {
		return 0, "", err
	}
	if u.runMigrations != nil {
		if err := u.runMigrations(); err != nil {
			return 0, "", err
		}
	}
	nowMs := u.now()
	state.lastCheck = &nowMs
	state.updateAvailable = false
	state.target = nil
	if err := u.writeState(state); err != nil {
		return 0, "", err
	}
	return updated, result.target, nil
}

// platformTokens are the asset-name tokens Node's process.platform and
// process.arch give: what the release assets have always been named by.
func platformTokens() (string, string) {
	osToken := runtime.GOOS
	if osToken == "windows" {
		osToken = "win32"
	}
	arch := runtime.GOARCH
	switch arch {
	case "amd64":
		arch = "x64"
	case "386":
		arch = "ia32"
	}
	return osToken, arch
}

func buildUpdater() *updater {
	repo := "costaluu/taskthing"
	if r, ok := os.LookupEnv("TASKTHING_REPO"); ok {
		repo = r
	}
	osToken, arch := platformTokens()
	return &updater{
		currentVersion: Version,
		os:             osToken,
		arch:           arch,
		now:            func() float64 { return float64(time.Now().UnixMilli()) },
		readState: func() (updateState, error) {
			c, err := readConfig()
			if err != nil {
				return updateState{}, err
			}
			s := updateState{updateAvailable: boolean(c, "updateAvailable"), autoUpdate: str(c, "autoUpdate"), target: strPtr(c, "updateTarget")}
			if n, ok := c.Value("lastUpdateCheck").(float64); ok {
				s.lastCheck = &n
			}
			return s, nil
		},
		writeState: func(s updateState) error {
			c, err := readConfig()
			if err != nil {
				return err
			}
			var last any
			if s.lastCheck != nil {
				last = *s.lastCheck
			}
			return writeConfig(with(c, "lastUpdateCheck", last, "updateAvailable", s.updateAvailable,
				"updateTarget", nullable(s.target), "autoUpdate", s.autoUpdate))
		},
		latest: func() (release, error) {
			req, _ := http.NewRequest("GET", "https://api.github.com/repos/"+repo+"/releases/latest", nil)
			req.Header.Set("User-Agent", "taskthing")
			req.Header.Set("Accept", "application/vnd.github+json")
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				return release{}, fetchError(err)
			}
			defer res.Body.Close()
			if res.StatusCode < 200 || res.StatusCode > 299 {
				return release{}, fmt.Errorf("GitHub release lookup failed (%d)", res.StatusCode)
			}
			body, err := io.ReadAll(res.Body)
			if err != nil {
				return release{}, err
			}
			v, err := js.Parse(string(body))
			if err != nil {
				return release{}, err
			}
			o, _ := v.(*js.Object)
			if o == nil {
				return release{}, errors.New("undefined is not an object (evaluating 'json.tag_name.replace')")
			}
			tag, ok := o.Value("tag_name").(string)
			if !ok {
				return release{}, errors.New("undefined is not an object (evaluating 'json.tag_name.replace')")
			}
			r := release{version: strings.TrimPrefix(tag, "v")}
			if arr, ok := o.Value("assets").([]any); ok {
				for _, a := range arr {
					if ao, ok := a.(*js.Object); ok {
						r.assets = append(r.assets, asset{str(ao, "name"), str(ao, "browser_download_url")})
					}
				}
			}
			return r, nil
		},
		download: func(url string, progress func(int64, *int64)) ([]byte, error) {
			res, err := http.Get(url)
			if err != nil {
				return nil, fetchError(err)
			}
			defer res.Body.Close()
			if res.StatusCode < 200 || res.StatusCode > 299 {
				return nil, fmt.Errorf("download failed (%d)", res.StatusCode)
			}
			var total *int64
			if res.ContentLength > 0 {
				t := res.ContentLength
				total = &t
			}
			var out []byte
			buf := make([]byte, 64*1024)
			for {
				n, err := res.Body.Read(buf)
				if n > 0 {
					out = append(out, buf[:n]...)
					if progress != nil {
						progress(int64(len(out)), total)
					}
				}
				if err == io.EOF {
					return out, nil
				}
				if err != nil {
					return nil, err
				}
			}
		},
		cleanupStale: func() {
			if runtime.GOOS != "windows" {
				return
			}
			if exe, err := executablePath(); err == nil {
				os.Remove(exe + ".old")
			}
		},
		// Download-then-replace: write beside the running binary, then rename
		// over it. Windows locks a running exe against being overwritten, so it
		// is renamed aside to `.old` first (ADR-0008).
		swap: func(bytes []byte) error {
			target, err := executablePath()
			if err != nil {
				return err
			}
			tmp := target + ".new"
			if err := nodefs.WriteFile(tmp, string(bytes)); err != nil {
				return err
			}
			if err := os.Chmod(tmp, 0o755); err != nil {
				return nodefs.Wrap("chmod", tmp, err)
			}
			if runtime.GOOS == "windows" {
				os.Remove(target + ".old")
				if err := nodefs.Rename(target, target+".old"); err != nil {
					return err
				}
				if err := nodefs.Rename(tmp, target); err != nil {
					return err
				}
				os.Remove(target + ".old")
				return nil
			}
			return nodefs.Rename(tmp, target)
		},
		runMigrations: runAllMigrations,
	}
}

// executablePath is process.execPath: the running binary, symlinks resolved.
func executablePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

// fetchError is how Bun's fetch reports a connection that never happened.
func fetchError(err error) error {
	return fmt.Errorf("Unable to connect. Is the computer able to access the url?")
}
