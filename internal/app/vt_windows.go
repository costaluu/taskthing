package app

import (
	"os"

	"golang.org/x/sys/windows"
)

// The console only understands the ANSI sequences every screen draws with
// once virtual-terminal processing is on — Bun turned it on for the TS binary.
func init() {
	for _, f := range []*os.File{os.Stdout, os.Stderr} {
		h := windows.Handle(f.Fd())
		var mode uint32
		if windows.GetConsoleMode(h, &mode) == nil {
			windows.SetConsoleMode(h, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING|windows.ENABLE_PROCESSED_OUTPUT)
		}
	}
}
