package tui

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/marvindinges/ccg/internal/ai"
	"github.com/marvindinges/ccg/internal/commit"
	"github.com/marvindinges/ccg/internal/config"
	"github.com/marvindinges/ccg/internal/git"
)

type fakeGit struct {
	files       []git.FileStatus
	staged      []string
	unstaged    []string
	committed   string
	pushed      bool
	setUpstream bool
	hasUpstream bool
	diff        string
	commitErr   error
}

func (f *fakeGit) Status() ([]git.FileStatus, error) { return f.files, nil }
func (f *fakeGit) Stage(p []string) error            { f.staged = append(f.staged, p...); return nil }
func (f *fakeGit) Unstage(p []string) error          { f.unstaged = append(f.unstaged, p...); return nil }
func (f *fakeGit) StagedDiff() (string, error)       { return f.diff, nil }
func (f *fakeGit) HasStagedChanges() (bool, error)   { return f.diff != "", nil }
func (f *fakeGit) Commit(msg string) error           { f.committed = msg; return f.commitErr }
func (f *fakeGit) Push(set bool) error               { f.pushed = true; f.setUpstream = set; return nil }
func (f *fakeGit) HasUpstream() (bool, error)        { return f.hasUpstream, nil }
func (f *fakeGit) CurrentBranch() (string, error)    { return "main", nil }

type fakeAI struct {
	out commit.Commit
	err error
}

func (a fakeAI) Suggest(ctx context.Context, in ai.SuggestInput) (commit.Commit, error) {
	return a.out, a.err
}

func baseModel(g *fakeGit, a aiClient) Model {
	return New(Options{Cfg: config.Config{}, Git: g, AI: a})
}

// stagedModel returns a model in stepMain with one file staged (so hasStaged()
// is true and the Commit panel is interactive), focused on the editor.
func stagedModel(g *fakeGit, a aiClient) Model {
	m := baseModel(g, a)
	m.step = stepMain
	m.activePanel = panelEditor
	return markStaged(m)
}

// markStaged gives a model one staged file and a diff so hasStaged() is true.
func markStaged(m Model) Model {
	m.files = []git.FileStatus{{Path: "staged.go", Staged: 'M'}}
	m.filesSelected = map[string]bool{"staged.go": true}
	m.diff = "some diff"
	return m
}

func runCmd(cmd tea.Cmd) tea.Msg {
	if cmd == nil {
		return nil
	}
	return cmd()
}

func TestReconcileStageCmd(t *testing.T) {
	g := &fakeGit{diff: "some diff"}
	msg := runCmd(reconcileStage(g, []string{"a.txt"}, []string{"b.txt"}))
	if _, ok := msg.(stagedMsg); !ok {
		t.Fatalf("expected stagedMsg, got %T", msg)
	}
	if len(g.unstaged) != 1 || g.unstaged[0] != "b.txt" {
		t.Errorf("unstaged = %v, want [b.txt]", g.unstaged)
	}
	if len(g.staged) != 1 || g.staged[0] != "a.txt" {
		t.Errorf("staged = %v, want [a.txt]", g.staged)
	}
}

func TestDoCommitRendersMessage(t *testing.T) {
	g := &fakeGit{}
	c := commit.Commit{Type: "feat", Description: "do thing"}
	msg := runCmd(doCommit(g, c))
	if _, ok := msg.(committedMsg); !ok {
		t.Fatalf("expected committedMsg, got %T", msg)
	}
	if g.committed != "feat: do thing\n" {
		t.Errorf("committed = %q", g.committed)
	}
}

func TestDoCommitError(t *testing.T) {
	g := &fakeGit{commitErr: errors.New("boom")}
	msg := runCmd(doCommit(g, commit.Commit{Type: "fix", Description: "x"}))
	if _, ok := msg.(errMsg); !ok {
		t.Fatalf("expected errMsg, got %T", msg)
	}
}

func TestDoPushSetsUpstreamWhenMissing(t *testing.T) {
	g := &fakeGit{hasUpstream: false}
	msg := runCmd(doPush(g))
	pm, ok := msg.(pushedMsg)
	if !ok {
		t.Fatalf("expected pushedMsg, got %T", msg)
	}
	if !pm.setUpstream {
		t.Error("expected setUpstream=true when no upstream")
	}
}

func TestGenerateSuccessAndFailure(t *testing.T) {
	ok := runCmd(generate(fakeAI{out: commit.Commit{Type: "feat", Description: "x"}}, ai.SuggestInput{}))
	if _, isDraft := ok.(draftMsg); !isDraft {
		t.Errorf("expected draftMsg, got %T", ok)
	}
	bad := runCmd(generate(fakeAI{err: errors.New("nope")}, ai.SuggestInput{}))
	if _, isErr := bad.(aiErrMsg); !isErr {
		t.Errorf("expected aiErrMsg, got %T", bad)
	}
}

