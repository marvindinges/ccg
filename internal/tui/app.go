// Package tui implements the ccg workflow as a lazygit-style vertical accordion:
// three stacked panels — Files, Hint (AI only), and Commit (a review hub showing
// the rendered message with key-driven editing). The focused panel expands to
// fill the height; the others collapse to a one-line summary. tab cycles focus.
package tui

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/marvindinges/ccg/internal/ai"
	"github.com/marvindinges/ccg/internal/commit"
	"github.com/marvindinges/ccg/internal/config"
	"github.com/marvindinges/ccg/internal/git"
)

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

type aiClient interface {
	Suggest(ctx context.Context, in ai.SuggestInput) (commit.Commit, error)
}

type step int

const (
	stepBusy   step = iota // status load / commit / push (full-screen spinner)
	stepMain               // vertical accordion panel layout
	stepEdit               // single-field huh form overlay (from a panel)
	stepReview             // full multi-field huh form (via 'e')
	stepModal              // dismissable AI-failure overlay
	stepPush               // push-confirmation huh form
	stepDone
	stepError
)

type panel int

const (
	panelFiles  panel = iota
	panelEditor       // the review hub
)

// Options configures a Model.
type Options struct {
	Cfg       config.Config
	Git       gitRunner
	AI        aiClient
	Hint      string
	SelectAll bool
	AutoPush  bool
	NoPush    bool
	DryRun    bool
}

// Model is the parent Bubble Tea model.
type Model struct {
	opts   Options
	styles styles

	step step
	form *huh.Form

	files         []git.FileStatus
	filesSelected map[string]bool // staged intent, applied to git on toggle
	filesCursor   int
	filesScroll   int

	generating bool
	busyMsg    string

	hint  string
	diff  string // current staged diff (refreshed as files are staged/unstaged)
	draft commit.Commit

	width     int
	height    int
	frame     int
	editField string

	activePanel panel

	notice    string
	modalText string

	committed       bool
	pushed          bool
	pushSetUpstream bool
	aborted         bool
	err             error
}

// New builds the initial model.
func New(opts Options) Model {
	primary := parseColor(opts.Cfg.PrimaryColor())
	secondary := parseColor(opts.Cfg.SecondaryColor())
	return Model{
		opts:          opts,
		styles:        newStyles(primary, secondary),
		hint:          opts.Hint,
		step:          stepBusy,
		filesSelected: make(map[string]bool),
		busyMsg:       "Loading changes",
	}
}

func (m Model) styleForm(f *huh.Form) *huh.Form {
	return f.WithTheme(m.styles.huhTheme()).WithWidth(modalFormWidth(m.width)).WithShowHelp(false)
}

// modalFormWidth is the inner width of the huh form inside a popup modal: narrow
// enough to leave a margin around the centered box on any reasonable terminal.
func modalFormWidth(termW int) int {
	w := termW - 12 // leave room for the border, padding, and a screen margin
	if w > 60 {
		w = 60
	}
	if w < 30 {
		w = 30
	}
	return w
}

func Run(m Model) (Model, error) {
	p := tea.NewProgram(m)
	fm, err := p.Run()
	if err != nil {
		return m, err
	}
	final, _ := fm.(Model)
	return final, nil
}

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

func (m Model) isLoading() bool {
	return m.step == stepBusy || (m.step == stepMain && m.generating)
}

// visiblePanels lists the panels in vertical order. The AI hint is collected via
// a popup modal before generating, so there is no dedicated hint panel.
func (m Model) visiblePanels() []panel {
	return []panel{panelFiles, panelEditor}
}

// countStaged is how many files are currently marked staged.
func (m Model) countStaged() int {
	n := 0
	for _, f := range m.files {
		if m.filesSelected[f.Path] {
			n++
		}
	}
	return n
}

// hasStaged reports whether at least one file is staged (intent-based, so it is
// true immediately on toggle without waiting for the async diff to load).
func (m Model) hasStaged() bool { return m.countStaged() > 0 }

