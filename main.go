// Command interloom is the entry point for the Interloom CLI.
package main

import (
	"os"

	"github.com/interloom/cli/cmd"
)

func main() {
	os.Exit(cmd.Execute())
}
