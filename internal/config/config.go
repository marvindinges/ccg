// Package config loads ccg configuration from YAML with the precedence
// env > project .ccg.yaml > global ~/.config/ccg/config.yaml > built-in
// defaults. It resolves the active provider and the allowed commit types.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/marvindinges/ccg/internal/commit"
	"gopkg.in/yaml.v3"
)

// ProjectFileName is the per-project config file, looked up at the repo root.
const ProjectFileName = ".ccg.yaml"

// Default accent colors. These are terminal-palette colors (ANSI names) so the
// TUI matches the user's terminal theme out of the box.
const (
	DefaultPrimaryColor   = "bright-blue"
	DefaultSecondaryColor = "bright-magenta"
)

// DefaultCountdownSeconds is the abortable delay before a commit/push. 0 disables
// the countdown (the action runs immediately).
const DefaultCountdownSeconds = 3

// ProviderConfig describes an OpenAI-compatible chat-completions endpoint.
// A single generic client covers OpenAI, OpenRouter, Groq, LM Studio,
// llama.cpp and any compatible server — only these fields differ.
type ProviderConfig struct {
	BaseURL string `yaml:"base_url"`
	Model   string `yaml:"model"`
	// APIKeyEnv is the NAME of the environment variable holding the API key.
	// The key itself is never stored on disk. May be empty for local servers.
	APIKeyEnv string `yaml:"api_key_env"`
	// StrictSchema opts into json_schema strict mode for providers that honor
	// it; otherwise plain JSON mode + defensive parsing is used.
	StrictSchema bool `yaml:"strict_schema"`
}

// ColorConfig holds the TUI accent colors. Each value is a color spec: a
// terminal color name ("bright-blue", "magenta", …), an ANSI 256 index ("141"),
// or a hex value ("#a06bff"). Empty values fall back to the defaults.
type ColorConfig struct {
	Primary   string `yaml:"primary"`
	Secondary string `yaml:"secondary"`
}

// CommitConfig holds commit-related settings.
type CommitConfig struct {
	// Types are additional/custom commit types (merged with defaults when
	// UseDefaults is true, otherwise used as the complete set).
	Types []commit.CommitType `yaml:"types"`
	// MaxHeaderLen is the recommended header length limit. nil => default.
	MaxHeaderLen *int `yaml:"max_header_len"`
}

// Config is the merged configuration.
type Config struct {
	Provider ProviderConfig `yaml:"provider"`
	Commit   CommitConfig   `yaml:"commit"`
	Colors   ColorConfig    `yaml:"colors"`
	// UseDefaults includes the built-in commit types (git-cm `defaults = true`).
	// Defaults to true when unset in YAML (see Load).
	UseDefaults *bool `yaml:"defaults"`
	// Countdown is the abortable delay (seconds) before a commit/push. nil =>
	// default; 0 disables the countdown.
	Countdown *int `yaml:"countdown_seconds"`

	// sources records where each top-level value was resolved from, for
	// `ccg config`. Not serialized.
	sources map[string]string `yaml:"-"`
}

// APIKey returns the resolved API key from the configured env var (or "").
func (c Config) APIKey() string {
	if c.Provider.APIKeyEnv == "" {
		return ""
	}
	return os.Getenv(c.Provider.APIKeyEnv)
}

// HasProvider reports whether AI generation is configured. A base URL and model
// are required; the API key may be empty for local servers that ignore it.
func (c Config) HasProvider() bool {
	return c.Provider.BaseURL != "" && c.Provider.Model != ""
}

// PrimaryColor returns the configured primary accent color spec, or the default.
func (c Config) PrimaryColor() string {
	if c.Colors.Primary != "" {
		return c.Colors.Primary
	}
	return DefaultPrimaryColor
}

// SecondaryColor returns the configured secondary accent color spec, or default.
func (c Config) SecondaryColor() string {
	if c.Colors.Secondary != "" {
		return c.Colors.Secondary
	}
	return DefaultSecondaryColor
}

// MaxHeaderLen returns the effective header length limit.
func (c Config) MaxHeaderLen() int {
	if c.Commit.MaxHeaderLen != nil && *c.Commit.MaxHeaderLen > 0 {
		return *c.Commit.MaxHeaderLen
	}
	return commit.DefaultMaxHeaderLen
}

// CountdownSeconds returns the abortable pre-commit/push delay in seconds (0
// means no countdown).
func (c Config) CountdownSeconds() int {
	if c.Countdown != nil && *c.Countdown >= 0 {
		return *c.Countdown
	}
	return DefaultCountdownSeconds
}

// AllowedTypes returns the effective commit type set: built-in defaults
// (unless disabled) plus any custom types, de-duplicated by name.
func (c Config) AllowedTypes() []commit.CommitType {
	useDefaults := c.UseDefaults == nil || *c.UseDefaults
	var out []commit.CommitType
	seen := map[string]bool{}
	add := func(types []commit.CommitType) {
		for _, t := range types {
			if t.Name == "" || seen[t.Name] {
				continue
			}
			seen[t.Name] = true
			out = append(out, t)
		}
	}
	if useDefaults {
		add(commit.DefaultTypes())
	}
	add(c.Commit.Types)
	if len(out) == 0 {
		// Never return an empty set; fall back to defaults.
		return commit.DefaultTypes()
	}
	return out
}

