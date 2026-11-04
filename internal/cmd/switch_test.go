package cmd

import (
	"slices"
	"testing"

	"github.com/mvwi/wt/internal/config"
	"github.com/mvwi/wt/internal/git"
)

func testContext(t *testing.T) *cmdContext {
	return &cmdContext{
		Config:       &config.Config{BaseBranch: "main", Remote: "origin"},
		RepoName:     "repo",
		MainWorktree: "/src/repo",
		ParentDir:    t.TempDir(), // empty, so step 1 (exact dir) never hits
		Username:     "me",
	}
}

func TestMatchWorktrees(t *testing.T) {
	wts := []git.Worktree{
		{Path: "/src/repo", Branch: "main"},
		{Path: "/src/wt-repo/api-auth", Branch: "me/api-auth"},
		{Path: "/src/wt-repo/api-authz", Branch: "me/api-authz"},
		{Path: "/src/wt-repo/ui-auth", Branch: "me/ui-auth"},
		{Path: "/src/wt-repo/sidebar", Branch: "me/sidebar"},
	}
	tests := []struct {
		name      string
		want      []string
		wantFuzzy bool
	}{
		{"main", []string{"/src/repo"}, false},
		{"repo", []string{"/src/repo"}, false},
		{"me/sidebar", []string{"/src/wt-repo/sidebar"}, false},
		{"auth", []string{"/src/wt-repo/api-auth", "/src/wt-repo/ui-auth"}, false},
		{"authz", []string{"/src/wt-repo/api-authz"}, false},
		{"side", []string{"/src/wt-repo/sidebar"}, true},
		{"api", []string{"/src/wt-repo/api-auth", "/src/wt-repo/api-authz"}, true},
		{"nope", nil, true},
	}
	ctx := testContext(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, fuzzy := matchWorktrees(ctx, wts, tt.name)
			var paths []string
			for _, m := range got {
				paths = append(paths, m.Path)
			}
			if !slices.Equal(paths, tt.want) || (len(got) > 0 && fuzzy != tt.wantFuzzy) {
				t.Errorf("matchWorktrees(%q) = %v, fuzzy=%v; want %v, fuzzy=%v", tt.name, paths, fuzzy, tt.want, tt.wantFuzzy)
			}
		})
	}
}

func TestResolveWorktreeAmbiguousErrors(t *testing.T) {
	// go test's stdin is not a terminal, so no fzf: expect an error listing both.
	wts := []git.Worktree{
		{Path: "/src/wt-repo/api-auth", Branch: "me/api-auth"},
		{Path: "/src/wt-repo/ui-auth", Branch: "me/ui-auth"},
	}
	got, _, err := resolveWorktree(testContext(t), wts, "auth")
	if err == nil || got != "" {
		t.Fatalf("resolveWorktree = %q, %v; want an error", got, err)
	}
}

func TestValidateName(t *testing.T) {
	ctx := testContext(t)
	for _, tt := range []struct {
		name string
		ok   bool
	}{
		{"sidebar", true},
		{"fix-123", true},
		{"main", false},
		{"master", false},
		{"repo", false},
		{"has space", false},
		{"-x", false},
		{"bad..dots", false},
		{"ends.lock", false},
	} {
		if err := validateName(ctx, tt.name); (err == nil) != tt.ok {
			t.Errorf("validateName(%q) = %v, want ok=%v", tt.name, err, tt.ok)
		}
	}
}
