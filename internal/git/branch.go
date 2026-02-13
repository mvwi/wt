package git

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// CurrentBranch returns the current branch name, or "" for detached HEAD.
func CurrentBranch() (string, error) {
	return Run("rev-parse", "--abbrev-ref", "HEAD")
}

// CurrentBranchIn returns the current branch for a specific worktree.
func CurrentBranchIn(dir string) (string, error) {
	return RunIn(dir, "rev-parse", "--abbrev-ref", "HEAD")
}

// BranchExists checks if a local branch exists.
func BranchExists(name string) bool {
	return RunSilent("show-ref", "--verify", "--quiet", "refs/heads/"+name) == nil
}

// RemoteBranchExists checks if a remote branch exists (e.g., "origin/feature").
func RemoteBranchExists(name string) bool {
	return RunSilent("show-ref", "--verify", "--quiet", "refs/remotes/"+name) == nil
}

// ResolveBranch tries to find a branch ref in order: local, remotes/<name>, remotes/origin/<name>.
// Returns the ref, whether it's remote, and any error.
func ResolveBranch(name, remote string) (ref string, isRemote bool, err error) {
	// Local branch
	if BranchExists(name) {
		return name, false, nil
	}
	// Remote with explicit prefix (e.g., "origin/branch")
	if RemoteBranchExists(name) {
		return name, true, nil
	}
	// Remote without prefix
	fullRemote := remote + "/" + name
	if RemoteBranchExists(fullRemote) {
		return fullRemote, true, nil
	}
	return "", false, fmt.Errorf("branch not found: %s (tried local and remote refs)", name)
}

// RenameBranch renames a local branch.
func RenameBranch(oldName, newName string) error {
	_, err := Run("branch", "-m", oldName, newName)
	return err
}

// DeleteBranch force-deletes a local branch.
func DeleteBranch(name string) error {
	_, err := Run("branch", "-D", name)
	return err
}

// DeleteRemoteBranch deletes a branch on the remote.
func DeleteRemoteBranch(remote, branch string) error {
	_, err := Run("push", remote, "--delete", branch)
	return err
}

// AheadBehind returns how many commits a branch is ahead/behind a ref.
type AheadBehind struct {
	Ahead  int
	Behind int
}

// GetAheadBehind computes ahead/behind between HEAD and a remote ref.
func GetAheadBehind(remoteRef string) (AheadBehind, error) {
	return GetAheadBehindIn("", remoteRef)
}

// GetAheadBehindIn computes ahead/behind for a specific worktree.
func GetAheadBehindIn(dir, remoteRef string) (AheadBehind, error) {
	ab := AheadBehind{}

	behind, err := RunIn(dir, "rev-list", "--count", "HEAD.."+remoteRef)
	if err != nil {
		return ab, err
	}
	ab.Behind, err = strconv.Atoi(strings.TrimSpace(behind))
	if err != nil {
		return ab, fmt.Errorf("invalid behind count %q: %w", behind, err)
	}

	ahead, err := RunIn(dir, "rev-list", "--count", remoteRef+"..HEAD")
	if err != nil {
		return ab, err
	}
	ab.Ahead, err = strconv.Atoi(strings.TrimSpace(ahead))
	if err != nil {
		return ab, fmt.Errorf("invalid ahead count %q: %w", ahead, err)
	}

	return ab, nil
}

// Upstream returns the upstream tracking branch, or "" if none.
func Upstream() string {
	out, err := Run("rev-parse", "--abbrev-ref", "@{upstream}")
	if err != nil {
		return ""
	}
	return out
}

// SetUpstream sets the upstream tracking branch.
func SetUpstream(remote, branch string) error {
	return RunSilent("branch", "--set-upstream-to="+remote+"/"+branch)
}

// LastCommitDaysAgo returns the number of days since the last commit in a directory.
// Returns -1 if the age cannot be determined.
func LastCommitDaysAgo(dir string) int {
	out, err := RunIn(dir, "log", "-1", "--format=%ct")
	if err != nil {
		return -1
	}
	ts, err := strconv.ParseInt(strings.TrimSpace(out), 10, 64)
	if err != nil {
		return -1
	}
	return int(time.Since(time.Unix(ts, 0)).Hours() / 24)
}

// LastCommitAge returns the relative age of the last commit (e.g., "2 hours ago").
func LastCommitAge(dir string) string {
	out, err := RunIn(dir, "log", "-1", "--format=%cr")
	if err != nil {
		return ""
	}
	return shortenAge(out)
}

// shortenAge converts "2 hours ago" → "2h", "3 days ago" → "3d", etc.
func shortenAge(s string) string {
	s = strings.TrimSuffix(s, " ago")
	replacements := []struct{ long, short string }{
		{"seconds", "s"}, {"second", "s"},
		{"minutes", "m"}, {"minute", "m"},
		{"hours", "h"}, {"hour", "h"},
		{"days", "d"}, {"day", "d"},
		{"weeks", "w"}, {"week", "w"},
		{"months", "mo"}, {"month", "mo"},
		{"years", "y"}, {"year", "y"},
	}
	for _, r := range replacements {
		s = strings.Replace(s, " "+r.long, r.short, 1)
	}
	return s
}
