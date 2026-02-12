package git

import (
	"testing"
)

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
}
