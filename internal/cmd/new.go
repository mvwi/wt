package cmd

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/mvwi/wt/internal/git"
	"github.com/mvwi/wt/internal/github"
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

Use --from to create a worktree from an existing branch or PR number.`,
	Example: `  wt new sidebar-card              Create <user>/sidebar-card from base branch
  wt new --from feature/old        Create worktree from existing branch
  wt new fix --from origin/hotfix  Create worktree with custom name from remote
  wt new --from #123               Create worktree from PR #123's branch
  wt new feature --init            Create + auto-initialize`,
	Args: cobra.MaximumNArgs(1),
	RunE: runNew,
}

var (
	newFromBranch string
	newDoInit     bool
)

func init() {
	newCmd.Flags().StringVarP(&newFromBranch, "from", "f", "", "base on an existing branch or PR number")
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
	// Check if --from is a PR number (e.g., "#123" or "123")
	if prNumber, ok := parsePRNumber(fromBranch); ok {
		return newFromPR(ctx, name, prNumber)
	}

	// Fetch to get latest refs (needed for ResolveBranch to find remote branches)
	spin := ui.NewSpinner("Fetching latest refs")
	if err := git.FetchAll(ctx.Config.Remote); err != nil {
		spin.Stop()
		ui.Warn("Fetch failed: %v", err)
	} else {
		spin.Stop()
	}

	ref, isRemote, err := git.ResolveBranch(fromBranch, ctx.Config.Remote)
	if err != nil {
		return err
	}

	// Derive name from branch if not provided
	if name == "" {
		name = nameFromBranch(ref, ctx.Config.Remote)
	}

	if isRemote {
		return createWorktreeFromRemote(ctx, name, ref, newDoInit)
	}

	// Local branch path
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

	if newDoInit {
		return runInitIn(wtPath, ctx)
	}
	fmt.Printf("  → wt switch %s && wt init\n", name)
	return nil
}

// parsePRNumber checks if s looks like a PR number ("#123" or "123")
// and returns the parsed number if so.
func parsePRNumber(s string) (int, bool) {
	s = strings.TrimPrefix(s, "#")
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

func newFromPR(ctx *cmdContext, name string, number int) error {
	if !github.IsAvailable() {
		return fmt.Errorf("gh CLI is required to resolve PR numbers (brew install gh)")
	}

	spin := ui.NewSpinner(fmt.Sprintf("Fetching PR #%d", number))
	pr, err := github.GetPRByNumber(number)
	spin.Stop()
	if err != nil {
		return fmt.Errorf("failed to fetch PR #%d: %w", number, err)
	}

	fmt.Printf("PR #%d: %s\n", pr.Number, pr.Title)
	fmt.Println()

	if name == "" {
		name = nameFromBranch(pr.HeadRefName, ctx.Config.Remote)
	}

	return createWorktreeFromRemote(ctx, name, pr.HeadRefName, newDoInit)
}

// nameFromBranch derives a short worktree name from a branch ref.
// Takes the last segment after "/" and strips the remote prefix if present.
//
//	"origin/user/fix-login" → "fix-login"
//	"user/fix-login"        → "fix-login"
//	"fix-login"             → "fix-login"
func nameFromBranch(ref, remote string) string {
	parts := strings.Split(ref, "/")
	name := parts[len(parts)-1]
	return strings.TrimPrefix(name, remote+"/")
}

// createWorktreeFromRemote handles the common flow for checking out a remote
// branch into a new worktree: check if it already exists, fetch, create, and
// optionally run init. Used by both `wt new --from` and `wt pr`.
func createWorktreeFromRemote(ctx *cmdContext, name, remoteBranch string, doInit bool) error {
	wtPath := ctx.worktreePath(name)

	if isDir(wtPath) {
		return fmt.Errorf("worktree already exists: %s\n   Use wt switch %s to switch to it", wtPath, name)
	}

	// Fetch to get latest refs
	spin := ui.NewSpinner("Fetching latest refs")
	if err := git.FetchAll(ctx.Config.Remote); err != nil {
		spin.Stop()
		ui.Warn("Fetch failed: %v", err)
	} else {
		spin.Stop()
	}

	localBranch := strings.TrimPrefix(remoteBranch, ctx.Config.Remote+"/")
	remoteRef := ctx.Config.Remote + "/" + localBranch

	fmt.Println("Creating worktree from existing branch...")
	fmt.Printf("  Directory: %s\n", wtPath)
	fmt.Printf("  Branch: %s\n", localBranch)
	fmt.Println()

	err := git.AddWorktreeFromRemote(wtPath, localBranch, remoteRef)
	if err != nil {
		// Branch might already exist locally
		err = git.AddWorktreeFromExisting(wtPath, localBranch)
	}
	if err != nil {
		return fmt.Errorf("failed to create worktree: %w", err)
	}

	fmt.Println()
	ui.Success("Created worktree")
	fmt.Println()

	if doInit {
		return runInitIn(wtPath, ctx)
	}
	fmt.Printf("  → wt switch %s && wt init\n", name)
	return nil
}

func newFromBase(ctx *cmdContext, name string) error {
	branch := ctx.branchName(name)
	wtPath := ctx.worktreePath(name)

	if isDir(wtPath) {
		return fmt.Errorf("worktree already exists: %s\n   Use wt switch %s to switch to it", wtPath, name)
	}

	if git.BranchExists(branch) {
		return fmt.Errorf("branch already exists: %s\n   Use wt new --from %s to create a worktree for it\n   Or wt switch %s if the worktree already exists", branch, branch, name)
	}

	fmt.Println("Creating worktree...")
	fmt.Printf("  Directory: %s\n", wtPath)
	fmt.Printf("  Branch: %s (from %s)\n", branch, ctx.Config.BaseBranch)
	fmt.Println()

	// Fetch latest base branch
	spin := ui.NewSpinner(fmt.Sprintf("Fetching %s", ctx.Config.BaseBranch))
	if err := git.Fetch(ctx.Config.Remote, ctx.Config.BaseBranch); err != nil {
		spin.Stop()
		ui.Warn("Fetch failed: %v", err)
	} else {
		spin.Stop()
	}

	if err := git.AddWorktree(wtPath, branch, ctx.baseRef()); err != nil {
		return fmt.Errorf("failed to create worktree: %w", err)
	}

	fmt.Println()
	ui.Success("Created worktree")
	fmt.Println()

	if newDoInit {
		return runInitIn(wtPath, ctx)
	}
	fmt.Printf("  → wt switch %s && wt init\n", name)
	return nil
}
