package ai

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/marvindinges/ccg/internal/commit"
)

// aiCommit is the JSON shape we ask the model to produce. It maps onto
// commit.Commit. Kept separate so the wire format is decoupled from the domain.
type aiCommit struct {
	Type                string `json:"type"`
	Scope               string `json:"scope"`
	Breaking            bool   `json:"breaking"`
	BreakingDescription string `json:"breaking_description"`
	Description         string `json:"description"`
	Body                string `json:"body"`
	Footers             []struct {
		Token string `json:"token"`
		Value string `json:"value"`
	} `json:"footers"`
}

// jsonSchema is the JSON Schema embedded in the prompt and (optionally) sent as
// response_format when strict mode is enabled.
func jsonSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"type", "description"},
		"properties": map[string]any{
			"type":                 map[string]any{"type": "string"},
			"scope":                map[string]any{"type": "string"},
			"breaking":             map[string]any{"type": "boolean"},
			"breaking_description": map[string]any{"type": "string"},
			"description":          map[string]any{"type": "string"},
			"body":                 map[string]any{"type": "string"},
			"footers": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"required":             []string{"token", "value"},
					"properties": map[string]any{
						"token": map[string]any{"type": "string"},
						"value": map[string]any{"type": "string"},
					},
				},
			},
		},
	}
}

// parseCommit extracts a commit.Commit from a raw model response. It is
// deliberately tolerant: it strips Markdown code fences and surrounding prose,
// then unmarshals the first JSON object found. Unknown types are left as-is for
// the user to fix during review (the caller may default them).
func parseCommit(raw string) (commit.Commit, error) {
	jsonText, err := extractJSON(raw)
	if err != nil {
		return commit.Commit{}, err
	}
	var ac aiCommit
	if err := json.Unmarshal([]byte(jsonText), &ac); err != nil {
		return commit.Commit{}, fmt.Errorf("unmarshal model JSON: %w", err)
	}
	return ac.toCommit(), nil
}

func (ac aiCommit) toCommit() commit.Commit {
	c := commit.Commit{
		Type:        strings.TrimSpace(ac.Type),
		Scope:       strings.TrimSpace(ac.Scope),
		Breaking:    ac.Breaking,
		Description: strings.TrimSpace(ac.Description),
		Body:        strings.TrimRight(ac.Body, "\n"),
	}
	for _, f := range ac.Footers {
		if strings.TrimSpace(f.Token) == "" || strings.TrimSpace(f.Value) == "" {
			continue
		}
		c.Footers = append(c.Footers, commit.Footer{Token: f.Token, Value: f.Value})
	}
	// If the model flagged a breaking change with a description but no explicit
	// footer, surface it as one.
	if ac.Breaking && ac.BreakingDescription != "" {
		c.Footers = append(c.Footers, commit.Footer{Token: "BREAKING CHANGE", Value: strings.TrimSpace(ac.BreakingDescription)})
	}
	return c
}

// extractJSON pulls the first JSON object out of a possibly-decorated string:
// it removes ```json fences and grabs from the first '{' to the matching last
// '}'.
func extractJSON(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	// Strip code fences.
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimPrefix(s, "json")
		s = strings.TrimPrefix(s, "JSON")
		if i := strings.LastIndex(s, "```"); i >= 0 {
			s = s[:i]
		}
		s = strings.TrimSpace(s)
	}
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start < 0 || end < 0 || end < start {
		return "", fmt.Errorf("no JSON object found in model response")
	}
	return s[start : end+1], nil
}
