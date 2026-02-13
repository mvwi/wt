package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mvwi/wt/internal/git"
	"github.com/mvwi/wt/internal/ui"
	"github.com/spf13/cobra"
)

const prevWorktreeStateFile = "wt-prev-worktree"

var switchCmd = &cobra.Command{
	Use:     "switch [name]",
	Aliases: []string{"sw", "cd", "checkout", "co"},
	GroupID: groupWorkflow,
	Short:   "Switch to a worktree",
	Long: `Switch to a different worktree.

Without arguments, opens an interactive picker (requires fzf).
With a name, resolves the worktree using fuzzy matching.
Use "-" to switch back to the previous worktree.

Resolution order:
  1. Exact match: wt-<repo>-<name>
  2. Main repo: 'main', base branch name, or repo name
  3. Suffix match: any worktree ending with -<name>
  4. Fuzzy match: worktree containing the search term`,
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: completeWorktreeNames,
	RunE:              runSwitch,
}

func init() {
	rootCmd.AddCommand(switchCmd)
}

func runSwitch(cmd *cobra.Command, args []string) error {
	ctx, err := newContext()
	if err != nil {
		return err
	}

	worktrees, err := git.ListWorktrees()
	if err != nil {
		return err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("cannot determine current directory: %w", err)
	}

	// Handle "wt switch -" — toggle to previous worktree
	if len(args) == 1 && args[0] == "-" {
		return switchPrevious(cwd)
	}

	// No args: interactive fzf picker
	if len(args) == 0 {
		return switchInteractive(ctx, worktrees, cwd)
	}

	return switchByName(ctx, worktrees, cwd, args[0])
}

func switchPrevious(cwd string) error {
	prev, err := git.ReadStateFile(prevWorktreeStateFile)
	if err != nil {
		return fmt.Errorf("no previous worktree to switch to\n   Use wt switch <name> to specify one")
	}

	prev = strings.TrimSpace(prev)
	if !isDir(prev) {
		return fmt.Errorf("previous worktree no longer exists: %s\n   It may have been closed. Run wt list to see available worktrees", prev)
	}

	savePreviousWorktree(cwd)
	ui.PrintCdHint(prev)
	return nil
}

func savePreviousWorktree(cwd string) {
	_ = git.SaveStateFile(prevWorktreeStateFile, cwd)
}

func switchInteractive(ctx *cmdContext, worktrees []git.Worktree, cwd string) error {
	if _, err := exec.LookPath("fzf"); err != nil {
		fmt.Println("Usage: wt switch <name>")
		fmt.Println()
		ui.DimF("Tip: Install fzf for interactive picker (brew install fzf)\n")
		return nil
	}

	// Build fzf input
	var lines []string
	for _, wt := range worktrees {
		short := ctx.shortName(wt.Path)
		branch, _ := git.CurrentBranchIn(wt.Path)
		marker := "  "
		if wt.Path == cwd || isSubpath(cwd, wt.Path) {
			marker = ui.Current + " "
		}
		lines = append(lines, fmt.Sprintf("%s%s|%s|%s", marker, short, branch, wt.Path))
	}

	input := strings.Join(lines, "\n")
	fzfCmd := exec.Command("fzf", "--ansi", "--no-sort", "--reverse",
		"--prompt=Switch to: ",
		"--header=↑↓ navigate  enter select  esc cancel",
		"--delimiter=|",
		"--with-nth=1,2",
		"--preview-window=hidden")
	fzfCmd.Stdin = strings.NewReader(input)
	fzfCmd.Stderr = os.Stderr

	out, err := fzfCmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			// exit code 130 = interrupted (Ctrl-C/Esc), 1 = no match
			if exitErr.ExitCode() == 130 || exitErr.ExitCode() == 1 {
				fmt.Println("Cancelled")
				return nil
			}
		}
		return fmt.Errorf("fzf error: %w", err)
	}

	selection := strings.TrimSpace(string(out))
	parts := strings.Split(selection, "|")
	if len(parts) < 3 {
		return fmt.Errorf("unexpected fzf output")
	}

	target := parts[2]
	savePreviousWorktree(cwd)
	ui.PrintCdHint(target)
	showSwitchSummary(target, ctx)
	return nil
}

func switchByName(ctx *cmdContext, worktrees []git.Worktree, cwd, name string) error {
	target := resolveWorktree(ctx, worktrees, name)

	if target == "" {
		return fmt.Errorf("worktree not found: %s\n   Run wt list to see available worktrees", name)
	}

	savePreviousWorktree(cwd)
	ui.PrintCdHint(target)
	showSwitchSummary(target, ctx)
	return nil
}

// resolveWorktree finds a worktree path by name using the resolution chain.
func resolveWorktree(ctx *cmdContext, worktrees []git.Worktree, name string) string {
	// 1. Exact match with configured prefix
	exact := ctx.worktreePath(name)
	if isDir(exact) {
		return exact
	}

	// 2. Main repo match (base branch name, "main", "master", or repo name)
	if name == ctx.RepoName || ctx.isBaseBranch(name) {
		return ctx.MainWorktree
	}

	// 3. Suffix match
	for _, wt := range worktrees {
		base := filepath.Base(wt.Path)
		if strings.HasSuffix(base, "-"+name) || strings.HasSuffix(base, "/"+name) {
			return wt.Path
		}
	}

	// 4. Fuzzy match (contains)
	var fuzzyMatches []git.Worktree
	for _, wt := range worktrees {
		short := ctx.shortName(wt.Path)
		if strings.Contains(strings.ToLower(short), strings.ToLower(name)) {
			fuzzyMatches = append(fuzzyMatches, wt)
		}
	}

	if len(fuzzyMatches) == 1 {
		short := ctx.shortName(fuzzyMatches[0].Path)
		ui.DimF("Fuzzy matched: %s → %s\n", name, short)
		return fuzzyMatches[0].Path
	}
	if len(fuzzyMatches) > 1 {
		fmt.Printf("Multiple matches for '%s':\n\n", name)
		for _, m := range fuzzyMatches {
			fmt.Printf("  %s\n", ctx.shortName(m.Path))
		}
		fmt.Println()
		fmt.Println("Be more specific, or use 'wt switch' for interactive picker")
		return ""
	}

	return ""
}

func showSwitchSummary(path string, ctx *cmdContext) {
	short := ctx.shortName(path)
	branch, _ := git.CurrentBranchIn(path)

	fmt.Printf("%s %s", ui.Yellow(ui.Current), short)

	if branch != short && !ctx.isBaseBranch(branch) {
		fmt.Printf("  %s", ui.Dim(branch))
	}

	if !ctx.isBaseBranch(branch) {
		ab, err := git.GetAheadBehindIn(path, ctx.baseRef())
		if err == nil {
			if ab.Behind > 0 {
				fmt.Printf("  %s", ui.Yellow(fmt.Sprintf("⚠ %d behind", ab.Behind)))
			} else if ab.Ahead > 0 {
				fmt.Printf("  %s", ui.Green(ui.Pass))
			}
		}
	}

	fmt.Println()
}
