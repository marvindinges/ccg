// Package git is a thin, CGO-free wrapper around the `git` CLI via os/exec.
// It exposes only the operations ccg needs (status, stage, diff, commit, push)
// and has no knowledge of the TUI. Commands run with a deterministic
// environment so output parsing is stable across locales and WSL.
package git

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Runner runs git commands in a working directory.
type Runner struct {
	Dir string // repo working directory; "" means the current directory
	git string // resolved path to the git binary
}

// New locates the git binary and returns a Runner rooted at the current
// directory. It errors if git is not on PATH or the cwd is not a work tree.
func New() (*Runner, error) {
	bin, err := exec.LookPath("git")
	if err != nil {
		return nil, fmt.Errorf("git not found on PATH: %w", err)
	}
	r := &Runner{git: bin}
	if _, err := r.run(context.Background(), "", "rev-parse", "--is-inside-work-tree"); err != nil {
		return nil, fmt.Errorf("not a git repository: %w", err)
	}
	return r, nil
}

// NewInDir is like New but rooted at dir (used by tests).
func NewInDir(dir string) (*Runner, error) {
	bin, err := exec.LookPath("git")
	if err != nil {
		return nil, fmt.Errorf("git not found on PATH: %w", err)
	}
	return &Runner{Dir: dir, git: bin}, nil
}

// run executes git with the given args, optionally writing stdin, and returns
// stdout. Stderr is folded into the returned error. The environment is forced
// to a deterministic, lock-light configuration for stable parsing on WSL.
func (r *Runner) run(ctx context.Context, stdin string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, r.git, args...)
	cmd.Dir = r.Dir
	cmd.Env = append(os.Environ(), "LC_ALL=C", "GIT_OPTIONAL_LOCKS=0")
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return stdout.String(), nil
}

// Root returns the absolute path to the repository top-level directory.
func (r *Runner) Root() (string, error) {
	out, err := r.run(context.Background(), "", "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// Status returns the working-tree status (changed, staged, untracked, unmerged).
func (r *Runner) Status() ([]FileStatus, error) {
	out, err := r.run(context.Background(), "",
		"status", "--porcelain=v2", "-z", "--untracked-files=all")
	if err != nil {
		return nil, err
	}
	return parseStatus(out)
}

// Stage runs `git add` on the given paths.
func (r *Runner) Stage(paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	args := append([]string{"add", "--"}, paths...)
	_, err := r.run(context.Background(), "", args...)
	return err
}

// Unstage removes the given paths from the index. On a normal branch this uses
// `git restore --staged`; before the first commit (no HEAD) it falls back to
// `git rm --cached`, which restore cannot do on an unborn branch.
func (r *Runner) Unstage(paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	if r.hasHead() {
		args := append([]string{"restore", "--staged", "--"}, paths...)
		_, err := r.run(context.Background(), "", args...)
		return err
	}
	args := append([]string{"rm", "--cached", "--quiet", "--"}, paths...)
	_, err := r.run(context.Background(), "", args...)
	return err
}

// hasHead reports whether HEAD points at a commit (false on an unborn branch).
func (r *Runner) hasHead() bool {
	_, err := r.run(context.Background(), "", "rev-parse", "--verify", "--quiet", "HEAD")
	return err == nil
}

// StagedDiff returns the diff of staged changes (`git diff --cached`).
func (r *Runner) StagedDiff() (string, error) {
	return r.run(context.Background(), "",
		"diff", "--cached", "--no-color", "--diff-algorithm=histogram")
}

// HasStagedChanges reports whether anything is currently staged.
func (r *Runner) HasStagedChanges() (bool, error) {
	out, err := r.run(context.Background(), "", "diff", "--cached", "--name-only")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

// Commit creates a commit with the given message, passed via stdin to avoid
// argv length/escaping issues with multi-line bodies and footers.
func (r *Runner) Commit(message string) error {
	_, err := r.run(context.Background(), message, "commit", "-F", "-")
	return err
}

// CurrentBranch returns the current branch name.
func (r *Runner) CurrentBranch() (string, error) {
	out, err := r.run(context.Background(), "", "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// HasUpstream reports whether the current branch has a configured upstream.
func (r *Runner) HasUpstream() (bool, error) {
	_, err := r.run(context.Background(), "",
		"rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	if err != nil {
		// No upstream configured is an expected, non-fatal condition.
		return false, nil
	}
	return true, nil
}

// Push pushes the current branch. When setUpstream is true (no upstream yet),
// it runs `git push -u origin <branch>`.
func (r *Runner) Push(setUpstream bool) error {
	if !setUpstream {
		_, err := r.run(context.Background(), "", "push")
		return err
	}
	branch, err := r.CurrentBranch()
	if err != nil {
		return err
	}
	_, err = r.run(context.Background(), "", "push", "-u", "origin", branch)
	return err
}
