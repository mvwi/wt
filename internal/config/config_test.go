package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMergeConfig(t *testing.T) {
	t.Run("overrides non-zero string fields", func(t *testing.T) {
		dst := &Config{BaseBranch: "main", Remote: "origin"}
		src := &Config{BaseBranch: "staging"}
		mergeConfig(dst, src)

		if dst.BaseBranch != "staging" {
			t.Errorf("BaseBranch = %q, want %q", dst.BaseBranch, "staging")
		}
		if dst.Remote != "origin" {
			t.Errorf("Remote = %q, want %q (should not be overwritten by zero value)", dst.Remote, "origin")
		}
	})

	t.Run("zero values do not override", func(t *testing.T) {
		dst := &Config{
			BaseBranch:     "develop",
			Remote:         "upstream",
			BranchPrefix:   "fix",
			WorktreePrefix: "wt-",
			StaleThreshold: 14,
			Init: InitConfig{
				CopyFiles: []string{".env"},
				Commands:  []string{"make build"},
			},
		}
		src := &Config{} // all zero values
		mergeConfig(dst, src)

		if dst.BaseBranch != "develop" {
			t.Errorf("BaseBranch was overwritten by zero value")
		}
		if dst.StaleThreshold != 14 {
			t.Errorf("StaleThreshold was overwritten by zero value")
		}
		if len(dst.Init.CopyFiles) != 1 || dst.Init.CopyFiles[0] != ".env" {
			t.Errorf("CopyFiles was overwritten by zero value")
		}
	})

	t.Run("slices replace entirely", func(t *testing.T) {
		dst := &Config{Init: InitConfig{CopyFiles: []string{".env", ".env.local"}}}
		src := &Config{Init: InitConfig{CopyFiles: []string{".env.production"}}}
		mergeConfig(dst, src)

		if len(dst.Init.CopyFiles) != 1 || dst.Init.CopyFiles[0] != ".env.production" {
			t.Errorf("CopyFiles = %v, want [.env.production]", dst.Init.CopyFiles)
		}
	})
}

func TestLoad(t *testing.T) {
	t.Run("defaults when no config files exist", func(t *testing.T) {
		dir := t.TempDir()
		cfg, err := Load(dir, "myrepo")
		if err != nil {
			t.Fatal(err)
		}

		if cfg.BaseBranch != "main" {
			t.Errorf("BaseBranch = %q, want %q", cfg.BaseBranch, "main")
		}
		if cfg.Remote != "origin" {
			t.Errorf("Remote = %q, want %q", cfg.Remote, "origin")
		}
	})

	t.Run("repo-local .wt.toml overrides defaults", func(t *testing.T) {
		dir := t.TempDir()
		err := os.WriteFile(filepath.Join(dir, ".wt.toml"), []byte(`
base_branch = "staging"
branch_prefix = "fix"
`), 0644)
		if err != nil {
			t.Fatal(err)
		}

		cfg, err := Load(dir, "myrepo")
		if err != nil {
			t.Fatal(err)
		}

		if cfg.BaseBranch != "staging" {
			t.Errorf("BaseBranch = %q, want %q", cfg.BaseBranch, "staging")
		}
		if cfg.BranchPrefix != "fix" {
			t.Errorf("BranchPrefix = %q, want %q", cfg.BranchPrefix, "fix")
		}
		if cfg.Remote != "origin" {
			t.Errorf("Remote = %q, want %q (default should survive)", cfg.Remote, "origin")
		}
	})

	t.Run("malformed toml returns error", func(t *testing.T) {
		dir := t.TempDir()
		err := os.WriteFile(filepath.Join(dir, ".wt.toml"), []byte(`not valid toml ===`), 0644)
		if err != nil {
			t.Fatal(err)
		}

		_, err = Load(dir, "myrepo")
		if err == nil {
			t.Error("expected error for malformed TOML, got nil")
		}
	})

	t.Run("init section loads correctly", func(t *testing.T) {
		dir := t.TempDir()
		err := os.WriteFile(filepath.Join(dir, ".wt.toml"), []byte(`
[init]
copy_files = [".env", ".env.local"]
commands = ["pnpm install", "npx prisma generate"]
`), 0644)
		if err != nil {
			t.Fatal(err)
		}

		cfg, err := Load(dir, "myrepo")
		if err != nil {
			t.Fatal(err)
		}

		if len(cfg.Init.CopyFiles) != 2 || cfg.Init.CopyFiles[0] != ".env" {
			t.Errorf("CopyFiles = %v, want [.env .env.local]", cfg.Init.CopyFiles)
		}
		if len(cfg.Init.Commands) != 2 || cfg.Init.Commands[0] != "pnpm install" {
			t.Errorf("Commands = %v, want [pnpm install, npx prisma generate]", cfg.Init.Commands)
		}
	})
}

func TestEffectiveBranchName(t *testing.T) {
	tests := []struct {
		name        string
		prefix      string
		gitUsername string
		input       string
		want        string
	}{
		{"with config prefix", "michael", "ignored", "sidebar", "michael/sidebar"},
		{"falls back to git username", "", "jane", "sidebar", "jane/sidebar"},
		{"no prefix at all", "", "", "sidebar", "sidebar"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{BranchPrefix: tt.prefix}
			got := cfg.EffectiveBranchName(tt.input, tt.gitUsername)
			if got != tt.want {
				t.Errorf("EffectiveBranchName(%q, %q) = %q, want %q", tt.input, tt.gitUsername, got, tt.want)
			}
		})
	}
}
