package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/mvwi/wt/internal/git"
	"github.com/mvwi/wt/internal/github"
	"github.com/mvwi/wt/internal/ui"
	"github.com/spf13/cobra"
)

var renameCmd = &cobra.Command{
	Use:     "rename <name>",
	Aliases: []string{"rn"},
	GroupID: groupManage,
	Short:   "Rename worktree, branch, and remote",
	Long: `Rename the current worktree's directory, branch, and remote branch to match conventions.

Renames:
  - Local branch: <old> → <prefix>/<name>
  - Worktree directory: wt-<repo>-<old> → wt-<repo>-<name>
  - Remote branch: origin/<old> → origin/<prefix>/<name> (recreates open PRs)`,
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeWorktreeNames,
	RunE:              runRename,
}

var renameLocalOnly bool

func init() {
	renameCmd.Flags().BoolVar(&renameLocalOnly, "local", false, "only rename locally (skip remote)")
	rootCmd.AddCommand(renameCmd)
}

func runRename(cmd *cobra.Command, args []string) error {
	ctx, err := newContext()
	if err != nil {
		return err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("cannot determine current directory: %w", err)
	}
	currentBranch, err := git.CurrentBranch()
	if err != nil {
		return fmt.Errorf("not in a git repository or detached HEAD\n   Run this from inside a worktree")
	}

	// Safety: must be in a worktree, not main repo
	if cwd == ctx.MainWorktree || isSubpath(cwd, ctx.MainWorktree) {
		return fmt.Errorf("cannot rename the main repository\n   Switch to a worktree first: wt switch <name>")
	}
	if ctx.isBaseBranch(currentBranch) {
		return fmt.Errorf("cannot rename the base branch (%s)\n   Switch to a feature worktree first", currentBranch)
	}

	newName := args[0]

	// Strip prefix if user accidentally includes it
	var prefix string
	if ctx.Config.BranchPrefix != nil {
		prefix = *ctx.Config.BranchPrefix
	} else {
		prefix = ctx.Username
	}
	if prefix != "" {
		newName = strings.TrimPrefix(newName, prefix+"/")
	}

	newBranch := ctx.branchName(newName)
	newPath := ctx.worktreePath(newName)

	// Already correct?
	if currentBranch == newBranch && cwd == newPath {
		ui.Success("Already named correctly")
		return nil
	}

	// Check collisions
	if currentBranch != newBranch && git.BranchExists(newBranch) {
		return fmt.Errorf("branch already exists: %s", newBranch)
	}
	if cwd != newPath && isDir(newPath) {
		return fmt.Errorf("directory already exists: %s", newPath)
	}

	// Check remote and PR
	hasRemote := false
	var prDetails *github.PRDetails

	if !renameLocalOnly {
		if git.RemoteBranchExists(ctx.Config.Remote + "/" + currentBranch) {
			hasRemote = true
			prDetails, _ = github.GetPRDetails(currentBranch)
		}
	}

	// Preview
	fmt.Println("Rename plan:")
	if currentBranch != newBranch {
		fmt.Printf("  Branch:    %s → %s\n", currentBranch, newBranch)
	} else {
		fmt.Printf("  Branch:    %s\n", ui.Dim(currentBranch+" (no change)"))
	}

	currentShort := ctx.shortName(cwd)
	if cwd != newPath {
		fmt.Printf("  Directory: %s → %s\n", currentShort, ctx.worktreeDir(newName))
	} else {
		fmt.Printf("  Directory: %s\n", ui.Dim(currentShort+" (no change)"))
	}

	if renameLocalOnly {
		fmt.Printf("  Remote:    %s\n", ui.Dim("(skipped, --local)"))
	} else if hasRemote {
		fmt.Printf("  Remote:    %s/%s → %s/%s\n", ctx.Config.Remote, currentBranch, ctx.Config.Remote, newBranch)
		if prDetails != nil {
			fmt.Printf("             └─ PR #%d will be recreated\n", prDetails.Number)
		}
	} else {
		fmt.Printf("  Remote:    %s\n", ui.Dim("(no remote branch)"))
	}

	fmt.Println()
	if !ui.Confirm("Proceed?", true) {
		fmt.Println("Cancelled")
		return nil
	}
	fmt.Println()

	// Step 1: Rename local branch
	if currentBranch != newBranch {
		fmt.Println("Renaming branch...")
		if err := git.RenameBranch(currentBranch, newBranch); err != nil {
			return fmt.Errorf("failed to rename branch: %w", err)
		}
	}

	// Step 2: Move worktree directory
	if cwd != newPath {
		fmt.Println("Moving worktree...")
		if err := git.MoveWorktree(cwd, newPath); err != nil {
			// Rollback branch rename
			if currentBranch != newBranch {
				fmt.Println("   Rolling back branch rename...")
				if rbErr := git.RenameBranch(newBranch, currentBranch); rbErr != nil {
					return fmt.Errorf("failed to move worktree: %w (rollback also failed: %v)", err, rbErr)
				}
			}
			return fmt.Errorf("failed to move worktree: %w", err)
		}
		ui.PrintCdHint(newPath)
	}

	// Step 3: Update remote branch
	if hasRemote && !renameLocalOnly {
		fmt.Println("Pushing new branch...")
		if err := git.PushSetUpstream(ctx.Config.Remote); err != nil {
			fmt.Println()
			ui.Warn("Failed to push new branch")
			fmt.Printf("   Local rename succeeded, but remote is still: %s/%s\n", ctx.Config.Remote, currentBranch)
			fmt.Println("   To fix manually:")
			fmt.Printf("   %s\n", ui.Cyan(fmt.Sprintf("git push -u %s HEAD && git push %s --delete %s", ctx.Config.Remote, ctx.Config.Remote, currentBranch)))
		} else {
			fmt.Println("Removing old remote branch...")
			if err := git.DeleteRemoteBranch(ctx.Config.Remote, currentBranch); err != nil {
				ui.Warn("Failed to delete old remote branch: " + currentBranch)
				fmt.Printf("   Delete manually: %s\n", ui.Cyan(fmt.Sprintf("git push %s --delete %s", ctx.Config.Remote, currentBranch)))
			}

			// Recreate PR if one existed
			if prDetails != nil {
				fmt.Println("Recreating PR...")
				labels := make([]string, len(prDetails.Labels))
				for i, l := range prDetails.Labels {
					labels[i] = l.Name
				}
				url, err := github.CreatePR(newBranch, prDetails.BaseRefName, prDetails.Title, prDetails.Body, prDetails.IsDraft, labels)
				if err != nil {
					ui.Warn("Failed to recreate PR")
					fmt.Printf("   Create manually: %s\n", ui.Cyan("gh pr create"))
				} else {
					fmt.Printf("   %s\n", url)
				}
			}
		}
	}

	fmt.Println()
	ui.Success("Renamed!")

	newCtx, _ := newContext()
	if newCtx != nil {
		path := newPath
		if cwd == newPath {
			path = cwd
		}
		showSwitchSummary(path, newCtx)
	}
	return nil
}
