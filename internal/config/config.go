package config

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Config holds all wt configuration. Every field has a sensible default.
// Users override via .wt.toml in the repo root (main worktree).
type Config struct {
	// BaseBranch is the branch worktrees are created from and rebased onto.
	// Common values: "main", "staging", "develop"
	BaseBranch string `toml:"base_branch"`

	// Remote is the git remote name. Almost always "origin".
	Remote string `toml:"remote"`

	// BranchPrefix is prepended to new branch names: "<prefix>/<name>".
	// Default: git user's first name (lowercase). Set to "" to disable prefixing.
	BranchPrefix string `toml:"branch_prefix"`

	// WorktreePrefix controls the directory naming: "<prefix><name>".
	// Default: "wt-<repo>-". Set to customize (e.g., "wt-" for shorter names).
	WorktreePrefix string `toml:"worktree_prefix"`

	// Init configures the `wt init` command behavior.
	Init InitConfig `toml:"init"`
}

// InitConfig controls what `wt init` does.
type InitConfig struct {
	// EnvFiles to copy from the main worktree. Empty = use defaults.
	EnvFiles []string `toml:"env_files"`

	// PackageManager overrides auto-detection. Values: "pnpm", "yarn", "npm", "bun", or "" for auto.
	PackageManager string `toml:"package_manager"`

	// PostCommands are shell commands to run after init (replaces .wt-init scripts).
	PostCommands []string `toml:"post_commands"`
}

// DefaultEnvFiles are copied when no env_files config is set.
var DefaultEnvFiles = []string{".env", ".env.local", ".env.development.local"}

// Load reads config from .wt.toml in the given directory (typically main worktree root).
// Returns defaults if the file doesn't exist. Returns an error only for malformed TOML.
func Load(dir string) (*Config, error) {
	cfg := &Config{
		BaseBranch: "main",
		Remote:     "origin",
	}

	path := filepath.Join(dir, ".wt.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		// File doesn't exist — return defaults
		return cfg, nil
	}

	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// EffectiveEnvFiles returns the env files list, falling back to defaults.
func (c *Config) EffectiveEnvFiles() []string {
	if len(c.Init.EnvFiles) > 0 {
		return c.Init.EnvFiles
	}
	return DefaultEnvFiles
}

// EffectiveBranchName builds the full branch name for a new worktree.
// If BranchPrefix is set, returns "prefix/name". If empty, returns just "name".
func (c *Config) EffectiveBranchName(name, gitUsername string) string {
	prefix := c.BranchPrefix
	if prefix == "" {
		prefix = gitUsername
	}
	if prefix == "" {
		return name
	}
	return prefix + "/" + name
}

// EffectiveWorktreeDir builds the worktree directory name.
// Default pattern: "wt-<repo>-<name>"
func (c *Config) EffectiveWorktreeDir(repoName, name string) string {
	if c.WorktreePrefix != "" {
		return c.WorktreePrefix + name
	}
	return "wt-" + repoName + "-" + name
}
