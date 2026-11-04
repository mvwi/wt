package cmd

import "golang.org/x/sys/unix"

// cloneTree clones the whole directory tree in one clonefile(2) call. It fails
// on volumes without clone support instead of copying.
func cloneTree(src, dst string) error {
	return unix.Clonefile(src, dst, unix.CLONE_NOFOLLOW)
}
