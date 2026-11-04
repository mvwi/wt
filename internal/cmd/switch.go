package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mattn/go-isatty"
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
  3. Branch name: exact match against the git branch
  4. Suffix match: any worktree ending with -<name>
  5. Fuzzy match: worktree or branch containing the search term

If a step matches more than one worktree, wt opens fzf on just those
(or lists them when fzf or a terminal isn't available).`,
	Example: `  wt switch           Interactive picker (requires fzf)
  wt switch sidebar   Switch to "sidebar" worktree (fuzzy match)
  wt switch -         Switch back to previous worktree
  wt switch main      Switch to main repository`,
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: completeWorktreeNames,
	RunE:              runSwitch,
}

func init() {
	addOutputFlag(switchCmd)
	rootCmd.AddCommand(switchCmd)
}

func runSwitch(cmd *cobra.Command, args []string) error {
	if err := startOutput(); err != nil {
		return err
	}
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
		return switchPrevious(ctx, cwd)
	}

	// No args: interactive fzf picker
	if len(args) == 0 {
		return switchInteractive(ctx, worktrees, cwd)
	}

	return switchByName(ctx, worktrees, cwd, args[0])
}

func switchPrevious(ctx *cmdContext, cwd string) error {
	prev, err := git.ReadStateFile(prevWorktreeStateFile)
	if err != nil {
		return fmt.Errorf("no previous worktree to switch to\n   Use wt switch <name> to specify one")
	}

	prev = strings.TrimSpace(prev)
	if !isDir(prev) {
		return fmt.Errorf("previous worktree no longer exists: %s\n   It may have been closed. Run wt list to see available worktrees", prev)
	}

	return finishSwitch(ctx, cwd, prev)
}

func savePreviousWorktree(cwd string) {
	_ = git.SaveStateFile(prevWorktreeStateFile, cwd)
}

func switchInteractive(ctx *cmdContext, worktrees []git.Worktree, cwd string) error {
	if jsonOut != nil {
		return fmt.Errorf("no worktree name given, and --output json never opens the picker\n   Run wt switch <name> --output json")
	}
	if _, err := exec.LookPath("fzf"); err != nil {
		return fmt.Errorf("no worktree name given, and fzf isn't installed for the picker\n   Run wt switch <name>, or install fzf (brew install fzf)")
	}

	target, err := pickWorktree(ctx, worktrees, cwd, "")
	if err != nil {
		return err
	}
	if target == "" {
		fmt.Println("Cancelled")
		return nil
	}

	return finishSwitch(ctx, cwd, target)
}

// pickWorktree opens fzf over worktrees, starting with query typed in.
// Returns "" when the user cancels.
func pickWorktree(ctx *cmdContext, worktrees []git.Worktree, cwd, query string) (string, error) {
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

	fzfCmd := exec.Command("fzf", "--ansi", "--no-sort", "--reverse",
		"--prompt=Switch to: ",
		"--header=↑↓ navigate  enter select  esc cancel",
		"--delimiter=|",
		"--with-nth=1,2",
		"--preview-window=hidden",
		"--query="+query)
	fzfCmd.Stdin = strings.NewReader(strings.Join(lines, "\n"))
	fzfCmd.Stderr = os.Stderr

	out, err := fzfCmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			// exit code 130 = interrupted (Ctrl-C/Esc), 1 = no match
			if exitErr.ExitCode() == 130 || exitErr.ExitCode() == 1 {
				return "", nil
			}
		}
		return "", fmt.Errorf("fzf error: %w", err)
	}

	parts := strings.Split(strings.TrimSpace(string(out)), "|")
	if len(parts) < 3 {
		return "", fmt.Errorf("unexpected fzf output")
	}
	return parts[2], nil
}

func switchByName(ctx *cmdContext, worktrees []git.Worktree, cwd, name string) error {
	target, _, err := resolveWorktree(ctx, worktrees, name)
	if err != nil {
		return err
	}
	if target == "" {
		return fmt.Errorf("worktree not found: %s\n   Run wt list to see available worktrees", name)
	}

	return finishSwitch(ctx, cwd, target)
}

