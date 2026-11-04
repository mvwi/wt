package cmd

import (
	"testing"

	"github.com/mvwi/wt/internal/config"
)

func TestListLegendShowsOnlyUsedGlyphs(t *testing.T) {
	tests := []struct {
		table, want string
	}{
		{"  feat ⇡2 —\n", "⇡ ahead of base"},
		{"  feat ✓ #12 ○ ✗ 2/36\n", "✓ ok  ✗ fail/changes  ○ none yet"},
		{"  feat — —\n", ""},
	}
	for _, tt := range tests {
		if got := listLegend(tt.table); got != tt.want {
			t.Errorf("listLegend(%q) = %q, want %q", tt.table, got, tt.want)
		}
	}
}

func TestShortName(t *testing.T) {
	flat := "repo-"
	tests := []struct {
		name   string
		prefix *string
		path   string
		want   string
	}{
		{"nested", nil, "/src/wt-repo/feat/foo", "feat/foo"},
		{"nested flat name", nil, "/src/wt-repo/foo", "foo"},
		{"flat prefix", &flat, "/src/repo-feat/foo", "feat/foo"},
		{"main worktree", nil, "/src/repo", "repo"},
		{"outside parent", nil, "/elsewhere/x/foo", "foo"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &cmdContext{
				Config:    &config.Config{WorktreePrefix: tt.prefix},
				RepoName:  "repo",
				ParentDir: "/src",
			}
			if got := ctx.shortName(tt.path); got != tt.want {
				t.Errorf("shortName(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}