// statusMsg immediately enters the panel layout (stepMain).
func TestOnStatusEntersPanelLayout(t *testing.T) {
	g := &fakeGit{}
	m := baseModel(g, nil)
	out, _ := m.Update(statusMsg{files: []git.FileStatus{{Path: "a.txt", Untracked: true}}})
	got := out.(Model)
	if got.step != stepMain {
		t.Errorf("statusMsg should enter panel layout, got step=%v", got.step)
	}
	if got.form != nil {
		t.Error("panel layout has no full-screen form")
	}
}

func TestOnStatusEmpty(t *testing.T) {
	g := &fakeGit{}
	m := baseModel(g, nil)
	out, _ := m.Update(statusMsg{files: nil})
	got := out.(Model)
	if got.step != stepError || got.err == nil {
		t.Errorf("expected error step for no changes, got step=%v err=%v", got.step, got.err)
	}
}

func TestOnStatusPreselectsStaged(t *testing.T) {
	g := &fakeGit{}
	m := baseModel(g, nil)
	files := []git.FileStatus{
		{Path: "staged.go", Staged: 'M'},
		{Path: "unstaged.go", Unstaged: 'M'},
	}
	out, _ := m.Update(statusMsg{files: files})
	got := out.(Model)
	if !got.filesSelected["staged.go"] {
		t.Error("staged file should be pre-selected")
	}
	if got.filesSelected["unstaged.go"] {
		t.Error("unstaged file should not be pre-selected")
	}
}

// stagedMsg only refreshes the diff; it never changes focus or generates.
func TestStagedMsgUpdatesDiff(t *testing.T) {
	g := &fakeGit{}
	m := baseModel(g, nil)
	m.step = stepMain
	m.activePanel = panelFiles
	out, _ := m.Update(stagedMsg{diff: "some diff"})
	got := out.(Model)
	if got.diff != "some diff" {
		t.Errorf("stagedMsg should set the diff, got %q", got.diff)
	}
	if got.generating {
		t.Error("stagedMsg should not start generation")
	}
	if got.activePanel != panelFiles {
		t.Error("stagedMsg should not change focus")
	}
}

// Pressing enter in the Files panel (with something staged) and AI configured
// pops the hint modal.
func TestFilesEnterWithAIOpensHintModal(t *testing.T) {
	g := &fakeGit{}
	m := baseModel(g, fakeAI{})
	m.step = stepMain
	m.activePanel = panelFiles
	m.files = []git.FileStatus{{Path: "a.go", Staged: 'M'}}
	m.filesSelected = map[string]bool{"a.go": true}
	m.diff = "some diff"
	out, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	got := out.(Model)
	if got.step != stepEdit || got.editField != keyHint || got.form == nil {
		t.Errorf("enter should open the hint modal, got step=%v field=%q", got.step, got.editField)
	}
	if cmd == nil {
		t.Error("expected the form's Init command")
	}
}

// Submitting the hint modal runs generation.
func TestHintModalSubmitGenerates(t *testing.T) {
	m := stagedModel(&fakeGit{}, fakeAI{})
	mm, _ := m.openFieldEdit(keyHint)
	got := mm.(Model)
	// Simulate the form completing (completeEdit reads the bound hint value).
	out, cmd := got.completeEdit()
	res := out.(Model)
	if !res.generating || cmd == nil {
		t.Errorf("submitting hint should start generation, generating=%v", res.generating)
	}
	if res.step != stepMain {
		t.Errorf("after submit step should be stepMain (generating), got %v", res.step)
	}
	if res.activePanel != panelEditor {
		t.Error("generation should focus the Commit panel for its spinner")
	}
}

// Regenerate (r) in the editor also pops the hint modal first.
func TestEditorRegenerateOpensHintModal(t *testing.T) {
	m := stagedModel(&fakeGit{}, fakeAI{})
	out, _ := m.Update(tea.KeyPressMsg{Code: 'r'})
	got := out.(Model)
	if got.step != stepEdit || got.editField != keyHint {
		t.Errorf("r should open the hint modal, got step=%v field=%q", got.step, got.editField)
	}
}

// Toggling a file with space stages it (and applies it to git) immediately.
func TestFilesSpaceStagesImmediately(t *testing.T) {
	g := &fakeGit{diff: "d"}
	m := baseModel(g, nil)
	m.step = stepMain
	m.activePanel = panelFiles
	m.files = []git.FileStatus{{Path: "a.go", Untracked: true}}
	m.filesSelected = map[string]bool{"a.go": false}
	out, cmd := m.Update(tea.KeyPressMsg{Code: ' '})
	got := out.(Model)
	if !got.filesSelected["a.go"] {
		t.Error("space should mark the file staged")
	}
	if cmd == nil {
		t.Fatal("space should run a git staging command")
	}
	if _, ok := runCmd(cmd).(stagedMsg); !ok {
		t.Error("staging command should return a stagedMsg with the new diff")
	}
	if len(g.staged) != 1 || g.staged[0] != "a.go" {
		t.Errorf("a.go should have been git-staged, got %v", g.staged)
	}
}

