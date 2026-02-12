package git

import "testing"

func TestParseWorktreeList(t *testing.T) {
	t.Run("empty output", func(t *testing.T) {
		got := ParseWorktreeList("")
		if len(got) != 0 {
			t.Errorf("got %d worktrees, want 0", len(got))
		}
	})

	t.Run("single worktree", func(t *testing.T) {
		input := "worktree /home/user/project\nbranch refs/heads/main\n\n"
		got := ParseWorktreeList(input)
		if len(got) != 1 {
			t.Fatalf("got %d worktrees, want 1", len(got))
		}
		if got[0].Path != "/home/user/project" {
			t.Errorf("Path = %q, want %q", got[0].Path, "/home/user/project")
		}
		if got[0].Branch != "main" {
			t.Errorf("Branch = %q, want %q", got[0].Branch, "main")
		}
	})

	t.Run("strips refs/heads/ prefix", func(t *testing.T) {
		input := "worktree /tmp/wt\nbranch refs/heads/michael/sidebar\n\n"
		got := ParseWorktreeList(input)
		if len(got) != 1 {
			t.Fatalf("got %d worktrees, want 1", len(got))
		}
		if got[0].Branch != "michael/sidebar" {
			t.Errorf("Branch = %q, want %q", got[0].Branch, "michael/sidebar")
		}
	})

	t.Run("multiple worktrees", func(t *testing.T) {
		input := "worktree /home/user/project\nbranch refs/heads/main\n\nworktree /home/user/wt-project-feat\nbranch refs/heads/michael/feat\n\n"
		got := ParseWorktreeList(input)
		if len(got) != 2 {
			t.Fatalf("got %d worktrees, want 2", len(got))
		}
		if got[1].Branch != "michael/feat" {
			t.Errorf("second worktree Branch = %q, want %q", got[1].Branch, "michael/feat")
		}
	})

	t.Run("no trailing newline", func(t *testing.T) {
		// Real git output sometimes doesn't end with a blank line
		input := "worktree /tmp/wt\nbranch refs/heads/main"
		got := ParseWorktreeList(input)
		if len(got) != 1 {
			t.Fatalf("got %d worktrees, want 1", len(got))
		}
		if got[0].Branch != "main" {
			t.Errorf("Branch = %q, want %q", got[0].Branch, "main")
		}
	})

	t.Run("detached HEAD worktree", func(t *testing.T) {
		// Detached HEAD has no branch line
		input := "worktree /tmp/wt\nHEAD abc1234\ndetached\n\n"
		got := ParseWorktreeList(input)
		if len(got) != 1 {
			t.Fatalf("got %d worktrees, want 1", len(got))
		}
		if got[0].Branch != "" {
			t.Errorf("Branch = %q, want empty for detached HEAD", got[0].Branch)
		}
	})

	t.Run("path with spaces", func(t *testing.T) {
		input := "worktree /home/user/my project/main\nbranch refs/heads/main\n\n"
		got := ParseWorktreeList(input)
		if len(got) != 1 {
			t.Fatalf("got %d worktrees, want 1", len(got))
		}
		if got[0].Path != "/home/user/my project/main" {
			t.Errorf("Path = %q, want %q", got[0].Path, "/home/user/my project/main")
		}
	})
}