// ── Update ────────────────────────────────────────────────────────────────────

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if m.form != nil {
			m.form = m.form.WithWidth(modalFormWidth(m.width))
		}
		return m, nil

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			m.aborted = true
			return m, tea.Quit
		}
		if m.step == stepMain {
			return m.handleMainKey(msg.String())
		}
		if m.step == stepModal {
			m.modalText = ""
			m.step = stepMain
			m.activePanel = panelEditor
			return m, nil
		}
		// esc closes an open edit/review/push popup without quitting the app.
		if hasForm(m.step) && msg.String() == "esc" {
			return m.cancelModal()
		}

	case statusMsg:
		return m.onStatus(msg)

	case stagedMsg:
		return m.onStaged(msg)

	case draftMsg:
		m.draft = msg.commit
		m.generating = false
		return m, nil

	case aiErrMsg:
		m.generating = false
		m.modalText = msg.err.Error()
		m.step = stepModal
		m.form = nil
		return m, nil

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
		if m.isLoading() {
			m.frame++
			return m, tickAnim()
		}
		return m, nil
	}

	if m.form != nil && hasForm(m.step) {
		return m.updateForm(msg)
	}
	return m, nil
}

func hasForm(s step) bool {
	return s == stepReview || s == stepEdit || s == stepPush
}

func (m Model) onStatus(msg statusMsg) (tea.Model, tea.Cmd) {
	m.files = msg.files
	if len(m.files) == 0 {
		m.err = fmt.Errorf("no changes to commit")
		m.step = stepError
		return m, tea.Quit
	}
	// Seed the staged intent from git (or stage everything with --all) and
	// reconcile git to match, then load the resulting staged diff.
	var toStage, toUnstage []string
	for _, f := range m.files {
		want := m.opts.SelectAll || f.IsStaged()
		m.filesSelected[f.Path] = want
		switch {
		case want && !f.IsStaged():
			toStage = append(toStage, f.Path)
		case !want && f.IsStaged():
			toUnstage = append(toUnstage, f.Path)
		}
	}
	m.step = stepMain
	return m, reconcileStage(m.opts.Git, toStage, toUnstage)
}

// onStaged refreshes the staged diff after any stage/unstage. It never changes
// focus or triggers generation — staging is now incremental and reversible.
func (m Model) onStaged(msg stagedMsg) (tea.Model, tea.Cmd) {
	m.diff = msg.diff
	return m, nil
}

// toggleStage flips the staged state of the file under the cursor and applies it
// to git immediately, refreshing the diff.
// toggleRow stages or unstages every file in a row (one file, or a whole folder
// for a directory row) and applies it to git immediately. A partially-staged
// folder stages fully; a fully-staged one unstages.
func (m Model) toggleRow(r fileRow) (tea.Model, tea.Cmd) {
	m.notice = ""
	staged, total := m.rowStaged(r)
	target := staged < total // not fully staged → stage all, else unstage all

	var toStage, toUnstage []string
	for _, p := range r.paths {
		if m.filesSelected[p] == target {
			continue
		}
		m.filesSelected[p] = target
		if target {
			toStage = append(toStage, p)
		} else {
			toUnstage = append(toUnstage, p)
		}
	}
	if len(toStage) == 0 && len(toUnstage) == 0 {
		return m, nil
	}
	return m, reconcileStage(m.opts.Git, toStage, toUnstage)
}

// toggleStageAll stages every file if any is unstaged, otherwise unstages all.
func (m Model) toggleStageAll() (tea.Model, tea.Cmd) {
	m.notice = ""
	stageAll := m.countStaged() < len(m.files)
	var toStage, toUnstage []string
	for _, f := range m.files {
		m.filesSelected[f.Path] = stageAll
		if stageAll {
			toStage = append(toStage, f.Path)
		} else {
			toUnstage = append(toUnstage, f.Path)
		}
	}
	return m, reconcileStage(m.opts.Git, toStage, toUnstage)
}

