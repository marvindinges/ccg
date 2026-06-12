// Package tui implements the interactive Conventional Commits workflow with
// Bubble Tea v2: select files -> (optional) hint -> (optional) AI generate ->
// review/edit every segment -> confirm -> (optional) push. It works fully
// without AI, in which case the review step starts blank (git-cm behavior).
package tui

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/marvindinges/ccg/internal/ai"
	"github.com/marvindinges/ccg/internal/commit"
	"github.com/marvindinges/ccg/internal/config"
	"github.com/marvindinges/ccg/internal/git"
)

// gitRunner is the subset of *git.Runner the TUI needs (an interface so tests
// can inject fakes).
type gitRunner interface {
	Status() ([]git.FileStatus, error)
	Stage(paths []string) error
	Unstage(paths []string) error
	StagedDiff() (string, error)
	HasStagedChanges() (bool, error)
	Commit(message string) error
	Push(setUpstream bool) error
	HasUpstream() (bool, error)
	CurrentBranch() (string, error)
}

// aiClient is the subset of *ai.Client the TUI needs (nil when no provider).
type aiClient interface {
	Suggest(ctx context.Context, in ai.SuggestInput) (commit.Commit, error)
}

type step int

const (
	stepStage step = iota
	stepHint
	stepGenerate
	stepReview  // full multi-field form (manual entry / "edit all")
	stepEdit    // single-field edit launched from the summary
	stepSummary // review hub: shows the draft + keybindings to edit/commit
	stepPush
	stepBusy
	stepDone
	stepError
)

// Options configures a Model.
type Options struct {
	Cfg       config.Config
	Git       gitRunner
	AI        aiClient // nil disables AI generation
	Hint      string   // preset hint (skips the hint step when non-empty)
	SelectAll bool     // pre-select all changed files
	AutoPush  bool     // push without asking
	NoPush    bool     // skip the push step entirely
	DryRun    bool     // render the message but don't commit
}

// Model is the parent Bubble Tea model holding all step state.
type Model struct {
	opts   Options
	styles styles

	step     step
	form     *huh.Form
	files    []git.FileStatus
	selected []string
	hint     string
	diff     string
	draft    commit.Commit

	width     int
	height    int
	frame     int    // animation tick counter for the loading view
	editField string // which segment a single-field edit is editing

	busyText string
	notice   string // transient banner above a form (e.g. validation errors)

	// outcome
	committed       bool
	pushed          bool
	pushSetUpstream bool
	aborted         bool
	err             error
}

// New builds the initial model.
func New(opts Options) Model {
	return Model{
		opts:     opts,
		styles:   newStyles(),
		hint:     opts.Hint,
		step:     stepBusy,
		busyText: "Loading changes…",
	}
}

// Run starts the program and returns the final model for summary printing.
func Run(m Model) (Model, error) {
	p := tea.NewProgram(m)
	fm, err := p.Run()
	if err != nil {
		return m, err
	}
	final, _ := fm.(Model)
	return final, nil
}

// Outcome accessors used by the caller after Run.
func (m Model) Committed() bool         { return m.committed }
func (m Model) Pushed() bool            { return m.pushed }
func (m Model) SetUpstream() bool       { return m.pushSetUpstream }
func (m Model) Aborted() bool           { return m.aborted }
func (m Model) Err() error              { return m.err }
func (m Model) Message() string         { return m.draft.Render() }
func (m Model) Branch() (string, error) { return m.opts.Git.CurrentBranch() }

func (m Model) Init() tea.Cmd {
	return tea.Batch(tickAnim(), loadStatus(m.opts.Git))
}

