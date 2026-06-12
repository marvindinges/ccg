package git

import "testing"

func TestParseStatus(t *testing.T) {
	// Build a -z (NUL-terminated) porcelain v2 stream by hand.
	rec := func(s string) string { return s + "\x00" }
	out := rec("1 .M N... 100644 100644 100644 aaa bbb modified.txt") +
		rec("1 M. N... 100644 100644 100644 aaa bbb staged.txt") +
		rec("1 A. N... 000000 100644 100644 000 ccc added.txt") +
		rec("? untracked.txt") +
		rec("! ignored.txt") +
		// rename: entry record then original-path record
		rec("2 R. N... 100644 100644 100644 ddd ddd R100 new name.txt") +
		rec("old name.txt")

	files, err := parseStatus(out)
	if err != nil {
		t.Fatalf("parseStatus: %v", err)
	}
	if len(files) != 5 { // ignored entry is skipped
		t.Fatalf("got %d files, want 5: %+v", len(files), files)
	}

	by := map[string]FileStatus{}
	for _, f := range files {
		by[f.Path] = f
	}

	if f := by["modified.txt"]; !f.IsModifiedInWorktree() || f.IsStaged() {
		t.Errorf("modified.txt: staged=%v worktree=%v", f.IsStaged(), f.IsModifiedInWorktree())
	}
	if f := by["staged.txt"]; !f.IsStaged() {
		t.Errorf("staged.txt should be staged: %+v", f)
	}
	if f := by["untracked.txt"]; !f.Untracked {
		t.Errorf("untracked.txt should be untracked")
	}
	if f := by["new name.txt"]; f.OrigPath != "old name.txt" {
		t.Errorf("rename orig path = %q, want %q", f.OrigPath, "old name.txt")
	}
	if _, ok := by["ignored.txt"]; ok {
		t.Errorf("ignored.txt should have been skipped")
	}
}

func TestLabel(t *testing.T) {
	tests := []struct {
		f    FileStatus
		want string
	}{
		{FileStatus{Untracked: true}, "??"},
		{FileStatus{Staged: 'M', Unstaged: '.'}, "M."},
		{FileStatus{Staged: '.', Unstaged: 'M'}, ".M"},
		{FileStatus{Staged: 'A', Unstaged: 0}, "A."},
	}
	for _, tt := range tests {
		if got := tt.f.Label(); got != tt.want {
			t.Errorf("Label(%+v) = %q, want %q", tt.f, got, tt.want)
		}
	}
}
