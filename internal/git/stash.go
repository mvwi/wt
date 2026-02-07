package git

// StashPush stashes uncommitted changes (including untracked files) with a message.
func StashPush(message string) error {
	_, err := Run("stash", "push", "-u", "-m", message)
	return err
}

// StashPop restores the most recent stash.
func StashPop() error {
	return RunPassthrough("stash", "pop")
}
