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

// styleForm applies the shared theme, width and help styling to a form.
func styleForm(f *huh.Form, termWidth int) *huh.Form {
	return f.
		WithTheme(huh.ThemeFunc(huh.ThemeCharm)).
		WithWidth(formWidth(termWidth)).
		WithShowHelp(true)
}

// Form field keys.
const (
	keyFiles    = "files"
	keyHint     = "hint"
	keyType     = "type"
	keyScope    = "scope"
	keyBreaking = "breaking"
	keyDesc     = "description"
	keyBody     = "body"
	keyFooters  = "footers"
	keyConfirm  = "confirm"
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
// segment is editable. allowed drives the type picker.
func newReviewForm(draft commit.Commit, allowed []commit.CommitType) *huh.Form {
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

	scope := huh.NewInput().
		Key(keyScope).
		Title("Scope (optional)").
		Placeholder("component or area").
		Value(&scopeVal).
		Validate(func(s string) error {
			if strings.ContainsAny(s, " \t)") {
				return fmt.Errorf("scope must not contain spaces or ')'")
			}
			return nil
		})

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

// newConfirmForm asks the user to confirm creating the commit. dryRun changes
// the wording (no commit is created).
func newConfirmForm(dryRun bool) *huh.Form {
	v := true
	title := "Create this commit?"
	if dryRun {
		title = "Dry run — print this message and exit?"
	}
	c := huh.NewConfirm().Key(keyConfirm).Title(title).Value(&v)
	return huh.NewForm(huh.NewGroup(c))
}

// newPushForm asks whether to push now.
func newPushForm(branch string) *huh.Form {
	v := false
	c := huh.NewConfirm().
		Key(keyConfirm).
		Title(fmt.Sprintf("Push %q now?", branch)).
		Value(&v)
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
