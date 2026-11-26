package git

import (
	"os"
	"path/filepath"
	"slices"
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

// Without -z git quotes these names ("my file.txt" keeps its quotes, é turns
// into \303\251), and wt move then could not find the files it was moving.
func TestStatusPorcelainInKeepsOddNames(t *testing.T) {
	dir := t.TempDir()
	if _, err := RunIn(dir, "init", "-q"); err != nil {
		t.Fatal(err)
	}
	want := []string{"café.txt", "my file.txt"}
	for _, name := range want {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	got, err := StatusPorcelainIn(dir)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, c := range got {
		paths = append(paths, c.Path)
	}
	slices.Sort(paths)
	if !slices.Equal(paths, want) {
		t.Errorf("paths = %q, want %q", paths, want)
	}
}

func TestParsePorcelainOutput(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []FileChange
	}{
		{"empty output", "", nil},
		{"modified file", " M src/main.go\x00", []FileChange{{Status: " M", Path: "src/main.go"}}},
		{"rename: new path, then old path as its own entry", "R  new.go\x00old.go\x00?? x\x00",
			[]FileChange{{Status: "R ", Path: "new.go", OldPath: "old.go"}, {Status: "??", Path: "x"}}},
		{"multiple changes", " M file1.go\x00A  file2.go\x00?? file3.go\x00",
			[]FileChange{{Status: " M", Path: "file1.go"}, {Status: "A ", Path: "file2.go"}, {Status: "??", Path: "file3.go"}}},
		{"spaces, arrows and newlines stay in the path", " M my -> file\n.go\x00",
			[]FileChange{{Status: " M", Path: "my -> file\n.go"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ParsePorcelainOutput(tt.in); !slices.Equal(got, tt.want) {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

// A branch with no upstream used to count 0 unpushed commits, so close -y
// deleted it without asking. The count must cover every commit that exists
// only on the branch.
func TestUnsavedCountIn(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		if _, err := RunIn(dir, append([]string{"-c", "user.name=t", "-c", "user.email=t@t"}, args...)...); err != nil {
			t.Fatal(err)
		}
	}
	run("init", "-q", "-b", "main")
	run("commit", "-q", "--allow-empty", "-m", "init")
	run("checkout", "-q", "-b", "feat")

	if n := UnsavedCountIn(dir, "main"); n != 0 {
		t.Errorf("fresh branch: got %d, want 0", n)
	}

	run("commit", "-q", "--allow-empty", "-m", "a")
	run("commit", "-q", "--allow-empty", "-m", "b")
	if n := UnsavedCountIn(dir, "main"); n != 2 {
		t.Errorf("no upstream, 2 commits off main: got %d, want 2", n)
	}
	if n := UnpushedCountIn(dir); n != 0 {
		t.Errorf("UnpushedCountIn with no upstream: got %d, want 0 (the old blind spot)", n)
	}

	// Commits saved on the upstream don't count.
	run("branch", "saved", "HEAD~1")
	run("branch", "--set-upstream-to=saved")
	if n := UnsavedCountIn(dir, "main"); n != 1 {
		t.Errorf("1 commit past the upstream: got %d, want 1", n)
	}

	// Commits landed on the base don't count.
	run("branch", "-f", "main", "HEAD")
	if n := UnsavedCountIn(dir, "main"); n != 0 {
		t.Errorf("landed on main: got %d, want 0", n)
	}

	// A base that doesn't resolve can't hide commits.
	run("branch", "--unset-upstream")
	if n := UnsavedCountIn(dir, "origin/main"); n != 3 {
		t.Errorf("missing base, no upstream: got %d, want 3", n)
	}
}
