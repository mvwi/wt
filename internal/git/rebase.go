package git

import (
	"os"
	"path/filepath"
	"strings"
)

// Rebase runs `git rebase <onto>` with passthrough output.
func Rebase(onto string) error {
	return RunPassthrough("rebase", onto)
}

// RebaseIn runs rebase in a specific directory silently. Returns error on conflict.
func RebaseIn(dir, onto string) error {
	return RunSilentIn(dir, "rebase", onto)
}

// RebaseContinue continues an in-progress rebase.
func RebaseContinue() error {
	return RunPassthrough("rebase", "--continue")
}

// RebaseAbort aborts an in-progress rebase.
func RebaseAbort() error {
	return RunPassthrough("rebase", "--abort")
}

// RebaseAbortIn aborts a rebase in a specific directory.
func RebaseAbortIn(dir string) {
	_ = RunSilentIn(dir, "rebase", "--abort")
}

// IsRebaseInProgress checks if a rebase is currently active.
func IsRebaseInProgress() (bool, error) {
	gitDir, err := GitDir()
	if err != nil {
		return false, err
	}
	return isDir(filepath.Join(gitDir, "rebase-merge")) || isDir(filepath.Join(gitDir, "rebase-apply")), nil
}

// MergeFF does a fast-forward merge.
func MergeFF(ref string) (string, error) {
	return Run("merge", "--ff-only", ref)
}

// MergeFFIn does a fast-forward merge in a specific directory.
func MergeFFIn(dir, ref string) error {
	return RunSilentIn(dir, "merge", "--ff-only", ref)
}

// Push pushes with force-with-lease.
func PushForceWithLease() error {
	return RunPassthrough("push", "--force-with-lease")
}

// PushSetUpstream pushes and sets the upstream.
func PushSetUpstream(remote string) error {
	return RunPassthrough("push", "-u", remote, "HEAD")
}

// FetchPrune fetches and prunes dead remote refs.
func FetchPrune() {
	_ = RunSilent("fetch", "--prune")
}

// SaveStateFile writes the rebase state to a file in the git dir.
func SaveStateFile(name, content string) error {
	gitDir, err := GitDir()
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(gitDir, name), []byte(content), 0600)
}

// ReadStateFile reads a state file from the git dir.
func ReadStateFile(name string) (string, error) {
	gitDir, err := GitDir()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Join(gitDir, name))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// RemoveStateFile deletes a state file. Errors are ignored since
// leftover state files in .git/ are harmless.
func RemoveStateFile(name string) {
	gitDir, err := GitDir()
	if err != nil {
		return
	}
	_ = os.Remove(filepath.Join(gitDir, name))
}

// PotentialConflicts returns files modified on both HEAD and baseRef since they diverged.
// These files could potentially conflict during a rebase.
func PotentialConflicts(baseRef string) ([]string, error) {
	// Files changed on the base branch since divergence
	baseOut, err := Run("diff", "--name-only", "HEAD..."+baseRef)
	if err != nil {
		return nil, err
	}
	// Files changed on our branch since divergence
	oursOut, err := Run("diff", "--name-only", baseRef+"...HEAD")
	if err != nil {
		return nil, err
	}

	baseFiles := toSet(baseOut)
	var conflicts []string
	for _, line := range strings.Split(oursOut, "\n") {
		f := strings.TrimSpace(line)
		if f != "" && baseFiles[f] {
			conflicts = append(conflicts, f)
		}
	}
	return conflicts, nil
}

// toSet splits newline-delimited output into a set of non-empty strings.
func toSet(s string) map[string]bool {
	m := make(map[string]bool)
	for _, line := range strings.Split(s, "\n") {
		f := strings.TrimSpace(line)
		if f != "" {
			m[f] = true
		}
	}
	return m
}

// StateFileExists checks if a state file exists.
func StateFileExists(name string) bool {
	gitDir, err := GitDir()
	if err != nil {
		return false
	}
	return fileExists(filepath.Join(gitDir, name))
}
