package app

import (
	"errors"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/costaluu/taskthing/internal/js"
	"github.com/costaluu/taskthing/internal/mdwal"
	"github.com/costaluu/taskthing/internal/nodefs"
)

// ── build info (build.ts) ────────────────────────────────────────────────────

// Version is this binary's semver; releases stamp it from the tag via
// -ldflags "-X github.com/costaluu/taskthing/internal/app.Version=<v>".
var Version = "0.1.0"

// Migration is an embedded structural migration: its version and its log.
type Migration struct{ Version, Content string }

// ConfigMigration is a version-keyed rewrite of config.md.
type ConfigMigration struct {
	Version string
	Apply   func(*js.Object) *js.Object
}

// The embedded migration sets, empty until the first one ships.
var (
	Migrations       []Migration
	ConfigMigrations []ConfigMigration
	Transforms       []mdwal.Transform
)

// ── per-peer monotonic clock (ADR-0006) ──────────────────────────────────────

func monotonicClock(now func() int64, seed int64) func() int64 {
	last := seed
	return func() int64 {
		n := now()
		next := last + 1
		if n > last {
			next = n
		}
		last = next
		return next
	}
}

// ── author (ADR-0002) ────────────────────────────────────────────────────────

// resolveGitAuthor is the global git user.name; unset is a hard error.
func resolveGitAuthor() (string, error) {
	out, err := exec.Command("git", "config", "--global", "user.name").Output()
	name := strings.TrimSpace(string(out))
	if err != nil || name == "" {
		return "", errors.New("no git identity: set one with `git config --global user.name \"<your name>\"`")
	}
	return name, nil
}

// ── kv_store (ephemeral number ↔ nanoid) ─────────────────────────────────────

type kvStore struct{ path string }

func globalKvStore(dataHome string) kvStore {
	return kvStore{filepath.Join(dataHome, ".kv", "global.json")}
}

func workspaceKvStore(root, kind string) kvStore {
	return kvStore{filepath.Join(root, ".taskthing", "kv", kind+".json")}
}

func (s kvStore) load() *js.Object {
	text, err := nodefs.ReadFile(s.path)
	if err != nil {
		return js.NewObject()
	}
	v, err := js.Parse(text)
	o, ok := v.(*js.Object)
	if err != nil || !ok {
		return js.NewObject()
	}
	return o
}

func (s kvStore) save(entries *js.Object) error {
	if err := nodefs.MkdirAll(filepath.Dir(s.path)); err != nil {
		return err
	}
	return nodefs.WriteFile(s.path, js.Stringify(entries))
}

// get is the id a number refers to.
func (s kvStore) get(number float64) (string, bool) {
	v, ok := s.load().Get(js.NumberToString(number))
	str, isStr := v.(string)
	return str, ok && isStr
}

// set assigns the next number to an id.
func (s kvStore) set(id string) (int, error) {
	entries := s.load()
	next := entries.Len() + 1
	entries.Set(strconv.Itoa(next), id)
	return next, s.save(entries)
}

func (s kvStore) reset() error { return s.save(js.NewObject()) }

// ── Repository<Model> ────────────────────────────────────────────────────────

// repository is a thin adapter over mdwal for one entity type. Models are the
// materialized objects mdwal reads: fields, LWW companions, timestamps.
type repository struct {
	m          *mdwal.Mdwal
	entityType string
}

func (r repository) create(model *js.Object) (*js.Object, error) {
	return r.m.CreateEntity(r.entityType, model, nil)
}

// findByID: a missing (or unreadable) entity is a miss, not a failure.
func (r repository) findByID(id string) *js.Object {
	e, err := r.m.Read(r.entityType, id)
	if err != nil {
		return nil
	}
	return e
}

func (r repository) findAll() ([]*js.Object, error) { return r.m.ReadAll(r.entityType) }

func (r repository) filter(pred func(*js.Object) bool) ([]*js.Object, error) {
	all, err := r.findAll()
	if err != nil {
		return nil, err
	}
	var out []*js.Object
	for _, e := range all {
		if pred(e) {
			out = append(out, e)
		}
	}
	return out, nil
}

// unchanged compares by content, so a model read and written back logs nothing.
func unchanged(next, stored any) bool {
	isObjectish := func(v any) bool {
		switch v.(type) {
		case nil, *js.Object, []any:
			return true
		}
		return false
	}
	switch a := next.(type) {
	case string:
		b, ok := stored.(string)
		return ok && a == b
	case bool:
		b, ok := stored.(bool)
		return ok && a == b
	case float64:
		b, ok := stored.(float64)
		return ok && a == b
	case int64:
		b, ok := stored.(int64)
		return ok && a == b
	case js.Undefined:
		_, ok := stored.(js.Undefined)
		return ok
	}
	if isObjectish(next) && isObjectish(stored) {
		return js.Equal(next, stored)
	}
	return false
}

// update writes one field event per changed field.
func (r repository) update(model *js.Object) (bool, error) {
	id, _ := model.Value("id").(string)
	current := r.findByID(id)
	if current == nil {
		return false, nil
	}
	for _, field := range model.Keys() {
		if field == "id" {
			continue
		}
		stored, ok := current.Get(field)
		if !ok {
			stored = js.Undefined{}
		}
		if unchanged(model.Value(field), stored) {
			continue
		}
		if err := r.m.UpdateField(r.entityType, id, field, model.Value(field), nil); err != nil {
			return false, err
		}
	}
	return true, nil
}

func (r repository) setDeleted(id string, value bool) (bool, error) {
	if r.findByID(id) == nil {
		return false, nil
	}
	return true, r.m.UpdateField(r.entityType, id, "deleted", value, nil)
}

func taskRepository(m *mdwal.Mdwal) repository  { return repository{m, "task"} }
func boardRepository(m *mdwal.Mdwal) repository { return repository{m, "board"} }

// with is `{ ...model, key: value }`.
func with(model *js.Object, kv ...any) *js.Object {
	out := model.Clone()
	for i := 0; i+1 < len(kv); i += 2 {
		out.Set(kv[i].(string), kv[i+1])
	}
	return out
}

func str(o *js.Object, key string) string   { s, _ := o.Value(key).(string); return s }
func boolean(o *js.Object, key string) bool { b, _ := o.Value(key).(bool); return b }

// strPtr reads a nullable string field.
func strPtr(o *js.Object, key string) *string {
	if s, ok := o.Value(key).(string); ok {
		return &s
	}
	return nil
}

func nullable(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}
