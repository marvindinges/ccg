package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func tCtx() context.Context { return context.Background() }

// trimTrailing removes trailing whitespace from each line and trailing blank
// lines, matching how git stores/normalizes commit messages.
func trimTrailing(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t\r")
	}
	out := strings.Join(lines, "\n")
	return strings.TrimRight(out, "\n")
}

// newTestRepo creates a throwaway git repo in a temp dir and returns a Runner.
func newTestRepo(t *testing.T) *Runner {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	r, err := NewInDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	mustRun := func(args ...string) {
		t.Helper()
		if _, err := r.run(tCtx(), "", args...); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
	mustRun("init", "-b", "main")
	mustRun("config", "user.email", "test@example.com")
	mustRun("config", "user.name", "Test User")
	mustRun("config", "commit.gpgsign", "false")
	return r
}

func writeFile(t *testing.T, r *Runner, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(r.Dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestStageDiffCommitFlow(t *testing.T) {
	r := newTestRepo(t)

	writeFile(t, r, "a.txt", "hello\n")
	writeFile(t, r, "b.txt", "world\n")

	// Both files should appear as untracked.
	files, err := r.Status()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 untracked files, got %d: %+v", len(files), files)
	}

	// Stage only a.txt.
	if err := r.Stage([]string{"a.txt"}); err != nil {
		t.Fatal(err)
	}
	has, err := r.HasStagedChanges()
	if err != nil {
		t.Fatal(err)
	}
	if !has {
		t.Fatal("expected staged changes after staging a.txt")
	}

	diff, err := r.StagedDiff()
	if err != nil {
		t.Fatal(err)
	}
	if diff == "" {
		t.Fatal("expected non-empty staged diff")
	}

	// Commit with a multi-line message via stdin.
	msg := "feat: add a\n\nThis is the body.\n\nRefs: #1\n"
	if err := r.Commit(msg); err != nil {
		t.Fatal(err)
	}

	got, err := r.run(tCtx(), "", "log", "-1", "--format=%B")
	if err != nil {
		t.Fatal(err)
	}
	// git normalizes trailing whitespace; compare trimmed bodies line-wise.
	want := "feat: add a\n\nThis is the body.\n\nRefs: #1"
	if trimTrailing(got) != want {
		t.Errorf("commit message =\n%q\nwant\n%q", trimTrailing(got), want)
	}
}

func TestPushAndUpstream(t *testing.T) {
	r := newTestRepo(t)

	// Create a bare "remote" and wire it as origin.
	remote := t.TempDir()
	if _, err := r.run(tCtx(), "", "init", "--bare", remote); err != nil {
		t.Fatal(err)
	}
	if _, err := r.run(tCtx(), "", "remote", "add", "origin", remote); err != nil {
		t.Fatal(err)
	}

	writeFile(t, r, "a.txt", "x\n")
	if err := r.Stage([]string{"a.txt"}); err != nil {
		t.Fatal(err)
	}
	if err := r.Commit("feat: initial\n"); err != nil {
		t.Fatal(err)
	}

	branch, err := r.CurrentBranch()
	if err != nil {
		t.Fatal(err)
	}
	if branch != "main" {
		t.Errorf("branch = %q, want main", branch)
	}

	if has, _ := r.HasUpstream(); has {
		t.Error("expected no upstream before first push")
	}
	if err := r.Push(true); err != nil {
		t.Fatalf("push -u failed: %v", err)
	}
	if has, _ := r.HasUpstream(); !has {
		t.Error("expected upstream after push -u")
	}

	// A second commit should push without setting upstream again.
	writeFile(t, r, "b.txt", "y\n")
	_ = r.Stage([]string{"b.txt"})
	_ = r.Commit("feat: more\n")
	if err := r.Push(false); err != nil {
		t.Fatalf("plain push failed: %v", err)
	}
}

func TestRoot(t *testing.T) {
	r := newTestRepo(t)
	root, err := r.Root()
	if err != nil {
		t.Fatal(err)
	}
	if root == "" {
		t.Error("expected non-empty root")
	}
}

func TestUnstage(t *testing.T) {
	r := newTestRepo(t)
	writeFile(t, r, "a.txt", "x\n")
	if err := r.Stage([]string{"a.txt"}); err != nil {
		t.Fatal(err)
	}
	if err := r.Unstage([]string{"a.txt"}); err != nil {
		t.Fatal(err)
	}
	has, err := r.HasStagedChanges()
	if err != nil {
		t.Fatal(err)
	}
	if has {
		t.Fatal("expected no staged changes after unstaging")
	}
}
