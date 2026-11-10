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
