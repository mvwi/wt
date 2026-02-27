package git

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Worktree represents a git worktree entry.
type Worktree struct {
	Path   string
	Branch string
}

// ListWorktrees returns all worktrees from `git worktree list --porcelain`.
func ListWorktrees() ([]Worktree, error) {
	out, err := Run("worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	return ParseWorktreeList(out), nil
}

// ParseWorktreeList parses the porcelain output of `git worktree list`.
func ParseWorktreeList(out string) []Worktree {
	var worktrees []Worktree
	var current Worktree

	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			current = Worktree{Path: strings.TrimPrefix(line, "worktree ")}
		case strings.HasPrefix(line, "branch "):
			ref := strings.TrimPrefix(line, "branch ")
			// "refs/heads/feature" → "feature"
			current.Branch = strings.TrimPrefix(ref, "refs/heads/")
		case line == "":
			if current.Path != "" {
				worktrees = append(worktrees, current)
				current = Worktree{}
			}
		}
	}
	// Last entry (porcelain output may not end with blank line)
	if current.Path != "" {
		worktrees = append(worktrees, current)
	}

	return worktrees
}

// MainWorktree returns the path of the main (first) worktree.
func MainWorktree() (string, error) {
	wts, err := ListWorktrees()
	if err != nil {
		return "", err
	}
	if len(wts) == 0 {
		return "", fmt.Errorf("no worktrees found (not a git repository?)")
	}
	return wts[0].Path, nil
}

// ParentDir returns the parent directory of the main worktree
// (where sibling worktrees are created).
func ParentDir() (string, error) {
	main, err := MainWorktree()
	if err != nil {
		return "", err
	}
	return filepath.Dir(main), nil
}

// AddWorktree creates a new worktree with a new branch from a base ref.
func AddWorktree(path, branch, baseRef string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	_, err := Run("worktree", "add", "-b", branch, path, baseRef)
	return err
}

// AddWorktreeFromExisting creates a worktree checking out an existing branch.
func AddWorktreeFromExisting(path, branch string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	_, err := Run("worktree", "add", path, branch)
	return err
}

// AddWorktreeFromRemote creates a worktree with a local tracking branch from a remote.
func AddWorktreeFromRemote(path, localBranch, remoteBranch string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	_, err := Run("worktree", "add", "-b", localBranch, path, remoteBranch)
	return err
}

// RemoveWorktree removes a worktree (force).
// After removal, cleans up the parent directory if it's empty.
func RemoveWorktree(path string) error {
	_, err := Run("worktree", "remove", path, "--force")
	if err != nil {
		return err
	}
	// Clean up empty parent dir (e.g. wt-repo/ after last worktree removed).
	// os.Remove only succeeds on empty directories, so this is safe.
	_ = os.Remove(filepath.Dir(path))
	return nil
}

// MoveWorktree moves a worktree to a new path.
func MoveWorktree(oldPath, newPath string) error {
	if err := os.MkdirAll(filepath.Dir(newPath), 0755); err != nil {
		return err
	}
	_, err := Run("worktree", "move", oldPath, newPath)
	return err
}

// PruneWorktrees cleans up stale worktree bookkeeping.
func PruneWorktrees() {
	_ = RunSilent("worktree", "prune")
}
