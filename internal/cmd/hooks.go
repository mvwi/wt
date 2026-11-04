package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mvwi/wt/internal/git"
	"github.com/mvwi/wt/internal/ui"
)

// indexFile holds a worktree's index inside its own git dir, so it survives
// renames and goes away when git removes the worktree.
const indexFile = "wt-index"

// finishNew runs init (if asked) and the post_new hooks for a new worktree,
// then prints the next step. new, pr and pull all end here.
func finishNew(ctx *cmdContext, name, wtPath string, doInit bool) error {
	if doInit {
		if err := runInitIn(wtPath, ctx); err != nil {
			return err
		}
	}
	warnRepoHooks(ctx)
	if hooks := ctx.Config.Hooks.PostNew; len(hooks) > 0 {
		if err := runHooks("post_new", hooks, wtPath, worktreeEnv(worktreeVars(ctx, wtPath))); err != nil {
			ui.Warn("%v", err)
		}
	}
	if !doInit {
		printToolHint(wtPath)
		printSwitchHint(name)
	}
	return nil
}

// runHooks runs each command with sh -c in dir and stops at the first failure.
// warnRepoHooks says that [hooks] in .wt.toml were skipped and where they belong.
func warnRepoHooks(ctx *cmdContext) {
	if ctx.Config.RepoHooksIgnored {
		ui.Warn("Skipped [hooks] in .wt.toml: hooks only run from ~/.config/wt/config.toml (under [repos.%s] for this repo)", ctx.RepoName)
	}
}

func runHooks(event string, cmds []string, dir string, env []string) error {
	for _, c := range cmds {
		fmt.Println(ui.Dim(event + ": " + c))
		if err := runShellString(dir, c, env...); err != nil {
			return fmt.Errorf("%s hook failed: %s: %w", event, c, err)
		}
	}
	return nil
}

// worktreeVars returns the values exposed to hooks, init commands and [init]
// env templates. "index" and "port" are missing if no index could be assigned.
func worktreeVars(ctx *cmdContext, path string) map[string]string {
	branch, _ := git.CurrentBranchIn(path)
	v := map[string]string{
		"name":   ctx.shortName(path),
		"path":   path,
		"branch": branch,
		"main":   ctx.MainWorktree,
		"repo":   ctx.RepoName,
	}
	gitDir, err := git.RunIn(path, "rev-parse", "--absolute-git-dir")
	var n int
	if err == nil {
		n, err = worktreeIndex(gitDir)
	}
	if err != nil {
		ui.Warn("Could not assign a worktree index: %v", err)
		return v
	}
	v["index"] = strconv.Itoa(n)
	v["port"] = strconv.Itoa(ctx.Config.EffectivePortBase() + n)
	return v
}

// worktreeEnv turns vars into WT_* environment entries.
func worktreeEnv(vars map[string]string) []string {
	env := make([]string, 0, len(vars))
	for k, v := range vars {
		env = append(env, "WT_"+strings.ToUpper(k)+"="+v)
	}
	return env
}

// expandVars replaces {key} in s with vars[key].
func expandVars(s string, vars map[string]string) string {
	for k, v := range vars {
		s = strings.ReplaceAll(s, "{"+k+"}", v)
	}
	return s
}

// worktreeIndex returns the index stored in gitDir, or stores the lowest
// number no sibling worktree uses. The main worktree is always 0.
// shortcut: two wt runs at the same moment can pick the same number.
func worktreeIndex(gitDir string) (int, error) {
	// Linked worktrees keep their git dir at <common>/worktrees/<id>.
	if filepath.Base(filepath.Dir(gitDir)) != "worktrees" {
		return 0, nil
	}
	own := filepath.Join(gitDir, indexFile)
	if n, ok := readIndex(own); ok {
		return n, nil
	}
	used := map[int]bool{}
	siblings, _ := filepath.Glob(filepath.Join(filepath.Dir(gitDir), "*", indexFile))
	for _, f := range siblings {
		if n, ok := readIndex(f); ok {
			used[n] = true
		}
	}
	n := 1
	for used[n] {
		n++
	}
	return n, os.WriteFile(own, []byte(strconv.Itoa(n)+"\n"), 0644)
}

func readIndex(path string) (int, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(data)))
	return n, err == nil && n > 0
}

// printToolHint tells the user to trust direnv/mise files; wt never runs them.
func printToolHint(dir string) {
	var cmds []string
	if fileExists(filepath.Join(dir, ".envrc")) {
		cmds = append(cmds, "direnv allow")
	}
	for _, f := range []string{"mise.toml", ".mise.toml", ".tool-versions"} {
		if fileExists(filepath.Join(dir, f)) {
			cmds = append(cmds, "mise trust")
			break
		}
	}
	if len(cmds) > 0 {
		fmt.Println(ui.Dim("Tip: run " + strings.Join(cmds, " and ") + " in this worktree"))
	}
}
