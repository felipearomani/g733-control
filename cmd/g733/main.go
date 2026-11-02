// Command g733 controls a Logitech G733 headset without G HUB.
//
// With no arguments it opens the interactive interface; with a subcommand it
// runs once and exits (see `g733 help`).
package main

import (
	"fmt"
	"os"

	"github.com/felipearomani/g733-control/internal/cli"
	"github.com/felipearomani/g733-control/internal/i18n"
	"github.com/felipearomani/g733-control/internal/tui"
)

var version = "dev"

func main() {
	args := i18n.Init(os.Args[1:]) // consumes --lang, else detects

	if len(args) == 0 || args[0] == "tui" {
		if err := tui.Run(); err != nil {
			fmt.Fprintln(os.Stderr, "g733:", err)
			os.Exit(1)
		}
		return
	}
	os.Exit(cli.Run(args, version))
}
