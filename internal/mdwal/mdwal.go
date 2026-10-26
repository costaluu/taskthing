// Package mdwal is the engine every workspace operation goes through: entity
// files as markdown-with-frontmatter, LWW companions per field, and — in a
// remote workspace — one append-only events.log line per operation. The file
// and log formats are byte-for-byte those of the TS implementation.
package mdwal

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/costaluu/taskthing/internal/js"
	"github.com/costaluu/taskthing/internal/nodefs"
)

// ROOT_FOLDER is the folder-name sentinel for the workspace root.
const ROOT_FOLDER = "$root"

// Event is one events.log line.
type Event struct {
	Ts         int64
	Op         string
	EntityType string
	EntityID   string
	Payload    *js.Object
}

// Transform is a data migration applied on read (ADR-0007).
type Transform struct {
	Version string
	Up      func(Event) Event
}

// SchemaEntry binds an entity type to its schema; order is declaration order.
type SchemaEntry struct {
	Type   string
	Schema *Schema
}

type Config struct {
	Root    string
	Schemas []SchemaEntry
	// Mode answers "local" or "remote", live.
	Mode       func() (string, error)
	Clock      func() int64
	Author     func() string
	IDs        func() string
	Version    string // "" is undefined
	Transforms []Transform
}

// LogOption is `{ log?: boolean }`; nil means log.
type LogOption struct{ NoLog bool }

type Mdwal struct {
	cfg  Config
	root string
}

func New(cfg Config) *Mdwal { return &Mdwal{cfg: cfg, root: cfg.Root} }

// Root is the current workspace root (it moves with a $root rename).
func (m *Mdwal) Root() string { return m.root }

func (m *Mdwal) logPath() string          { return filepath.Join(m.root, "events.log") }
func folderName(entityType string) string { return entityType + "s" }
func (m *Mdwal) folder(t string) string   { return filepath.Join(m.root, folderName(t)) }
func (m *Mdwal) file(t, id string) string { return filepath.Join(m.folder(t), id+".md") }

func (m *Mdwal) schemaFor(t string) (*Schema, error) {
	for _, e := range m.cfg.Schemas {
		if e.Type == t {
			return e.Schema, nil
		}
	}
	return nil, fmt.Errorf("no schema for entity type %s", js.Quote(t))
}

func (m *Mdwal) shouldLog(opts *LogOption) (bool, error) {
	if opts != nil && opts.NoLog {
		return false, nil
	}
	mode, err := m.cfg.Mode()
	if err != nil {
		return false, err
	}
	return mode == "remote", nil
}

// ── nanoid (21-char, URL-safe alphabet) ──────────────────────────────────────

const nanoidAlphabet = "useandom-26T198340PX75pxJACKVERYMINDBUSHWOLF_GQZbfghjklqvwyzrict"

func Nanoid() string {
	b := make([]byte, 21)
	_, _ = rand.Read(b)
	id := make([]byte, 21)
	for i := range b {
		id[i] = nanoidAlphabet[b[i]&63]
	}
	return string(id)
}

func (m *Mdwal) newID() string {
	gen := m.cfg.IDs
	if gen == nil {
		gen = Nanoid
	}
	id := gen()
	for id == INBOX {
		id = gen()
	}
	return id
}

// ── frontmatter ──────────────────────────────────────────────────────────────

// isTimestampKey: the nanosecond companions are BigInts, written bare.
func isTimestampKey(k string) bool {
	return strings.HasSuffix(k, "_lastModified") || k == "createdAt"
}

// SerializeEntity renders fields as markdown-with-frontmatter.
func SerializeEntity(fields *js.Object) string {
	var lines []string
	for _, k := range fields.Keys() {
		v := fields.Value(k)
		switch x := v.(type) {
		case int64:
			lines = append(lines, k+": "+strconv.FormatInt(x, 10))
		case js.Undefined:
			lines = append(lines, k+": undefined")
		default:
			lines = append(lines, k+": "+js.Stringify(x))
		}
	}
	return "---\n" + strings.Join(lines, "\n") + "\n---\n"
}

var frontmatterRe = regexp.MustCompile(`^---\n((?s:.*?))\n---`)

