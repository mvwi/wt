package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorktreeIndex(t *testing.T) {
	common := t.TempDir()
	mk := func(id string) string {
		d := filepath.Join(common, "worktrees", id)
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
		return d
	}
	get := func(gitDir string) int {
		n, err := worktreeIndex(gitDir)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}

	if n := get(common); n != 0 {
		t.Errorf("main worktree index = %d, want 0", n)
	}
	a, b, c := mk("a"), mk("b"), mk("c")
	if get(a) != 1 || get(b) != 2 {
		t.Fatalf("first indexes = %d, %d, want 1, 2", get(a), get(b))
	}
	if get(a) != 1 {
		t.Error("index is not stable")
	}
	// Closing a frees its number for the next worktree.
	if err := os.RemoveAll(a); err != nil {
		t.Fatal(err)
	}
	if n := get(c); n != 1 {
		t.Errorf("index after free = %d, want 1", n)
	}
}

func TestUpsertEnv(t *testing.T) {
	tests := []struct {
		name, before, want string
	}{
		{"new file", "", "A=1\nB=2\n"},
		{"replace and append", "# keep\nB=old\nOTHER=x", "# keep\nB=2\nOTHER=x\nA=1\n"},
		{"keeps export", "export A=old\n", "export A=1\nB=2\n"},
		{"ignores comment with key", "# A=old\n", "# A=old\nA=1\nB=2\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), ".env")
			if tt.before != "" {
				if err := os.WriteFile(path, []byte(tt.before), 0644); err != nil {
					t.Fatal(err)
				}
			}
			if err := upsertEnv(path, map[string]string{"A": "1", "B": "2"}); err != nil {
				t.Fatal(err)
			}
			got, _ := os.ReadFile(path)
			if string(got) != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCloneTree(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(filepath.Join(src, "pkg"), 0755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(src, "pkg", "index.js"), "x")
	dst := filepath.Join(dir, "dst")

	// File systems without copy-on-write must fail cleanly, not copy.
	if err := cloneTree(src, dst); err != nil {
		if fileExists(dst) {
			t.Errorf("failed clone left %s behind: %v", dst, err)
		}
		t.Skipf("no copy-on-write here: %v", err)
	}
	if !fileExists(filepath.Join(dst, "pkg", "index.js")) {
		t.Error("clone is missing pkg/index.js")
	}
}

func TestHookEnv(t *testing.T) {
	vars := map[string]string{"name": "feat", "port": "3002", "repo": "app"}
	if got := expandVars("{repo}-{name}:{port}", vars); got != "app-feat:3002" {
		t.Errorf("expandVars = %q", got)
	}

	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	err := runHooks("post_new", []string{`echo "$WT_NAME $WT_PORT $PWD" > out`}, dir, worktreeEnv(vars))
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(out)
	realDir, _ := filepath.EvalSymlinks(dir)
	if s := strings.TrimSpace(string(got)); s != "feat 3002 "+dir && s != "feat 3002 "+realDir {
		t.Errorf("hook saw %q", s)
	}

	if err := runHooks("pre_close", []string{"exit 3", "touch never"}, dir, nil); err == nil {
		t.Error("failing hook returned nil")
	}
	if fileExists(filepath.Join(dir, "never")) {
		t.Error("hooks kept running after a failure")
	}
}
