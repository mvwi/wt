package git

import (
	"strconv"
	"strings"
)

// FileChange represents a single file change from git status --porcelain.
type FileChange struct {
	Status string // Two-character status (e.g., "M ", " M", "??", "R ")
	Path   string
	// For renames: original path
	OldPath string
}

// IsRename returns true if this change is a rename.
func (f FileChange) IsRename() bool {
	return strings.HasPrefix(f.Status, "R")
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
	return Run("status", "--short")
}

// StatusPorcelain returns parsed file changes.
func StatusPorcelain() ([]FileChange, error) {
	return StatusPorcelainIn("")
}

// StatusPorcelainIn returns parsed file changes for a specific directory.
func StatusPorcelainIn(dir string) ([]FileChange, error) {
	out, err := RunIn(dir, "status", "--porcelain")
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(out) == "" {
		return nil, nil
	}

	var changes []FileChange
	for _, line := range strings.Split(out, "\n") {
		if len(line) < 3 {
			continue
		}
		status := line[:2]
		path := line[3:]

		fc := FileChange{Status: status}

		// Handle renames: "R  old -> new"
		if strings.HasPrefix(status, "R") && strings.Contains(path, " -> ") {
			parts := strings.SplitN(path, " -> ", 2)
			fc.OldPath = parts[0]
			fc.Path = parts[1]
		} else {
			fc.Path = path
		}

		changes = append(changes, fc)
	}

	return changes, nil
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
