package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/marvindinges/ccg/internal/ai"
	"github.com/marvindinges/ccg/internal/commit"
	"github.com/marvindinges/ccg/internal/config"
	"github.com/marvindinges/ccg/internal/git"
)

// fakeGit records calls and returns canned values.
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

func TestOnStatusEmpty(t *testing.T) {
	g := &fakeGit{files: nil}
	m := baseModel(g, nil)
	out, _ := m.Update(statusMsg{files: nil})
	got := out.(Model)
	if got.step != stepError || got.err == nil {
		t.Errorf("expected error step for no changes, got step=%v err=%v", got.step, got.err)
	}
}

func TestOnStatusBuildsStageForm(t *testing.T) {
	g := &fakeGit{}
	m := baseModel(g, nil)
	out, _ := m.Update(statusMsg{files: []git.FileStatus{{Path: "a.txt", Untracked: true}}})
	got := out.(Model)
	if got.step != stepStage || got.form == nil {
		t.Errorf("expected stepStage with form, got step=%v form=%v", got.step, got.form != nil)
	}
}

func TestOnStagedManualGoesToReview(t *testing.T) {
	g := &fakeGit{}
	m := baseModel(g, nil) // no AI
	out, _ := m.Update(stagedMsg{diff: "diff"})
	got := out.(Model)
	if got.step != stepReview || got.form == nil {
		t.Errorf("manual mode should go to review, got step=%v", got.step)
	}
}

func TestOnStagedWithAIGoesToHint(t *testing.T) {
	g := &fakeGit{}
	m := baseModel(g, fakeAI{})
	out, _ := m.Update(stagedMsg{diff: "diff"})
	got := out.(Model)
	if got.step != stepHint {
		t.Errorf("AI mode should go to hint, got step=%v", got.step)
	}
}

func TestOnStagedWithAIAndPresetHintSkipsToGenerate(t *testing.T) {
	g := &fakeGit{}
	m := New(Options{Cfg: config.Config{}, Git: g, AI: fakeAI{}, Hint: "fix the bug"})
	out, _ := m.Update(stagedMsg{diff: "diff"})
	got := out.(Model)
	if got.step != stepGenerate {
		t.Errorf("preset hint should skip to generate, got step=%v", got.step)
	}
}

func TestDraftMsgEntersReview(t *testing.T) {
	g := &fakeGit{}
	m := baseModel(g, fakeAI{})
	out, _ := m.Update(draftMsg{commit: commit.Commit{Type: "feat", Description: "x"}})
	got := out.(Model)
	if got.step != stepReview || got.draft.Type != "feat" {
		t.Errorf("draft should enter review, got step=%v draft=%+v", got.step, got.draft)
	}
}

func TestAIErrorFallsBackToReview(t *testing.T) {
	g := &fakeGit{}
	m := baseModel(g, fakeAI{})
	out, _ := m.Update(aiErrMsg{err: errors.New("down")})
	got := out.(Model)
	if got.step != stepReview || got.notice == "" {
		t.Errorf("AI error should fall back to review with notice, got step=%v notice=%q", got.step, got.notice)
	}
}

func TestOnCommittedNoPushFinishes(t *testing.T) {
	g := &fakeGit{}
	m := New(Options{Cfg: config.Config{}, Git: g, NoPush: true})
	out, cmd := m.Update(committedMsg{})
	got := out.(Model)
	if !got.committed || got.step != stepDone {
		t.Errorf("expected done after commit with NoPush, got step=%v", got.step)
	}
	_ = cmd
}

func TestOnCommittedAutoPush(t *testing.T) {
	g := &fakeGit{}
	m := New(Options{Cfg: config.Config{}, Git: g, AutoPush: true})
	out, _ := m.Update(committedMsg{})
	got := out.(Model)
	if got.step != stepBusy {
		t.Errorf("expected busy(push) after commit with AutoPush, got step=%v", got.step)
	}
}

func TestOnCommittedPromptsPush(t *testing.T) {
	g := &fakeGit{}
	m := baseModel(g, nil)
	out, _ := m.Update(committedMsg{})
	got := out.(Model)
	if got.step != stepPush || got.form == nil {
		t.Errorf("expected push prompt, got step=%v", got.step)
	}
}

func TestCtrlCAborts(t *testing.T) {
	g := &fakeGit{}
	m := baseModel(g, nil)
	out, _ := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	got := out.(Model)
	if !got.aborted {
		t.Errorf("ctrl+c should abort")
	}
}

func TestViewDoesNotPanicAcrossSteps(t *testing.T) {
	g := &fakeGit{}
	m := baseModel(g, nil)
	m.draft = commit.Commit{Type: "feat", Scope: "ui", Description: "x"}
	m.form = styleForm(newConfirmForm(false), 80)
	for _, s := range []step{stepStage, stepHint, stepGenerate, stepReview, stepConfirm, stepPush, stepBusy, stepDone, stepError} {
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

func TestPreviewBoxShowsHeader(t *testing.T) {
	g := &fakeGit{}
	m := baseModel(g, nil)
	m.draft = commit.Commit{Type: "fix", Scope: "auth", Description: "handle nil"}
	out := m.previewBox()
	if !strings.Contains(out, "fix(auth): handle nil") {
		t.Errorf("preview missing header, got:\n%s", out)
	}
}

func TestStepLabels(t *testing.T) {
	if stepLabel(stepReview) != "review & edit" {
		t.Errorf("unexpected label: %q", stepLabel(stepReview))
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

func TestOnStagedEmptyDiffErrors(t *testing.T) {
	g := &fakeGit{}
	m := baseModel(g, nil)
	out, _ := m.Update(stagedMsg{diff: "   "})
	got := out.(Model)
	if got.step != stepError {
		t.Errorf("empty diff should error, got step=%v", got.step)
	}
}

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
	// junk lines ignored
	if got := parseFooters("not a footer\nRefs: #2"); len(got) != 1 {
		t.Errorf("expected 1 footer, got %+v", got)
	}
}
