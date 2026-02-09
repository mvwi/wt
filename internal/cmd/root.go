package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/fatih/color"
	"github.com/mvwi/wt/internal/git"
	"github.com/mvwi/wt/internal/github"
	"github.com/mvwi/wt/internal/ui"
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
  Global:    ~/.config/wt/config.toml
  Per-repo:  .wt.toml in repo root`,
	SilenceUsage:              true,
	SilenceErrors:             true,
	SuggestionsMinimumDistance: 2,
	Version:                   Version,
	RunE:                      runStatus,
}

// Command group IDs
const (
	groupWorkflow = "workflow"
	groupSync     = "sync"
	groupManage   = "manage"
)

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		// Cobra includes "Did you mean this?" in the error message
		// when SuggestionsMinimumDistance is set. Since we silence
		// errors to control output, we print the error ourselves.
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	// The shell wrapper redirects stdout to a temp file to intercept cd hints,
	// which causes fatih/color to detect a non-TTY and disable colors.
	// The wrapper sets WT_FORCE_COLOR=1 to preserve colored output.
	// NO_COLOR takes precedence (explicit user preference to disable colors).
	if os.Getenv("WT_FORCE_COLOR") != "" && os.Getenv("NO_COLOR") == "" {
		color.NoColor = false
	}

	rootCmd.SetVersionTemplate("wt version {{.Version}}\n")
	rootCmd.CompletionOptions.HiddenDefaultCmd = true

	rootCmd.AddGroup(
		&cobra.Group{ID: groupWorkflow, Title: "Workflow:"},
		&cobra.Group{ID: groupSync, Title: "Sync:"},
		&cobra.Group{ID: groupManage, Title: "Manage:"},
	)
}

// runStatus is called when `wt` is run with no subcommand.
// Shows a quick status of the current worktree, or help if not in a repo.
func runStatus(cmd *cobra.Command, args []string) error {
	if !git.IsInsideWorkTree() {
		return cmd.Help()
	}

	ctx, err := newContext()
	if err != nil {
		return cmd.Help()
	}

	cwd, _ := os.Getwd()
	branch, err := git.CurrentBranch()
	if err != nil {
		return cmd.Help()
	}

	// Worktree name
	short := shortName(cwd, ctx)
	isMain := cwd == ctx.MainWorktree || isSubpath(cwd, ctx.MainWorktree)

	fmt.Printf("%s %s", ui.Yellow(ui.Current), short)
	if branch != short {
		fmt.Printf("  %s", ui.Dim(branch))
	}
	fmt.Println()

	// Base/main branch: just show it
	if isMain || branch == ctx.Config.BaseBranch || branch == "main" || branch == "master" {
		return nil
	}

	// Sync status
	ab, err := git.GetAheadBehind(ctx.baseRef())
	if err == nil {
		var parts []string
		if ab.Behind > 0 {
			parts = append(parts, ui.Yellow(fmt.Sprintf("%s%d behind %s", ui.ArrowDown, ab.Behind, ctx.Config.BaseBranch)))
		}
		if ab.Ahead > 0 {
			parts = append(parts, ui.Green(fmt.Sprintf("%s%d ahead", ui.ArrowUp, ab.Ahead)))
		}
		if len(parts) > 0 {
			fmt.Printf("  %s\n", strings.Join(parts, "  "))
		} else {
			fmt.Printf("  %s\n", ui.Green("up to date with "+ctx.Config.BaseBranch))
		}
	}

	// Uncommitted changes
	if git.HasChanges() {
		statusShort, _ := git.StatusShort()
		lines := strings.Split(strings.TrimSpace(statusShort), "\n")
		fmt.Printf("  %s\n", ui.Yellow(fmt.Sprintf("%d uncommitted change(s)", len(lines))))
	}

	// Unpushed commits
	unpushed := git.UnpushedCountIn(cwd)
	if unpushed > 0 {
		fmt.Printf("  %s\n", ui.Cyan(fmt.Sprintf("%s%d unpushed commit(s)", ui.PushUp, unpushed)))
	}

	// PR status
	if github.IsAvailable() {
		pr := github.GetPRForBranch(branch)
		if pr != nil && pr.State == "OPEN" {
			rs := pr.GetReviewSummary()
			cs := pr.GetCISummary()

			prStr := fmt.Sprintf("PR #%d", pr.Number)
			var details []string
			if rs.Approved > 0 {
				details = append(details, fmt.Sprintf("%d approved", rs.Approved))
			}
			if rs.Changes > 0 {
				details = append(details, fmt.Sprintf("%d changes requested", rs.Changes))
			}
			if rs.Pending > 0 {
				details = append(details, fmt.Sprintf("%d pending", rs.Pending))
			}
			if cs.Fail > 0 {
				details = append(details, ui.Red("CI failing"))
			} else if cs.Pending > 0 {
				details = append(details, ui.Yellow("CI pending"))
			} else if cs.Pass > 0 {
				details = append(details, ui.Green("CI passing"))
			}

			if len(details) > 0 {
				fmt.Printf("  %s  %s\n", ui.Blue(prStr), strings.Join(details, ", "))
			} else {
				fmt.Printf("  %s\n", ui.Blue(prStr))
			}
		}
	}

	return nil
}
