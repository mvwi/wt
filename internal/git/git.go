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

// RunIn executes a git command in a specific directory and returns its trimmed stdout.
func RunIn(dir string, args ...string) (string, error) {
	out, err := runRawIn(dir, args...)
	return strings.TrimSpace(out), err
}

// runRawIn is RunIn without trimming. Porcelain output needs this: its first
// line can start with a space (" M file").
func runRawIn(dir string, args ...string) (string, error) {
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

	return stdout.String(), nil
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

// runQuiet runs cmd with stdin and stdout on the terminal, but drops git's
// "hint:" lines and the "Could not apply <sha>... # <subject>" echo from
// stderr. wt prints its own guidance, and "error: could not apply" stays.
func runQuiet(cmd *exec.Cmd) error {
	var stderr bytes.Buffer
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	fmt.Fprint(os.Stderr, filterGitNoise(stderr.String()))
	return err
}

func filterGitNoise(s string) string {
	var b strings.Builder
	for line := range strings.SplitAfterSeq(s, "\n") {
		// Drop "Rebasing (1/2)\r" progress; on a terminal \r would have erased it.
		if i := strings.LastIndex(line, "\r"); i >= 0 {
			line = line[i+1:]
		}
		if strings.HasPrefix(line, "hint:") || strings.HasPrefix(line, "Could not apply ") {
			continue
		}
		b.WriteString(line)
	}
	return b.String()
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
