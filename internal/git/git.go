package git

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// gitCmd creates a git command with LC_ALL=C to ensure English output for parsing.
func gitCmd(args ...string) *exec.Cmd {
	cmd := exec.Command("git", args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	return cmd
}

// Run executes a git command and returns its trimmed stdout.
// If the command fails, the error includes stderr.
func Run(args ...string) (string, error) {
	return RunIn("", args...)
}

// RunIn executes a git command in a specific directory.
func RunIn(dir string, args ...string) (string, error) {
	cmd := gitCmd(args...)
	if dir != "" {
		cmd.Dir = dir
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg == "" {
			errMsg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), errMsg)
	}

	return strings.TrimSpace(stdout.String()), nil
}

// RunPassthrough executes a git command with stdout/stderr connected to the terminal.
// Used for commands where the user needs to see real-time output (rebase, push, etc.).
func RunPassthrough(args ...string) error {
	return RunPassthroughIn("", args...)
}

// RunPassthroughIn executes a git command in a directory with passthrough I/O.
func RunPassthroughIn(dir string, args ...string) error {
	cmd := gitCmd(args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// RunSilent executes a git command and discards all output.
// Returns only whether it succeeded.
func RunSilent(args ...string) error {
	cmd := gitCmd(args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run()
}

// RunSilentIn executes a git command in a directory silently.
func RunSilentIn(dir string, args ...string) error {
	cmd := gitCmd(args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run()
}
