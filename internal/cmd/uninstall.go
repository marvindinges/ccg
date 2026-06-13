package cmd

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
)

var uninstallYes bool

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove ccg: the binary, source checkout, and global config (keeps Go)",
	Long: `Uninstall ccg by running the uninstall script from the source checkout.

It removes the ccg binary, the source checkout (default ~/.local/share/ccg), and
the global config (~/.config/ccg). It does NOT remove the Go toolchain and does
NOT touch your shell rc files. You're asked for one confirmation first (skip it
with -y).

Requires the source checkout from the install script. If ccg was installed some
other way, run the uninstaller directly:
  curl -fsSL https://raw.githubusercontent.com/marvindinges/ccg/main/uninstall.sh | sh`,
	Args: cobra.NoArgs,
	RunE: runUninstall,
}

func init() {
	uninstallCmd.Flags().BoolVarP(&uninstallYes, "yes", "y", false, "skip the confirmation prompt")
	rootCmd.AddCommand(uninstallCmd)
}

func runUninstall(cmd *cobra.Command, args []string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot locate the running ccg binary: %w", err)
	}
	installDir := filepath.Dir(exe)
	srcDir := sourceDir()
	script := filepath.Join(srcDir, "uninstall.sh")

	data, err := os.ReadFile(script)
	if err != nil {
		return fmt.Errorf(
			"no uninstaller found at %s.\nRun it directly instead:\n  curl -fsSL https://raw.githubusercontent.com/marvindinges/ccg/main/uninstall.sh | sh",
			script)
	}

	// Feed the script to sh via stdin so it's held in memory: the script deletes
	// its own source directory, so reading it from disk mid-run would be fragile.
	// Its confirmation prompt reads /dev/tty, so interactivity still works.
	env := append(os.Environ(),
		"CCG_INSTALL_DIR="+installDir,
		"CCG_SRC_DIR="+srcDir,
	)
	if uninstallYes {
		env = append(env, "CCG_ASSUME_YES=1")
	}

	c := exec.Command("sh", "-s")
	c.Env = env
	c.Stdin = bytes.NewReader(data)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("uninstall failed: %w", err)
	}
	return nil
}
