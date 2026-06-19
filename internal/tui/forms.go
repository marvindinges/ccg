package tui

import (
	"fmt"
	"strings"

	"charm.land/huh/v2"
	"github.com/marvindinges/ccg/internal/commit"
	"github.com/marvindinges/ccg/internal/git"
)

// formWidth clamps the form width to a comfortable range.
func formWidth(termWidth int) int {
	w := termWidth - 2
	if w < 40 {
		w = 40
	}
	if w > 96 {
		w = 96
	}
	return w
}

// Form field keys.
const (
	keyFiles      = "files"
	keyHint       = "hint"
	keyType       = "type"
	keyScope      = "scope"
	keyBreaking   = "breaking"
	keyDesc       = "description"
	keyBody       = "body"
	keyFooters    = "footers"
	keyConfirm    = "confirm"
	keyPassphrase = "passphrase"
)

// newStageForm builds the file-selection step. Files already staged (or all
// files when selectAll) are pre-selected. The value is the list of paths.
func newStageForm(files []git.FileStatus, selectAll bool) *huh.Form {
	opts := make([]huh.Option[string], 0, len(files))
	for _, f := range files {
		label := fmt.Sprintf("[%s] %s", f.Label(), displayPath(f))
		o := huh.NewOption(label, f.Path)
		if selectAll || f.IsStaged() {
			o = o.Selected(true)
		}
		opts = append(opts, o)
	}
	ms := huh.NewMultiSelect[string]().
		Key(keyFiles).
		Title("Select files to include in the commit").
		Description("space to toggle · enter to confirm").
		Options(opts...).
		Height(min(len(opts)+2, 10))
	return huh.NewForm(huh.NewGroup(ms))
}

// newHintForm builds the optional natural-language hint step.
func newHintForm(preset string) *huh.Form {
	v := preset
	in := huh.NewInput().
		Key(keyHint).
		Title("Optional: describe this change in your own words").
		Description("Helps the AI. Leave blank to let it infer from the diff.").
		Placeholder("e.g. fix race condition in the cache loader").
		Value(&v)
	return huh.NewForm(huh.NewGroup(in))
}

// newReviewForm builds the commit-editing step, pre-populated from draft. Every
// segment is editable. allowed drives the type picker; scopes/strictScopes
// control whether the scope field is a free-form input or a restricted select.
func newReviewForm(draft commit.Commit, allowed []commit.CommitType, scopes []string, strictScopes bool) *huh.Form {
	typeOpts := make([]huh.Option[string], 0, len(allowed))
	for _, t := range allowed {
		typeOpts = append(typeOpts, huh.NewOption(fmt.Sprintf("%s — %s", t.Name, t.Description), t.Name))
	}

	// Bound copies; values are read back via the form keys after completion.
	typeVal := draft.Type
	if typeVal == "" && len(allowed) > 0 {
		typeVal = allowed[0].Name
	}
	scopeVal := draft.Scope
	breakingVal := draft.Breaking
	descVal := draft.Description
	bodyVal := draft.Body
	footersVal := footersToText(draft.Footers)

	typeSel := huh.NewSelect[string]().
		Key(keyType).
		Title("Type").
		Options(typeOpts...).
		Height(6).
		Value(&typeVal)

	scope := newScopeField(keyScope, &scopeVal, scopes, strictScopes)

	breaking := huh.NewConfirm().
		Key(keyBreaking).
		Title("Breaking change?").
		Value(&breakingVal)

	desc := huh.NewInput().
		Key(keyDesc).
		Title("Short description").
		Placeholder("imperative summary, no trailing period").
		Value(&descVal).
		Validate(func(s string) error {
			if strings.TrimSpace(s) == "" {
				return fmt.Errorf("description is required")
			}
			return nil
		})

	body := huh.NewText().
		Key(keyBody).
		Title("Body (optional · alt+enter for newline)").
		Placeholder("Explain what and why, not how.").
		Lines(2).
		Value(&bodyVal)

	footers := huh.NewText().
		Key(keyFooters).
		Title("Footers (optional · one per line)").
		Placeholder("Refs: #123").
		Lines(2).
		Value(&footersVal)

	// Paginate the review across short groups instead of one tall page: huh shows
	// one group at a time, so the screen stays compact. enter advances within and
	// between groups; shift+tab goes back.
	return huh.NewForm(
		huh.NewGroup(typeSel).Title("Type"),
		huh.NewGroup(scope, breaking, desc).Title("Header"),
		huh.NewGroup(body, footers).Title("Details (optional)"),
	)
}

