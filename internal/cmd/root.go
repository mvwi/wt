package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Version is set at build time via -ldflags.
var Version = "dev"

var rootCmd = &cobra.Command{
	Use:   "wt",
	Short: "Git worktree manager",
	Long: `wt - Git Worktree Manager

A streamlined workflow for managing git worktrees. Create feature branches,
sync with your base branch, track PR status, and clean up when done.

Typical workflow:
  wt new feature        Create worktree + branch
  wt init               Install deps, copy .env, generate prisma
  ...work on feature...
  wt submit             Rebase on base branch + push
  ...merge PR...
  wt prune              Clean up merged worktrees

Configuration:
  Add .wt.toml to your repo root to customize behavior (base branch,
  branch prefix, init steps, etc.). See 'wt help' for details.`,
	SilenceUsage:  true,
	SilenceErrors: true,
	Version:       Version,
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.SetVersionTemplate("wt version {{.Version}}\n")
	rootCmd.CompletionOptions.HiddenDefaultCmd = true
}