// The Files panel stays editable after staging: you can unstage a file later.
func TestFilesRemainEditableAfterStaging(t *testing.T) {
	g := &fakeGit{diff: "d"}
	m := baseModel(g, nil)
	m.step = stepMain
	m.activePanel = panelFiles
	m.files = []git.FileStatus{{Path: "a.go", Staged: 'M'}}
	m.filesSelected = map[string]bool{"a.go": true}
	// Unstage it again with space.
	out, cmd := m.Update(tea.KeyPressMsg{Code: ' '})
	got := out.(Model)
	if got.filesSelected["a.go"] {
		t.Error("space should unstage a previously staged file")
	}
	if _, ok := runCmd(cmd).(stagedMsg); !ok {
		t.Error("unstaging should run a git command returning stagedMsg")
	}
	if len(g.unstaged) != 1 || g.unstaged[0] != "a.go" {
		t.Errorf("a.go should have been unstaged, got %v", g.unstaged)
	}
}

// draftMsg stops generating and stores the draft.
func TestDraftMsgStopsGenerating(t *testing.T) {
	g := &fakeGit{}
	m := baseModel(g, fakeAI{})
	m.step = stepMain
	m.generating = true
	out, _ := m.Update(draftMsg{commit: commit.Commit{Type: "feat", Description: "x"}})
	got := out.(Model)
	if got.generating {
		t.Error("generating should stop after draftMsg")
	}
	if got.draft.Type != "feat" {
		t.Errorf("draft not stored, got type=%q", got.draft.Type)
	}
}

// Files panel: j/k moves cursor, space toggles, enter confirms staging.
func TestFilesPanelNavigation(t *testing.T) {
	g := &fakeGit{}
	m := baseModel(g, nil)
	m.step = stepMain
	m.activePanel = panelFiles
	m.files = []git.FileStatus{{Path: "a.go"}, {Path: "b.go"}}
	m.filesSelected = map[string]bool{"a.go": true, "b.go": false}

	// j moves cursor down
	out, _ := m.Update(tea.KeyPressMsg{Code: 'j'})
	if out.(Model).filesCursor != 1 {
		t.Errorf("j should move cursor to 1, got %d", out.(Model).filesCursor)
	}

	// k moves cursor back up
	out2, _ := out.(Model).Update(tea.KeyPressMsg{Code: 'k'})
	if out2.(Model).filesCursor != 0 {
		t.Errorf("k should move cursor to 0, got %d", out2.(Model).filesCursor)
	}
}

func TestFilesPanelSpaceToggles(t *testing.T) {
	g := &fakeGit{}
	m := baseModel(g, nil)
	m.step = stepMain
	m.activePanel = panelFiles
	m.files = []git.FileStatus{{Path: "a.go"}}
	m.filesSelected = map[string]bool{"a.go": false}
	m.filesCursor = 0

	out, _ := m.Update(tea.KeyPressMsg{Code: ' '})
	if !out.(Model).filesSelected["a.go"] {
		t.Error("space should toggle file on")
	}
	out2, _ := out.(Model).Update(tea.KeyPressMsg{Code: ' '})
	if out2.(Model).filesSelected["a.go"] {
		t.Error("space again should toggle file off")
	}
}

// Without AI, enter in the Files panel just moves focus to the Commit panel
// (staging already happened incrementally via space).
func TestFilesPanelEnterFocusesEditor(t *testing.T) {
	g := &fakeGit{diff: "diff"}
	m := baseModel(g, nil)
	m.step = stepMain
	m.activePanel = panelFiles
	m.files = []git.FileStatus{{Path: "a.go", Staged: 'M'}}
	m.filesSelected = map[string]bool{"a.go": true}

	out, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	got := out.(Model)
	if got.step != stepMain || got.activePanel != panelEditor {
		t.Errorf("enter should focus the Commit panel, got step=%v panel=%v", got.step, got.activePanel)
	}
	if got.generating {
		t.Error("enter without AI should not generate")
	}
}

func TestFilesPanelEnterRequiresSelection(t *testing.T) {
	g := &fakeGit{}
	m := baseModel(g, nil)
	m.step = stepMain
	m.activePanel = panelFiles
	m.files = []git.FileStatus{{Path: "a.go"}}
	m.filesSelected = map[string]bool{"a.go": false}

	out, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	got := out.(Model)
	if got.notice == "" {
		t.Error("enter with no selection should set notice")
	}
}

