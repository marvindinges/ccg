package cmd

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

// Version is overridable at build time with -ldflags "-X ...cmd.Version=...".
var Version = "dev"

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("ccg %s (%s/%s, %s)\n", Version, runtime.GOOS, runtime.GOARCH, runtime.Version())
	},
}
