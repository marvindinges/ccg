package config

import (
	"os"
	"path/filepath"
	"testing"
)

// setupGlobal points os.UserConfigDir at a temp dir and writes a global config.
func setupGlobal(t *testing.T, yaml string) {
	t.Helper()
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	dir := filepath.Join(cfgHome, "ccg")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if yaml != "" {
		if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(yaml), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func writeProject(t *testing.T, yaml string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ProjectFileName), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestPrecedenceProjectOverridesGlobal(t *testing.T) {
	setupGlobal(t, `
provider:
  base_url: https://api.openai.com/v1
  model: gpt-4o-mini
  api_key_env: OPENAI_API_KEY
`)
	root := writeProject(t, `
provider:
  model: llama3
`)

	cfg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider.Model != "llama3" {
		t.Errorf("model = %q, want project override llama3", cfg.Provider.Model)
	}
	// base_url not set in project; should retain global value.
	if cfg.Provider.BaseURL != "https://api.openai.com/v1" {
		t.Errorf("base_url = %q, want global value", cfg.Provider.BaseURL)
	}
	if cfg.Source("provider.model") != "project" {
		t.Errorf("model source = %q, want project", cfg.Source("provider.model"))
	}
	if cfg.Source("provider.base_url") != "global" {
		t.Errorf("base_url source = %q, want global", cfg.Source("provider.base_url"))
	}
}

func TestEnvOverridesAll(t *testing.T) {
	setupGlobal(t, `
provider:
  base_url: https://global/v1
  model: global-model
`)
	root := writeProject(t, `
provider:
  model: project-model
`)
	t.Setenv("CCG_MODEL", "env-model")
	t.Setenv("CCG_BASE_URL", "https://env/v1")

	cfg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider.Model != "env-model" {
		t.Errorf("model = %q, want env-model", cfg.Provider.Model)
	}
	if cfg.Provider.BaseURL != "https://env/v1" {
		t.Errorf("base_url = %q, want env value", cfg.Provider.BaseURL)
	}
	if cfg.Source("provider.model") != "env" {
		t.Errorf("model source = %q, want env", cfg.Source("provider.model"))
	}
}

func TestHasProvider(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want bool
	}{
		{"complete", Config{Provider: ProviderConfig{BaseURL: "x", Model: "y"}}, true},
		{"no key still ok (local)", Config{Provider: ProviderConfig{BaseURL: "http://localhost:1234/v1", Model: "local"}}, true},
		{"missing model", Config{Provider: ProviderConfig{BaseURL: "x"}}, false},
		{"missing url", Config{Provider: ProviderConfig{Model: "y"}}, false},
		{"empty", Config{}, false},
	}
	for _, tt := range tests {
		if got := tt.cfg.HasProvider(); got != tt.want {
			t.Errorf("%s: HasProvider() = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestAllowedTypesMergesCustom(t *testing.T) {
	setupGlobal(t, "")
	root := writeProject(t, `
defaults: true
commit:
  types:
    - name: infra
      description: Infra changes
`)
	cfg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	types := cfg.AllowedTypes()
	names := map[string]bool{}
	for _, ty := range types {
		names[ty.Name] = true
	}
	if !names["feat"] {
		t.Error("expected default type feat present")
	}
	if !names["infra"] {
		t.Error("expected custom type infra present")
	}
}

func TestDefaultsFalseUsesOnlyCustom(t *testing.T) {
	setupGlobal(t, "")
	root := writeProject(t, `
defaults: false
commit:
  types:
    - name: only
      description: just this
`)
	cfg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	types := cfg.AllowedTypes()
	if len(types) != 1 || types[0].Name != "only" {
		t.Errorf("expected only custom type, got %+v", types)
	}
}

func TestStrictSchemaEnv(t *testing.T) {
	setupGlobal(t, "")
	t.Setenv("CCG_STRICT_SCHEMA", "true")
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Provider.StrictSchema {
		t.Error("expected StrictSchema=true from CCG_STRICT_SCHEMA")
	}
}

func TestEnvKnobsForEveryOption(t *testing.T) {
	setupGlobal(t, "")
	t.Setenv("CCG_PRIMARY_COLOR", "cyan")
	t.Setenv("CCG_SECONDARY_COLOR", "121")
	t.Setenv("CCG_MAX_HEADER_LEN", "60")
	t.Setenv("CCG_COUNTDOWN_SECONDS", "0")
	t.Setenv("CCG_DEFAULTS", "false")
	t.Setenv("CCG_TYPES", "build:Build system; perf:Perf improvements")

	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PrimaryColor() != "cyan" || cfg.SecondaryColor() != "121" {
		t.Errorf("colors not overridden: %q / %q", cfg.PrimaryColor(), cfg.SecondaryColor())
	}
	if cfg.MaxHeaderLen() != 60 {
		t.Errorf("max_header_len = %d, want 60", cfg.MaxHeaderLen())
	}
	if cfg.CountdownSeconds() != 0 {
		t.Errorf("countdown = %d, want 0", cfg.CountdownSeconds())
	}
	// defaults=false + custom types => only the custom types.
	types := cfg.AllowedTypes()
	if len(types) != 2 || types[0].Name != "build" || types[1].Name != "perf" {
		t.Errorf("CCG_TYPES not applied: %+v", types)
	}
	if types[1].Description != "Perf improvements" {
		t.Errorf("type description trimmed wrong: %q", types[1].Description)
	}
	if cfg.Source("countdown_seconds") != "env" || cfg.Source("colors.primary") != "env" {
		t.Error("env sources not recorded")
	}
}

func TestCountdownDefault(t *testing.T) {
	if (Config{}).CountdownSeconds() != DefaultCountdownSeconds {
		t.Errorf("default countdown = %d, want %d", (Config{}).CountdownSeconds(), DefaultCountdownSeconds)
	}
}

func TestAPIKeyResolution(t *testing.T) {
	t.Setenv("MY_KEY", "abc123")
	cfg := Config{Provider: ProviderConfig{APIKeyEnv: "MY_KEY"}}
	if cfg.APIKey() != "abc123" {
		t.Errorf("APIKey() = %q, want abc123", cfg.APIKey())
	}
	empty := Config{Provider: ProviderConfig{APIKeyEnv: ""}}
	if empty.APIKey() != "" {
		t.Errorf("APIKey() with no env should be empty")
	}
}

func TestSourceDefaultsToDefault(t *testing.T) {
	cfg := Config{sources: map[string]string{}}
	if cfg.Source("provider.model") != "default" {
		t.Errorf("unset source should be 'default'")
	}
}

func TestGlobalConfigPath(t *testing.T) {
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	p, err := GlobalConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	want := cfgHome + "/ccg/config.yaml"
	if p != want {
		t.Errorf("GlobalConfigPath() = %q, want %q", p, want)
	}
}

func TestColorsDefaultsAndOverride(t *testing.T) {
	// Defaults when unset.
	def := Config{}
	if def.PrimaryColor() != DefaultPrimaryColor || def.SecondaryColor() != DefaultSecondaryColor {
		t.Errorf("defaults = %q/%q, want %q/%q",
			def.PrimaryColor(), def.SecondaryColor(), DefaultPrimaryColor, DefaultSecondaryColor)
	}
	// Loaded from project config.
	setupGlobal(t, "")
	root := writeProject(t, `
colors:
  primary: cyan
  secondary: "#ff8800"
`)
	cfg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PrimaryColor() != "cyan" || cfg.SecondaryColor() != "#ff8800" {
		t.Errorf("got %q/%q", cfg.PrimaryColor(), cfg.SecondaryColor())
	}
}

func TestMissingFilesUseDefaults(t *testing.T) {
	setupGlobal(t, "")
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HasProvider() {
		t.Error("expected no provider with empty config")
	}
	if cfg.MaxHeaderLen() != 72 {
		t.Errorf("MaxHeaderLen = %d, want 72", cfg.MaxHeaderLen())
	}
	if len(cfg.AllowedTypes()) == 0 {
		t.Error("expected default types")
	}
}
