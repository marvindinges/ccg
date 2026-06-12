// Package cmd wires the CLI (cobra) to config, git and the TUI.
package cmd

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/marvindinges/ccg/internal/ai"
	"github.com/marvindinges/ccg/internal/config"
	"github.com/marvindinges/ccg/internal/git"
	"github.com/marvindinges/ccg/internal/tui"
	"github.com/spf13/cobra"
)

var (
	flagNoAI   bool
	flagPush   bool
	flagNoPush bool
	flagHint   string
	flagAll    bool
	flagDryRun bool
	flagDebug  bool
)

var rootCmd = &cobra.Command{
	Use:   "ccg",
	Short: "Create Conventional Commits interactively, with optional AI assistance",
	Long: `ccg guides you through staging files and writing a Conventional Commit.

If an AI provider is configured (per project via .ccg.yaml or globally), ccg can
draft the message from your staged diff — which you always review and edit before
committing. With no provider configured, it falls back to a fully manual guided
flow. Configuration precedence: env > project .ccg.yaml > global config > defaults.`,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE:          run,
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "ccg:", err)
		os.Exit(1)
	}
}

func init() {
	f := rootCmd.Flags()
	f.BoolVar(&flagNoAI, "no-ai", false, "force manual mode even if a provider is configured")
	f.BoolVar(&flagPush, "push", false, "push automatically after committing")
	f.BoolVar(&flagNoPush, "no-push", false, "skip the push step")
	f.StringVar(&flagHint, "hint", "", "natural-language hint for the AI (skips the hint prompt)")
	f.BoolVar(&flagAll, "all", false, "pre-select all changed files for staging")
	f.BoolVar(&flagDryRun, "dry-run", false, "render the commit message but do not commit")
	f.BoolVar(&flagDebug, "debug", false, "print resolved config and the AI request/response for troubleshooting")

	rootCmd.AddCommand(configCmd, versionCmd)
}

func run(cmd *cobra.Command, args []string) error {
	g, err := git.New()
	if err != nil {
		return err
	}
	root, err := g.Root()
	if err != nil {
		return err
	}

	cfg, err := config.Load(root)
	if err != nil {
		return err
	}

	opts := tui.Options{
		Cfg:       cfg,
		Git:       g,
		Hint:      flagHint,
		SelectAll: flagAll,
		AutoPush:  flagPush,
		NoPush:    flagNoPush,
		DryRun:    flagDryRun,
	}

	// Only attach an AI client when a provider is configured and not disabled.
	// Assigning a typed-nil here would make the interface non-nil, so guard it.
	var dbgBuf *bytes.Buffer
	if cfg.HasProvider() && !flagNoAI {
		client := ai.New(cfg)
		if flagDebug {
			dbgBuf = &bytes.Buffer{}
			client = client.WithLogger(log.New(dbgBuf, "", log.Ltime))
		}
		opts.AI = client
	}

	if flagDebug {
		printDebugConfig(os.Stderr, cfg, flagNoAI)
	}

	final, err := tui.Run(tui.New(opts))
	if flagDebug && dbgBuf != nil {
		fmt.Fprintln(os.Stderr, "\n--- ccg AI debug log ---")
		if dbgBuf.Len() == 0 {
			fmt.Fprintln(os.Stderr, "(no AI request was made)")
		} else {
			fmt.Fprint(os.Stderr, dbgBuf.String())
		}
		fmt.Fprintln(os.Stderr, "--- end debug log ---")
	}
	if err != nil {
		return err
	}
	return report(final)
}

// printDebugConfig writes the resolved provider configuration to w, including
// whether the API key environment variable actually resolved to a value — the
// usual reason AI works in curl but not in ccg.
func printDebugConfig(w io.Writer, cfg config.Config, noAI bool) {
	fmt.Fprintln(w, "--- ccg debug: resolved config ---")
	fmt.Fprintf(w, "provider.base_url    = %q [%s]\n", cfg.Provider.BaseURL, cfg.Source("provider.base_url"))
	fmt.Fprintf(w, "provider.model       = %q [%s]\n", cfg.Provider.Model, cfg.Source("provider.model"))
	fmt.Fprintf(w, "provider.api_key_env = %q [%s]\n", cfg.Provider.APIKeyEnv, cfg.Source("provider.api_key_env"))
	if cfg.Provider.APIKeyEnv == "" {
		fmt.Fprintln(w, "api key              = (no env var configured; a placeholder is sent)")
	} else if cfg.APIKey() == "" {
		fmt.Fprintf(w, "api key              = $%s is EMPTY/unset in this environment — likely the problem\n", cfg.Provider.APIKeyEnv)
	} else {
		fmt.Fprintf(w, "api key              = $%s resolved (%d chars)\n", cfg.Provider.APIKeyEnv, len(cfg.APIKey()))
	}
	fmt.Fprintf(w, "request URL          = %s/chat/completions\n", trimRightSlash(cfg.Provider.BaseURL))
	switch {
	case !cfg.HasProvider():
		fmt.Fprintln(w, "AI                   = disabled (no provider configured)")
	case noAI:
		fmt.Fprintln(w, "AI                   = disabled (--no-ai)")
	default:
		fmt.Fprintln(w, "AI                   = enabled")
	}
	fmt.Fprintln(w, "----------------------------------")
}

func trimRightSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}

// report prints a concise summary of what happened after the TUI exits.
func report(m tui.Model) error {
	switch {
	case m.Err() != nil:
		return m.Err()
	case m.Aborted():
		fmt.Println("Aborted. Nothing was committed.")
		return nil
	case flagDryRun:
		fmt.Print(m.Message())
		return nil
	case m.Committed():
		fmt.Println("Created commit.")
		if m.Pushed() {
			if m.SetUpstream() {
				fmt.Println("Pushed (set upstream).")
			} else {
				fmt.Println("Pushed.")
			}
		}
		return nil
	}
	return nil
}