// ParseFrontmatter reads markdown-with-frontmatter back into bare fields.
func ParseFrontmatter(text string) (*js.Object, error) {
	out := js.NewObject()
	body := ""
	if m := frontmatterRe.FindStringSubmatch(text); m != nil {
		body = m[1]
	}
	for _, line := range strings.Split(body, "\n") {
		if js.Trim(line) == "" {
			continue
		}
		idx := strings.Index(line, ": ")
		if idx == -1 {
			continue
		}
		key, token := line[:idx], line[idx+2:]
		if isTimestampKey(key) {
			n, err := BigInt(token)
			if err != nil {
				return nil, err
			}
			out.Set(key, n)
			continue
		}
		v, err := js.Parse(token)
		if err != nil {
			return nil, err
		}
		out.Set(key, v)
	}
	return out, nil
}

// BigInt is BigInt(string) for the decimal timestamps the files carry.
func BigInt(token string) (int64, error) {
	t := js.Trim(token)
	if t == "" {
		return 0, nil
	}
	n, ok := new(big.Int).SetString(t, 0)
	if !ok || strings.ContainsAny(t, "_") {
		return 0, errors.New("Failed to parse String to BigInt")
	}
	return n.Int64(), nil
}

// ── log lines: <ts>::<op>::<entityType>::<entityId>::<payload_json> ──────────

func serializeEvent(ev Event) string {
	return strconv.FormatInt(ev.Ts, 10) + "::" + ev.Op + "::" + ev.EntityType + "::" + ev.EntityID + "::" + js.Stringify(ev.Payload)
}

// SerializeLog renders events as log text; the inverse of ParseEvents.
func SerializeLog(events []Event) string {
	if len(events) == 0 {
		return ""
	}
	lines := make([]string, len(events))
	for i, ev := range events {
		lines[i] = serializeEvent(ev)
	}
	return strings.Join(lines, "\n") + "\n"
}

