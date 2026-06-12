package ai

import "testing"

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"plain", `{"a":1}`, `{"a":1}`, false},
		{"fenced", "```json\n{\"a\":1}\n```", `{"a":1}`, false},
		{"fenced upper", "```JSON\n{\"a\":1}\n```", `{"a":1}`, false},
		{"preamble", "Here you go:\n{\"a\":1}\nbye", `{"a":1}`, false},
		{"nested", `prefix {"a":{"b":2}} suffix`, `{"a":{"b":2}}`, false},
		{"none", "no json here", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractJSON(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("extractJSON(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseCommitDefaults(t *testing.T) {
	c, err := parseCommit(`{"type":"feat","scope":"x","description":"do","breaking":true,"breaking_description":"drops y","footers":[{"token":"Refs","value":"#1"},{"token":"","value":"skip"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if c.Type != "feat" || c.Scope != "x" || !c.Breaking {
		t.Errorf("commit = %+v", c)
	}
	// Empty-token footer dropped; breaking_description added as a footer.
	var hasRefs, hasBreaking bool
	for _, f := range c.Footers {
		if f.Token == "Refs" {
			hasRefs = true
		}
		if f.Token == "BREAKING CHANGE" && f.Value == "drops y" {
			hasBreaking = true
		}
	}
	if !hasRefs || !hasBreaking {
		t.Errorf("footers = %+v", c.Footers)
	}
}
