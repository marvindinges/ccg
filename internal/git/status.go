package git

import (
	"fmt"
	"strings"
)

// FileStatus is one entry from `git status --porcelain=v2`.
type FileStatus struct {
	Path      string // current path
	OrigPath  string // original path for renames/copies, else ""
	Staged    byte   // index (staged) status code: '.', 'M', 'A', 'D', 'R', 'C', 'T'
	Unstaged  byte   // worktree (unstaged) status code
	Untracked bool   // '?' entry
	Unmerged  bool   // 'u' entry (merge conflict)
}

// IsStaged reports whether the file has staged changes.
func (f FileStatus) IsStaged() bool {
	return !f.Untracked && f.Staged != '.' && f.Staged != ' ' && f.Staged != 0
}

// IsModifiedInWorktree reports whether the file has unstaged worktree changes.
func (f FileStatus) IsModifiedInWorktree() bool {
	return f.Unstaged != '.' && f.Unstaged != ' ' && f.Unstaged != 0
}

// Label returns a short human status tag like "M", "A", "??", "R" for display.
func (f FileStatus) Label() string {
	switch {
	case f.Untracked:
		return "??"
	case f.Unmerged:
		return "UU"
	}
	staged := normalizeCode(f.Staged)
	unstaged := normalizeCode(f.Unstaged)
	return fmt.Sprintf("%c%c", staged, unstaged)
}

func normalizeCode(c byte) byte {
	if c == 0 || c == ' ' {
		return '.'
	}
	return c
}

// parseStatus parses NUL-terminated `git status --porcelain=v2 -z` output.
//
// Record formats (fields space-separated, records NUL-terminated):
//
//	1 <XY> <sub> <mH> <mI> <mW> <hH> <hI> <path>
//	2 <XY> <sub> <mH> <mI> <mW> <hH> <hI> <Xscore> <path>\0<origPath>
//	u <xy> <sub> <m1> <m2> <m3> <mW> <h1> <h2> <h3> <path>
//	? <path>
//	! <path>   (ignored; skipped)
func parseStatus(out string) ([]FileStatus, error) {
	records := strings.Split(out, "\x00")
	var files []FileStatus
	for i := 0; i < len(records); i++ {
		rec := records[i]
		if rec == "" {
			continue
		}
		switch rec[0] {
		case '?':
			files = append(files, FileStatus{Path: rec[2:], Untracked: true})
		case '!':
			// ignored, skip
		case '1':
			fs, err := parseChanged(rec, false)
			if err != nil {
				return nil, err
			}
			files = append(files, fs)
		case '2':
			fs, err := parseChanged(rec, true)
			if err != nil {
				return nil, err
			}
			// Rename/copy: the original path is the next NUL-separated record.
			if i+1 < len(records) {
				i++
				fs.OrigPath = records[i]
			}
			files = append(files, fs)
		case 'u':
			fs, err := parseUnmerged(rec)
			if err != nil {
				return nil, err
			}
			files = append(files, fs)
		default:
			return nil, fmt.Errorf("unrecognized status record: %q", rec)
		}
	}
	return files, nil
}

func parseChanged(rec string, renamed bool) (FileStatus, error) {
	// For "1": path is field index 8. For "2": an extra score field shifts it to 9.
	pathField := 8
	if renamed {
		pathField = 9
	}
	fields := strings.SplitN(rec, " ", pathField+1)
	if len(fields) < pathField+1 {
		return FileStatus{}, fmt.Errorf("malformed porcelain v2 record: %q", rec)
	}
	xy := fields[1]
	if len(xy) != 2 {
		return FileStatus{}, fmt.Errorf("malformed XY field %q in %q", xy, rec)
	}
	return FileStatus{
		Path:     fields[pathField],
		Staged:   xy[0],
		Unstaged: xy[1],
	}, nil
}

func parseUnmerged(rec string) (FileStatus, error) {
	// u <xy> <sub> <m1> <m2> <m3> <mW> <h1> <h2> <h3> <path>  -> path is field 10
	fields := strings.SplitN(rec, " ", 11)
	if len(fields) < 11 {
		return FileStatus{}, fmt.Errorf("malformed unmerged record: %q", rec)
	}
	xy := fields[1]
	return FileStatus{
		Path:     fields[10],
		Staged:   xy[0],
		Unstaged: xy[1],
		Unmerged: true,
	}, nil
}
