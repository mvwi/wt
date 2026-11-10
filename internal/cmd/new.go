package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
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
	Example: `  wt new sidebar-card               Create <user>/sidebar-card from base branch
  wt new --from feature/old         Create worktree from existing branch
  wt new fix --from origin/hotfix   Create worktree with custom name from remote
  wt new --from #123                Create worktree from PR #123's branch
  wt new feature --init             Create + auto-initialize
  wt new fix-123 -i --run "claude 'fix issue 123'"
                                    Create, init, then start a command in it`,
	Args: cobra.MaximumNArgs(1),
	RunE: runNew,
}

var newFromBranch string

// Flags shared by new, pr and pull. Only one command runs per process.
var (
	createInit bool
	createEdit bool
	createRun  string
)

func init() {
	newCmd.Flags().StringVarP(&newFromBranch, "from", "f", "", "base on an existing branch or PR number")
	addCreateFlags(newCmd)
	rootCmd.AddCommand(newCmd)
}

func addCreateFlags(c *cobra.Command) {
	c.Flags().BoolVarP(&createInit, "init", "i", false, "run 'wt init' after creating")
	c.Flags().BoolVarP(&createEdit, "edit", "e", false, "open the new worktree in your editor")
	c.Flags().StringVar(&createRun, "run", "", "run a shell command in the new worktree and exit with its code")
	addOutputFlag(c)
}

func runNew(cmd *cobra.Command, args []string) error {
	if err := startOutput(); err != nil {
		return err
	}
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
		if prNumber, ok := parsePRNumber(newFromBranch); ok {
			return newFromPR(ctx, name, prNumber)
		}
		return createFromBranch(ctx, name, newFromBranch)
	}

	if name == "" {
		return fmt.Errorf("name is required\n\nUsage: wt new <name>\n       wt new --from <branch>")
	}
	if err := validateName(ctx, name); err != nil {
		return err
	}

	return newFromBase(ctx, name)
}

// createFromBranch creates a worktree for an existing local or remote
// branch. Used by `wt new --from` and `wt pull`.
func createFromBranch(ctx *cmdContext, name, branch string) error {
	// Fetch so ResolveBranch can find remote branches
	spin := ui.NewSpinner("Fetching latest refs")
	err := git.FetchAll(ctx.Config.Remote)
	spin.Stop()
	if err != nil {
		ui.Warn("Fetch failed: %v", err)
	}

	ref, _, err := git.ResolveBranch(branch, ctx.Config.Remote)
	if err != nil {
		return err
	}

	if name == "" {
		name = nameFromBranch(ref)
	}
	if err := validateName(ctx, name); err != nil {
		return err
	}

	return createWorktreeFromRemote(ctx, name, ref, 0)
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

	return checkoutPR(ctx, name, pr)
}

// prBranch returns the local branch for a PR. A fork PR's head branch does not
// exist on our remote, so it gets its own pr/<N>-<head> name to avoid clashing
// with local branches.
func prBranch(pr *github.PRInfo) string {
	if pr.IsCrossRepository {
		return fmt.Sprintf("pr/%d-%s", pr.Number, pr.HeadRefName)
	}
	return pr.HeadRefName
}

// checkoutPR fetches a PR's head and creates a worktree for it. Used by both
// `wt pr` and `wt new --from <number>`.
func checkoutPR(ctx *cmdContext, name string, pr *github.PRInfo) error {
	branch := prBranch(pr)
	if path := worktreeForBranch(branch); path != "" {
		short := ctx.shortName(path)
		return fmt.Errorf("PR #%d is already checked out as %s\n   Run wt switch %s", pr.Number, short, short)
	}
	if name == "" {
		name = nameFromBranch(pr.HeadRefName)
	}
	if err := validateName(ctx, name); err != nil {
		return err
	}

	// Fork PRs: GitHub mirrors every PR head at pull/<N>/head on the base repo.
	ref := pr.HeadRefName
	if pr.IsCrossRepository {
		ref = fmt.Sprintf("pull/%d/head:%s", pr.Number, branch)
	}
	spin := ui.NewSpinner(fmt.Sprintf("Fetching PR #%d", pr.Number))
	err := git.Fetch(ctx.Config.Remote, ref)
	spin.Stop()
	if err != nil {
		ui.Warn("Fetch failed: %v", err)
	}

	if pr.IsCrossRepository {
		ui.Warn("PR #%d comes from the fork %s. Pushes from %s go nowhere until you add that fork as a remote.",
			pr.Number, pr.HeadRepository.NameWithOwner, branch)
		if pr.MaintainerCanModify {
			owner, _, _ := strings.Cut(pr.HeadRepository.NameWithOwner, "/")
			fmt.Printf("   The author allows edits: git remote add %s https://github.com/%s.git\n",
				owner, pr.HeadRepository.NameWithOwner)
			fmt.Printf("   then: git push %s HEAD:%s\n", owner, pr.HeadRefName)
		}
		fmt.Println()
	}

	return createWorktreeFromRemote(ctx, name, branch, pr.Number)
}

