package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mvwi/wt/internal/git"
	"github.com/mvwi/wt/internal/ui"
	"github.com/spf13/cobra"
)

var moveCmd = &cobra.Command{
	Use:     "move <name>",
	Aliases: []string{"mv", "teleport", "tp"},
	GroupID: groupManage,
	Short:   "Move uncommitted changes to another worktree",
	Long: `Move all uncommitted changes (modified, new, deleted files) to another worktree.

If the target worktree doesn't exist, offers to create a new one from the base branch.
Checks for conflicting changes in the destination before overwriting.`,
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeWorktreeNames,
	RunE:              runMove,
}

func init() {
	rootCmd.AddCommand(moveCmd)
}

func runMove(cmd *cobra.Command, args []string) error {
	ctx, err := newContext()
	if err != nil {
		return err
	}

	targetName := args[0]

	sourceRoot, err := git.TopLevel()
	if err != nil {
		return err
	}

	// Get changes
	changes, err := git.StatusPorcelain()
	if err != nil {
		return err
	}
	if len(changes) == 0 {
		return fmt.Errorf("no changes to move\n   Nothing modified, staged, or untracked in current worktree")
	}

	// Categorize: copy vs delete
	var filesToCopy, filesToDelete []string
	for _, c := range changes {
		if c.IsRename() {
			filesToDelete = append(filesToDelete, c.OldPath)
			src := filepath.Join(sourceRoot, c.Path)
			if fileExists(src) {
				filesToCopy = append(filesToCopy, c.Path)
			}
		} else if fileExists(filepath.Join(sourceRoot, c.Path)) {
			filesToCopy = append(filesToCopy, c.Path)
		} else {
			filesToDelete = append(filesToDelete, c.Path)
		}
	}

	totalCount := len(filesToCopy) + len(filesToDelete)
	if totalCount == 0 {
		return fmt.Errorf("no files to move")
	}

	// Resolve target worktree
	targetPath, createdNew, err := resolveOrCreateTarget(ctx, targetName, sourceRoot)
	if err != nil {
		return err
	}

	// Prevent moving to self
	if sourceRoot == targetPath {
		return fmt.Errorf("can't move changes to the same worktree\n   Specify a different worktree: wt move <name>")
	}

	targetShort := ctx.shortName(targetPath)

	// Safety: check for conflicting changes in destination
	destChanges, err := git.StatusPorcelainIn(targetPath)
	if err != nil {
		return fmt.Errorf("failed to check destination for conflicts: %w", err)
	}
	if len(destChanges) > 0 {
		var destFiles []string
		for _, dc := range destChanges {
			destFiles = append(destFiles, dc.Path)
		}

		var conflicts []string
		allFiles := append(filesToCopy, filesToDelete...)
		for _, f := range allFiles {
			for _, df := range destFiles {
				if f == df {
					conflicts = append(conflicts, f)
				}
			}
		}

		if len(conflicts) > 0 {
			ui.Warn("Destination has its own changes to %d file(s):", len(conflicts))
			for _, f := range conflicts {
				fmt.Printf("   • %s\n", f)
			}
			fmt.Println()
			if !ui.Confirm("Overwrite?", false) {
				fmt.Println("Aborted")
				return nil
			}
			fmt.Println()
		}
	}

	// Move files
	fmt.Printf("Moving %d file(s) → %s\n", totalCount, targetShort)

	var copied, deleted, errors int

	for _, file := range filesToCopy {
		destDir := filepath.Dir(filepath.Join(targetPath, file))
		if err := os.MkdirAll(destDir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", destDir, err)
		}

		if err := copyFile(filepath.Join(sourceRoot, file), filepath.Join(targetPath, file)); err != nil {
			fmt.Printf("  %s Failed to copy: %s\n", ui.Red(ui.Fail), file)
			errors++
		} else {
			copied++
		}
	}

	for _, file := range filesToDelete {
		dest := filepath.Join(targetPath, file)
		if fileExists(dest) {
			if err := os.Remove(dest); err != nil {
				fmt.Printf("  %s Failed to delete: %s\n", ui.Red(ui.Fail), file)
				errors++
			} else {
				deleted++
			}
		}
	}

	// Clean up source worktree
	if errors == 0 {
		_ = git.RunSilentIn(sourceRoot, "reset", ".")
		if err := git.RunSilentIn(sourceRoot, "checkout", "."); err != nil {
			ui.Warn("Could not restore source worktree: %v", err)
		}
		if err := git.RunSilentIn(sourceRoot, "clean", "-fd"); err != nil {
			ui.Warn("Could not clean source untracked files: %v", err)
		}
	}

	// Summary
	fmt.Println()
	if errors == 0 {
		ui.Success("Moved to %s", targetShort)
	} else {
		ui.Warn("Done with %d error(s) — source worktree not cleaned up", errors)
	}

	var parts []string
	if copied > 0 {
		parts = append(parts, fmt.Sprintf("%d copied", copied))
	}
	if deleted > 0 {
		parts = append(parts, fmt.Sprintf("%d deleted", deleted))
	}
	if len(parts) > 0 {
		fmt.Printf("  %s\n", strings.Join(parts, ", "))
	}

	fmt.Println()
	fmt.Println("To continue working there:")
	fmt.Printf("  %s\n", ui.Cyan("wt switch "+targetName))
	if createdNew {
		fmt.Println("  wt init    # run init commands from .wt.toml")
	}
	return nil
}

func resolveOrCreateTarget(ctx *cmdContext, name, sourceRoot string) (string, bool, error) {
	worktrees, err := git.ListWorktrees()
	if err != nil {
		return "", false, err
	}

	// Try to resolve existing worktree
	target := resolveWorktree(ctx, worktrees, name)
	if target != "" {
		return target, false, nil
	}

	// Not found — offer to create
	branch := ctx.branchName(name)
	wtPath := ctx.worktreePath(name)

	fmt.Printf("No worktree '%s' found.\n", name)
	fmt.Printf("  Branch: %s\n", branch)
	fmt.Printf("  Path:   %s\n", wtPath)
	if !ui.Confirm("Create new worktree?", false) {
		return "", false, fmt.Errorf("aborted")
	}
	fmt.Println()

	if isDir(wtPath) {
		return "", false, fmt.Errorf("directory already exists: %s", wtPath)
	}
	if git.BranchExists(branch) {
		return "", false, fmt.Errorf("branch already exists: %s\n   Use 'wt new --from %s' first, then 'wt move' to it", branch, branch)
	}

	_ = git.Fetch(ctx.Config.Remote, ctx.Config.BaseBranch)

	if err := git.AddWorktree(wtPath, branch, ctx.baseRef()); err != nil {
		return "", false, fmt.Errorf("failed to create worktree: %w", err)
	}

	ui.Success("Created worktree")
	fmt.Printf("  Path: %s\n", wtPath)
	fmt.Printf("  Branch: %s (from %s)\n", branch, ctx.Config.BaseBranch)
	fmt.Println()

	return wtPath, true, nil
}

func copyFile(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err = io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	return out.Sync()
}
