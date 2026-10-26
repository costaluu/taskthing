package mdwal

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/costaluu/taskthing/internal/js"
)

// The golden files under testdata/ were written by the TS engine; this one
// has to reproduce them byte for byte, and read them back to the same state.

const testdata = "../../testdata"

func tree(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(path)
		rel, _ := filepath.Rel(dir, path)
		out[rel] = string(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func sameTree(t *testing.T, got, want map[string]string) {
	t.Helper()
	for path, w := range want {
		if g, ok := got[path]; !ok {
			t.Errorf("missing %s", path)
		} else if g != w {
			t.Errorf("%s differs:\n got: %q\nwant: %q", path, g, w)
		}
	}
	for path := range got {
		if _, ok := want[path]; !ok {
			t.Errorf("unexpected %s", path)
		}
	}
}

func engine(root, author string, start int64) *Mdwal {
	ts := start
	n := 0
	return New(Config{
		Root:    root,
		Schemas: []SchemaEntry{{"task", TaskFields}, {"board", BoardFields}},
		Mode:    func() (string, error) { return "remote", nil },
		Clock:   func() int64 { ts += 1000; return ts },
		Author:  func() string { return author },
		IDs: func() string {
			n++
			return fmt.Sprintf("%s%0*d", author, 21-len(author), n)
		},
		Version: "0.1.0",
	})
}

const tricky = "\"quoted\" back\\slash <b>&amp; ação 🎉 tab\tline para end"

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func TestGoldenWorkspace(t *testing.T) {
	root := t.TempDir()
	alice := engine(root, "alice", 1_784_000_000_000_000_000)

	board := must(alice.CreateEntity("board", js.Obj("name", "work", "icon", "💼", "color", "blue"), nil))
	trickyTask := must(alice.CreateEntity("task", js.Obj("title", tricky, "board", board.Value("id")), nil))
	plain := must(alice.CreateEntity("task", js.Obj("title", "plain"), nil))

	must(0, alice.UpdateField("task", trickyTask.Value("id").(string), "description", "multi\nline \"q\"", nil))
	must(0, alice.UpdateField("task", trickyTask.Value("id").(string), "rrule", "DTSTART:20260722T090000Z\nRRULE:FREQ=WEEKLY;BYDAY=MO", nil))
	must(0, alice.UpdateField("task", plain.Value("id").(string), "completed", true, nil))
	must(0, alice.UpdateField("task", plain.Value("id").(string), "description", nil, nil))

	offline := must(alice.CreateEntity("task", js.Obj("title", "offline"), &LogOption{NoLog: true}))
	must(0, alice.LogExisting("task", offline.Value("id").(string)))

	must(0, alice.CreateFolder("note", nil))
	must(0, alice.RenameFolder("note", "memo", nil))
	must(0, alice.TruncateFolder("memo", nil))
	must(0, alice.DeleteFolder("memo", nil))

	sameTree(t, tree(t, root), tree(t, filepath.Join(testdata, "mdwal/workspace")))

	// ...and what it wrote reads back as entities.
	tasks := must(alice.ReadAll("task"))
	if len(tasks) != 3 {
		t.Fatalf("read %d tasks", len(tasks))
	}
}

func TestGoldenReplay(t *testing.T) {
	dir := filepath.Join(testdata, "mdwal/replay")
	text := must(os.ReadFile(filepath.Join(dir, "events.log")))
	events := must(ParseEvents(string(text)))
	if SerializeLog(events) != string(text) {
		t.Fatal("events.log does not round-trip")
	}
	root := t.TempDir()
	m := engine(root, "alice", 0)
	state := must(m.Replay(NewSnapshot(), events))
	must(0, m.Materialize(state))
	sameTree(t, tree(t, root), tree(t, filepath.Join(dir, "expected")))
}

func TestGoldenConfig(t *testing.T) {
	config := must(ConfigSchema.Parse(js.Obj("currentWorkspace", "main")))
	want := must(os.ReadFile(filepath.Join(testdata, "config/default.md")))
	if got := SerializeEntity(config); got != string(want) {
		t.Errorf("default config:\n got %q\nwant %q", got, want)
	}
	updated := config.Clone()
	updated.Set("lastUpdateCheck", 1_784_000_000_000.0)
	updated.Set("updateAvailable", true)
	updated.Set("updateTarget", "0.2.0")
	want = must(os.ReadFile(filepath.Join(testdata, "config/updated.md")))
	if got := SerializeEntity(updated); got != string(want) {
		t.Errorf("updated config:\n got %q\nwant %q", got, want)
	}
	back := must(ParseFrontmatter(string(want)))
	if SerializeEntity(must(ConfigSchema.Parse(back))) != string(want) {
		t.Error("config does not round-trip")
	}
}
