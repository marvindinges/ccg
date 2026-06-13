package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/marvindinges/ccg/internal/git"
)

func treeModel() Model {
	m := baseModel(&fakeGit{diff: "d"}, nil)
	m.step = stepMain
	m.activePanel = panelFiles
	m.files = []git.FileStatus{
		{Path: "README.md", Unstaged: 'M'},
		{Path: "internal/tui/app.go", Staged: 'M'},
		{Path: "internal/tui/styles.go", Unstaged: 'M'},
		{Path: "internal/git/git.go", Untracked: true},
	}
	m.filesSelected = map[string]bool{"internal/tui/app.go": true}
	m.diff = "d"
	return m
}

func TestFileRowsGroupByFolder(t *testing.T) {
	rows := treeModel().fileRows()

	// Expected flattened order (dirs first, alphabetical), with compression:
	// internal/ , git/ , git.go , tui/ , app.go , styles.go , README.md
	want := []struct {
		label string
		isDir bool
		depth int
	}{
		{"internal/", true, 0},
		{"git/", true, 1},
		{"git.go", false, 2},
		{"tui/", true, 1},
		{"app.go", false, 2},
		{"styles.go", false, 2},
		{"README.md", false, 0},
	}
	if len(rows) != len(want) {
		t.Fatalf("got %d rows, want %d: %+v", len(rows), len(want), rows)
	}
	for i, w := range want {
		if rows[i].label != w.label || rows[i].isDir != w.isDir || rows[i].depth != w.depth {
			t.Errorf("row %d = {%q dir=%v depth=%d}, want {%q dir=%v depth=%d}",
				i, rows[i].label, rows[i].isDir, rows[i].depth, w.label, w.isDir, w.depth)
		}
	}
}

func TestFolderRowSpansItsFiles(t *testing.T) {
	rows := treeModel().fileRows()
	var tui fileRow
	for _, r := range rows {
		if r.label == "tui/" {
			tui = r
		}
	}
	if len(tui.paths) != 2 {
		t.Fatalf("tui/ should span 2 files, got %v", tui.paths)
	}
}

func TestToggleFolderStagesAllItsFiles(t *testing.T) {
	m := treeModel()
	g := m.opts.Git.(*fakeGit)

	// Find and select the tui/ folder row, then space to stage it.
	rows := m.fileRows()
	for i, r := range rows {
		if r.label == "tui/" {
			m.filesCursor = i
		}
	}
	out, cmd := m.Update(tea.KeyPressMsg{Code: ' '})
	got := out.(Model)

	// styles.go was unstaged → now both tui files are staged.
	if !got.filesSelected["internal/tui/styles.go"] || !got.filesSelected["internal/tui/app.go"] {
		t.Error("staging the tui/ folder should stage all its files")
	}
	// The git command should stage only the not-yet-staged file.
	runCmd(cmd)
	if len(g.staged) != 1 || g.staged[0] != "internal/tui/styles.go" {
		t.Errorf("expected styles.go staged, got %v", g.staged)
	}
}

func TestFolderGlyphPartial(t *testing.T) {
	m := treeModel() // internal/tui has app.go staged, styles.go not → partial
	rows := m.fileRows()
	for _, r := range rows {
		if r.label == "tui/" {
			if g := m.rowGlyph(r); g != "◐" {
				t.Errorf("partially-staged folder glyph = %q, want ◐", g)
			}
		}
		if r.label == "git/" { // nothing staged
			if g := m.rowGlyph(r); g != "○" {
				t.Errorf("unstaged folder glyph = %q, want ○", g)
			}
		}
	}
}