// Editor panel keybindings.
func TestEditorToggleBreaking(t *testing.T) {
	g := &fakeGit{}
	m := baseModel(g, nil)
	m.step = stepMain
	m.activePanel = panelEditor
	m = markStaged(m)
	m.draft = commit.Commit{Type: "feat", Description: "x"}
	out, _ := m.Update(tea.KeyPressMsg{Code: '!'})
	if !out.(Model).draft.Breaking {
		t.Error("'!' should toggle breaking on")
	}
}

func TestEditorQuickKeyOpensFieldForm(t *testing.T) {
	g := &fakeGit{}
	m := baseModel(g, nil)
	m.step = stepMain
	m.activePanel = panelEditor
	m = markStaged(m)
	m.draft = commit.Commit{Type: "feat", Description: "x"}
	out, _ := m.Update(tea.KeyPressMsg{Code: 'd'})
	got := out.(Model)
	if got.step != stepEdit || got.editField != keyDesc || got.form == nil {
		t.Errorf("'d' should open description edit, got step=%v field=%q", got.step, got.editField)
	}
}

// Both c and enter in the Commit panel start the commit flow by asking whether
// to push (the push decision comes before anything runs).
func TestEditorCommitKeysOpenPushModal(t *testing.T) {
	for _, code := range []rune{'c', tea.KeyEnter} {
		m := stagedModel(&fakeGit{}, nil)
		m.draft = commit.Commit{Type: "feat", Description: "do thing"}
		out, cmd := m.Update(tea.KeyPressMsg{Code: code})
		got := out.(Model)
		if got.step != stepPush || got.form == nil || cmd == nil {
			t.Errorf("key %q should open the push modal, got step=%v", string(code), got.step)
		}
	}
}

func TestEditorCopyKey(t *testing.T) {
	m := stagedModel(&fakeGit{}, nil)
	m.draft = commit.Commit{Type: "feat", Description: "do thing"}
	out, cmd := m.Update(tea.KeyPressMsg{Code: 'y'})
	if out.(Model).step != stepMain || cmd == nil {
		t.Error("'y' should run a copy command and stay on the panels")
	}
	if _, ok := runCmd(cmd).(copiedMsg); !ok {
		t.Error("'y' should produce a copiedMsg")
	}
}

func TestGitCommitCommand(t *testing.T) {
	c := commit.Commit{Type: "feat", Scope: "ui", Description: "it's done"}
	got := gitCommitCommand(c)
	// Whole command, not just the message.
	if !strings.HasPrefix(got, "git commit -m '") {
		t.Errorf("expected a git commit command, got %q", got)
	}
	if !strings.Contains(got, "feat(ui): it'\\''s done") {
		t.Errorf("single quote not escaped for the shell: %q", got)
	}
}

func TestEditorCommitBlockedWhenInvalid(t *testing.T) {
	g := &fakeGit{}
	m := baseModel(g, nil)
	m.step = stepMain
	m.activePanel = panelEditor
	m = markStaged(m)
	m.draft = commit.Commit{} // no type/description
	out, _ := m.Update(tea.KeyPressMsg{Code: 'c'})
	got := out.(Model)
	if got.step != stepMain || got.notice == "" {
		t.Errorf("invalid draft should stay with notice, got step=%v", got.step)
	}
}

func TestEditorRegenerateNeedsAI(t *testing.T) {
	g := &fakeGit{}
	// With AI, r opens the hint modal (which then generates on submit).
	m := baseModel(g, fakeAI{})
	m.step = stepMain
	m.activePanel = panelEditor
	m = markStaged(m)
	m.diff = "some diff"
	out, _ := m.Update(tea.KeyPressMsg{Code: 'r'})
	got := out.(Model)
	if got.step != stepEdit || got.editField != keyHint {
		t.Errorf("'r' with AI should open the hint modal, got step=%v field=%q", got.step, got.editField)
	}

	// Without AI, r is a no-op (stays on the panels).
	m2 := baseModel(g, nil)
	m2.step = stepMain
	m2.activePanel = panelEditor
	m2 = markStaged(m2)
	out2, _ := m2.Update(tea.KeyPressMsg{Code: 'r'})
	res2 := out2.(Model)
	if res2.generating || res2.step != stepMain {
		t.Error("'r' without AI should do nothing")
	}
}

func TestEditorNotInteractiveBeforeStaging(t *testing.T) {
	g := &fakeGit{}
	m := baseModel(g, nil)
	m.step = stepMain
	m.activePanel = panelEditor
	m.files = []git.FileStatus{{Path: "a.go", Untracked: true}}
	m.filesSelected = map[string]bool{"a.go": false} // nothing staged
	m.draft = commit.Commit{Type: "feat", Description: "x"}
	// 'c' should be a no-op while nothing is staged
	out, cmd := m.Update(tea.KeyPressMsg{Code: 'c'})
	got := out.(Model)
	if got.step != stepMain || cmd != nil {
		t.Error("editor should be inactive while nothing is staged")
	}
}

