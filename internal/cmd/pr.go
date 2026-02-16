package cmd

import (
	"fmt"
	"strconv"

	"github.com/mvwi/wt/internal/github"
	"github.com/spf13/cobra"
)

var prCmd = &cobra.Command{
	Use:     "pr <number>",
	GroupID: groupWorkflow,
	Short:   "Checkout a PR into a worktree",
	Long: `Fetch a GitHub pull request into its own worktree.

Looks up the PR by number, resolves the branch, and creates a worktree
for it — useful for code review workflows.

Requires the GitHub CLI (gh) to be installed.`,
	Example: `  wt pr 123          Checkout PR #123 into a worktree
  wt pr 123 --init   Checkout + auto-initialize`,
	Args: cobra.ExactArgs(1),
	RunE: runPR,
}

var prDoInit bool

func init() {
	prCmd.Flags().BoolVarP(&prDoInit, "init", "i", false, "run 'wt init' after creating")
	rootCmd.AddCommand(prCmd)
}

func runPR(cmd *cobra.Command, args []string) error {
	if !github.IsAvailable() {
		return fmt.Errorf("gh CLI is required for this command (brew install gh)")
	}

	number, err := strconv.Atoi(args[0])
	if err != nil {
		return fmt.Errorf("invalid PR number: %s", args[0])
	}

	ctx, err := newContext()
	if err != nil {
		return err
	}

	pr, err := github.GetPRByNumber(number)
	if err != nil {
		return fmt.Errorf("failed to fetch PR #%d: %w", number, err)
	}

	if pr.State != "OPEN" {
		return fmt.Errorf("PR #%d is %s", number, pr.State)
	}

	name := nameFromBranch(pr.HeadRefName, ctx.Config.Remote)

	fmt.Printf("PR #%d: %s\n", pr.Number, pr.Title)
	fmt.Println()

	return createWorktreeFromRemote(ctx, name, pr.HeadRefName, prDoInit)
}
