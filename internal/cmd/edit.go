package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mvwi/wt/internal/git"
	"github.com/spf13/cobra"
)

var editCmd = &cobra.Command{
	Use:     "edit [name]",
	GroupID: groupWorkflow,
	Short:   "Open a worktree in your editor",
	Long: `Open a worktree in your editor. Without a name, opens the current worktree.

wt uses $WT_EDITOR, then $VISUAL, then the first of cursor, code or zed
on your PATH. A terminal editor (vim, nvim, hx, ...) runs in the
foreground inside the worktree; a GUI editor opens and wt returns at once.`,
	Example: `  wt edit            Open the current worktree
  wt edit sidebar    Open the "sidebar" worktree
  WT_EDITOR="code -n" wt edit sidebar`,
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: completeWorktreeNames,
	RunE:              runEdit,
}

func init() {
	rootCmd.AddCommand(editCmd)
}

func runEdit(cmd *cobra.Command, args []string) error {
	ctx, err := newContext()
	if err != nil {
		return err
	}
	if len(args) == 0 {
		top, err := git.TopLevel()
		if err != nil {
			return err
		}
		return openEditor(top)
	}

	worktrees, err := git.ListWorktrees()
	if err != nil {
		return err
	}
	path, _, err := resolveWorktree(ctx, worktrees, args[0])
	if err != nil {
		return err
	}
	if path == "" {
		return fmt.Errorf("worktree not found: %s\n   Run wt list to see available worktrees", args[0])
	}
	return openEditor(path)
}

// terminalEditors run in the foreground; anything else counts as a GUI editor.
var terminalEditors = map[string]bool{
	"vi": true, "vim": true, "nvim": true, "hx": true, "helix": true,
	"nano": true, "emacs": true, "micro": true, "kak": true,
}

// resolveEditor picks the editor command line: $WT_EDITOR, $VISUAL, then the
// first of cursor, code, zed on PATH.
func resolveEditor(getenv func(string) string, lookPath func(string) (string, error)) (string, error) {
	for _, env := range []string{"WT_EDITOR", "VISUAL"} {
		if e := strings.TrimSpace(getenv(env)); e != "" {
			return e, nil
		}
	}
	for _, e := range []string{"cursor", "code", "zed"} {
		if _, err := lookPath(e); err == nil {
			return e, nil
		}
	}
	return "", fmt.Errorf("no editor found\n   Set $WT_EDITOR (e.g. export WT_EDITOR=code), or install cursor, code or zed")
}

func isTerminalEditor(editor string) bool {
	fields := strings.Fields(editor)
	return len(fields) > 0 && terminalEditors[filepath.Base(fields[0])]
}

// openEditor opens path in the user's editor.
func openEditor(path string) error {
	editor, err := resolveEditor(os.Getenv, exec.LookPath)
	if err != nil {
		return err
	}
	// sh -c lets the editor setting carry flags, such as "code -n".
	c := exec.Command("sh", "-c", editor+` "$1"`, "sh", path)
	if isTerminalEditor(editor) {
		c.Dir = path
		c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
		return c.Run()
	}
	if err := c.Start(); err != nil {
		return fmt.Errorf("failed to start %s: %w", editor, err)
	}
	fmt.Printf("Opened %s in %s\n", path, editor)
	return nil
}
