package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
)

var upgradeCmd = &cobra.Command{
	Use:   "upgrade [ref]",
	Short: "Update ccg to the latest version (rebuilds from the source checkout)",
	Long: `Upgrade ccg by pulling the latest source and rebuilding.

It re-runs the installer against the local source checkout created at install
time (default ~/.local/share/ccg/src), reinstalling the binary next to the
current one. Optionally pass a git ref (branch, tag, or commit) to build, e.g.
"ccg upgrade v0.2.0".

Requires the source checkout from the install script. If ccg was installed some
other way, re-run the installer instead:
  curl -fsSL https://raw.githubusercontent.com/marvindinges/ccg/main/install.sh | sh`,
	Args: cobra.MaximumNArgs(1),
	RunE: runUpgrade,
}

func init() {
	rootCmd.AddCommand(upgradeCmd)
}

func runUpgrade(cmd *cobra.Command, args []string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot locate the running ccg binary: %w", err)
	}
	installDir := filepath.Dir(exe)
	srcDir := sourceDir()
	script := filepath.Join(srcDir, "install.sh")

	if _, err := os.Stat(script); err != nil {
		return fmt.Errorf(
			"no source checkout found at %s.\nReinstall to enable upgrades:\n  curl -fsSL https://raw.githubusercontent.com/marvindinges/ccg/main/install.sh | sh",
			srcDir)
	}

	fmt.Printf("Upgrading ccg (current: %s)…\n", Version)

	// Re-run the installer non-interactively: rebuild only, leave PATH/config alone.
	// Force CGO off so the build never needs a C compiler (ccg is pure Go).
	env := append(os.Environ(),
		"CCG_ASSUME_YES=1",
		"CCG_SKIP_PATH=1",
		"CCG_SKIP_CONFIG=1",
		"CCG_INSTALL_DIR="+installDir,
		"CCG_SRC_DIR="+srcDir,
		"CGO_ENABLED=0",
	)
	if len(args) == 1 && args[0] != "" {
		env = append(env, "CCG_REF="+args[0])
	}

	c := exec.Command("sh", script)
	c.Env = env
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	c.Stdin = os.Stdin
	if err := c.Run(); err != nil {
		return fmt.Errorf("upgrade failed: %w", err)
	}
	return nil
}

// sourceDir resolves the persistent source checkout location, matching the
// install script's defaults (honours $CCG_SRC_DIR and $XDG_DATA_HOME).
func sourceDir() string {
	if d := os.Getenv("CCG_SRC_DIR"); d != "" {
		return d
	}
	data := os.Getenv("XDG_DATA_HOME")
	if data == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "."
		}
		data = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(data, "ccg", "src")
}