// worktreeForBranch returns the path of the worktree that has branch checked
// out, or "" if none does.
func worktreeForBranch(branch string) string {
	wts, err := git.ListWorktrees()
	if err != nil {
		return ""
	}
	for _, wt := range wts {
		if wt.Branch == branch {
			return wt.Path
		}
	}
	return ""
}

// nameFromBranch derives a short worktree name from a branch ref: the last
// segment after "/".
//
//	"origin/user/fix-login" → "fix-login"
//	"user/fix-login"        → "fix-login"
//	"fix-login"             → "fix-login"
func nameFromBranch(ref string) string {
	return ref[strings.LastIndex(ref, "/")+1:]
}

// createWorktreeFromRemote checks out an existing branch, local or remote,
// into a new worktree. Callers fetch first. pr is the PR number, or 0. Used by
// `wt new --from`, `wt pull` and `wt pr`.
func createWorktreeFromRemote(ctx *cmdContext, name, remoteBranch string, pr int) error {
	wtPath := ctx.worktreePath(name)

	if isDir(wtPath) {
		return fmt.Errorf("worktree already exists: %s\n   Use wt switch %s to switch to it", wtPath, name)
	}

	localBranch := strings.TrimPrefix(remoteBranch, ctx.Config.Remote+"/")
	remoteRef := ctx.Config.Remote + "/" + localBranch

	if path := worktreeForBranch(localBranch); path != "" {
		short := ctx.shortName(path)
		return fmt.Errorf("%s is already checked out as %s\n   Run wt switch %s", localBranch, short, short)
	}

	fmt.Println("Creating worktree from existing branch…")
	fmt.Printf("  Directory: %s\n", wtPath)
	fmt.Printf("  Branch: %s\n", localBranch)
	fmt.Println()

	var err error
	if git.BranchExists(localBranch) {
		err = git.AddWorktreeFromExisting(wtPath, localBranch)
	} else {
		err = git.AddWorktree(wtPath, localBranch, remoteRef)
	}
	if err != nil {
		return fmt.Errorf("failed to create worktree: %w", err)
	}

	fmt.Println()
	ui.Success("Created worktree")
	fmt.Println()

	return finishCreate(ctx, name, wtPath, localBranch, pr)
}

// finishCreate runs the steps that follow a new worktree: finishNew (init,
// hooks, hint; the hint goes to stderr in JSON mode), then the JSON, --edit
// and --run.
func finishCreate(ctx *cmdContext, name, path, branch string, pr int) error {
	if err := finishNew(ctx, name, path, createInit); err != nil {
		return err
	}
	if jsonOut != nil {
		cta := []string{"wt switch " + name}
		if !createInit {
			cta = append(cta, "wt init")
		}
		if err := printJSON(worktreeJSON{Name: name, Path: path, Branch: branch, Base: ctx.Config.BaseBranch, PR: pr, CTA: cta}); err != nil {
			return err
		}
	}

	if createEdit {
		if err := openEditor(path); err != nil {
			return err
		}
	}
	if createRun != "" {
		return runCommandIn(path, createRun)
	}
	return nil
}

// runCommandIn runs command with sh -c inside dir, attached to the terminal.
// It returns an exitCode error when the command fails, so wt exits with the
// same code.
func runCommandIn(dir, command string) error {
	c := exec.Command("sh", "-c", command)
	c.Dir = dir
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr

	// Ctrl-C belongs to the command. Catching it here keeps wt alive until
	// the command exits; the command still gets the signal.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	defer signal.Stop(sig)

	err := c.Run()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitCode(max(exitErr.ExitCode(), 1))
	}
	return err
}

// printSwitchHint prints the next-step command.
func printSwitchHint(name string) {
	fmt.Printf("  → wt switch %s && wt init\n", name)
	ui.PrintWrapperHint()
	ui.PrintCTA("wt switch "+name, "wt init")
}

// validateName rejects worktree names that would break git or hide the
// main checkout from 'wt switch'. Call it before doing any work.
func validateName(ctx *cmdContext, name string) error {
	if strings.HasPrefix(name, "-") {
		return fmt.Errorf("invalid name %q: it can't start with a dash", name)
	}
	if ctx.isBaseBranch(name) || name == ctx.RepoName {
		return fmt.Errorf("invalid name %q: 'wt switch %s' would open this worktree instead of the main checkout\n   Pick another name", name, name)
	}
	if branch := ctx.branchName(name); !git.IsValidBranchName(branch) {
		return fmt.Errorf("invalid name %q: %q is not a valid git branch name", name, branch)
	}
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

	fmt.Println("Creating worktree…")
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

	return finishCreate(ctx, name, wtPath, branch, 0)
}
