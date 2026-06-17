package cmd

import (
	"fmt"
	"runtime/debug"

	"github.com/spf13/cobra"
)

// version is injected at build time via
//
//	-ldflags "-X github.com/interloom/cli/cmd.version=<v>"
//
// (GoReleaser sets this from the git tag). When unset it falls back to the
// module version embedded by `go install <module>@<version>`.
var version = "dev"

func cliVersion() string {
	if version != "dev" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		if v := bi.Main.Version; v != "" && v != "(devel)" {
			return v
		}
	}
	return version
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the CLI version",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return printResult([]byte(fmt.Sprintf(`{"version":%q}`, cliVersion())))
		},
	}
}