// isLoading reports whether the current step shows the animated loader.
func (m Model) isLoading() bool {
	return m.step == stepGenerate || m.step == stepBusy
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if m.form != nil {
			m.form = m.form.WithWidth(formWidth(m.width))
		}
		return m, nil

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			m.aborted = true
			return m, tea.Quit
		}
		// The summary screen is key-driven (not a form).
		if m.step == stepSummary {
			return m.handleSummaryKey(msg.String())
		}

	case statusMsg:
		return m.onStatus(msg)

	case stagedMsg:
		return m.onStaged(msg)

	case draftMsg:
		// After a successful AI draft, land on the summary hub (not the full form).
		m.draft = msg.commit
		return m.enterSummary()

	case aiErrMsg:
		m.notice = fmt.Sprintf("AI generation failed (%v) — edit manually.", msg.err)
		return m.enterReview()

	case committedMsg:
		return m.onCommitted()

	case pushedMsg:
		m.pushed = true
		m.pushSetUpstream = msg.setUpstream
		m.step = stepDone
		return m, tea.Quit

	case errMsg:
		m.err = msg.err
		m.step = stepError
		return m, tea.Quit

	case animMsg:
		// Advance the loader and keep ticking only while in a loading step, so
		// the loop self-terminates and doesn't repaint forms.
		if m.isLoading() {
			m.frame++
			return m, tickAnim()
		}
		return m, nil
	}

	// Route everything else to the active form, if any.
	if m.form != nil && hasForm(m.step) {
		return m.updateForm(msg)
	}
	return m, nil
}

func hasForm(s step) bool {
	switch s {
	case stepStage, stepHint, stepReview, stepEdit, stepPush:
		return true
	}
	return false
}

// onStatus presents the file-selection form once status is loaded.
func (m Model) onStatus(msg statusMsg) (tea.Model, tea.Cmd) {
	m.files = msg.files
	if len(m.files) == 0 {
		m.err = fmt.Errorf("no changes to commit")
		m.step = stepError
		return m, tea.Quit
	}
	m.form = styleForm(newStageForm(m.files, m.opts.SelectAll), m.width)
	m.step = stepStage
	return m, m.form.Init()
}

// onStaged advances past staging into hint/generate (AI) or review (manual).
func (m Model) onStaged(msg stagedMsg) (tea.Model, tea.Cmd) {
	m.diff = msg.diff
	if strings.TrimSpace(m.diff) == "" {
		m.err = fmt.Errorf("nothing staged to commit")
		m.step = stepError
		return m, tea.Quit
	}

	if m.opts.AI == nil {
		// Manual mode: straight to a blank review form.
		return m.enterReview()
	}
	if strings.TrimSpace(m.hint) != "" {
		// Hint preset via flag: skip the hint step.
		return m.enterGenerate()
	}
	m.form = styleForm(newHintForm(m.hint), m.width)
	m.step = stepHint
	return m, m.form.Init()
}

func (m Model) enterGenerate() (tea.Model, tea.Cmd) {
	m.step = stepGenerate
	in := ai.SuggestInput{
		Diff:         m.diff,
		Hint:         m.hint,
		Types:        m.opts.Cfg.AllowedTypes(),
		MaxHeaderLen: m.opts.Cfg.MaxHeaderLen(),
	}
	return m, tea.Batch(tickAnim(), generate(m.opts.AI, in))
}

func (m Model) enterReview() (tea.Model, tea.Cmd) {
	m.form = styleForm(newReviewForm(m.draft, m.opts.Cfg.AllowedTypes()), m.width)
	m.step = stepReview
	return m, m.form.Init()
}

// enterSummary shows the review hub: the rendered draft plus keybindings to
// edit individual segments, toggle breaking, regenerate, or commit.
func (m Model) enterSummary() (tea.Model, tea.Cmd) {
	m.form = nil
	m.step = stepSummary
	return m, nil
}

// summaryKeys maps a pressed key to the segment field it edits.
var summaryKeys = map[string]string{
	"t": keyType,
	"s": keyScope,
	"d": keyDesc,
	"b": keyBody,
	"f": keyFooters,
}

// handleSummaryKey dispatches a keypress on the summary hub.
func (m Model) handleSummaryKey(key string) (tea.Model, tea.Cmd) {
	m.notice = ""
	switch key {
	case "enter":
		return m.commitFromSummary()
	case "e": // edit everything via the full form
		m.form = styleForm(newReviewForm(m.draft, m.opts.Cfg.AllowedTypes()), m.width)
		m.step = stepReview
		return m, m.form.Init()
	case "!": // toggle breaking change in place
		m.draft.Breaking = !m.draft.Breaking
		return m, nil
	case "r": // regenerate from the diff (only when AI is configured)
		if m.opts.AI != nil && strings.TrimSpace(m.diff) != "" {
			return m.enterGenerate()
		}
		return m, nil
	case "q", "esc":
		m.aborted = true
		return m, tea.Quit
	}
	if field, ok := summaryKeys[key]; ok {
		m.editField = field
		m.form = newFieldForm(field, m.draft, m.opts.Cfg.AllowedTypes(), m.width)
		m.step = stepEdit
		return m, m.form.Init()
	}
	return m, nil
}

