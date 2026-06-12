package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/marvindinges/ccg/internal/config"
)

func TestPrintDebugConfigKeySet(t *testing.T) {
	t.Setenv("CCG_TEST_KEY", "secret")
	cfg := config.Config{Provider: config.ProviderConfig{
		BaseURL: "https://api.example.com/v1", Model: "m", APIKeyEnv: "CCG_TEST_KEY",
	}}
	var buf bytes.Buffer
	printDebugConfig(&buf, cfg, false)
	out := buf.String()
	for _, want := range []string{"api.example.com/v1", "resolved", "AI                   = enabled", "chat/completions"} {
		if !strings.Contains(out, want) {
			t.Errorf("debug output missing %q:\n%s", want, out)
		}
	}
}

func TestPrintDebugConfigKeyEmpty(t *testing.T) {
	t.Setenv("CCG_TEST_KEY", "")
	cfg := config.Config{Provider: config.ProviderConfig{
		BaseURL: "https://x/v1", Model: "m", APIKeyEnv: "CCG_TEST_KEY",
	}}
	var buf bytes.Buffer
	printDebugConfig(&buf, cfg, false)
	if !strings.Contains(buf.String(), "EMPTY") {
		t.Errorf("expected empty-key warning, got:\n%s", buf.String())
	}
}

func TestPrintDebugConfigNoAI(t *testing.T) {
	cfg := config.Config{Provider: config.ProviderConfig{BaseURL: "x", Model: "m"}}
	var buf bytes.Buffer
	printDebugConfig(&buf, cfg, true)
	if !strings.Contains(buf.String(), "--no-ai") {
		t.Errorf("expected --no-ai note, got:\n%s", buf.String())
	}
}

func TestTrimRightSlash(t *testing.T) {
	if trimRightSlash("http://x/v1///") != "http://x/v1" {
		t.Errorf("got %q", trimRightSlash("http://x/v1///"))
	}
	if trimRightSlash("http://x/v1") != "http://x/v1" {
		t.Error("no-slash case changed")
	}
}

func TestConfigHelpers(t *testing.T) {
	if orNone("") != "(unset)" || orNone("x") != "x" {
		t.Error("orNone broken")
	}
	if aiState(config.Config{Provider: config.ProviderConfig{BaseURL: "x", Model: "y"}}) != "enabled" {
		t.Error("aiState should be enabled")
	}
	if !strings.Contains(aiState(config.Config{}), "disabled") {
		t.Error("aiState should be disabled when no provider")
	}
}
