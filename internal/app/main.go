package app

import (
	"slices"
	"strings"
)

// Main is the entry: pick how output is drawn, then hand argv to the tree.
func Main(argv []string) int {
	// The shells' completion hook: read-only, never draws, never fails.
	if len(argv) > 0 && argv[0] == "__complete" {
		printCandidates(BuildCLI().complete(argv[1:]))
		return 0
	}
	// Asked once, before an everyday command, never before help or setup.
	if len(argv) > 0 && !strings.HasPrefix(argv[0], "-") && !slices.Contains([]string{"help", "install", "completion"}, argv[0]) {
		offerCompletion()
	}
	if stdoutTTY {
		present = inkPresenter{}
	}
	// `help` is a verb users already type; Bunli spells it as a flag.
	if len(argv) > 0 && argv[0] == "help" {
		argv = append([]string{"--help"}, argv[1:]...)
	}
	return BuildCLI().Run(argv)
}
