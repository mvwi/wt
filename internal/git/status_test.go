package git

import (
	"os"
	"path/filepath"
	"testing"
)

// The first porcelain line of a modified tracked file starts with a space.
// Trimming it once turned " M README.md" into path "ADME.md".
func TestStatusPorcelainInKeepsLeadingSpace(t *testing.T) {
	dir := t.TempDir()
	write := func(s string) {
		if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run := func(args ...string) {
		if _, err := RunIn(dir, args...); err != nil {
			t.Fatal(err)
		}
	}
	run("init", "-q")
	write("a\n")
	run("add", "README.md")
	run("-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "add")
	write("b\n")

	got, err := StatusPorcelainIn(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Status != " M" || got[0].Path != "README.md" {
		t.Errorf("got %+v, want [{Status:\" M\" Path:\"README.md\"}]", got)
	}
}

func TestParsePorcelainOutput(t *testing.T) {
	t.Run("empty output", func(t *testing.T) {
		got := ParsePorcelainOutput("")
		if got != nil {
			t.Errorf("got %v, want nil", got)
		}
	})

	t.Run("modified file", func(t *testing.T) {
		got := ParsePorcelainOutput(" M src/main.go\n")
		if len(got) != 1 {
			t.Fatalf("got %d changes, want 1", len(got))
		}
		if got[0].Status != " M" || got[0].Path != "src/main.go" {
			t.Errorf("got {%q, %q}, want {\" M\", \"src/main.go\"}", got[0].Status, got[0].Path)
		}
	})

	t.Run("rename parses old and new paths", func(t *testing.T) {
		got := ParsePorcelainOutput("R  old.go -> new.go\n")
		if len(got) != 1 {
			t.Fatalf("got %d changes, want 1", len(got))
		}
		if got[0].OldPath != "old.go" || got[0].Path != "new.go" {
			t.Errorf("got {old=%q, new=%q}, want {old=\"old.go\", new=\"new.go\"}", got[0].OldPath, got[0].Path)
		}
	})

	t.Run("rename with arrow in filename uses first separator only", func(t *testing.T) {
		got := ParsePorcelainOutput("R  old.go -> dir/new -> file.go\n")
		if len(got) != 1 {
			t.Fatalf("got %d changes, want 1", len(got))
		}
		if got[0].OldPath != "old.go" || got[0].Path != "dir/new -> file.go" {
			t.Errorf("got {old=%q, new=%q}, want {old=\"old.go\", new=\"dir/new -> file.go\"}", got[0].OldPath, got[0].Path)
		}
	})

	t.Run("multiple changes", func(t *testing.T) {
		input := " M file1.go\nA  file2.go\n?? file3.go\n"
		got := ParsePorcelainOutput(input)
		if len(got) != 3 {
			t.Fatalf("got %d changes, want 3", len(got))
		}
	})

	t.Run("path with spaces", func(t *testing.T) {
		got := ParsePorcelainOutput(" M path/to/my file.go\n")
		if len(got) != 1 {
			t.Fatalf("got %d changes, want 1", len(got))
		}
		if got[0].Path != "path/to/my file.go" {
			t.Errorf("Path = %q, want %q", got[0].Path, "path/to/my file.go")
		}
	})

	t.Run("untracked directory entry has trailing slash", func(t *testing.T) {
		// Without -uall, git status --porcelain collapses untracked dirs into "?? dir/"
		// With -uall, individual files are listed instead. This test documents
		// that the parser handles the collapsed form (which we no longer produce
		// but should still parse correctly).
		got := ParsePorcelainOutput("?? new-dir/\n")
		if len(got) != 1 {
			t.Fatalf("got %d changes, want 1", len(got))
		}
		if got[0].Status != "??" || got[0].Path != "new-dir/" {
			t.Errorf("got {%q, %q}, want {\"??\", \"new-dir/\"}", got[0].Status, got[0].Path)
		}
	})
}
