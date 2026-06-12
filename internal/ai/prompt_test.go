package ai

import (
	"strings"
	"testing"

	"github.com/marvindinges/ccg/internal/commit"
)

func TestSystemPromptIncludesSpecAndTypes(t *testing.T) {
	in := SuggestInput{Types: commit.DefaultTypes(), MaxHeaderLen: 72}
	p := systemPrompt(in)

	for _, want := range []string{
		"Conventional Commits 1.0.0", // the spec
		"BREAKING CHANGE",            // breaking-change rule
		"imperative mood",            // description rule
		"feat: A new feature",        // an allowed type with its description
		"72 characters",              // header length from config
	} {
		if !strings.Contains(p, want) {
			t.Errorf("system prompt missing %q", want)
		}
	}
	// It must NOT dump a raw JSON schema (small models echo it).
	if strings.Contains(p, "\"additionalProperties\"") || strings.Contains(p, "\"$schema\"") {
		t.Errorf("system prompt should not contain a raw JSON schema")
	}
}

func TestUserPromptIncludesDiffAndHint(t *testing.T) {
	p := userPrompt(SuggestInput{Diff: "DIFFBODY", Hint: "fix the bug"})
	if !strings.Contains(p, "DIFFBODY") {
		t.Error("user prompt missing diff")
	}
	if !strings.Contains(p, "fix the bug") {
		t.Error("user prompt missing hint")
	}
	// No hint => no hint section.
	if strings.Contains(userPrompt(SuggestInput{Diff: "x"}), "human hint") {
		t.Error("unexpected hint section when no hint provided")
	}
}

func TestTruncateDiff(t *testing.T) {
	big := strings.Repeat("a", maxDiffBytes+500)
	got := truncateDiff(big)
	if !strings.Contains(got, "truncated") {
		t.Error("expected truncation marker")
	}
	if len(got) > maxDiffBytes+100 {
		t.Errorf("truncated diff too long: %d", len(got))
	}
}