// startGeneration kicks off an async AI suggestion using the current hint and
// focuses the Commit panel so its spinner is visible.
func (m Model) startGeneration() (tea.Model, tea.Cmd) {
	m.generating = true
	m.activePanel = panelEditor
	m.busyMsg = "Generating commit message"
	in := ai.SuggestInput{
		Diff:         m.diff,
		Hint:         m.hint,
		Types:        m.opts.Cfg.AllowedTypes(),
		MaxHeaderLen: m.opts.Cfg.MaxHeaderLen(),
	}
	return m, tea.Batch(tickAnim(), generate(m.opts.AI, in))
}

// ── Panel key handling ────────────────────────────────────────────────────────

func (m Model) handleMainKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "q", "esc":
		m.aborted = true
		return m, tea.Quit
	case "tab":
		m.activePanel = m.cyclePanel(1)
		return m, nil
	case "shift+tab":
		m.activePanel = m.cyclePanel(-1)
		return m, nil
	}
	switch m.activePanel {
	case panelFiles:
		return m.handleFilesPanelKey(key)
	case panelEditor:
		return m.handleEditorPanelKey(key)
	}
	return m, nil
}

// cyclePanel returns the panel `dir` steps away from the active one, wrapping.
func (m Model) cyclePanel(dir int) panel {
	vis := m.visiblePanels()
	idx := 0
	for i, p := range vis {
		if p == m.activePanel {
			idx = i
			break
		}
	}
	idx = (idx + dir + len(vis)) % len(vis)
	return vis[idx]
}

// handleFilesPanelKey drives the always-interactive Files panel: navigate with
// the cursor, space stages/unstages the file under it (applied to git right
// away), a toggles all, and enter proceeds to writing the commit.
func (m Model) handleFilesPanelKey(key string) (tea.Model, tea.Cmd) {
	rows := m.fileRows()
	switch key {
	case "j", "down":
		if m.filesCursor < len(rows)-1 {
			m.filesCursor++
		}
	case "k", "up":
		if m.filesCursor > 0 {
			m.filesCursor--
		}
	case " ", "space":
		if m.filesCursor < len(rows) {
			return m.toggleRow(rows[m.filesCursor])
		}
	case "a":
		if len(m.files) > 0 {
			return m.toggleStageAll()
		}
	case "enter":
		if !m.hasStaged() {
			m.notice = "Stage at least one file with [space]."
			return m, nil
		}
		m.notice = ""
		m.activePanel = panelEditor
		// With AI, jump straight into the hint modal → generate on submit.
		if m.opts.AI != nil {
			return m.openFieldEdit(keyHint)
		}
		return m, nil
	}
	return m, nil
}

func (m Model) handleEditorPanelKey(key string) (tea.Model, tea.Cmd) {
	if !m.hasStaged() || m.generating {
		return m, nil
	}
	m.notice = ""
	switch key {
	case "t":
		return m.openFieldEdit(keyType)
	case "s":
		return m.openFieldEdit(keyScope)
	case "d", "enter":
		return m.openFieldEdit(keyDesc)
	case "b":
		return m.openFieldEdit(keyBody)
	case "f":
		return m.openFieldEdit(keyFooters)
	case "!":
		m.draft.Breaking = !m.draft.Breaking
	case "r":
		if m.opts.AI != nil && strings.TrimSpace(m.diff) != "" {
			// Pop the hint modal first; submitting it regenerates.
			return m.openFieldEdit(keyHint)
		}
	case "e":
		m.form = m.styleForm(newReviewForm(m.draft, m.opts.Cfg.AllowedTypes()))
		m.step = stepReview
		return m, m.form.Init()
	case "c":
		return m.commitFromMain()
	}
	return m, nil
}

