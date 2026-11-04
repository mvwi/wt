package cmd

import (
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

func init() {
	addCreateFlags(pullCmd)
	rootCmd.AddCommand(pullCmd)
}

func runPull(cmd *cobra.Command, args []string) error {
	if err := startOutput(); err != nil {
		return err
	}
	ctx, err := newContext()
	if err != nil {
		return err
	}

	var name string
	if len(args) > 1 {
		name = args[1]
	}
	return createFromBranch(ctx, name, args[0])
}
