// Package commit defines the Conventional Commits domain model: a structured
// Commit, rendering to a commit message string, parsing back from one, and
// validation against the spec. It has no external dependencies and no
// knowledge of git, AI, or the TUI.
package commit

import (
	"fmt"
	"regexp"
	"strings"
)

// DefaultMaxHeaderLen is the conventional soft limit on the header line length.
const DefaultMaxHeaderLen = 72

// breakingFooterToken is the canonical breaking-change footer token.
const breakingFooterToken = "BREAKING CHANGE"

// Footer is a single Conventional Commits footer / git trailer (e.g.
// "Refs: #123" or "BREAKING CHANGE: drops X").
type Footer struct {
	Token string
	Value string
}

// Commit is a structured Conventional Commit. It is the contract the AI fills
// in, the user edits in the TUI, and what gets rendered to `git commit`.
type Commit struct {
	Type        string   // required, must be in the allowed set
	Scope       string   // optional
	Breaking    bool     // the "!" marker / BREAKING CHANGE footer
	Description string   // required short summary (the header subject)
	Body        string   // optional, may be multi-paragraph
	Footers     []Footer // optional
}

// headerRe parses "type(scope)!: description". Scope and "!" are optional.
var headerRe = regexp.MustCompile(`^(\w+)(?:\(([^)]*)\))?(!)?: (.+)$`)

// footerRe matches a footer line: "Token: value" or "Token #value".
// Tokens use "-" instead of spaces per the spec, except "BREAKING CHANGE".
var footerRe = regexp.MustCompile(`^([A-Za-z][\w-]*|BREAKING CHANGE|BREAKING-CHANGE)(?:: | #)(.+)$`)

// Header returns just the first line of the commit message:
// "type(scope)!: description".
func (c Commit) Header() string {
	var b strings.Builder
	b.WriteString(c.Type)
	if c.Scope != "" {
		b.WriteString("(")
		b.WriteString(c.Scope)
		b.WriteString(")")
	}
	if c.Breaking {
		b.WriteString("!")
	}
	b.WriteString(": ")
	b.WriteString(c.Description)
	return b.String()
}

// Render produces the full commit message: header, optional body, optional
// footers, separated by blank lines. When Breaking is set and no explicit
// BREAKING CHANGE footer exists, one is appended so downstream tooling sees it.
func (c Commit) Render() string {
	var parts []string
	parts = append(parts, c.Header())

	if body := strings.TrimRight(c.Body, "\n"); body != "" {
		parts = append(parts, body)
	}

	footers := c.renderFooters()
	if footers != "" {
		parts = append(parts, footers)
	}

	return strings.Join(parts, "\n\n") + "\n"
}

// renderFooters renders the footer block, injecting a BREAKING CHANGE footer
// when Breaking is set but none was provided explicitly.
func (c Commit) renderFooters() string {
	footers := c.Footers
	if c.Breaking && !hasBreakingFooter(footers) {
		value := c.Description
		footers = append([]Footer{{Token: breakingFooterToken, Value: value}}, footers...)
	}
	if len(footers) == 0 {
		return ""
	}
	lines := make([]string, 0, len(footers))
	for _, f := range footers {
		token := strings.TrimSpace(f.Token)
		val := strings.TrimSpace(f.Value)
		if token == "" || val == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s: %s", token, val))
	}
	return strings.Join(lines, "\n")
}

func hasBreakingFooter(footers []Footer) bool {
	for _, f := range footers {
		t := strings.ToUpper(strings.TrimSpace(f.Token))
		if t == "BREAKING CHANGE" || t == "BREAKING-CHANGE" {
			return true
		}
	}
	return false
}

// ValidationError describes a single validation problem. Fatal errors block
// committing; non-fatal ones are warnings the user may override.
type ValidationError struct {
	Msg   string
	Fatal bool
}

func (e ValidationError) Error() string { return e.Msg }