func (m Model) openFieldEdit(field string) (tea.Model, tea.Cmd) {
	m.editField = field
	var f *huh.Form
	if field == keyHint {
		v := m.hint
		f = huh.NewForm(huh.NewGroup(
			huh.NewInput().Key(keyHint).
				Title("Hint for the AI (optional)").
				Placeholder("e.g. fix race condition in the cache loader").
				Value(&v),
		))
	} else {
		f = newFieldForm(field, m.draft, m.opts.Cfg.AllowedTypes())
	}
	m.form = m.styleForm(f)
	m.step = stepEdit
	return m, m.form.Init()
}

func (m Model) commitFromMain() (tea.Model, tea.Cmd) {
	errs := m.draft.Validate(m.opts.Cfg.AllowedTypes(), m.opts.Cfg.MaxHeaderLen())
	if commit.HasFatal(errs) {
		m.notice = "Fix the following before committing:\n" + formatErrors(errs)
		return m, nil
	}
	if m.opts.DryRun {
		m.step = stepDone
		return m, tea.Quit
	}
	m.busyMsg = "Creating commit"
	m.step = stepBusy
	return m, tea.Batch(tickAnim(), doCommit(m.opts.Git, m.draft))
}

func (m Model) onCommitted() (tea.Model, tea.Cmd) {
	m.committed = true
	if m.opts.NoPush {
		m.step = stepDone
		return m, tea.Quit
	}
	if m.opts.AutoPush {
		m.busyMsg = "Pushing"
		m.step = stepBusy
		return m, tea.Batch(tickAnim(), doPush(m.opts.Git))
	}
	branch, _ := m.opts.Git.CurrentBranch()
	m.form = m.styleForm(newPushForm(branch))
	m.step = stepPush
	return m, m.form.Init()
}

func (m Model) updateForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	model, cmd := m.form.Update(msg)
	if f, ok := model.(*huh.Form); ok {
		m.form = f
	}
	switch m.form.State {
	case huh.StateAborted:
		// huh aborts (e.g. its own cancel key) close the popup, not the app.
		return m.cancelModal()
	case huh.StateCompleted:
		return m.onFormComplete()
	}
	return m, cmd
}

// cancelModal closes an edit/review/push popup and returns to the panel layout,
// discarding any in-progress edit. Cancelling the push prompt finishes instead.
func (m Model) cancelModal() (tea.Model, tea.Cmd) {
	if m.step == stepPush {
		m.step = stepDone
		return m, tea.Quit
	}
	m.form = nil
	m.editField = ""
	m.notice = ""
	m.step = stepMain
	return m, nil
}

func (m Model) onFormComplete() (tea.Model, tea.Cmd) {
	m.notice = ""
	switch m.step {
	case stepReview:
		return m.completeReview()
	case stepEdit:
		return m.completeEdit()
	case stepPush:
		if m.form.GetBool(keyConfirm) {
			m.busyMsg = "Pushing"
			m.step = stepBusy
			return m, tea.Batch(tickAnim(), doPush(m.opts.Git))
		}
		m.step = stepDone
		return m, tea.Quit
	}
	return m, nil
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
		m.form = m.styleForm(newReviewForm(m.draft, m.opts.Cfg.AllowedTypes()))
		m.step = stepReview
		return m, m.form.Init()
	}
	if len(errs) > 0 {
		m.notice = "Warnings:\n" + formatErrors(errs)
	}
	m.form = nil
	m.step = stepMain
	return m, nil
}

