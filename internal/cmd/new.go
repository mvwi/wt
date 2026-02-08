package cmd

import (
	"fmt"
	"strings"

	"github.com/mvwi/wt/internal/git"
	"github.com/mvwi/wt/internal/ui"
	"github.com/spf13/cobra"
)

var newCmd = &cobra.Command{
	Use:     "new <name>",
	Aliases: []string{"create"},
	GroupID: groupWorkflow,
	Short:   "Create a new worktree with a feature branch",
	Long: `Create a new worktree with a feature branch based on your configured base branch.

By default, creates a branch named "<prefix>/<name>" from the base branch
(configurable in .wt.toml, defaults to "main").

Use --from to create a worktree from an existing local or remote branch.`,
	Example: `  wt new sidebar-card              Create <user>/sidebar-card from base branch
  wt new --from feature/old        Create worktree from existing branch
  wt new fix --from origin/hotfix  Create worktree with custom name from remote
  wt new feature --init            Create + auto-initialize`,
	Args: cobra.MaximumNArgs(1),
	RunE: runNew,
}

var (
	newFromBranch string
	newDoInit     bool
)

func init() {
	newCmd.Flags().StringVarP(&newFromBranch, "from", "f", "", "base on an existing branch")
	newCmd.Flags().BoolVarP(&newDoInit, "init", "i", false, "run 'wt init' after creating")
	rootCmd.AddCommand(newCmd)
}

func runNew(cmd *cobra.Command, args []string) error {
	ctx, err := newContext()
	if err != nil {
		return err
	}

	var name string
	if len(args) > 0 {
		name = args[0]
	}

	// --from mode: create worktree from existing branch
	if newFromBranch != "" {
		return newFromExisting(ctx, name, newFromBranch)
	}

	if name == "" {
		return fmt.Errorf("name is required\n\nUsage: wt new <name>\n       wt new --from <branch>")
	}

	return newFromBase(ctx, name)
}

func newFromExisting(ctx *cmdContext, name, fromBranch string) error {
	// Fetch to get latest refs
	_ = git.FetchAll(ctx.Config.Remote)

	ref, isRemote, err := git.ResolveBranch(fromBranch, ctx.Config.Remote)
	if err != nil {
		return err
	}

	// Derive name from branch if not provided
	if name == "" {
		parts := strings.Split(ref, "/")
		name = parts[len(parts)-1]
		// Remove remote prefix if present
		name = strings.TrimPrefix(name, ctx.Config.Remote+"/")
	}

	wtPath := ctx.worktreePath(name)

	if git.IsDir(wtPath) {
		return fmt.Errorf("worktree already exists: %s", wtPath)
	}

	fmt.Println("Creating worktree from existing branch...")
	fmt.Printf("  Directory: %s\n", wtPath)
	fmt.Printf("  Branch: %s\n", ref)
	fmt.Println()

	if isRemote {
		localBranch := strings.TrimPrefix(ref, ctx.Config.Remote+"/")
		err = git.AddWorktreeFromRemote(wtPath, localBranch, ref)
		if err != nil {
			// Branch might already exist locally
			err = git.AddWorktreeFromExisting(wtPath, localBranch)
		}
	} else {
		err = git.AddWorktreeFromExisting(wtPath, ref)
	}
	if err != nil {
		return fmt.Errorf("failed to create worktree: %w", err)
	}

	fmt.Println()
	ui.Success("Created worktree")
	fmt.Println()

	if ui.Confirm("Switch to new worktree?", true) {
		ui.PrintCdHint(wtPath)
		if newDoInit {
			return runInitIn(wtPath, ctx)
		}
		fmt.Println()
		fmt.Println("Next step: wt init")
	} else {
		fmt.Println()
		fmt.Println("To start working:")
		fmt.Printf("  %s\n", ui.Cyan("wt switch "+name))
		if !newDoInit {
			fmt.Println("  # then: wt init")
		}
	}
	return nil
}

func newFromBase(ctx *cmdContext, name string) error {
	branch := ctx.branchName(name)
	wtPath := ctx.worktreePath(name)

	if git.IsDir(wtPath) {
		return fmt.Errorf("worktree already exists: %s", wtPath)
	}

	if git.BranchExists(branch) {
		return fmt.Errorf("branch already exists: %s\n   Use 'wt new --from %s' to create a worktree for it\n   Or 'wt switch %s' if worktree already exists", branch, branch, name)
	}

	fmt.Println("Creating worktree...")
	fmt.Printf("  Directory: %s\n", wtPath)
	fmt.Printf("  Branch: %s (from %s)\n", branch, ctx.Config.BaseBranch)
	fmt.Println()

	// Fetch latest base branch
	_ = git.Fetch(ctx.Config.Remote, ctx.Config.BaseBranch)

	if err := git.AddWorktree(wtPath, branch, ctx.baseRef()); err != nil {
		return fmt.Errorf("failed to create worktree: %w", err)
	}

	fmt.Println()
	ui.Success("Created worktree")
	fmt.Println()

	if ui.Confirm("Switch to new worktree?", true) {
		ui.PrintCdHint(wtPath)
		if newDoInit {
			return runInitIn(wtPath, ctx)
		}
		fmt.Println()
		fmt.Println("Next step: wt init")
	} else {
		fmt.Println()
		fmt.Println("To start working:")
		fmt.Printf("  %s\n", ui.Cyan("wt switch "+name))
		if !newDoInit {
			fmt.Println("  # then: wt init")
		}
	}
	return nil
}
