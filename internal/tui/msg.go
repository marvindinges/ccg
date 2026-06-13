package tui

import (
	"github.com/marvindinges/ccg/internal/commit"
	"github.com/marvindinges/ccg/internal/git"
)

// statusMsg carries the result of loading git status (and the current branch).
type statusMsg struct {
	files  []git.FileStatus
	branch string
}

// stagedMsg signals that selected files were staged and carries the diff.
type stagedMsg struct {
	diff string
}

// draftMsg carries an AI-generated commit draft.
type draftMsg struct {
	commit commit.Commit
}

// aiErrMsg signals AI generation failed; the flow degrades to manual editing.
type aiErrMsg struct {
	err error
}

// committedMsg signals the commit was created.
type committedMsg struct{}

// pushedMsg signals the push completed.
type pushedMsg struct {
	setUpstream bool
}

// errMsg is a fatal error that aborts the workflow.
type errMsg struct {
	err error
}

// animMsg drives the loading animation tick.
type animMsg struct{}

// countdownMsg ticks the abortable pre-commit / pre-push countdown (once a second).
type countdownMsg struct{}

// copiedMsg reports the result of copying the commit message to the clipboard.
type copiedMsg struct {
	err error
}
