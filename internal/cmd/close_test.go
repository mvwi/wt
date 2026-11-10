package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvwi/wt/internal/config"
	"github.com/mvwi/wt/internal/git"
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
