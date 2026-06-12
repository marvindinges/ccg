package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"github.com/marvindinges/ccg/internal/ai"
	"github.com/marvindinges/ccg/internal/commit"
)

// loadStatus fetches the working-tree status.
func loadStatus(g gitRunner) tea.Cmd {
	return func() tea.Msg {
		files, err := g.Status()
		if err != nil {
			return errMsg{err}
		}
		return statusMsg{files: files}
	}
}

// reconcileStage unstages deselected files, stages the selected ones, then
// returns the resulting staged diff.
func reconcileStage(g gitRunner, toStage, toUnstage []string) tea.Cmd {
	return func() tea.Msg {
		if err := g.Unstage(toUnstage); err != nil {
			return errMsg{err}
		}
		if err := g.Stage(toStage); err != nil {
			return errMsg{err}
		}
		diff, err := g.StagedDiff()
		if err != nil {
			return errMsg{err}
		}
		return stagedMsg{diff: diff}
	}
}

// generate asks the AI for a commit draft. Failure is non-fatal (aiErrMsg).
func generate(c aiClient, in ai.SuggestInput) tea.Cmd {
	return func() tea.Msg {
		cm, err := c.Suggest(context.Background(), in)
		if err != nil {
			return aiErrMsg{err}
		}
		return draftMsg{commit: cm}
	}
}

// doCommit renders and creates the commit.
func doCommit(g gitRunner, c commit.Commit) tea.Cmd {
	return func() tea.Msg {
		if err := g.Commit(c.Render()); err != nil {
			return errMsg{err}
		}
		return committedMsg{}
	}
}

// doPush pushes the current branch, setting upstream when needed.
func doPush(g gitRunner) tea.Cmd {
	return func() tea.Msg {
		hasUpstream, err := g.HasUpstream()
		if err != nil {
			return errMsg{err}
		}
		setUpstream := !hasUpstream
		if err := g.Push(setUpstream); err != nil {
			return errMsg{err}
		}
		return pushedMsg{setUpstream: setUpstream}
	}
}
