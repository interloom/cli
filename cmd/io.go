package cmd

import (
	"encoding/json"
	"os"

	"github.com/interloom/cli/internal/output"
	"golang.org/x/term"
)

// printResult writes a successful JSON result to stdout.
func printResult(raw json.RawMessage) error {
	return output.JSON(os.Stdout, raw)
}

func stdinIsTTY() bool { return term.IsTerminal(int(os.Stdin.Fd())) }
