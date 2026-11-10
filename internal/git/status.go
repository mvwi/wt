package git

import (
	"strconv"
	"strings"
)

// FileChange represents a single file change from git status --porcelain.
type FileChange struct {
	Status string // Two-character status (e.g., "M ", " M", "??", "R ")
	Path   string
	// For renames and copies: original path
	OldPath string
}

// IsRename returns true if this change is a rename.
func (f FileChange) IsRename() bool {
	return strings.Contains(f.Status, "R")
}

// HasChanges returns true if the working tree has uncommitted changes.
func HasChanges() bool {
	out, err := Run("status", "--porcelain")
	if err != nil {
		return false
	}
	return strings.TrimSpace(out) != ""
}

// HasChangesIn returns true if a specific worktree has uncommitted changes.
func HasChangesIn(dir string) bool {
	out, err := RunIn(dir, "status", "--porcelain")
	if err != nil {
		return false
	}
	return strings.TrimSpace(out) != ""
}

// StatusShort returns the short status output for display.
func StatusShort() (string, error) {
	return StatusShortIn("")
}

// StatusShortIn returns short status for a directory. It keeps the leading
// space of " M file" lines so the columns line up.
func StatusShortIn(dir string) (string, error) {
	out, err := runRawIn(dir, "status", "--short")
	return strings.TrimRight(out, "\n"), err
}

// StatusPorcelain returns parsed file changes.
func StatusPorcelain() ([]FileChange, error) {
	return StatusPorcelainIn("")
}

// StatusPorcelainIn returns parsed file changes for a specific directory.
// Uses -uall to list individual files inside untracked directories,
// rather than collapsing them into a single directory entry.
func StatusPorcelainIn(dir string) ([]FileChange, error) {
	out, err := runRawIn(dir, "status", "--porcelain", "-z", "-uall")
	if err != nil {
		return nil, err
	}
	return ParsePorcelainOutput(out), nil
}

// ParsePorcelainOutput parses the output of `git status --porcelain -z`.
// -z keeps paths verbatim. Without it git quotes any path that has a space or
// a non-ASCII byte, and the quoted name matches no file on disk.
func ParsePorcelainOutput(out string) []FileChange {
	var changes []FileChange
	entries := strings.Split(out, "\x00")
	for i := 0; i < len(entries); i++ {
		e := entries[i]
		if len(e) < 4 {
			continue
		}
		fc := FileChange{Status: e[:2], Path: e[3:]}
		// A rename or copy is followed by its original path as the next entry.
		if strings.ContainsAny(fc.Status, "RC") && i+1 < len(entries) {
			i++
			fc.OldPath = entries[i]
		}
		changes = append(changes, fc)
	}
	return changes
}

// UnpushedCountIn returns the number of commits ahead of the upstream.
func UnpushedCountIn(dir string) int {
	out, err := RunIn(dir, "rev-list", "--count", "@{upstream}..HEAD")
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(out))
	return n
}