// ParseEvents reads log text.
func ParseEvents(text string) ([]Event, error) {
	var out []Event
	for _, line := range strings.Split(text, "\n") {
		if line == "" {
			continue
		}
		ev, err := parseEvent(line)
		if err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, nil
}

func parseEvent(line string) (Event, error) {
	rest := line
	var header []string
	for k := 0; k < 4; k++ {
		idx := strings.Index(rest, "::")
		if idx == -1 {
			// `rest.slice(0, -1)` then `rest.slice(1)`: a malformed header still
			// parses, the way the TS did.
			if len(rest) > 0 {
				header = append(header, rest[:len(rest)-1])
				rest = rest[1:]
			} else {
				header = append(header, "")
			}
			continue
		}
		header = append(header, rest[:idx])
		rest = rest[idx+2:]
	}
	ts, err := BigInt(header[0])
	if err != nil {
		return Event{}, err
	}
	payload, err := js.Parse(rest)
	if err != nil {
		return Event{}, err
	}
	obj, _ := payload.(*js.Object)
	if obj == nil {
		obj = js.NewObject()
	}
	return Event{Ts: ts, Op: header[1], EntityType: header[2], EntityID: header[3], Payload: obj}, nil
}

func (m *Mdwal) version() any {
	if m.cfg.Version == "" {
		return js.Undefined{}
	}
	return m.cfg.Version
}

func (m *Mdwal) appendEvent(ev Event) error {
	f, err := os.OpenFile(m.logPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nodefs.Wrap("open", m.logPath(), err)
	}
	defer f.Close()
	_, err = f.WriteString(serializeEvent(ev) + "\n")
	return err
}

// ── semver (migrate-on-read) ─────────────────────────────────────────────────

// CompareSemver orders two binary versions (NaN parts compare as NaN, as in TS).
func CompareSemver(a, b string) float64 {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	part := func(p []string, i int) float64 {
		if i >= len(p) {
			return 0
		}
		return js.ToNumber(p[i])
	}
	for i := 0; i < 3; i++ {
		d := part(pa, i) - part(pb, i)
		if d != 0 || math.IsNaN(d) {
			return d
		}
	}
	return 0
}

func (m *Mdwal) migrateEvent(ev Event) (Event, error) {
	v, ok := ev.Payload.Value("version").(string)
	if !ok {
		return ev, nil
	}
	if m.cfg.Version != "" && CompareSemver(v, m.cfg.Version) > 0 {
		return ev, fmt.Errorf("event authored by a newer version %s; update taskthing to >= %s", v, v)
	}
	var chain []Transform
	for _, t := range m.cfg.Transforms {
		if CompareSemver(t.Version, v) > 0 {
			chain = append(chain, t)
		}
	}
	for i := 1; i < len(chain); i++ {
		for j := i; j > 0 && CompareSemver(chain[j-1].Version, chain[j].Version) > 0; j-- {
			chain[j-1], chain[j] = chain[j], chain[j-1]
		}
	}
	for _, t := range chain {
		ev = t.Up(ev)
	}
	return ev, nil
}

// ── replay / LWW ─────────────────────────────────────────────────────────────

// Snapshot is entity key (`type/id`) → its stored fields, in insertion order.
type Snapshot struct {
	Keys     []string
	Entities map[string]*js.Object
}

func NewSnapshot() *Snapshot { return &Snapshot{Entities: map[string]*js.Object{}} }

func (s *Snapshot) entity(key string) *js.Object {
	e, ok := s.Entities[key]
	if !ok {
		e = js.NewObject()
		s.Entities[key] = e
		s.Keys = append(s.Keys, key)
	}
	return e
}

// applyField: a value wins only if strictly newer; a tie keeps the incumbent.
func applyField(entity *js.Object, field string, value any, ts int64, author any) {
	if current, ok := entity.Value(field + "_lastModified").(int64); ok && ts <= current {
		return
	}
	entity.Set(field, value)
	entity.Set(field+"_lastModified", ts)
	entity.Set(field+"_lastModifiedBy", author)
}

// Replay folds events over base under per-field LWW, without logging.
func (m *Mdwal) Replay(base *Snapshot, events []Event) (*Snapshot, error) {
	state := NewSnapshot()
	for _, k := range base.Keys {
		state.entity(k).Assign(base.Entities[k])
	}
	for _, raw := range events {
		ev, err := m.migrateEvent(raw)
		if err != nil {
			return nil, err
		}
		entity := state.entity(ev.EntityType + "/" + ev.EntityID)
		author := ev.Payload.Value("author")
		if _, ok := ev.Payload.Get("author"); !ok {
			author = js.Undefined{}
		}
		if ev.Op == "CREATE" {
			snapshot, _ := ev.Payload.Value("snapshot").(*js.Object)
			if snapshot != nil {
				for _, f := range snapshot.Keys() {
					applyField(entity, f, snapshot.Value(f), ev.Ts, author)
				}
			}
			continue
		}
		field, ok := ev.Payload.Value("field").(string)
		if !ok {
			field = "undefined"
		}
		value, has := ev.Payload.Get("value")
		if !has {
			value = js.Undefined{}
		}
		applyField(entity, field, value, ev.Ts, author)
	}
	return state, nil
}

// ── engine operations ────────────────────────────────────────────────────────

func (m *Mdwal) readFile(path string) (string, error) { return nodefs.ReadFile(path) }
func (m *Mdwal) writeFile(path, content string) error { return nodefs.WriteFile(path, content) }
func (m *Mdwal) mkdir(dir string) error               { return nodefs.MkdirAll(dir) }

func (m *Mdwal) CreateEntity(t string, snapshot *js.Object, opts *LogOption) (*js.Object, error) {
	schema, err := m.schemaFor(t)
	if err != nil {
		return nil, err
	}
	fields, err := schema.Parse(snapshot)
	if err != nil {
		return nil, err
	}
	id := m.newID()
	ts := m.cfg.Clock()
	author := m.cfg.Author()

	file := js.Obj("createdAt", ts)
	for _, f := range fields.Keys() {
		file.Set(f, fields.Value(f))
		file.Set(f+"_lastModified", ts)
		file.Set(f+"_lastModifiedBy", author)
	}
	if err := m.mkdir(m.folder(t)); err != nil {
		return nil, err
	}
	if err := m.writeFile(m.file(t, id), SerializeEntity(file)); err != nil {
		return nil, err
	}
	log, err := m.shouldLog(opts)
	if err != nil {
		return nil, err
	}
	if log {
		payload := js.Obj("author", author, "snapshot", fields, "version", m.version())
		if err := m.appendEvent(Event{Ts: ts, Op: "CREATE", EntityType: t, EntityID: id, Payload: payload}); err != nil {
			return nil, err
		}
	}
	return js.Obj("id", id).Assign(fields), nil
}

func (m *Mdwal) UpdateField(t, id, field string, value any, opts *LogOption) error {
	ts := m.cfg.Clock()
	author := m.cfg.Author()
	path := m.file(t, id)
	text, err := m.readFile(path)
	if err != nil {
		return err
	}
	raw, err := ParseFrontmatter(text)
	if err != nil {
		return err
	}
	raw.Set(field, value)
	raw.Set(field+"_lastModified", ts)
	raw.Set(field+"_lastModifiedBy", author)
	if err := m.writeFile(path, SerializeEntity(raw)); err != nil {
		return err
	}
	log, err := m.shouldLog(opts)
	if err != nil || !log {
		return err
	}
	payload := js.Obj("author", author, "field", field, "value", value, "version", m.version())
	return m.appendEvent(Event{Ts: ts, Op: "UPDATE_FIELD", EntityType: t, EntityID: id, Payload: payload})
}

func (m *Mdwal) LogExisting(t, id string) error {
	schema, err := m.schemaFor(t)
	if err != nil {
		return err
	}
	text, err := m.readFile(m.file(t, id))
	if err != nil {
		return err
	}
	raw, err := ParseFrontmatter(text)
	if err != nil {
		return err
	}
	snapshot, err := schema.Parse(raw)
	if err != nil {
		return err
	}
	log, err := m.shouldLog(nil)
	if err != nil || !log {
		return err
	}
	ts := m.cfg.Clock()
	payload := js.Obj("author", m.cfg.Author(), "snapshot", snapshot, "version", m.version())
	return m.appendEvent(Event{Ts: ts, Op: "CREATE", EntityType: t, EntityID: id, Payload: payload})
}

func (m *Mdwal) Discard(t, id string) error {
	err := os.Remove(m.file(t, id))
	if err != nil && !os.IsNotExist(err) {
		return nodefs.Wrap("rm", m.file(t, id), err)
	}
	return nil
}

func (m *Mdwal) folderEvent(op, t string, ts int64, payload *js.Object, opts *LogOption) error {
	log, err := m.shouldLog(opts)
	if err != nil || !log {
		return err
	}
	return m.appendEvent(Event{Ts: ts, Op: op, EntityType: t, EntityID: "", Payload: payload})
}

func (m *Mdwal) CreateFolder(t string, opts *LogOption) error {
	ts := m.cfg.Clock()
	author := m.cfg.Author()
	if err := m.mkdir(m.folder(t)); err != nil {
		return err
	}
	return m.folderEvent("CREATE_FOLDER", t, ts, js.Obj("author", author, "version", m.version()), opts)
}

func (m *Mdwal) RenameFolder(t, to string, opts *LogOption) error {
	ts := m.cfg.Clock()
	author := m.cfg.Author()
	if t == ROOT_FOLDER {
		moved := filepath.Join(filepath.Dir(m.root), to)
		if err := os.Rename(m.root, moved); err != nil {
			return nodefs.WrapRename(m.root, moved, err)
		}
		m.root = moved
	} else if err := os.Rename(m.folder(t), m.folder(to)); err != nil {
		return nodefs.WrapRename(m.folder(t), m.folder(to), err)
	}
	return m.folderEvent("RENAME_FOLDER", t, ts, js.Obj("author", author, "to", to, "version", m.version()), opts)
}

func (m *Mdwal) TruncateFolder(t string, opts *LogOption) error {
	ts := m.cfg.Clock()
	author := m.cfg.Author()
	if err := os.RemoveAll(m.folder(t)); err != nil {
		return nodefs.Wrap("rm", m.folder(t), err)
	}
	if err := m.mkdir(m.folder(t)); err != nil {
		return err
	}
	return m.folderEvent("TRUNCATE_FOLDER", t, ts, js.Obj("author", author, "version", m.version()), opts)
}

func (m *Mdwal) DeleteFolder(t string, opts *LogOption) error {
	ts := m.cfg.Clock()
	author := m.cfg.Author()
	if err := os.RemoveAll(m.folder(t)); err != nil {
		return nodefs.Wrap("rm", m.folder(t), err)
	}
	return m.folderEvent("DELETE_FOLDER", t, ts, js.Obj("author", author, "version", m.version()), opts)
}

// EntityTypes are the declared types, in order.
func (m *Mdwal) EntityTypes() []string {
	var out []string
	for _, e := range m.cfg.Schemas {
		out = append(out, e.Type)
	}
	return out
}

// Folders are the fixed entity folders derived from the schemas.
func (m *Mdwal) Folders() []string {
	var out []string
	for _, t := range m.EntityTypes() {
		out = append(out, folderName(t))
	}
	return out
}

func (m *Mdwal) Version() string { return m.cfg.Version }

// idsIn lists an entity folder in directory order, as readdir does.
func (m *Mdwal) idsIn(t string) []string {
	names, err := nodefs.Readdir(m.folder(t))
	if err != nil {
		return nil
	}
	var ids []string
	for _, n := range names {
		if strings.HasSuffix(n, ".md") {
			ids = append(ids, strings.TrimSuffix(n, ".md"))
		}
	}
	return ids
}

// Snapshot is the whole workspace's stored state, as replay folds over it.
func (m *Mdwal) Snapshot() (*Snapshot, error) {
	state := NewSnapshot()
	for _, t := range m.EntityTypes() {
		for _, id := range m.idsIn(t) {
			text, err := m.readFile(m.file(t, id))
			if err != nil {
				return nil, err
			}
			raw, err := ParseFrontmatter(text)
			if err != nil {
				return nil, err
			}
			state.entity(t + "/" + id).Assign(raw)
		}
	}
	return state, nil
}

// Materialize writes a replayed snapshot to the derived files, logging nothing.
func (m *Mdwal) Materialize(state *Snapshot) error {
	for _, key := range state.Keys {
		t, id, _ := strings.Cut(key, "/")
		if err := m.mkdir(m.folder(t)); err != nil {
			return err
		}
		if err := m.writeFile(m.file(t, id), SerializeEntity(state.Entities[key])); err != nil {
			return err
		}
	}
	return nil
}

// Read materializes one entity: schema fields, LWW companions, createdAt and
// the derived updatedAt.
func (m *Mdwal) Read(t, id string) (*js.Object, error) {
	schema, err := m.schemaFor(t)
	if err != nil {
		return nil, err
	}
	text, err := m.readFile(m.file(t, id))
	if err != nil {
		return nil, err
	}
	raw, err := ParseFrontmatter(text)
	if err != nil {
		return nil, err
	}
	fields, err := schema.Parse(raw)
	if err != nil {
		return nil, err
	}
	out := js.Obj("id", id).Assign(fields)
	var updatedAt any = js.Undefined{}
	for _, k := range raw.Keys() {
		v := raw.Value(k)
		if strings.HasSuffix(k, "_lastModified") {
			out.Set(k, v)
			if ts, ok := v.(int64); ok {
				if cur, set := updatedAt.(int64); !set || ts > cur {
					updatedAt = ts
				}
			}
		} else if strings.HasSuffix(k, "_lastModifiedBy") {
			out.Set(k, v)
		}
	}
	createdAt, ok := raw.Get("createdAt")
	if !ok {
		createdAt = js.Undefined{}
	}
	out.Set("createdAt", createdAt)
	out.Set("updatedAt", updatedAt)
	return out, nil
}

// ReadAll materializes every entity of a type (none if its folder is absent).
func (m *Mdwal) ReadAll(t string) ([]*js.Object, error) {
	var out []*js.Object
	for _, id := range m.idsIn(t) {
		e, err := m.Read(t, id)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

// ParseLog reads this workspace's log; an unreadable one reads as empty.
func (m *Mdwal) ParseLog() []Event {
	b, err := os.ReadFile(m.logPath())
	if err != nil {
		return nil
	}
	events, err := ParseEvents(string(b))
	if err != nil {
		return nil
	}
	return events
}

// Purge drops this peer's events with ts <= threshold.
func (m *Mdwal) Purge(threshold int64) error {
	var kept []Event
	for _, ev := range m.ParseLog() {
		if ev.Ts > threshold {
			kept = append(kept, ev)
		}
	}
	return m.writeFile(m.logPath(), SerializeLog(kept))
}