func TestAIErrorShowsOverlayThenResumes(t *testing.T) {
	g := &fakeGit{}
	m := baseModel(g, fakeAI{})
	m.step = stepMain
	out, _ := m.Update(aiErrMsg{err: errors.New("provider returned 400: very long message")})
	got := out.(Model)
	if got.step != stepModal || got.modalText == "" {
		t.Fatalf("AI error should show overlay, got step=%v modalText=%q", got.step, got.modalText)
	}
	if !strings.Contains(got.View().Content, "very long message") {
		t.Error("overlay should render the error text")
	}
	out2, _ := got.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if out2.(Model).step != stepMain {
		t.Errorf("dismissing overlay should return to panel layout, got %v", out2.(Model).step)
	}
}

// onCommitted pushes when that was chosen, else finishes.
func TestOnCommittedPushesWhenChosen(t *testing.T) {
	g := &fakeGit{}
	push := stagedModel(g, nil)
	push.willPush = true
	out, _ := push.Update(committedMsg{})
	if out.(Model).step != stepBusy {
		t.Errorf("willPush=true should push (busy), got step=%v", out.(Model).step)
	}

	noPush := stagedModel(g, nil)
	noPush.willPush = false
	out2, _ := noPush.Update(committedMsg{})
	if got := out2.(Model); !got.committed || got.step != stepDone {
		t.Errorf("willPush=false should finish, got step=%v", got.step)
	}
}

// --no-push skips the push modal and goes straight to the countdown (no push).
func TestCommitNoPushSkipsModal(t *testing.T) {
	g := &fakeGit{}
	m := New(Options{Cfg: config.Config{}, Git: g, NoPush: true})
	m.step, m.activePanel = stepMain, panelEditor
	m = markStaged(m)
	m.draft = commit.Commit{Type: "feat", Description: "x"}
	out, _ := m.Update(tea.KeyPressMsg{Code: 'c'})
	got := out.(Model)
	if got.step != stepCountdown || got.willPush {
		t.Errorf("--no-push should count down without pushing, got step=%v willPush=%v", got.step, got.willPush)
	}
}

// --push skips the modal and counts down with push intent.
func TestCommitAutoPushSkipsModal(t *testing.T) {
	g := &fakeGit{}
	m := New(Options{Cfg: config.Config{}, Git: g, AutoPush: true})
	m.step, m.activePanel = stepMain, panelEditor
	m = markStaged(m)
	m.draft = commit.Commit{Type: "feat", Description: "x"}
	out, _ := m.Update(tea.KeyPressMsg{Code: 'c'})
	got := out.(Model)
	if got.step != stepCountdown || !got.willPush {
		t.Errorf("--push should count down with push intent, got step=%v willPush=%v", got.step, got.willPush)
	}
}

// Answering the push modal records the choice and starts the countdown.
func TestPushModalThenCountdown(t *testing.T) {
	m := stagedModel(&fakeGit{}, nil)
	m.draft = commit.Commit{Type: "feat", Description: "x"}
	out, _ := m.Update(tea.KeyPressMsg{Code: 'c'}) // -> push modal
	got := out.(Model)
	// Decline (form's confirm defaults to false) and complete the form.
	done, _ := got.onFormComplete()
	res := done.(Model)
	if res.step != stepCountdown || res.willPush || res.countdownN != config.DefaultCountdownSeconds {
		t.Errorf("answering push should start the countdown, got step=%v willPush=%v n=%d", res.step, res.willPush, res.countdownN)
	}
}

// countdown_seconds: 0 commits immediately after the push decision.
func TestCountdownZeroIsImmediate(t *testing.T) {
	zero := 0
	g := &fakeGit{}
	m := New(Options{Cfg: config.Config{Countdown: &zero}, Git: g, NoPush: true})
	m.step, m.activePanel = stepMain, panelEditor
	m = markStaged(m)
	m.draft = commit.Commit{Type: "feat", Description: "x"}
	out, cmd := m.Update(tea.KeyPressMsg{Code: 'c'})
	got := out.(Model)
	if got.step != stepBusy || cmd == nil {
		t.Errorf("countdown 0 should commit immediately (busy), got step=%v", got.step)
	}
}

// The countdown commits when it reaches zero.
func TestCountdownFiresAtZero(t *testing.T) {
	m := stagedModel(&fakeGit{}, nil)
	m.draft = commit.Commit{Type: "feat", Description: "do thing"}
	m.step = stepCountdown
	m.countdownN = 1
	out, cmd := m.Update(countdownMsg{})
	got := out.(Model)
	if got.step != stepBusy || cmd == nil {
		t.Errorf("countdown reaching zero should commit (busy), got step=%v", got.step)
	}
}

