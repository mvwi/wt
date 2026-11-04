package git

// StashPush stashes uncommitted changes (including untracked files) with a message.
func StashPush(message string) error {
	_, err := Run("stash", "push", "-u", "-m", message)
	return err
}

// StashPop restores the most recent stash. It captures output so a clean pop
// doesn't print a full `git status`; on failure the error carries git's stderr.
func StashPop() error {
	_, err := Run("stash", "pop")
	return err
}