// finishSwitch records where we came from, hands target to the shell
// wrapper, then prints the summary or JSON.
func finishSwitch(ctx *cmdContext, cwd, target string) error {
	savePreviousWorktree(cwd)
	ui.PrintCdHint(target)
	if jsonOut == nil {
		showSwitchSummary(target, ctx)
		return nil
	}

	branch, _ := git.CurrentBranchIn(target)
	var cta []string
	if !ctx.isBaseBranch(branch) {
		cta = []string{"wt init"}
		if ab, err := git.GetAheadBehindIn(target, ctx.baseRef()); err == nil && ab.Behind > 0 {
			cta = []string{"wt rebase", "wt init"}
		}
	}
	return printJSON(worktreeJSON{
		Name:   ctx.shortName(target),
		Path:   target,
		Branch: branch,
		Base:   ctx.Config.BaseBranch,
		PR:     readPRCache()[ctx.MainWorktree][branch].Number,
		CTA:    cta,
	})
}

// resolveWorktree finds a worktree path by name using the resolution chain.
// Returns (path, fuzzy, nil) on success — fuzzy=true when the match came from
// the substring-contains step (step 5), meaning the caller should confirm
// before destructive actions. Returns ("", false, nil) when no match is found.
// When several worktrees match, it opens fzf on them if it can, and
// otherwise returns an error that lists them.
func resolveWorktree(ctx *cmdContext, worktrees []git.Worktree, name string) (string, bool, error) {
	matches, fuzzy := matchWorktrees(ctx, worktrees, name)
	switch len(matches) {
	case 0:
		return "", false, nil
	case 1:
		if fuzzy {
			ui.DimF("Fuzzy matched: %s → %s\n", name, ctx.shortName(matches[0].Path))
		}
		return matches[0].Path, fuzzy, nil
	}

	_, fzfErr := exec.LookPath("fzf")
	if fzfErr == nil && jsonOut == nil && isatty.IsTerminal(os.Stdin.Fd()) {
		target, err := pickWorktree(ctx, matches, "", name)
		if err != nil {
			return "", false, err
		}
		if target == "" {
			fmt.Println("Cancelled")
			return "", false, errSilent
		}
		return target, false, nil
	}

	var names []string
	for _, m := range matches {
		names = append(names, "     "+ctx.shortName(m.Path))
	}
	return "", false, fmt.Errorf("%q matches more than one worktree:\n%s\n   Use a longer name", name, strings.Join(names, "\n"))
}

// matchWorktrees returns the worktrees that match name at the first step of
// the resolution chain that matches any. fuzzy is true for step 5 matches.
func matchWorktrees(ctx *cmdContext, worktrees []git.Worktree, name string) ([]git.Worktree, bool) {
	// 1. Exact match with configured prefix
	if exact := ctx.worktreePath(name); isDir(exact) {
		return []git.Worktree{{Path: exact}}, false
	}

	// 2. Main repo match (base branch name, "main", "master", or repo name)
	if name == ctx.RepoName || ctx.isBaseBranch(name) {
		return []git.Worktree{{Path: ctx.MainWorktree}}, false
	}

	// 3. Branch name match (exact match against git branch)
	for _, wt := range worktrees {
		if wt.Branch == name {
			return []git.Worktree{wt}, false
		}
	}

	// 4. Suffix match
	var matches []git.Worktree
	for _, wt := range worktrees {
		base := filepath.Base(wt.Path)
		if strings.HasSuffix(base, "-"+name) || strings.HasSuffix(base, "/"+name) {
			matches = append(matches, wt)
		}
	}
	if len(matches) > 0 {
		return matches, false
	}

	// 5. Fuzzy match (contains) — checks both short name and branch
	nameLower := strings.ToLower(name)
	for _, wt := range worktrees {
		short := ctx.shortName(wt.Path)
		if strings.Contains(strings.ToLower(short), nameLower) ||
			strings.Contains(strings.ToLower(wt.Branch), nameLower) {
			matches = append(matches, wt)
		}
	}
	return matches, true
}

func showSwitchSummary(path string, ctx *cmdContext) {
	short := ctx.shortName(path)
	branch, _ := git.CurrentBranchIn(path)
	isBase := ctx.isBaseBranch(branch)

	fmt.Printf("%s %s", ui.Yellow(ui.Current), short)

	if branch != short && !isBase {
		fmt.Printf("  %s", ui.Dim(branch))
	}

	isBehind := false
	if !isBase {
		ab, err := git.GetAheadBehindIn(path, ctx.baseRef())
		if err == nil {
			if ab.Behind > 0 {
				fmt.Printf("  %s", ui.Yellow(fmt.Sprintf("⚠ %d behind", ab.Behind)))
				isBehind = true
			} else if ab.Ahead > 0 {
				fmt.Printf("  %s", ui.Green(ui.Pass))
			}
		}
	}

	fmt.Println()

	if !isBase {
		if isBehind {
			ui.PrintCTA("wt rebase", "wt init")
		} else {
			ui.PrintCTA("wt init")
		}
	}
}