func (m Model) completeEdit() (tea.Model, tea.Cmd) {
	field := m.editField
	switch field {
	case keyHint:
		m.hint = m.form.GetString(keyHint)
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
	m.form = nil
	m.step = stepMain

	// Submitting a hint generates immediately — the user just expressed intent.
	if field == keyHint && m.opts.AI != nil && m.hasStaged() && strings.TrimSpace(m.diff) != "" {
		return m.startGeneration()
	}
	return m, nil
}

// ── View ─────────────────────────────────────────────────────────────────────

func (m Model) View() tea.View {
	switch m.step {
	case stepMain:
		return tea.NewView(m.viewMain())
	case stepEdit, stepReview, stepPush:
		return tea.NewView(m.viewFormModal())
	case stepModal:
		return tea.NewView(m.viewErrorModal())
	case stepBusy:
		return tea.NewView(m.centered(m.styles.loading(m.frame, m.busyMsg)))
	case stepDone:
		return tea.NewView(m.centered(m.styles.success.Render("✓ Done.")))
	case stepError:
		body := ""
		if m.err != nil {
			body = m.styles.errBox.Render("Error: " + m.err.Error())
		}
		return tea.NewView(m.centered(body))
	}
	return tea.NewView("")
}

// viewFormModal overlays the active huh form as a centered popup on top of the
// (dimmed) panel layout.
func (m Model) viewFormModal() string {
	box := m.formModalBox()
	if m.width <= 0 || m.height <= 0 {
		return box
	}
	return composeOverlay(m.viewMain(), box, m.width, m.height)
}

// formModalBox renders the active form (plus its key hints) inside a titled,
// bordered popup box.
func (m Model) formModalBox() string {
	if m.form == nil {
		return ""
	}
	body := m.form.View()
	// Always advertise esc to abort; it's handled by us, not a huh keybinding,
	// so it won't appear in the form's own hints.
	hints := m.styles.hints(m.form.KeyBinds())
	esc := m.styles.key("esc", "cancel")
	if hints != "" {
		hints += "  " + esc
	} else {
		hints = esc
	}
	body += "\n\n" + hints
	if title := m.modalTitle(); title != "" {
		body = m.styles.previewT.Render(title) + "\n\n" + body
	}
	return m.styles.popup(m.styles.primary, body)
}

// modalTitle is the heading shown at the top of an edit/review/push popup.
func (m Model) modalTitle() string {
	switch m.step {
	case stepReview:
		return "Edit commit"
	case stepPush:
		return "Push"
	case stepEdit:
		switch m.editField {
		case keyHint:
			return "Edit hint"
		case keyType:
			return "Edit type"
		case keyScope:
			return "Edit scope"
		case keyDesc:
			return "Edit description"
		case keyBody:
			return "Edit body"
		case keyFooters:
			return "Edit footers"
		}
	}
	return ""
}

// viewErrorModal overlays the AI-failure message as a popup over the panels.
func (m Model) viewErrorModal() string {
	body := m.styles.modalTitle.Render("⚠  AI generation failed") + "\n\n" +
		m.modalText + "\n\n" +
		m.styles.subtle.Render("Press any key to continue.")
	box := m.styles.popup(colRed, body)
	if m.width <= 0 || m.height <= 0 {
		return box
	}
	return composeOverlay(m.viewMain(), box, m.width, m.height)
}

// centered places body in the middle of the screen (used for full-screen
// spinner / done / error states).
func (m Model) centered(body string) string {
	if m.width <= 0 || m.height <= 0 {
		return body
	}
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, body)
}

// composeOverlay draws box centered on top of base using lipgloss's layer
// compositor, so the panels remain visible behind the popup.
func composeOverlay(base, box string, termW, termH int) string {
	bw, bh := lipgloss.Width(box), lipgloss.Height(box)
	x := max(0, (termW-bw)/2)
	y := max(0, (termH-bh)/2)
	bg := lipgloss.NewLayer(base).X(0).Y(0).Z(0)
	fg := lipgloss.NewLayer(box).X(x).Y(y).Z(1)
	return lipgloss.NewCompositor(bg, fg).Render()
}

// collapsedOuterH is the height of a collapsed panel: title + 1 summary line +
// top/bottom borders.
const collapsedOuterH = 3

