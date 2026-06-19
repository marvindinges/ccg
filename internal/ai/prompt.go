package ai

import (
	"fmt"
	"strings"

	"github.com/marvindinges/ccg/internal/commit"
)

// maxDiffBytes caps how much diff we send to the model to control token cost.
// The head of the diff (file headers + first hunks) carries the most signal.
const maxDiffBytes = 12000

// conventionalCommitsSpec is a concise, faithful summary of the Conventional
// Commits 1.0.0 specification. It is included in the prompt so smaller models
// have the full ruleset for choosing a type and formatting breaking changes.
const conventionalCommitsSpec = `Conventional Commits 1.0.0 — the rules a message must follow:
- Structure:
    <type>[optional (scope)][optional !]: <description>
    <BLANK LINE>
    [optional body]
    <BLANK LINE>
    [optional footer(s)]
- type: a noun such as "feat" or "fix" (full allowed list is given below). "feat"
  introduces a feature; "fix" patches a bug. Other types are allowed too.
- scope: optional noun in parentheses describing the affected area, e.g. fix(parser):.
- description: a short summary of the change immediately after the "type(scope): ".
- body: optional, begins one blank line after the description; free-form prose,
  may span multiple paragraphs.
- footers: optional, begin one blank line after the body; each is "Token: value"
  (or "Token #value"). Tokens use "-" instead of spaces, EXCEPT "BREAKING CHANGE".
- Breaking changes: signalled by a "!" immediately before the ":" in the header,
  and/or a "BREAKING CHANGE: <description>" footer (the token must be uppercase).
  If "!" is used, the breaking change is described by the header description.
- The description should be in the imperative mood ("add", not "added"/"adds")
  and should not end with a period.`

// SuggestInput is everything the model needs to draft a commit.
type SuggestInput struct {
	Diff         string
	Hint         string
	Branch       string // current git branch, for extra context (may be empty)
	Types        []commit.CommitType
	MaxHeaderLen int
	Scopes       []string // pre-defined scopes from config (may be empty)
	StrictScopes bool     // when true, only Scopes values are allowed
}

// systemPrompt instructs the model to emit a single JSON object describing the
// commit. It deliberately uses a plain-language field list plus a concrete
// example rather than dumping a raw JSON Schema — small/local models tend to
// echo a schema back verbatim instead of filling it in.
func systemPrompt(in SuggestInput) string {
	var types strings.Builder
	for _, t := range in.Types {
		fmt.Fprintf(&types, "  - %s: %s\n", t.Name, t.Description)
	}

	maxLen := in.MaxHeaderLen
	if maxLen <= 0 {
		maxLen = commit.DefaultMaxHeaderLen
	}

	scopeInstruction := scopePrompt(in.Scopes, in.StrictScopes)

	return fmt.Sprintf(`You write a Conventional Commits message describing staged code changes, given a git diff.

%s

Respond with exactly ONE JSON object and nothing else: no explanation, no markdown,
no code fences, and do NOT repeat these instructions or output any schema.
The JSON object captures the parts of a Conventional Commit defined above.

The JSON object has these fields:
- "type" (string, required): the change type. Choose exactly one value from the allowed list below.
- "scope" (string): %s
- "description" (string, required): a concise summary in the imperative mood
  ("add", not "added"/"adds"); no trailing period. Keep the whole header
  "type(scope): description" within %d characters.
- "body" (string): a longer explanation, or "" if not needed.
- "breaking" (boolean): true only for a backward-incompatible change.
- "breaking_description" (string): what breaks if "breaking" is true, else "".
- "footers" (array): zero or more objects like {"token": "Refs", "value": "#123"}.

Allowed values for "type":
%s
Example of a valid response (structure only — describe the ACTUAL diff, do not copy this):
{"type":"fix","scope":"parser","description":"handle empty input","body":"","breaking":false,"breaking_description":"","footers":[]}

Now output the single JSON object for the diff the user provides.`, conventionalCommitsSpec, scopeInstruction, maxLen, types.String())
}

// scopePrompt returns the "scope" field description for the system prompt,
// incorporating pre-defined scopes when the config provides them.
func scopePrompt(scopes []string, strict bool) string {
	if len(scopes) == 0 {
		return `a short noun for the affected area, or "" if none.`
	}
	list := `"` + strings.Join(scopes, `", "`) + `"`
	if strict {
		return fmt.Sprintf(`the affected area. You MUST use one of the following values (or "" for no scope): %s.`, list)
	}
	return fmt.Sprintf(`the affected area. Prefer one of the following values when appropriate: %s. Use "" if none fits.`, list)
}

// userPrompt provides the diff and optional human hint.
func userPrompt(in SuggestInput) string {
	diff := truncateDiff(in.Diff)
	var b strings.Builder
	if branch := strings.TrimSpace(in.Branch); branch != "" {
		b.WriteString("Current git branch (may hint at the change's intent or a ticket id): ")
		b.WriteString(branch)
		b.WriteString("\n\n")
	}
	b.WriteString("Here is the staged git diff:\n\n")
	b.WriteString(diff)
	if strings.TrimSpace(in.Hint) != "" {
		b.WriteString("\n\nOptional human hint about the intent of this change (honor it):\n")
		b.WriteString(strings.TrimSpace(in.Hint))
	}
	return b.String()
}

func truncateDiff(diff string) string {
	if len(diff) <= maxDiffBytes {
		return diff
	}
	return diff[:maxDiffBytes] + "\n\n[... diff truncated for length ...]"
}