// Load resolves configuration. projectRoot is the repo top-level directory used
// to find the project .ccg.yaml; pass "" to skip the project file.
func Load(projectRoot string) (Config, error) {
	cfg := Config{sources: map[string]string{}}

	// 1. Global config file.
	globalPath, _ := GlobalConfigPath()
	if globalPath != "" {
		if err := mergeFile(&cfg, globalPath, "global"); err != nil {
			return cfg, err
		}
	}

	// 2. Project config file overlays the global one.
	if projectRoot != "" {
		projectPath := filepath.Join(projectRoot, ProjectFileName)
		if err := mergeFile(&cfg, projectPath, "project"); err != nil {
			return cfg, err
		}
	}

	// 3. Environment variables override everything.
	applyEnv(&cfg)

	return cfg, nil
}

// mergeFile decodes path on top of cfg if it exists. Missing files are ignored.
// yaml.v3 only overwrites fields present in the document, so later files
// override earlier ones field-by-field.
func mergeFile(cfg *Config, path, source string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read %s config %s: %w", source, path, err)
	}
	before := *cfg
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return fmt.Errorf("parse %s config %s: %w", source, path, err)
	}
	recordSources(cfg, before, source)
	return nil
}

// recordSources marks which fields this file layer changed.
func recordSources(cfg *Config, before Config, source string) {
	mark := func(key string, changed bool) {
		if changed {
			cfg.sources[key] = source
		}
	}
	mark("provider.base_url", cfg.Provider.BaseURL != before.Provider.BaseURL)
	mark("provider.model", cfg.Provider.Model != before.Provider.Model)
	mark("provider.api_key_env", cfg.Provider.APIKeyEnv != before.Provider.APIKeyEnv)
	mark("provider.strict_schema", cfg.Provider.StrictSchema != before.Provider.StrictSchema)
	mark("colors.primary", cfg.Colors.Primary != before.Colors.Primary)
	mark("colors.secondary", cfg.Colors.Secondary != before.Colors.Secondary)
	mark("commit.max_header_len", !eqIntPtr(cfg.Commit.MaxHeaderLen, before.Commit.MaxHeaderLen))
	mark("commit.types", len(cfg.Commit.Types) != len(before.Commit.Types))
	mark("defaults", !eqBoolPtr(cfg.UseDefaults, before.UseDefaults))
	mark("countdown_seconds", !eqIntPtr(cfg.Countdown, before.Countdown))
}

// applyEnv applies CCG_* environment overrides. There is a knob for every
// global config option.
func applyEnv(cfg *Config) {
	setStr := func(env, key string, dst *string) {
		if v, ok := os.LookupEnv(env); ok {
			*dst = v
			cfg.sources[key] = "env"
		}
	}
	setStr("CCG_BASE_URL", "provider.base_url", &cfg.Provider.BaseURL)
	setStr("CCG_MODEL", "provider.model", &cfg.Provider.Model)
	setStr("CCG_API_KEY_ENV", "provider.api_key_env", &cfg.Provider.APIKeyEnv)
	setStr("CCG_PRIMARY_COLOR", "colors.primary", &cfg.Colors.Primary)
	setStr("CCG_SECONDARY_COLOR", "colors.secondary", &cfg.Colors.Secondary)

	if v, ok := os.LookupEnv("CCG_STRICT_SCHEMA"); ok {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.Provider.StrictSchema = b
			cfg.sources["provider.strict_schema"] = "env"
		}
	}
	if v, ok := os.LookupEnv("CCG_DEFAULTS"); ok {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.UseDefaults = &b
			cfg.sources["defaults"] = "env"
		}
	}
	if v, ok := os.LookupEnv("CCG_MAX_HEADER_LEN"); ok {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Commit.MaxHeaderLen = &n
			cfg.sources["commit.max_header_len"] = "env"
		}
	}
	if v, ok := os.LookupEnv("CCG_COUNTDOWN_SECONDS"); ok {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Countdown = &n
			cfg.sources["countdown_seconds"] = "env"
		}
	}
	if v, ok := os.LookupEnv("CCG_TYPES"); ok {
		cfg.Commit.Types = parseTypes(v)
		cfg.sources["commit.types"] = "env"
	}
}

// parseTypes parses CCG_TYPES, a list of "name:description" pairs separated by
// ";", e.g. "build:Build system changes;perf:Performance improvements".
func parseTypes(s string) []commit.CommitType {
	var out []commit.CommitType
	for _, part := range strings.Split(s, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, desc, _ := strings.Cut(part, ":")
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		out = append(out, commit.CommitType{Name: name, Description: strings.TrimSpace(desc)})
	}
	return out
}

func eqIntPtr(a, b *int) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func eqBoolPtr(a, b *bool) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// Source returns where a dotted config key was resolved from ("global",
// "project", "env"), or "default" if unset.
func (c Config) Source(key string) string {
	if s, ok := c.sources[key]; ok {
		return s
	}
	return "default"
}