// viewMain renders the vertical accordion layout.
func (m Model) viewMain() string {
	if m.width == 0 {
		return ""
	}

	var b strings.Builder

	noticeH := 0
	if m.notice != "" {
		style := m.styles.warnBox
		if strings.HasPrefix(m.notice, "Fix") {
			style = m.styles.errBox
		}
		noticeStr := style.Width(m.width - 2).Render(m.notice)
		b.WriteString(noticeStr)
		b.WriteString("\n\n")
		noticeH = strings.Count(noticeStr, "\n") + 3
	}

	const footerLines = 1

	vis := m.visiblePanels()
	avail := m.height - noticeH - footerLines
	minAvail := len(vis) * collapsedOuterH
	if avail < minAvail {
		avail = minAvail
	}

	// The focused panel gets all the height the collapsed panels don't use.
	focusedH := avail - collapsedOuterH*(len(vis)-1)
	if focusedH < 4 {
		focusedH = 4
	}

	var panels []string
	for _, p := range vis {
		active := p == m.activePanel
		h := collapsedOuterH
		if active {
			h = focusedH
		}
		innerW := m.width - 2
		innerH := h - 2
		title, content := m.renderPanel(p, active, innerW, innerH)
		panels = append(panels, m.styles.panelBox(title, content, m.width, h, active))
	}

	b.WriteString(strings.Join(panels, "\n"))
	b.WriteString("\n")
	b.WriteString(m.mainFooter())

	return b.String()
}

// renderPanel returns the (title, content) for one panel. Collapsed (inactive)
// panels return a one-line summary; the focused panel returns full content.
func (m Model) renderPanel(p panel, active bool, innerW, innerH int) (string, string) {
	switch p {
	case panelFiles:
		return m.renderFilesPanel(active, innerW, innerH)
	case panelEditor:
		return m.renderEditorPanel(active, innerW, innerH)
	}
	return "", ""
}

func (m Model) renderFilesPanel(active bool, innerW, innerH int) (string, string) {
	title := fmt.Sprintf("Files (%d/%d staged)", m.countStaged(), len(m.files))

	if !active {
		return title, m.styles.subtle.Render(fmt.Sprintf("%d staged", m.countStaged()))
	}

	rows := m.fileRows()
	if len(rows) == 0 {
		return title, m.styles.subtle.Render("no changes")
	}

	var lines []string
	for i, r := range rows {
		focused := i == m.filesCursor
		indent := strings.Repeat("  ", r.depth)

		check := m.rowGlyph(r)
		var label string
		if r.isDir {
			label = r.label
		} else {
			f := m.files[r.fileIdx]
			label = fmt.Sprintf("[%s] %s", f.Label(), renameLabel(f, r.label))
		}

		cursor := "  "
		if focused {
			cursor = m.styles.editorFocused.Render("▶ ")
			check = m.styles.editorFocused.Render(check)
			label = m.styles.editorFocused.Render(label)
		}
		lines = append(lines, cursor+indent+check+" "+label)
	}

	start := m.filesScroll
	if m.filesCursor < start {
		start = m.filesCursor
	}
	if m.filesCursor >= start+innerH {
		start = m.filesCursor - innerH + 1
	}
	return title, strings.Join(lines[start:min(start+innerH, len(lines))], "\n")
}

// rowGlyph is the staged indicator for a row: ◉ all, ○ none, ◐ partial (a
// folder with only some of its files staged).
func (m Model) rowGlyph(r fileRow) string {
	staged, total := m.rowStaged(r)
	switch {
	case staged == 0:
		return "○"
	case staged == total:
		return "◉"
	default:
		return "◐"
	}
}

// renameLabel shows the basename, noting renames as "old → new" basenames.
func renameLabel(f git.FileStatus, base string) string {
	if f.OrigPath != "" {
		old := f.OrigPath
		if i := strings.LastIndexByte(old, '/'); i >= 0 {
			old = old[i+1:]
		}
		return old + " → " + base
	}
	return base
}

func (m Model) renderEditorPanel(active bool, innerW, innerH int) (string, string) {
	title := "Commit"

	if m.generating {
		if !active {
			return title, m.styles.subtle.Render("generating…")
		}
		return title, m.styles.loading(m.frame, m.busyMsg)
	}

	if !m.hasStaged() {
		if !active {
			return title, m.styles.subtle.Render("(stage files first)")
		}
		return title, m.styles.subtle.Render("Stage files to start writing the commit.")
	}

	preview, placeholder := m.editorPreview()

	if !active {
		line := preview
		if i := strings.IndexByte(line, '\n'); i >= 0 {
			line = line[:i]
		}
		line = clipLine(line, innerW)
		if placeholder {
			return title, m.styles.subtle.Render(line)
		}
		return title, m.styles.editorNormal.Render(line)
	}

	content := wrapClip(preview, innerW, innerH)
	if placeholder {
		content = m.styles.subtle.Render(content)
	}
	return title, content
}

