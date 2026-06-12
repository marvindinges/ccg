package commit

import (
	"strings"
	"testing"
)

func TestHeader(t *testing.T) {
	tests := []struct {
		name string
		c    Commit
		want string
	}{
		{"plain", Commit{Type: "feat", Description: "add login"}, "feat: add login"},
		{"scope", Commit{Type: "fix", Scope: "auth", Description: "handle nil"}, "fix(auth): handle nil"},
		{"breaking", Commit{Type: "feat", Breaking: true, Description: "drop v1"}, "feat!: drop v1"},
		{"scope+breaking", Commit{Type: "feat", Scope: "api", Breaking: true, Description: "drop v1"}, "feat(api)!: drop v1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.c.Header(); got != tt.want {
				t.Errorf("Header() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRender(t *testing.T) {
	tests := []struct {
		name string
		c    Commit
		want string
	}{
		{
			"header only",
			Commit{Type: "docs", Description: "fix typo"},
			"docs: fix typo\n",
		},
		{
			"with body",
			Commit{Type: "feat", Description: "add cache", Body: "Speeds up repeated reads.\nUses an LRU."},
			"feat: add cache\n\nSpeeds up repeated reads.\nUses an LRU.\n",
		},
		{
			"with footers",
			Commit{Type: "fix", Description: "patch", Footers: []Footer{{"Refs", "#12"}, {"Reviewed-by", "Z"}}},
			"fix: patch\n\nRefs: #12\nReviewed-by: Z\n",
		},
		{
			"breaking injects footer",
			Commit{Type: "feat", Breaking: true, Description: "drop v1 api"},
			"feat!: drop v1 api\n\nBREAKING CHANGE: drop v1 api\n",
		},
		{
			"breaking with explicit footer not duplicated",
			Commit{Type: "feat", Breaking: true, Description: "drop v1", Footers: []Footer{{"BREAKING CHANGE", "use v2 instead"}}},
			"feat!: drop v1\n\nBREAKING CHANGE: use v2 instead\n",
		},
		{
			"full",
			Commit{Type: "feat", Scope: "api", Description: "add endpoint", Body: "Body text.", Footers: []Footer{{"Refs", "#9"}}},
			"feat(api): add endpoint\n\nBody text.\n\nRefs: #9\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.c.Render(); got != tt.want {
				t.Errorf("Render() =\n%q\nwant\n%q", got, tt.want)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	allowed := DefaultTypes()
	tests := []struct {
		name      string
		c         Commit
		wantFatal bool
		wantWarn  bool
	}{
		{"valid", Commit{Type: "feat", Description: "do thing"}, false, false},
		{"empty type", Commit{Description: "x"}, true, false},
		{"bad type", Commit{Type: "wibble", Description: "x"}, true, false},
		{"empty desc", Commit{Type: "feat"}, true, false},
		{"scope with space", Commit{Type: "feat", Scope: "a b", Description: "x"}, true, false},
		{"trailing period", Commit{Type: "feat", Description: "do thing."}, false, true},
		{"long header", Commit{Type: "feat", Description: strings.Repeat("x", 100)}, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := tt.c.Validate(allowed, DefaultMaxHeaderLen)
			gotFatal := HasFatal(errs)
			gotWarn := len(errs) > 0 && !gotFatal
			if gotFatal != tt.wantFatal {
				t.Errorf("fatal = %v, want %v (errs: %v)", gotFatal, tt.wantFatal, errs)
			}
			if !tt.wantFatal && gotWarn != tt.wantWarn {
				t.Errorf("warn = %v, want %v (errs: %v)", gotWarn, tt.wantWarn, errs)
			}
		})
	}
}

func TestParseRoundTrip(t *testing.T) {
	cases := []Commit{
		{Type: "feat", Description: "add login"},
		{Type: "fix", Scope: "auth", Description: "handle nil"},
		{Type: "feat", Description: "add cache", Body: "Speeds up reads.\nUses LRU."},
		{Type: "fix", Description: "patch", Footers: []Footer{{"Refs", "#12"}}},
		{Type: "feat", Scope: "api", Description: "add endpoint", Body: "Para one.\n\nPara two.", Footers: []Footer{{"Refs", "#9"}}},
	}
	for _, in := range cases {
		t.Run(in.Header(), func(t *testing.T) {
			rendered := in.Render()
			got, err := Parse(rendered)
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", rendered, err)
			}
			if got.Header() != in.Header() {
				t.Errorf("header round-trip: got %q want %q", got.Header(), in.Header())
			}
			if strings.TrimRight(got.Body, "\n") != strings.TrimRight(in.Body, "\n") {
				t.Errorf("body round-trip: got %q want %q", got.Body, in.Body)
			}
			if len(got.Footers) != len(in.Footers) {
				t.Errorf("footers: got %d want %d (%+v)", len(got.Footers), len(in.Footers), got.Footers)
			}
		})
	}
}

func TestParseBreakingFooterSetsFlag(t *testing.T) {
	got, err := Parse("feat: x\n\nBREAKING CHANGE: removed y\n")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Breaking {
		t.Errorf("expected Breaking=true from BREAKING CHANGE footer")
	}
}

func TestParseNonConventional(t *testing.T) {
	got, err := Parse("just a normal message")
	if err == nil {
		t.Errorf("expected error for non-conventional header")
	}
	if got.Description != "just a normal message" {
		t.Errorf("expected description fallback, got %q", got.Description)
	}
}
