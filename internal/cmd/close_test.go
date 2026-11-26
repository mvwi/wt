package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mvwi/wt/internal/config"
	"github.com/mvwi/wt/internal/git"
	"github.com/mvwi/wt/internal/ui"
)

// close, prune and the dashboard all remove worktrees through closeWorktree,
// so hooks and the branch delete must happen there.
func TestCloseWorktree(t *testing.T) {
	main, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		if _, err := git.RunIn(main, append([]string{"-c", "user.name=t", "-c", "user.email=t@t"}, args...)...); err != nil {
			t.Fatal(err)
		}
	}
	run("init", "-q", "-b", "main")
	run("commit", "-q", "--allow-empty", "-m", "init")
	t.Chdir(main)

	add := func(name string) string {
		path := filepath.Join(filepath.Dir(main), "wt-"+filepath.Base(main), name)
		run("worktree", "add", "-q", "-b", "me/"+name, path)
		return path
	}
	ctx := &cmdContext{
		Config:       &config.Config{BaseBranch: "main", Remote: "origin"},
		RepoName:     filepath.Base(main),
		MainWorktree: main,
		ParentDir:    filepath.Dir(main),
	}
	ctx.Config.Hooks.PreClose = []string{`echo "$WT_NAME" > "$WT_MAIN/pre"`}
	ctx.Config.Hooks.PostClose = []string{`pwd > post`}

	path := add("a")
	deleted, err := closeWorktree(ctx, path, "me/a", true)
	if err != nil || !deleted {
		t.Fatalf("closeWorktree = %v, %v; want true, nil", deleted, err)
	}
	if isDir(path) || git.BranchExists("me/a") {
		t.Error("worktree or branch is still there")
	}
	if got, _ := os.ReadFile(filepath.Join(main, "pre")); string(got) != "a\n" {
		t.Errorf("pre_close hook saw WT_NAME %q, want a", got)
	}
	if got, _ := os.ReadFile(filepath.Join(main, "post")); string(got) != main+"\n" {
		t.Errorf("post_close hook ran in %q, want the main worktree", got)
	}

	// A failing pre_close hook stops the close.
	path = add("b")
	ctx.Config.Hooks.PreClose = []string{"exit 1"}
	if _, err := closeWorktree(ctx, path, "me/b", true); err == nil {
		t.Error("failing pre_close hook returned nil")
	}
	if !isDir(path) || !git.BranchExists("me/b") {
		t.Error("worktree was removed although pre_close failed")
	}
}

// guardRepo makes a clone of a bare "remote" with one commit on main, so
// origin/main exists. It sets HOME so no global config or hooks apply.
func guardRepo(t *testing.T) (main string, run func(dir string, args ...string)) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", root)
	run = func(dir string, args ...string) {
		t.Helper()
		if _, err := git.RunIn(dir, append([]string{"-c", "user.name=t", "-c", "user.email=t@t"}, args...)...); err != nil {
			t.Fatal(err)
		}
	}
	remote := filepath.Join(root, "remote.git")
	main = filepath.Join(root, "repo")
	run(root, "init", "-q", "--bare", "-b", "main", remote)
	run(root, "clone", "-q", remote, main)
	run(main, "config", "user.name", "t")
	run(main, "commit", "-q", "--allow-empty", "-m", "init")
	run(main, "push", "-q", "origin", "main")
	t.Chdir(main)
	return main, run
}

// close -y must refuse a branch whose commits exist nowhere else, even when
// the branch has no upstream to count against.
func TestCloseRefusesUnsavedCommits(t *testing.T) {
	main, run := guardRepo(t)
	oldYes := ui.YesFlag
	ui.YesFlag = true
	t.Cleanup(func() { ui.YesFlag = oldYes })

	path := filepath.Join(filepath.Dir(main), "wt-repo-a")
	run(main, "worktree", "add", "-q", "-b", "me/a", path, "main") // local start point: no upstream
	run(path, "commit", "-q", "--allow-empty", "-m", "work")

	if err := runClose(closeCmd, []string{"a"}); !errors.Is(err, ui.ErrNeedsForce) {
		t.Fatalf("close -y with an unsaved commit = %v, want ErrNeedsForce", err)
	}
	if !isDir(path) || !git.BranchExists("me/a") {
		t.Fatal("close removed work it was told not to discard")
	}

	// Once the commit is on origin/main, nothing is lost and close goes ahead.
	run(path, "push", "-q", "origin", "HEAD:main")
	if err := runClose(closeCmd, []string{"a"}); err != nil {
		t.Fatalf("close -y of a merged branch = %v, want nil", err)
	}
	if isDir(path) || git.BranchExists("me/a") {
		t.Error("merged worktree or branch is still there")
	}
}
