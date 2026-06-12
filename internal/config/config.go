// Package config loads ccg configuration from YAML with the precedence
// env > project .ccg.yaml > global ~/.config/ccg/config.yaml > built-in
// defaults. It resolves the active provider and the allowed commit types.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

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

// recordSources marks which provider fields this layer set.
func recordSources(cfg *Config, before Config, source string) {
	if cfg.Provider.BaseURL != before.Provider.BaseURL {
		cfg.sources["provider.base_url"] = source
	}
	if cfg.Provider.Model != before.Provider.Model {
		cfg.sources["provider.model"] = source
	}
	if cfg.Provider.APIKeyEnv != before.Provider.APIKeyEnv {
		cfg.sources["provider.api_key_env"] = source
	}
}

// applyEnv applies CCG_* environment overrides.
func applyEnv(cfg *Config) {
	if v, ok := os.LookupEnv("CCG_BASE_URL"); ok {
		cfg.Provider.BaseURL = v
		cfg.sources["provider.base_url"] = "env"
	}
	if v, ok := os.LookupEnv("CCG_MODEL"); ok {
		cfg.Provider.Model = v
		cfg.sources["provider.model"] = "env"
	}
	if v, ok := os.LookupEnv("CCG_API_KEY_ENV"); ok {
		cfg.Provider.APIKeyEnv = v
		cfg.sources["provider.api_key_env"] = "env"
	}
	if v, ok := os.LookupEnv("CCG_STRICT_SCHEMA"); ok {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.Provider.StrictSchema = b
		}
	}
}

// Source returns where a dotted config key was resolved from ("global",
// "project", "env"), or "default" if unset.
func (c Config) Source(key string) string {
	if s, ok := c.sources[key]; ok {
		return s
	}
	return "default"
}
