package git

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
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

// IsValidBranchName reports whether git accepts name as a new branch name.
func IsValidBranchName(name string) bool {
	return !strings.HasPrefix(name, "-") && RunSilent("check-ref-format", "--branch", name) == nil
}

// MergedBranches returns the local branches whose tips are reachable from ref.
func MergedBranches(ref string) (map[string]bool, error) {
	out, err := Run("branch", "--merged", ref, "--format=%(refname:short)")
	if err != nil {
		return nil, err
	}
	merged := make(map[string]bool)
	for b := range strings.FieldsSeq(out) {
		merged[b] = true
	}
	return merged, nil
}

// BranchMoved reports whether a branch's reflog shows any change since it was
// created. A fresh branch also counts as merged into its base, so callers use
// this to tell the two apart.
func BranchMoved(branch string) bool {
	out, err := Run("reflog", "show", "--format=%H", "refs/heads/"+branch, "--")
	return err == nil && strings.Contains(out, "\n")
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

// LastCommitAge returns a short relative age (e.g. "2h", "3d") of the commit
// checked out in the worktree, or "" if it can't be read.
func LastCommitAge(dir string) string {
	t, ok := lastCommitTime(dir)
	if !ok {
		return ""
	}
	return formatRelativeAge(time.Since(t))
}

// WorktreeAgeDays returns the number of days since the most recent activity
// in the worktree, or -1 if it can't be determined. Used for staleness checks.
func WorktreeAgeDays(dir string) int {
	t, ok := worktreeActivity(dir)
	if !ok {
		return -1
	}
	return int(time.Since(t).Hours() / 24)
}

// worktreeActivity returns the most recent of (worktree creation, last commit).
// Using max(...) keeps brand-new worktrees from looking stale because their
// base branch's tip is old.
func worktreeActivity(dir string) (time.Time, bool) {
	created, hasCreated := worktreeCreationTime(dir)
	committed, hasCommit := lastCommitTime(dir)
	switch {
	case hasCreated && hasCommit:
		if created.After(committed) {
			return created, true
		}
		return committed, true
	case hasCreated:
		return created, true
	case hasCommit:
		return committed, true
	}
	return time.Time{}, false
}

// worktreeCreationTime parses the first reflog entry for HEAD in the given
// worktree. Returns (zero, false) if the reflog is missing (e.g.
// core.logAllRefUpdates=false) or has been pruned by gc.
func worktreeCreationTime(dir string) (time.Time, bool) {
	pathOut, err := RunIn(dir, "rev-parse", "--git-path", "logs/HEAD")
	if err != nil {
		return time.Time{}, false
	}
	logPath := strings.TrimSpace(pathOut)
	if !filepath.IsAbs(logPath) {
		logPath = filepath.Join(dir, logPath)
	}

	f, err := os.Open(logPath)
	if err != nil {
		return time.Time{}, false
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		return time.Time{}, false
	}
	return parseReflogTime(scanner.Text())
}

// parseReflogTime extracts the unix timestamp from a reflog line of the form:
//
//	<old-sha> <new-sha> <name> <email> <unix-ts> <tz>[\t<reason>]
//
// The timestamp is the second-to-last whitespace field before the optional
// tab-separated reason.
func parseReflogTime(line string) (time.Time, bool) {
	if tab := strings.IndexByte(line, '\t'); tab >= 0 {
		line = line[:tab]
	}
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return time.Time{}, false
	}
	ts, err := strconv.ParseInt(fields[len(fields)-2], 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	return time.Unix(ts, 0), true
}

// lastCommitTime returns the committer timestamp of HEAD in the given worktree.
func lastCommitTime(dir string) (time.Time, bool) {
	out, err := RunIn(dir, "log", "-1", "--format=%ct")
	if err != nil {
		return time.Time{}, false
	}
	ts, err := strconv.ParseInt(strings.TrimSpace(out), 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	return time.Unix(ts, 0), true
}

// formatRelativeAge formats a duration as a short relative age string,
// roughly matching git's --date=relative thresholds: s, m, h, d, w, mo, y.
func formatRelativeAge(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	days := int(d.Hours() / 24)
	switch {
	case days < 14:
		return fmt.Sprintf("%dd", days)
	case days < 70:
		return fmt.Sprintf("%dw", days/7)
	case days < 365:
		return fmt.Sprintf("%dmo", days/30)
	}
	return fmt.Sprintf("%dy", days/365)
}