// newScopeField returns a huh field for the scope. When scopes are configured
// and strict_scopes is true it renders as a Select (enforced list); when scopes
// are configured but not strict it renders as a free-form Input with the
// available scopes shown as a description hint; otherwise it is a plain Input.
func newScopeField(key string, val *string, scopes []string, strict bool) huh.Field {
	if len(scopes) > 0 && strict {
		opts := []huh.Option[string]{huh.NewOption("(none)", "")}
		for _, s := range scopes {
			opts = append(opts, huh.NewOption(s, s))
		}
		return huh.NewSelect[string]().Key(key).Title("Scope").Options(opts...).Height(min(len(opts)+2, 10)).Value(val)
	}
	in := huh.NewInput().Key(key).Title("Scope (optional)").Value(val).
		Validate(func(s string) error {
			if strings.ContainsAny(s, " \t)") {
				return fmt.Errorf("scope must not contain spaces or ')'")
			}
			if strict && len(scopes) > 0 && s != "" {
				allowed := make(map[string]bool, len(scopes))
				for _, sc := range scopes {
					allowed[sc] = true
				}
				if !allowed[s] {
					return fmt.Errorf("scope must be one of: %s", strings.Join(scopes, ", "))
				}
			}
			return nil
		})
	if len(scopes) > 0 {
		in = in.Description("available: " + strings.Join(scopes, ", "))
	} else {
		in = in.Placeholder("component or area")
	}
	return in
}

// newFieldForm builds a single-field form for editing one segment of the commit
// from the summary screen, pre-filled from draft. The returned form's value is
// read back via the matching key on completion.
func newFieldForm(field string, draft commit.Commit, allowed []commit.CommitType, scopes []string, strictScopes bool) *huh.Form {
	var group *huh.Group
	switch field {
	case keyType:
		opts := make([]huh.Option[string], 0, len(allowed))
		for _, t := range allowed {
			opts = append(opts, huh.NewOption(fmt.Sprintf("%s — %s", t.Name, t.Description), t.Name))
		}
		v := draft.Type
		if v == "" && len(allowed) > 0 {
			v = allowed[0].Name
		}
		group = huh.NewGroup(huh.NewSelect[string]().Key(keyType).Title("Type").Options(opts...).Height(8).Value(&v))
	case keyScope:
		v := draft.Scope
		group = huh.NewGroup(newScopeField(keyScope, &v, scopes, strictScopes))
	case keyDesc:
		v := draft.Description
		group = huh.NewGroup(huh.NewInput().Key(keyDesc).Title("Short description").
			Placeholder("imperative summary, no trailing period").Value(&v).
			Validate(func(s string) error {
				if strings.TrimSpace(s) == "" {
					return fmt.Errorf("description is required")
				}
				return nil
			}))
	case keyBody:
		v := draft.Body
		group = huh.NewGroup(huh.NewText().Key(keyBody).Title("Body (alt+enter for newline)").
			Placeholder("Explain what and why, not how.").Lines(5).Value(&v))
	case keyFooters:
		v := footersToText(draft.Footers)
		group = huh.NewGroup(huh.NewText().Key(keyFooters).Title("Footers (one per line)").
			Placeholder("Refs: #123").Lines(4).Value(&v))
	default:
		group = huh.NewGroup(huh.NewNote().Title("Nothing to edit"))
	}
	return huh.NewForm(group)
}

// newPassphraseForm collects an SSH key passphrase (masked input).
func newPassphraseForm() *huh.Form {
	v := ""
	f := huh.NewInput().
		Key(keyPassphrase).
		Title("SSH key passphrase").
		EchoMode(huh.EchoModePassword).
		Value(&v)
	return huh.NewForm(huh.NewGroup(f))
}

// newPushForm asks whether to push now.
func newPushForm(branch string) *huh.Form {
	v := false
	title := "Push now?"
	if branch != "" {
		title = fmt.Sprintf("Push %q now?", branch)
	}
	c := huh.NewConfirm().Key(keyConfirm).Title(title).Value(&v)
	return huh.NewForm(huh.NewGroup(c))
}

// footersToText renders footers as one "Token: value" per line.
func footersToText(footers []commit.Footer) string {
	var lines []string
	for _, f := range footers {
		lines = append(lines, fmt.Sprintf("%s: %s", f.Token, f.Value))
	}
	return strings.Join(lines, "\n")
}

// parseFooters parses the footers text area back into footers. Lines that don't
// look like "Token: value" are ignored.
func parseFooters(text string) []commit.Footer {
	var out []commit.Footer
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		idx := strings.Index(line, ":")
		if idx <= 0 {
			continue
		}
		token := strings.TrimSpace(line[:idx])
		value := strings.TrimSpace(line[idx+1:])
		if token == "" || value == "" {
			continue
		}
		out = append(out, commit.Footer{Token: token, Value: value})
	}
	return out
}

// displayPath shows renames as "old -> new", else the path.
func displayPath(f git.FileStatus) string {
	if f.OrigPath != "" {
		return f.OrigPath + " -> " + f.Path
	}
	return f.Path
}
