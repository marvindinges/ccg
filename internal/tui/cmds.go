package tui

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/marvindinges/ccg/internal/ai"
	"github.com/marvindinges/ccg/internal/clip"
	"github.com/marvindinges/ccg/internal/commit"
)

// isSSHAuthError reports whether err looks like an SSH public-key auth failure.
// When Push runs with BatchMode=yes, a missing/locked key produces this error
// instead of hanging indefinitely waiting for a passphrase prompt.
func isSSHAuthError(err error) bool {
	s := err.Error()
	return strings.Contains(s, "Permission denied") &&
		(strings.Contains(s, "publickey") || strings.Contains(s, "public key"))
}

// animInterval is the loading-animation frame interval.
const animInterval = 90 * time.Millisecond

// tickAnim schedules the next animation frame.
func tickAnim() tea.Cmd {
	return tea.Tick(animInterval, func(time.Time) tea.Msg { return animMsg{} })
}

// tickCountdown schedules the next one-second countdown tick.
func tickCountdown() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return countdownMsg{} })
}

// copyToClipboard copies the full `git commit` command (message included) to the
// system clipboard, so it can be pasted and run directly.
func copyToClipboard(c commit.Commit) tea.Cmd {
	return func() tea.Msg {
		return copiedMsg{err: clip.Copy(gitCommitCommand(c))}
	}
}

// gitCommitCommand renders a ready-to-run `git commit -m '…'` command. The
// message is wrapped in single quotes with embedded single quotes escaped, so it
// pastes safely into a POSIX shell even when multi-line.
func gitCommitCommand(c commit.Commit) string {
	msg := strings.TrimRight(c.Render(), "\n")
	escaped := strings.ReplaceAll(msg, "'", `'\''`)
	return "git commit -m '" + escaped + "'"
}

// loadStatus fetches the working-tree status and the current branch name.
func loadStatus(g gitRunner) tea.Cmd {
	return func() tea.Msg {
		files, err := g.Status()
		if err != nil {
			return errMsg{err}
		}
		branch, _ := g.CurrentBranch() // best-effort; empty on detached HEAD
		return statusMsg{files: files, branch: branch}
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

// doPush pushes the current branch, setting upstream when needed. If Push
// fails with an SSH auth error (key locked, no agent), it returns
// sshPassphraseNeededMsg instead of a fatal error so the TUI can ask for the
// passphrase and retry.
func doPush(g gitRunner) tea.Cmd {
	return func() tea.Msg {
		hasUpstream, err := g.HasUpstream()
		if err != nil {
			return errMsg{err}
		}
		setUpstream := !hasUpstream
		if err := g.Push(setUpstream); err != nil {
			if isSSHAuthError(err) {
				return sshPassphraseNeededMsg{setUpstream: setUpstream}
			}
			return errMsg{err}
		}
		return pushedMsg{setUpstream: setUpstream}
	}
}

// doPushWithPassphrase retries the push using an SSH passphrase.
func doPushWithPassphrase(g gitRunner, setUpstream bool, passphrase string) tea.Cmd {
	return func() tea.Msg {
		if err := g.PushWithPassphrase(setUpstream, passphrase); err != nil {
			return errMsg{err}
		}
		return pushedMsg{setUpstream: setUpstream}
	}
}
