package main

import (
	"os"
	"path/filepath"
	"testing"
)

// The emoji picker only opens on a terminal, so these drive the real binary
// through zpty, like the completion-offer test does. `zpty -w` with an empty
// word sends nothing, so Enter is spelled out as a carriage return.

func pickerHome(t *testing.T) (string, []string) {
	t.Helper()
	home := dataHome(t)
	mustCode(t, taskthing(t, home, "board add", "seed"), 0)
	// the one-time completion offer would otherwise be the first thing asked
	os.WriteFile(filepath.Join(home, ".completion-offered"), nil, 0o644)
	dir, env := zshEnv(t, home)
	// zshEnv swaps HOME for an empty directory, which has no git identity
	cfg := filepath.Join(dir, "gitconfig")
	os.WriteFile(cfg, []byte("[user]\n\tname = testrunner\n\temail = t@example.invalid\n"), 0o644)
	return home, append(env, "GIT_CONFIG_GLOBAL="+cfg)
}

// iconOf reads a board's icon back through the engine.
func iconOf(t *testing.T, home, name string) string {
	t.Helper()
	for _, b := range readAll(t, "board", home) {
		if get(b, "name") == any(name) {
			s, _ := get(b, "icon").(string)
			return s
		}
	}
	t.Fatalf("no board named %q", name)
	return ""
}

func TestBoardAddOpensThePickerOnATerminal(t *testing.T) {
	home, env := pickerHome(t)
	out := zsh(t, env, `zpty z taskthing board add Groceries
zpty -r z out '*Search an emoji*'
zpty -w -n z 'shopping cart'
zpty -r z out '*> shopping cart*'
zpty -w -n z $'\r'
zpty -r z out '*created*'
print -r -- "$out"`)
	contains(t, out, "created")
	equal(t, iconOf(t, home, "Groceries"), "🛒")
}

func TestPickerEscapeCreatesTheBoardWithoutAnIcon(t *testing.T) {
	home, env := pickerHome(t)
	out := zsh(t, env, `zpty z taskthing board add Plain
zpty -r z out '*Search an emoji*'
zpty -w -n z $'\x1b'
zpty -r z out '*created*'
print -r -- "$out"`)
	contains(t, out, "icon: skipped")
	equal(t, iconOf(t, home, "Plain"), "")
}

func TestIconFlagSkipsThePicker(t *testing.T) {
	home, env := pickerHome(t)
	out := zsh(t, env, `zpty z taskthing board add Direct --icon 🎮
while zpty -r z chunk; do out+=$chunk; done
print -r -- "$out"`)
	lacks(t, out, "Search an emoji")
	contains(t, out, "created")
	equal(t, iconOf(t, home, "Direct"), "🎮")
}

func TestBoardSetIconOpensThePickerAndEscapeChangesNothing(t *testing.T) {
	home, env := pickerHome(t)
	mustCode(t, taskthing(t, home, "board add", "Trips", "--icon", "🎮"), 0)
	out := zsh(t, env, `zpty z taskthing board set icon Trips
zpty -r z out '*Search an emoji*'
zpty -w -n z $'\x1b'
while zpty -r z chunk; do out+=$chunk; done
print -r -- "$out"`)
	contains(t, out, "icon unchanged")
	equal(t, iconOf(t, home, "Trips"), "🎮") // untouched

	out = zsh(t, env, `zpty z taskthing board set icon Trips
zpty -r z out '*Search an emoji*'
zpty -w -n z 'airplane'
zpty -r z out '*> airplane*'
zpty -w -n z $'\r'
zpty -r z out '*updated*'
print -r -- "$out"`)
	contains(t, out, "icon updated")
	equal(t, iconOf(t, home, "Trips"), "✈️")
}

func TestBoardSetIconEnterOnNothingClearsIt(t *testing.T) {
	home, env := pickerHome(t)
	mustCode(t, taskthing(t, home, "board add", "Trips", "--icon", "🎮"), 0)
	out := zsh(t, env, `zpty z taskthing board set icon Trips
zpty -r z out '*Search an emoji*'
zpty -w -n z $'\r'
zpty -r z out '*updated*'
print -r -- "$out"`)
	contains(t, out, "icon: none")
	equal(t, iconOf(t, home, "Trips"), "")
}

// Off a terminal nothing asks: no value still clears, as it always did.
func TestBoardSetIconWithoutATerminalStillClears(t *testing.T) {
	home := dataHome(t)
	mustCode(t, taskthing(t, home, "board add", "Trips", "--icon", "🎮"), 0)
	r := taskthing(t, home, "board set icon", "Trips")
	mustCode(t, r, 0)
	lacks(t, r.stdout, "Search an emoji")
	equal(t, iconOf(t, home, "Trips"), "")
}
