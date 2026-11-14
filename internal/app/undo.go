package app

import (
	"errors"
	"path/filepath"
	"strings"

	"github.com/costaluu/taskthing/internal/js"
	"github.com/costaluu/taskthing/internal/mdwal"
	"github.com/costaluu/taskthing/internal/nodefs"
)

// ── undo ─────────────────────────────────────────────────────────────────────
//
// Every command that edits tasks or boards leaves one entry in its workspace's
// undo journal: what it reported, and each field it wrote with the value
// before and after. `undo` takes the newest entry back by writing the old
// values as new edits — in a remote workspace those are ordinary events, so
// the log stays append-only and peers converge on the undone state.
//
// The journal is local and derived (.taskthing/, gitignored): it works the
// same in a local workspace, which has no events.log to read back.

// undoKept bounds the journal: how many commands can be walked back.
const undoKept = 100

func undoJournal(root string) string { return filepath.Join(root, ".taskthing", "undo.json") }

// pending is what this process changed so far, per workspace root, and what
// it reported doing.
var (
	pendingRoots   []string
	pendingChanges = map[string][]any{}
	pendingSummary []any
)

func recordChange(root string, change *js.Object) {
	if _, ok := pendingChanges[root]; !ok {
		pendingRoots = append(pendingRoots, root)
	}
	pendingChanges[root] = append(pendingChanges[root], change)
}

func recordFieldChange(root, entityType, id, field string, from, to any) {
	recordChange(root, js.Obj("type", entityType, "id", id, "field", field, "from", from, "to", to))
}

func recordCreation(root, entityType, id string) {
	recordChange(root, js.Obj("type", entityType, "id", id, "created", true))
}

// recordOutcome keeps an outcome line as the undo entry's summary.
func recordOutcome(segments []segment) {
	var b strings.Builder
	for _, s := range segments {
		b.WriteString(s.text)
	}
	pendingSummary = append(pendingSummary, b.String())
}

func readUndoJournal(root string) []any {
	text, err := nodefs.ReadFile(undoJournal(root))
	if err != nil {
		return nil
	}
	parsed, _ := js.Parse(text)
	entries, _ := parsed.([]any)
	return entries
}

func writeUndoJournal(root string, entries []any) error {
	if entries == nil {
		entries = []any{}
	}
	if err := nodefs.MkdirAll(filepath.Dir(undoJournal(root))); err != nil {
		return err
	}
	return nodefs.WriteFile(undoJournal(root), js.Stringify(entries))
}

// flushUndo appends what this command changed to each workspace's journal.
func flushUndo() {
	for _, root := range pendingRoots {
		entries := append(readUndoJournal(root), js.Obj("summary", pendingSummary, "changes", pendingChanges[root]))
		if len(entries) > undoKept {
			entries = entries[len(entries)-undoKept:]
		}
		// ponytail: a journal that can't be written costs the undo, not the command.
		_ = writeUndoJournal(root, entries)
	}
}

// revert takes one recorded change back, unless the entity moved on since:
// a field someone edited after the command keeps its newer value.
func revert(m *mdwal.Mdwal, change *js.Object) bool {
	entityType, id := str(change, "type"), str(change, "id")
	raw, err := m.Read(entityType, id)
	if err != nil {
		return false
	}
	field, from, to := str(change, "field"), change.Value("from"), change.Value("to")
	if boolean(change, "created") {
		field, from, to = "deleted", true, false
	}
	if !unchanged(to, raw.Value(field)) {
		return false
	}
	return m.UpdateField(entityType, id, field, from, nil) == nil
}

func undoCommand() *Command {
	return &Command{Name: "undo", Description: "take back the last change to tasks or boards", Options: []Option{workspaceOption()},
		Handler: func(c *Ctx) error {
			return report(func(err error) string { return actionFailure("undo", err) }, func(*js.Object) ([]segment, error) {
				root, err := targetWorkspace(workspaceFlag(c))
				if err != nil {
					return nil, err
				}
				entries := readUndoJournal(root)
				if len(entries) == 0 {
					return nil, errors.New("nothing to undo")
				}
				entry, _ := entries[len(entries)-1].(*js.Object)
				if entry == nil {
					entry = js.NewObject()
				}
				m, _, err := openWorkspace(root)
				if err != nil {
					return nil, err
				}
				if err := writeUndoJournal(root, entries[:len(entries)-1]); err != nil {
					return nil, err
				}
				changes, _ := entry.Value("changes").([]any)
				reverted := 0
				for i := len(changes) - 1; i >= 0; i-- {
					if change, ok := changes[i].(*js.Object); ok && revert(m, change) {
						reverted++
					}
				}
				var summary []string
				lines, _ := entry.Value("summary").([]any)
				for _, l := range lines {
					if s, ok := l.(string); ok {
						summary = append(summary, s)
					}
				}
				what := strings.Join(summary, "; ")
				if what == "" {
					what = "the last change"
				}
				if reverted == 0 {
					return nil, errors.New("nothing left to take back from: " + what + " — it was changed again since")
				}
				return []segment{{"undone: ", false}, {what, true}}, nil
			})
		}}
}
