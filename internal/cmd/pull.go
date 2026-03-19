package cmd

import (
	"fmt"

	"github.com/mvwi/wt/internal/git"
	"github.com/mvwi/wt/internal/ui"
	"github.com/spf13/cobra"
)

var pullCmd = &cobra.Command{
	Use:     "pull <branch> [name]",
	GroupID: groupWorkflow,
	Short:   "Pull a remote branch into a new worktree",
	Long: `Fetch a remote branch and create a worktree for it.

This is a shorthand for "wt new [name] --from <branch>". If no name is
given, one is derived from the last segment of the branch name.`,
	Example: `  wt pull feature/login       Create worktree from remote branch
  wt pull user/sidebar card   Create worktree named "card" from remote branch
  wt pull feature/login -i    Pull + auto-initialize`,
	Args: cobra.RangeArgs(1, 2),
	RunE: runPull,
}

var pullDoInit bool

func init() {
	pullCmd.Flags().BoolVarP(&pullDoInit, "init", "i", false, "run 'wt init' after creating")
	rootCmd.AddCommand(pullCmd)
}

func runPull(cmd *cobra.Command, args []string) error {
	ctx, err := newContext()
	if err != nil {
		return err
	}

	branch := args[0]
	var name string
	if len(args) > 1 {
		name = args[1]
	}

	// Fetch to get latest refs
	spin := ui.NewSpinner("Fetching latest refs")
	if err := git.FetchAll(ctx.Config.Remote); err != nil {
		spin.Stop()
		ui.Warn("Fetch failed: %v", err)
	} else {
		spin.Stop()
	}

	ref, isRemote, err := git.ResolveBranch(branch, ctx.Config.Remote)
	if err != nil {
		return err
	}

	if name == "" {
		name = nameFromBranch(ref, ctx.Config.Remote)
	}

	if isRemote {
		return createWorktreeFromRemote(ctx, name, ref, pullDoInit)
	}

	// Local branch — same path as wt new --from with a local branch
	wtPath := ctx.worktreePath(name)

	if isDir(wtPath) {
		return fmt.Errorf("worktree already exists: %s\n   Use wt switch %s to switch to it", wtPath, name)
	}

	fmt.Println("Creating worktree from existing branch...")
	fmt.Printf("  Directory: %s\n", wtPath)
	fmt.Printf("  Branch: %s\n", ref)
	fmt.Println()

	if err := git.AddWorktreeFromExisting(wtPath, ref); err != nil {
		return fmt.Errorf("failed to create worktree: %w", err)
	}

	fmt.Println()
	ui.Success("Created worktree")
	fmt.Println()

	if pullDoInit {
		return runInitIn(wtPath, ctx)
	}
	printSwitchHint(name)
	return nil
}