// commitFromSummary validates the draft and creates the commit (or prints it on
// a dry run). Fatal validation errors keep the user on the summary.
func (m Model) commitFromSummary() (tea.Model, tea.Cmd) {
	errs := m.draft.Validate(m.opts.Cfg.AllowedTypes(), m.opts.Cfg.MaxHeaderLen())
	if commit.HasFatal(errs) {
		m.notice = "Fix the following before committing:\n" + formatErrors(errs)
		return m, nil
	}
	if m.opts.DryRun {
		m.step = stepDone
		return m, tea.Quit
	}
	m.busyText = "Creating commit…"
	m.step = stepBusy
	return m, tea.Batch(tickAnim(), doCommit(m.opts.Git, m.draft))
}

// onCommitted decides whether to push after a successful commit.
func (m Model) onCommitted() (tea.Model, tea.Cmd) {
	m.committed = true
	if m.opts.NoPush {
		m.step = stepDone
		return m, tea.Quit
	}
	if m.opts.AutoPush {
		m.busyText = "Pushing…"
		m.step = stepBusy
		return m, tea.Batch(tickAnim(), doPush(m.opts.Git))
	}
	branch, _ := m.opts.Git.CurrentBranch()
	m.form = styleForm(newPushForm(branch), m.width)
	m.step = stepPush
	return m, m.form.Init()
}

// updateForm forwards a message to the active form and reacts to completion.
func (m Model) updateForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	model, cmd := m.form.Update(msg)
	if f, ok := model.(*huh.Form); ok {
		m.form = f
	}
	switch m.form.State {
	case huh.StateAborted:
		m.aborted = true
		return m, tea.Quit
	case huh.StateCompleted:
		return m.onFormComplete()
	}
	return m, cmd
}

