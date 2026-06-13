package tui

import (
	"sort"
	"strings"
)

// fileRow is one visible line in the Files panel: either a directory node or a
// file leaf. Directories group every file beneath them so the whole folder can
// be staged at once (lazygit-style).
type fileRow struct {
	label   string   // display text (dir: "internal/tui/", file: basename)
	depth   int      // indentation level in the tree
	isDir   bool     // directory node vs file leaf
	fileIdx int      // index into Model.files for a file, -1 for a directory
	paths   []string // file paths this row stages/unstages (a dir spans many)
}

// ftNode is a node in the path tree built from the flat file list.
type ftNode struct {
	name     string
	children map[string]*ftNode
	fileIdx  int // -1 for directories
}

// fileRows builds the flattened, grouped tree view of the staged-area files.
// Single-child directory chains are compressed (e.g. "internal/tui/") to keep
// the tree compact, matching lazygit.
func (m Model) fileRows() []fileRow {
	root := &ftNode{children: map[string]*ftNode{}, fileIdx: -1}
	for i, f := range m.files {
		segs := strings.Split(f.Path, "/")
		cur := root
		for j, seg := range segs {
			child, ok := cur.children[seg]
			if !ok {
				child = &ftNode{name: seg, children: map[string]*ftNode{}, fileIdx: -1}
				cur.children[seg] = child
			}
			if j == len(segs)-1 {
				child.fileIdx = i
			}
			cur = child
		}
	}

	var rows []fileRow
	m.flattenNode(root, 0, &rows)
	return rows
}

// flattenNode appends node's children to rows in display order (directories
// first, then files, alphabetical within each), recursing into subdirectories.
func (m Model) flattenNode(node *ftNode, depth int, rows *[]fileRow) {
	for _, name := range sortedChildNames(node) {
		child := node.children[name]
		if child.fileIdx >= 0 {
			*rows = append(*rows, fileRow{
				label:   name,
				depth:   depth,
				isDir:   false,
				fileIdx: child.fileIdx,
				paths:   []string{m.files[child.fileIdx].Path},
			})
			continue
		}

		// Directory: compress single-subdirectory chains into one node.
		label := name + "/"
		cur := child
		for len(cur.children) == 1 {
			only := singleChild(cur)
			if only.fileIdx >= 0 { // the lone child is a file → stop compressing
				break
			}
			label += only.name + "/"
			cur = only
		}
		*rows = append(*rows, fileRow{
			label: label,
			depth: depth,
			isDir: true,
			fileIdx: -1,
			paths:  m.descendantPaths(cur),
		})
		m.flattenNode(cur, depth+1, rows)
	}
}

// sortedChildNames orders a node's children with directories before files,
// alphabetically within each group.
func sortedChildNames(node *ftNode) []string {
	var dirs, files []string
	for name, c := range node.children {
		if c.fileIdx < 0 {
			dirs = append(dirs, name)
		} else {
			files = append(files, name)
		}
	}
	sort.Strings(dirs)
	sort.Strings(files)
	return append(dirs, files...)
}

// singleChild returns the sole child of a node (caller guarantees len == 1).
func singleChild(node *ftNode) *ftNode {
	for _, c := range node.children {
		return c
	}
	return nil
}

// descendantPaths collects every file path under node (depth-first).
func (m Model) descendantPaths(node *ftNode) []string {
	var paths []string
	if node.fileIdx >= 0 {
		paths = append(paths, m.files[node.fileIdx].Path)
	}
	for _, name := range sortedChildNames(node) {
		paths = append(paths, m.descendantPaths(node.children[name])...)
	}
	return paths
}

// rowStaged reports how many of a row's files are staged: "none", "some", "all".
func (m Model) rowStaged(r fileRow) (staged, total int) {
	for _, p := range r.paths {
		total++
		if m.filesSelected[p] {
			staged++
		}
	}
	return staged, total
}
