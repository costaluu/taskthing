package app

// Main is the entry: pick how output is drawn, then hand argv to the tree.
func Main(argv []string) int {
	if stdoutTTY {
		present = inkPresenter{}
	}
	// `help` is a verb users already type; Bunli spells it as a flag.
	if len(argv) > 0 && argv[0] == "help" {
		argv = append([]string{"--help"}, argv[1:]...)
	}
	return BuildCLI().Run(argv)
}