// esc during the commit countdown returns to the panels without committing.
func TestCountdownEscCancelsCommit(t *testing.T) {
	g := &fakeGit{}
	m := stagedModel(g, nil)
	m.step = stepCountdown
	m.countdownN = 3
	out, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	got := out.(Model)
	if got.step != stepMain || got.aborted {
		t.Errorf("esc should cancel the commit back to panels, got step=%v aborted=%v", got.step, got.aborted)
	}
	if g.committed != "" {
		t.Error("nothing should have been committed")
	}
}

func TestCtrlCAborts(t *testing.T) {
	g := &fakeGit{}
	m := baseModel(g, nil)
	out, _ := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if !out.(Model).aborted {
		t.Error("ctrl+c should abort")
	}
}

func TestQQuitsFromAnyPanel(t *testing.T) {
	for _, p := range []panel{panelFiles, panelEditor} {
		m := baseModel(&fakeGit{}, fakeAI{})
		m.step = stepMain
		m.activePanel = p
		out, cmd := m.Update(tea.KeyPressMsg{Code: 'q'})
		if !out.(Model).aborted || cmd == nil {
			t.Errorf("q should quit from panel %v", p)
		}
	}
}

// Navigation works with both j/k and the arrow keys; the footer only displays
// the arrows.
func TestFilesPanelArrowAndViNav(t *testing.T) {
	mk := func() Model {
		m := baseModel(&fakeGit{}, nil)
		m.step = stepMain
		m.activePanel = panelFiles
		m.files = []git.FileStatus{{Path: "a.go"}, {Path: "b.go"}}
		m.filesSelected = map[string]bool{}
		return m
	}
	// j moves down
	if out, _ := mk().Update(tea.KeyPressMsg{Code: 'j'}); out.(Model).filesCursor != 1 {
		t.Error("j should move cursor down")
	}
	// down arrow moves down
	if out, _ := mk().Update(tea.KeyPressMsg{Code: tea.KeyDown}); out.(Model).filesCursor != 1 {
		t.Error("down arrow should move cursor down")
	}

	footer := stripANSI(mk().mainFooter())
	if !strings.Contains(footer, "↑/↓") {
		t.Errorf("footer should display arrow keys for navigation, got %q", footer)
	}
	if strings.Contains(footer, "j/k") {
		t.Error("footer should not display j/k")
	}
}

func TestFooterShowsQQuit(t *testing.T) {
	m := baseModel(&fakeGit{}, nil)
	m.activePanel = panelFiles
	footer := stripANSI(m.mainFooter())
	if !strings.Contains(footer, "[q]") || !strings.Contains(footer, "quit") {
		t.Errorf("footer should show [q] quit, got %q", footer)
	}
}

func TestAnimAdvancesWhileGenerating(t *testing.T) {
	g := &fakeGit{}
	m := baseModel(g, nil)
	m.step = stepMain
	m.generating = true
	out, cmd := m.Update(animMsg{})
	got := out.(Model)
	if got.frame != 1 {
		t.Errorf("frame should advance while generating, got %d", got.frame)
	}
	if cmd == nil {
		t.Error("expected follow-up tick while generating")
	}
}

func TestAnimStopsWhenIdle(t *testing.T) {
	g := &fakeGit{}
	m := baseModel(g, nil)
	m.step = stepMain
	m.generating = false
	out, cmd := m.Update(animMsg{})
	if out.(Model).frame != 0 {
		t.Errorf("frame should not advance when not loading, got %d", out.(Model).frame)
	}
	if cmd != nil {
		t.Error("tick loop should stop when idle")
	}
}

func TestLoadingViewAnimates(t *testing.T) {
	s := newStyles(parseColor("bright-blue"), parseColor("bright-magenta"))
	a := s.loading(0, "Working")
	b := s.loading(1, "Working")
	if a == b {
		t.Error("loading frames 0 and 1 should differ")
	}
	if !strings.Contains(stripANSI(a), "Working") {
		t.Errorf("loading should contain the label, got %q", stripANSI(a))
	}
}

func TestViewDoesNotPanicAcrossSteps(t *testing.T) {
	g := &fakeGit{}
	m := baseModel(g, fakeAI{})
	m.width = 120
	m.height = 40
	m.draft = commit.Commit{Type: "feat", Scope: "ui", Description: "x"}
	m.form = m.styleForm(newReviewForm(m.draft, commit.DefaultTypes()))
	for _, s := range []step{stepBusy, stepMain, stepEdit, stepReview, stepCountdown, stepDone, stepError} {
		m.step = s
		if s == stepError {
			m.err = errTest
		}
		v := m.View()
		if v.Content == "" && s != stepError {
			t.Errorf("step %v produced empty view", s)
		}
	}
}

