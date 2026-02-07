package git

import "os"

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
	RunSilentIn(dir, "rebase", "--abort")
}

// IsRebaseInProgress checks if a rebase is currently active.
func IsRebaseInProgress() (bool, error) {
	gitDir, err := GitDir()
	if err != nil {
		return false, err
	}
	return IsDir(gitDir+"/rebase-merge") || IsDir(gitDir+"/rebase-apply"), nil
}

// MergeFF does a fast-forward merge.
func MergeFF(ref string) (string, error) {
	return Run("merge", "--ff-only", ref)
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
	RunSilent("fetch", "--prune")
}

// SaveStateFile writes the rebase state to a file in the git dir.
func SaveStateFile(name, content string) error {
	gitDir, err := GitDir()
	if err != nil {
		return err
	}
	return os.WriteFile(gitDir+"/"+name, []byte(content), 0644)
}

// ReadStateFile reads a state file from the git dir.
func ReadStateFile(name string) (string, error) {
	gitDir, err := GitDir()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(gitDir + "/" + name)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// RemoveStateFile deletes a state file.
func RemoveStateFile(name string) {
	gitDir, _ := GitDir()
	if gitDir != "" {
		os.Remove(gitDir + "/" + name)
	}
}

// StateFileExists checks if a state file exists.
func StateFileExists(name string) bool {
	gitDir, err := GitDir()
	if err != nil {
		return false
	}
	return FileExists(gitDir + "/" + name)
}