// editorPreview returns the commit message to show in the Commit panel. When the
// draft is incomplete it returns a skeleton like "TYPE(SCOPE): DESCRIPTION" with
// placeholders for the missing required segments (placeholder=true).
func (m Model) editorPreview() (string, bool) {
	c := m.draft
	if c.Type != "" && c.Description != "" {
		return strings.TrimRight(c.Render(), "\n"), false
	}

	typ := c.Type
	if typ == "" {
		typ = "TYPE"
	}
	scope := "(SCOPE)"
	if c.Scope != "" {
		scope = "(" + c.Scope + ")"
	}
	bang := ""
	if c.Breaking {
		bang = "!"
	}
	desc := c.Description
	if desc == "" {
		desc = "DESCRIPTION"
	}
	parts := []string{typ + scope + bang + ": " + desc}
	if c.Body != "" {
		parts = append(parts, "", c.Body)
	}
	if len(c.Footers) > 0 {
		parts = append(parts, "", footersToText(c.Footers))
	}
	return strings.Join(parts, "\n"), true
}

// mainFooter renders the two-section footer: global | active-panel keybinds.
func (m Model) mainFooter() string {
	global := []string{
		m.styles.key("tab", "switch panel"),
		m.styles.key("q", "quit"),
	}

	var pk []string
	switch m.activePanel {
	case panelFiles:
		pk = []string{
			m.styles.key("↑/↓", "navigate"),
			m.styles.key("space", "stage/unstage"),
			m.styles.key("a", "all"),
		}
		proceed := "write commit"
		if m.opts.AI != nil {
			proceed = "generate"
		}
		pk = append(pk, m.styles.key("↵", proceed))
	case panelEditor:
		if m.hasStaged() && !m.generating {
			pk = []string{
				m.styles.key("t", "type"),
				m.styles.key("s", "scope"),
				m.styles.key("d", "description"),
				m.styles.key("b", "body"),
				m.styles.key("f", "footers"),
				m.styles.key("!", "breaking"),
			}
			if m.opts.AI != nil {
				pk = append(pk, m.styles.key("r", "regenerate"))
			}
			pk = append(pk,
				m.styles.key("e", "edit all"),
				m.styles.key("c", "commit"),
			)
		}
	}

	return m.styles.footerBarSplit(global, pk)
}

// previewBox is kept for tests.
func (m Model) previewBox() string {
	msg := strings.TrimRight(m.draft.Render(), "\n")
	title := m.styles.previewT.Render("Commit preview")
	return title + "\n" + m.styles.preview.Render(msg)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// wrapClip word-wraps s to width w and clips to at most h lines.
func wrapClip(s string, w, h int) string {
	if w < 1 {
		w = 1
	}
	wrapped := lipgloss.NewStyle().Width(w).Render(s)
	lines := strings.Split(wrapped, "\n")
	if h > 0 && len(lines) > h {
		lines = lines[:h]
	}
	return strings.Join(lines, "\n")
}

// clipLine truncates a single line to w columns, adding an ellipsis if cut.
func clipLine(s string, w int) string {
	if w < 1 || lipgloss.Width(s) <= w {
		return s
	}
	if w <= 1 {
		return "…"
	}
	return lipgloss.NewStyle().MaxWidth(w-1).Render(s) + "…"
}


func stepLabel(s step) string {
	switch s {
	case stepBusy:
		return "working"
	case stepMain:
		return "commit"
	case stepReview:
		return "edit all"
	case stepEdit:
		return "edit"
	case stepPush:
		return "push"
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
