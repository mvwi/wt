package cmd

import (
	"path/filepath"
	"strings"

	"github.com/mvwi/wt/internal/config"
	"github.com/mvwi/wt/internal/git"
	"github.com/spf13/cobra"
)

// cmdContext holds resolved repo info + config that most commands need.
type cmdContext struct {
	Config       *config.Config
	RepoName     string
	MainWorktree string
	ParentDir    string
	Username     string
}

// newContext builds shared context from the current repo.
func newContext() (*cmdContext, error) {
	mainWT, err := git.MainWorktree()
	if err != nil {
		return nil, err
	}

	repo, err := git.RepoName()
	if err != nil {
		return nil, err
	}

	cfg, err := config.Load(mainWT, repo)
	if err != nil {
		return nil, err
	}

	parentDir, err := git.ParentDir()
	if err != nil {
		return nil, err
	}

	username, _ := git.Username()

	return &cmdContext{
		Config:       cfg,
		RepoName:     repo,
		MainWorktree: mainWT,
		ParentDir:    parentDir,
		Username:     username,
	}, nil
}

// branchName builds a full branch name using config.
func (c *cmdContext) branchName(name string) string {
	return c.Config.EffectiveBranchName(name, c.Username)
}

// worktreeDir builds a worktree directory name using config.
func (c *cmdContext) worktreeDir(name string) string {
	return c.Config.EffectiveWorktreeDir(c.RepoName, name)
}

// worktreePath builds a full worktree path.
func (c *cmdContext) worktreePath(name string) string {
	return c.ParentDir + "/" + c.worktreeDir(name)
}

// baseRef returns the full remote ref for the base branch (e.g., "origin/staging").
func (c *cmdContext) baseRef() string {
	return c.Config.Remote + "/" + c.Config.BaseBranch
}

// completeWorktreeNames returns a Cobra ValidArgsFunction that suggests worktree short names.
func completeWorktreeNames(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	ctx, err := newContext()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	worktrees, err := git.ListWorktrees()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	prefix := ctx.Config.EffectiveWorktreeDir(ctx.RepoName, "")
	var names []string
	for _, wt := range worktrees {
		if wt.Path == ctx.MainWorktree {
			continue
		}
		short := strings.TrimPrefix(filepath.Base(wt.Path), prefix)
		if toComplete == "" || strings.HasPrefix(short, toComplete) {
			names = append(names, short)
		}
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}