func TestFooterVariesByAI(t *testing.T) {
	withAI := baseModel(&fakeGit{}, fakeAI{})
	withAI.activePanel = panelEditor
	withAI = markStaged(withAI)
	if !strings.Contains(stripANSI(withAI.mainFooter()), "regenerate") {
		t.Error("AI footer should offer regenerate")
	}
	noAI := baseModel(&fakeGit{}, nil)
	noAI.activePanel = panelEditor
	noAI = markStaged(noAI)
	if strings.Contains(stripANSI(noAI.mainFooter()), "regenerate") {
		t.Error("non-AI footer should not offer regenerate")
	}
}

func TestFooterSplitSections(t *testing.T) {
	s := newStyles(parseColor("bright-blue"), parseColor("bright-magenta"))
	footer := stripANSI(s.footerBarSplit(
		[]string{s.key("tab", "switch")},
		[]string{s.key("c", "commit")},
	))
	if !strings.Contains(footer, "tab") || !strings.Contains(footer, "commit") {
		t.Errorf("footer should contain both sections, got %q", footer)
	}
	if !strings.Contains(footer, "│") {
		t.Error("footer should contain │ divider between sections")
	}
}

func TestEditorPreviewPlaceholder(t *testing.T) {
	m := baseModel(&fakeGit{}, nil)

	// Empty draft → skeleton with placeholders.
	got, placeholder := m.editorPreview()
	if !placeholder || got != "TYPE(SCOPE): DESCRIPTION" {
		t.Errorf("empty draft preview = %q placeholder=%v", got, placeholder)
	}

	// Type only → keeps placeholders for the rest.
	m.draft = commit.Commit{Type: "feat"}
	got, placeholder = m.editorPreview()
	if !placeholder || got != "feat(SCOPE): DESCRIPTION" {
		t.Errorf("partial preview = %q placeholder=%v", got, placeholder)
	}

	// Complete draft → real rendered message, no placeholder.
	m.draft = commit.Commit{Type: "feat", Scope: "ui", Description: "add panels"}
	got, placeholder = m.editorPreview()
	if placeholder || got != "feat(ui): add panels" {
		t.Errorf("complete preview = %q placeholder=%v", got, placeholder)
	}
}

func TestMainViewHasNoHeaderBadge(t *testing.T) {
	m := baseModel(&fakeGit{}, nil)
	m.width, m.height = 80, 24
	m.files = []git.FileStatus{{Path: "a.go", Staged: 'M'}}
	m.filesSelected = map[string]bool{"a.go": true}
	m.step = stepMain
	content := stripANSI(m.viewMain())
	if strings.Contains(content, "ccg") {
		t.Error("main view should no longer show the ccg header badge")
	}
}

func TestEditOpensModalOverPanels(t *testing.T) {
	m := stagedModel(&fakeGit{}, nil)
	m.width, m.height = 80, 24
	m.draft = commit.Commit{Type: "feat", Description: "x"}

	out, _ := m.Update(tea.KeyPressMsg{Code: 'd'})
	got := out.(Model)
	if got.step != stepEdit {
		t.Fatalf("expected stepEdit, got %v", got.step)
	}
	content := stripANSI(got.View().Content)
	// The popup title is visible…
	if !strings.Contains(content, "Edit description") {
		t.Error("modal should show the edit title")
	}
	// …and the panels remain visible behind it.
	if !strings.Contains(content, "Files") {
		t.Error("panels should remain visible behind the popup modal")
	}
	// …and esc is advertised as the abort key.
	if !strings.Contains(content, "esc") || !strings.Contains(content, "cancel") {
		t.Error("modal should show the [esc] cancel hint")
	}
}

func TestPreviewBoxShowsHeader(t *testing.T) {
	g := &fakeGit{}
	m := baseModel(g, nil)
	m.draft = commit.Commit{Type: "fix", Scope: "auth", Description: "handle nil"}
	out := m.previewBox()
	if !strings.Contains(out, "fix(auth): handle nil") {
		t.Errorf("preview missing header, got:\n%s", out)
	}
}

func TestPanelBoxRendersTitle(t *testing.T) {
	s := newStyles(parseColor("bright-blue"), parseColor("bright-magenta"))
	box := stripANSI(s.panelBox("Files", "content line", 30, 5, false))
	if !strings.Contains(box, "Files") {
		t.Errorf("panel box should contain title, got:\n%s", box)
	}
	if !strings.Contains(box, "╭") || !strings.Contains(box, "╯") {
		t.Errorf("panel box should contain rounded box-drawing chars, got:\n%s", box)
	}
}

