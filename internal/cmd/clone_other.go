//go:build !darwin

package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// cloneTree copies src to dst with reflinks. --reflink=always makes cp fail
// on file systems without them instead of making a full copy.
func cloneTree(src, dst string) error {
	if out, err := exec.Command("cp", "-R", "--reflink=always", src, dst).CombinedOutput(); err != nil {
		_ = os.RemoveAll(dst) // dst did not exist before; drop the partial copy
		msg, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}
