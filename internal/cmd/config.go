package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/marvindinges/ccg/internal/config"
	"github.com/marvindinges/ccg/internal/git"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Show the resolved configuration and where each value came from",
	RunE:  runConfig,
}

var configPathCmd = &cobra.Command{
	Use:   "path",
	Short: "Print the global and project config file paths",
	RunE:  runConfigPath,
}

func init() {
	configCmd.AddCommand(configPathCmd)
}

// projectRoot returns the repo top-level, or "" if not inside a repo.
func projectRoot() string {
	if g, err := git.New(); err == nil {
		if root, err := g.Root(); err == nil {
			return root
		}
	}
	return ""
}

func runConfig(cmd *cobra.Command, args []string) error {
	root := projectRoot()
	cfg, err := config.Load(root)
	if err != nil {
		return err
	}

	fmt.Println("Resolved configuration (value [source]):")
	fmt.Printf("  provider.base_url     = %s [%s]\n", orNone(cfg.Provider.BaseURL), cfg.Source("provider.base_url"))
	fmt.Printf("  provider.model        = %s [%s]\n", orNone(cfg.Provider.Model), cfg.Source("provider.model"))
	fmt.Printf("  provider.api_key_env  = %s [%s]\n", orNone(cfg.Provider.APIKeyEnv), cfg.Source("provider.api_key_env"))
	fmt.Printf("  provider.strict_schema= %t [%s]\n", cfg.Provider.StrictSchema, cfg.Source("provider.strict_schema"))
	fmt.Printf("  commit.max_header_len = %d [%s]\n", cfg.MaxHeaderLen(), cfg.Source("commit.max_header_len"))
	fmt.Printf("  countdown_seconds     = %d [%s]\n", cfg.CountdownSeconds(), cfg.Source("countdown_seconds"))
	fmt.Printf("  colors.primary        = %s [%s]\n", cfg.PrimaryColor(), cfg.Source("colors.primary"))
	fmt.Printf("  colors.secondary      = %s [%s]\n", cfg.SecondaryColor(), cfg.Source("colors.secondary"))

	keyState := "not set"
	if cfg.Provider.APIKeyEnv != "" {
		if cfg.APIKey() != "" {
			keyState = fmt.Sprintf("$%s is set", cfg.Provider.APIKeyEnv)
		} else {
			keyState = fmt.Sprintf("$%s is empty", cfg.Provider.APIKeyEnv)
		}
	}
	fmt.Printf("  api key               = %s\n", keyState)

	fmt.Printf("\nAI generation: %s\n", aiState(cfg))

	var names []string
	for _, t := range cfg.AllowedTypes() {
		names = append(names, t.Name)
	}
	fmt.Printf("Allowed commit types: %s\n", strings.Join(names, ", "))
	return nil
}

func aiState(cfg config.Config) string {
	if cfg.HasProvider() {
		return "enabled"
	}
	return "disabled (no provider configured — manual mode)"
}

func runConfigPath(cmd *cobra.Command, args []string) error {
	global, err := config.GlobalConfigPath()
	if err != nil {
		return err
	}
	fmt.Printf("global:  %s%s\n", global, existsTag(global))

	root := projectRoot()
	if root == "" {
		fmt.Println("project: (not inside a git repository)")
		return nil
	}
	proj := filepath.Join(root, config.ProjectFileName)
	fmt.Printf("project: %s%s\n", proj, existsTag(proj))
	return nil
}

func existsTag(path string) string {
	if _, err := os.Stat(path); err == nil {
		return "  (exists)"
	}
	return "  (not found)"
}

func orNone(s string) string {
	if s == "" {
		return "(unset)"
	}
	return s
}
