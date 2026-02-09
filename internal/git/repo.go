package git

import (
	"os"
	"path/filepath"
	"strings"
)

// RepoName returns the basename of the main worktree (the true repo name).
// Uses MainWorktree() rather than --show-toplevel, which would return the
// linked worktree's directory name when called from inside one.
func RepoName() (string, error) {
	main, err := MainWorktree()
	if err != nil {
		return "", err
	}
	return filepath.Base(main), nil
}

// TopLevel returns the absolute path to the repository root.
func TopLevel() (string, error) {
	return Run("rev-parse", "--show-toplevel")
}

// GitDir returns the .git directory path (handles worktrees where .git is a file).
func GitDir() (string, error) {
	return Run("rev-parse", "--git-dir")
}

// Username returns the git user's first name, lowercased.
// e.g., "Michael Williams" → "michael"
func Username() (string, error) {
	name, err := Run("config", "user.name")
	if err != nil {
		return "", err
	}
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, " ", "-")
	parts := strings.SplitN(name, "-", 2)
	return parts[0], nil
}

// IsInsideWorkTree returns true if the current directory is inside a git repo.
func IsInsideWorkTree() bool {
	err := RunSilent("rev-parse", "--is-inside-work-tree")
	return err == nil
}

// Fetch fetches a specific ref from a remote.
func Fetch(remote string, refs ...string) error {
	args := append([]string{"fetch", remote}, refs...)
	return RunSilent(args...)
}

// FetchAll fetches all refs from a remote.
func FetchAll(remote string) error {
	return RunSilent("fetch", remote)
}

// IsDir returns true if the path exists and is a directory.
func IsDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// FileExists returns true if the path exists.
func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