// onFormComplete reads the finished form's values and advances the workflow.
func (m Model) onFormComplete() (tea.Model, tea.Cmd) {
	m.notice = ""
	switch m.step {
	case stepStage:
		return m.completeStage()
	case stepHint:
		m.hint = m.form.GetString(keyHint)
		return m.enterGenerate()
	case stepReview:
		return m.completeReview()
	case stepEdit:
		return m.completeEdit()
	case stepPush:
		if m.form.GetBool(keyConfirm) {
			m.busyText = "Pushing…"
			m.step = stepBusy
			return m, tea.Batch(tickAnim(), doPush(m.opts.Git))
		}
		m.step = stepDone
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) completeStage() (tea.Model, tea.Cmd) {
	selected, _ := m.form.Get(keyFiles).([]string)
	m.selected = selected

	// Reconcile: unstage any previously-staged file the user deselected.
	selectedSet := map[string]bool{}
	for _, p := range selected {
		selectedSet[p] = true
	}
	var toUnstage []string
	for _, f := range m.files {
		if f.IsStaged() && !selectedSet[f.Path] {
			toUnstage = append(toUnstage, f.Path)
		}
	}

	if len(selected) == 0 && len(toUnstage) == 0 {
		m.notice = "Select at least one file."
		m.form = styleForm(newStageForm(m.files, m.opts.SelectAll), m.width)
		return m, m.form.Init()
	}

	m.busyText = "Staging files…"
	m.step = stepBusy
	return m, tea.Batch(tickAnim(), reconcileStage(m.opts.Git, selected, toUnstage))
}

func (m Model) completeReview() (tea.Model, tea.Cmd) {
	m.draft = commit.Commit{
		Type:        m.form.GetString(keyType),
		Scope:       strings.TrimSpace(m.form.GetString(keyScope)),
		Breaking:    m.form.GetBool(keyBreaking),
		Description: strings.TrimSpace(m.form.GetString(keyDesc)),
		Body:        strings.TrimRight(m.form.GetString(keyBody), "\n"),
		Footers:     parseFooters(m.form.GetString(keyFooters)),
	}

	errs := m.draft.Validate(m.opts.Cfg.AllowedTypes(), m.opts.Cfg.MaxHeaderLen())
	if commit.HasFatal(errs) {
		m.notice = "Fix the following before committing:\n" + formatErrors(errs)
		// Re-enter review, preserving the user's edits (seeded from m.draft).
		m.form = styleForm(newReviewForm(m.draft, m.opts.Cfg.AllowedTypes()), m.width)
		m.step = stepReview
		return m, m.form.Init()
	}
	if len(errs) > 0 {
		m.notice = "Warnings:\n" + formatErrors(errs)
	}

	return m.enterSummary()
}

// completeEdit reads back the single segment edited from the summary and
// returns to the summary hub.
func (m Model) completeEdit() (tea.Model, tea.Cmd) {
	switch m.editField {
	case keyType:
		m.draft.Type = m.form.GetString(keyType)
	case keyScope:
		m.draft.Scope = strings.TrimSpace(m.form.GetString(keyScope))
	case keyDesc:
		m.draft.Description = strings.TrimSpace(m.form.GetString(keyDesc))
	case keyBody:
		m.draft.Body = strings.TrimRight(m.form.GetString(keyBody), "\n")
	case keyFooters:
		m.draft.Footers = parseFooters(m.form.GetString(keyFooters))
	}
	m.editField = ""
	return m.enterSummary()
}

func (m Model) View() tea.View {
	var b strings.Builder
	b.WriteString(m.styles.header(stepLabel(m.step)))
	b.WriteString("\n\n")

	if m.notice != "" {
		style := m.styles.warnBox
		if strings.HasPrefix(m.notice, "Fix the following") {
			style = m.styles.errBox
		}
		b.WriteString(style.Render(m.notice))
		b.WriteString("\n\n")
	}

	switch m.step {
	case stepGenerate:
		b.WriteString(m.styles.loading(m.frame, "Generating commit message"))
	case stepBusy:
		b.WriteString(m.styles.loading(m.frame, strings.TrimRight(m.busyText, "… ")))
	case stepError:
		if m.err != nil {
			b.WriteString(m.styles.errBox.Render("Error: " + m.err.Error()))
		}
	case stepDone:
		b.WriteString(m.styles.success.Render("✓ Done."))
	case stepSummary:
		b.WriteString(m.previewBox())
		b.WriteString("\n\n")
		b.WriteString(m.summaryLegend())
	default:
		if m.form != nil {
			b.WriteString(m.form.View())
		}
	}

	v := tea.NewView(b.String())
	return v
}

// previewBox renders the current draft commit message in a bordered box.
func (m Model) previewBox() string {
	msg := strings.TrimRight(m.draft.Render(), "\n")
	title := m.styles.previewT.Render("Commit preview")
	return title + "\n" + m.styles.preview.Render(msg)
}

// summaryLegend renders the keybinding hints shown on the summary hub.
func (m Model) summaryLegend() string {
	commitLabel := "commit"
	if m.opts.DryRun {
		commitLabel = "print & exit"
	}
	breaking := "mark breaking"
	if m.draft.Breaking {
		breaking = "unmark breaking"
	}
	hints := []string{
		m.styles.key("↵", commitLabel),
		m.styles.key("t", "type"),
		m.styles.key("s", "scope"),
		m.styles.key("d", "description"),
		m.styles.key("b", "body"),
		m.styles.key("f", "footers"),
		m.styles.key("!", breaking),
	}
	if m.opts.AI != nil {
		hints = append(hints, m.styles.key("r", "regenerate"))
	}
	hints = append(hints, m.styles.key("e", "edit all"), m.styles.key("q", "cancel"))
	return strings.Join(hints, m.styles.subtle.Render("  "))
}

func stepLabel(s step) string {
	switch s {
	case stepStage:
		return "stage files"
	case stepHint:
		return "describe (optional)"
	case stepGenerate:
		return "generating"
	case stepReview:
		return "edit all"
	case stepEdit:
		return "edit"
	case stepSummary:
		return "review"
	case stepPush:
		return "push"
	case stepBusy:
		return "working"
	case stepDone:
		return "done"
	case stepError:
		return "error"
	}
	return ""
}

func formatErrors(errs []commit.ValidationError) string {
	var lines []string
	for _, e := range errs {
		lines = append(lines, "  • "+e.Msg)
	}
	return strings.Join(lines, "\n")
}