// Validate checks the commit against the spec and config rules. It returns all
// problems found. Use HasFatal to decide whether committing must be blocked.
//
// Fatal: empty/disallowed type, empty description, scope with whitespace or ")".
// Warning: header over maxHeaderLen, description ending in a period.
func (c Commit) Validate(allowed []CommitType, maxHeaderLen int) []ValidationError {
	if maxHeaderLen <= 0 {
		maxHeaderLen = DefaultMaxHeaderLen
	}
	var errs []ValidationError

	switch {
	case strings.TrimSpace(c.Type) == "":
		errs = append(errs, ValidationError{"type is required", true})
	case len(allowed) > 0 && !HasType(allowed, c.Type):
		errs = append(errs, ValidationError{
			fmt.Sprintf("type %q is not one of the allowed types (%s)", c.Type, strings.Join(TypeNames(allowed), ", ")),
			true,
		})
	}

	if strings.TrimSpace(c.Description) == "" {
		errs = append(errs, ValidationError{"description is required", true})
	}

	if c.Scope != "" {
		if strings.ContainsAny(c.Scope, " \t\n)") {
			errs = append(errs, ValidationError{"scope must not contain whitespace or ')'", true})
		}
	}

	if n := len(c.Header()); n > maxHeaderLen {
		errs = append(errs, ValidationError{
			fmt.Sprintf("header is %d characters (recommended max %d)", n, maxHeaderLen),
			false,
		})
	}

	if d := strings.TrimSpace(c.Description); strings.HasSuffix(d, ".") {
		errs = append(errs, ValidationError{"description should not end with a period", false})
	}

	return errs
}

// HasFatal reports whether any of the validation errors is fatal.
func HasFatal(errs []ValidationError) bool {
	for _, e := range errs {
		if e.Fatal {
			return true
		}
	}
	return false
}

// Parse parses a commit message string back into a Commit. It is tolerant: a
// header that doesn't match the conventional pattern is returned as a Commit
// with an empty Type and the whole first line as the Description, plus an error
// so callers can decide how strict to be.
func Parse(msg string) (Commit, error) {
	msg = strings.ReplaceAll(msg, "\r\n", "\n")
	lines := strings.Split(strings.TrimRight(msg, "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) == "" {
		return Commit{}, fmt.Errorf("empty commit message")
	}

	var c Commit
	m := headerRe.FindStringSubmatch(lines[0])
	if m == nil {
		c.Description = strings.TrimSpace(lines[0])
		return c, fmt.Errorf("header does not match conventional commit format")
	}
	c.Type = m[1]
	c.Scope = m[2]
	c.Breaking = m[3] == "!"
	c.Description = m[4]

	// Remaining lines: body paragraphs followed by an optional footer block.
	rest := lines[1:]
	// Drop the single blank line after the header.
	for len(rest) > 0 && strings.TrimSpace(rest[0]) == "" {
		rest = rest[1:]
	}
	if len(rest) == 0 {
		return c, nil
	}

	// Identify a trailing footer block: a contiguous run of footer lines at the
	// end, separated from the body by a blank line.
	bodyLines, footerLines := splitBodyAndFooters(rest)
	c.Body = strings.TrimRight(strings.Join(bodyLines, "\n"), "\n")
	for _, fl := range footerLines {
		fm := footerRe.FindStringSubmatch(fl)
		if fm == nil {
			continue
		}
		token := fm[1]
		if token == "BREAKING-CHANGE" {
			token = breakingFooterToken
		}
		if strings.EqualFold(token, breakingFooterToken) {
			c.Breaking = true
		}
		c.Footers = append(c.Footers, Footer{Token: token, Value: fm[2]})
	}
	return c, nil
}

// splitBodyAndFooters separates body paragraphs from a trailing footer block.
// The footer block is the last paragraph if every one of its lines parses as a
// footer.
func splitBodyAndFooters(lines []string) (body, footers []string) {
	// Find paragraphs (blank-line separated).
	var paras [][]string
	var cur []string
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			if len(cur) > 0 {
				paras = append(paras, cur)
				cur = nil
			}
			continue
		}
		cur = append(cur, l)
	}
	if len(cur) > 0 {
		paras = append(paras, cur)
	}
	if len(paras) == 0 {
		return nil, nil
	}

	last := paras[len(paras)-1]
	allFooters := true
	for _, l := range last {
		if !footerRe.MatchString(l) {
			allFooters = false
			break
		}
	}

	if allFooters {
		footers = last
		paras = paras[:len(paras)-1]
	}
	for i, p := range paras {
		if i > 0 {
			body = append(body, "")
		}
		body = append(body, p...)
	}
	return body, footers
}
