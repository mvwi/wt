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
	Short:   "Rename worktree, branch, and remote",
	Long: `Rename the current worktree's directory, branch, and remote branch to match conventions.

Renames:
  - Local branch: <old> → <prefix>/<name>
  - Worktree directory: wt-<repo>-<old> → wt-<repo>-<name>
  - Remote branch: origin/<old> → origin/<prefix>/<name> (preserves PRs)`,
	Args: cobra.ExactArgs(1),
	RunE: runRename,
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

	cwd, _ := os.Getwd()
	currentBranch, err := git.CurrentBranch()
	if err != nil {
		return fmt.Errorf("not in a git repository or detached HEAD")
	}

	// Safety: must be in a worktree, not main repo
	if cwd == ctx.MainWorktree || isSubpath(cwd, ctx.MainWorktree) {
		return fmt.Errorf("cannot rename the main repository\n   Switch to a worktree first: wt switch <name>")
	}
	if currentBranch == ctx.Config.BaseBranch || currentBranch == "main" || currentBranch == "master" {
		return fmt.Errorf("cannot rename %s", currentBranch)
	}

	newName := args[0]

	// Strip prefix if user accidentally includes it
	prefix := ctx.Config.BranchPrefix
	if prefix == "" {
		prefix = ctx.Username
	}
	newName = strings.TrimPrefix(newName, prefix+"/")

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
	if cwd != newPath && git.IsDir(newPath) {
		return fmt.Errorf("directory already exists: %s", newPath)
	}

	// Check remote and PR
	hasRemote := false
	var prNum int
	var prState string

	if !renameLocalOnly {
		if git.RemoteBranchExists(ctx.Config.Remote + "/" + currentBranch) {
			hasRemote = true
			pr := github.GetPRForBranch(currentBranch)
			if pr != nil {
				prNum = pr.Number
				prState = pr.State
			}
		}
	}

	// Preview
	fmt.Println("Rename plan:")
	if currentBranch != newBranch {
		fmt.Printf("  Branch:    %s → %s\n", currentBranch, newBranch)
	} else {
		fmt.Printf("  Branch:    %s\n", ui.Dim(currentBranch+" (no change)"))
	}

	currentShort := shortName(cwd, ctx)
	if cwd != newPath {
		fmt.Printf("  Directory: %s → %s\n", currentShort, ctx.worktreeDir(newName))
	} else {
		fmt.Printf("  Directory: %s\n", ui.Dim(currentShort+" (no change)"))
	}

	if renameLocalOnly {
		fmt.Printf("  Remote:    %s\n", ui.Dim("(skipped, --local)"))
	} else if hasRemote {
		fmt.Printf("  Remote:    %s/%s → %s/%s\n", ctx.Config.Remote, currentBranch, ctx.Config.Remote, newBranch)
		if prNum > 0 && prState == "OPEN" {
			fmt.Printf("             └─ PR #%d will be updated automatically\n", prNum)
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
				_ = git.RenameBranch(newBranch, currentBranch)
			}
			return fmt.Errorf("failed to move worktree: %w", err)
		}
		ui.PrintCdHint(newPath)
	}

	// Step 3: Rename remote branch
	if hasRemote && !renameLocalOnly {
		fmt.Println("Renaming remote branch...")
		slug, err := github.RepoSlug()
		if err == nil {
			if err := github.RenameBranch(slug, currentBranch, newBranch); err != nil {
				fmt.Println()
				ui.Warn("Remote rename failed")
				fmt.Printf("   Local rename succeeded, but remote is still: %s/%s\n", ctx.Config.Remote, currentBranch)
				fmt.Println("   To fix manually:")
				fmt.Printf("   %s\n", ui.Cyan(fmt.Sprintf("git push -u %s HEAD && git push %s --delete %s", ctx.Config.Remote, ctx.Config.Remote, currentBranch)))
			} else {
				git.FetchPrune()
				_ = git.SetUpstream(ctx.Config.Remote, newBranch)
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
