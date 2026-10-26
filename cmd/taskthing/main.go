// Command taskthing is a git-based task manager.
package main

import (
	"os"

	"github.com/costaluu/taskthing/internal/app"
)

func main() { os.Exit(app.Main(os.Args[1:])) }