func TestPanelBoxActiveBorderDiffers(t *testing.T) {
	s := newStyles(parseColor("bright-blue"), parseColor("bright-magenta"))
	active := s.panelBox("Test", "line", 30, 5, true)
	inactive := s.panelBox("Test", "line", 30, 5, false)
	if stripANSI(active) != stripANSI(inactive) {
		t.Error("active/inactive boxes should have the same content sans ANSI")
	}
	if active == inactive {
		t.Error("active and inactive panel boxes should differ in ANSI styling")
	}
}

func TestStepLabel(t *testing.T) {
	if stepLabel(stepMain) != "commit" {
		t.Errorf("unexpected label: %q", stepLabel(stepMain))
	}
	if stepLabel(step(999)) != "" {
		t.Errorf("unknown step should be empty label")
	}
}

func TestFormWidthClamp(t *testing.T) {
	if formWidth(10) != 40 {
		t.Errorf("min clamp: got %d", formWidth(10))
	}
	if formWidth(500) != 96 {
		t.Errorf("max clamp: got %d", formWidth(500))
	}
	if w := formWidth(80); w != 78 {
		t.Errorf("normal: got %d", w)
	}
}

func TestDisplayPathRename(t *testing.T) {
	f := git.FileStatus{Path: "new.txt", OrigPath: "old.txt"}
	if displayPath(f) != "old.txt -> new.txt" {
		t.Errorf("got %q", displayPath(f))
	}
	if displayPath(git.FileStatus{Path: "a.txt"}) != "a.txt" {
		t.Errorf("plain path failed")
	}
}

func TestVisiblePanelsAlwaysTwo(t *testing.T) {
	for _, ai := range []aiClient{fakeAI{}, nil} {
		m := baseModel(&fakeGit{}, ai)
		vis := m.visiblePanels()
		if len(vis) != 2 || vis[0] != panelFiles || vis[1] != panelEditor {
			t.Errorf("expected Files/Editor, got %v (ai=%v)", vis, ai != nil)
		}
	}
}

func TestCyclePanel(t *testing.T) {
	m := baseModel(&fakeGit{}, fakeAI{})
	m.step = stepMain
	m.activePanel = panelFiles
	out, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if out.(Model).activePanel != panelEditor {
		t.Errorf("tab from Files should go to Editor, got %v", out.(Model).activePanel)
	}
	// Wraps back to Files.
	out2, _ := out.(Model).Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if out2.(Model).activePanel != panelFiles {
		t.Errorf("tab from Editor should wrap to Files, got %v", out2.(Model).activePanel)
	}
}

// esc closes an open edit popup and returns to the panels without quitting.
func TestEscCancelsModal(t *testing.T) {
	m := baseModel(&fakeGit{}, nil)
	m.step = stepMain
	m.activePanel = panelEditor
	m = markStaged(m)
	m.draft = commit.Commit{Type: "feat", Description: "x"}
	// Open the description editor.
	mm, _ := m.Update(tea.KeyPressMsg{Code: 'd'})
	got := mm.(Model)
	if got.step != stepEdit {
		t.Fatalf("expected stepEdit, got %v", got.step)
	}
	// esc cancels back to the panels.
	out, _ := got.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	res := out.(Model)
	if res.step != stepMain {
		t.Errorf("esc should return to stepMain, got %v", res.step)
	}
	if res.aborted {
		t.Error("esc should NOT quit the app")
	}
	if res.form != nil {
		t.Error("form should be cleared after cancel")
	}
}

func TestOnStagedEmptyDiffStaysOnPanels(t *testing.T) {
	g := &fakeGit{}
	m := baseModel(g, nil)
	m.step = stepMain
	out, _ := m.Update(stagedMsg{diff: ""})
	got := out.(Model)
	// An empty diff is normal (nothing staged): stay on the panels, no error.
	if got.step != stepMain || got.err != nil {
		t.Errorf("empty diff should stay on panels without error, got step=%v err=%v", got.step, got.err)
	}
}

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

func stripANSI(s string) string { return ansiRe.ReplaceAllString(s, "") }

var errTest = errTestType("boom")

type errTestType string

func (e errTestType) Error() string { return string(e) }

func TestParseFootersRoundTrip(t *testing.T) {
	in := []commit.Footer{{Token: "Refs", Value: "#1"}, {Token: "Reviewed-by", Value: "Z"}}
	text := footersToText(in)
	out := parseFooters(text)
	if len(out) != 2 || out[0].Token != "Refs" || out[1].Value != "Z" {
		t.Errorf("footers round-trip failed: %+v", out)
	}
	if got := parseFooters("not a footer\nRefs: #2"); len(got) != 1 {
		t.Errorf("expected 1 footer, got %+v", got)
	}
}
