package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Config holds all wt configuration. Every field has a sensible default.
// Resolved via: defaults → global (~/.config/wt/config.toml) → global per-repo → .wt.toml
type Config struct {
	// BaseBranch is the branch worktrees are created from and rebased onto.
	// Common values: "main", "staging", "develop"
	BaseBranch string `toml:"base_branch"`

	// Remote is the git remote name. Almost always "origin".
	Remote string `toml:"remote"`

	// BranchPrefix is prepended to new branch names: "<prefix>/<name>".
	// Default: git user's first name (lowercase). Set to "" to disable prefixing.
	// Pointer so we can distinguish "not set" (nil) from "explicitly empty" ("").
	BranchPrefix *string `toml:"branch_prefix"`

	// WorktreePrefix controls the directory naming: "<prefix><name>".
	// Default: "wt-<repo>-". Set to customize (e.g., "wt-" for shorter names).
	// Pointer so we can distinguish "not set" (nil) from "explicitly empty" ("").
	WorktreePrefix *string `toml:"worktree_prefix"`

	// StaleThreshold is the number of days after which a worktree with no open PR
	// is considered stale in `wt list`. Default: 7.
	StaleThreshold int `toml:"stale_threshold"`

	// Init configures the `wt init` command behavior.
	Init InitConfig `toml:"init"`
}

// globalFile is the on-disk shape of ~/.config/wt/config.toml.
// Top-level fields are defaults; [repos.<name>] sections override per repo.
type globalFile struct {
	Config
	Repos map[string]Config `toml:"repos"`
}

// InitConfig controls what `wt init` does. Entirely config-driven —
// no auto-detection. Configure in .wt.toml under [init].
type InitConfig struct {
	// CopyFiles are copied from the main worktree if missing in the target.
	CopyFiles []string `toml:"copy_files"`

	// Commands are shell commands run sequentially during init.
	Commands []string `toml:"commands"`
}

// Load reads config with layered precedence:
//  1. Hardcoded defaults (base_branch="main", remote="origin")
//  2. Global defaults (~/.config/wt/config.toml top-level fields)
//  3. Global per-repo ([repos.<repoName>] section)
//  4. Repo-local (.wt.toml in the main worktree root)
//
// Each layer only overrides fields it explicitly sets.
func Load(dir, repoName string) (*Config, error) {
	cfg := &Config{
		BaseBranch: "main",
		Remote:     "origin",
	}

	// Layer 1+2: global defaults + per-repo overrides
	if globalPath, err := globalConfigPath(); err == nil {
		if data, err := os.ReadFile(globalPath); err == nil {
			var gf globalFile
			if err := toml.Unmarshal(data, &gf); err != nil {
				return nil, fmt.Errorf("global config (%s): %w", globalPath, err)
			}
			// Apply global defaults
			mergeConfig(cfg, &gf.Config)
			// Apply per-repo overrides
			if repoName != "" {
				if repoCfg, ok := gf.Repos[repoName]; ok {
					mergeConfig(cfg, &repoCfg)
				}
			}
		}
	}

	// Layer 3: repo-local .wt.toml (overrides non-zero fields, same as other layers)
	repoPath := filepath.Join(dir, ".wt.toml")
	if data, err := os.ReadFile(repoPath); err == nil {
		var repoCfg Config
		if err := toml.Unmarshal(data, &repoCfg); err != nil {
			return nil, fmt.Errorf("repo config (%s): %w", repoPath, err)
		}
		mergeConfig(cfg, &repoCfg)
	}

	return cfg, nil
}

// mergeConfig copies non-zero fields from src into dst.
func mergeConfig(dst, src *Config) {
	if src.BaseBranch != "" {
		dst.BaseBranch = src.BaseBranch
	}
	if src.Remote != "" {
		dst.Remote = src.Remote
	}
	if src.BranchPrefix != nil {
		dst.BranchPrefix = src.BranchPrefix
	}
	if src.WorktreePrefix != nil {
		dst.WorktreePrefix = src.WorktreePrefix
	}
	if src.StaleThreshold > 0 {
		dst.StaleThreshold = src.StaleThreshold
	}
	if len(src.Init.CopyFiles) > 0 {
		dst.Init.CopyFiles = src.Init.CopyFiles
	}
	if len(src.Init.Commands) > 0 {
		dst.Init.Commands = src.Init.Commands
	}
}

// globalConfigPath returns ~/.config/wt/config.toml.
func globalConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "wt", "config.toml"), nil
}

// EffectiveBranchName builds the full branch name for a new worktree.
// If BranchPrefix is explicitly set (even to ""), uses that value.
// Otherwise falls back to gitUsername. Returns just "name" if both are empty.
func (c *Config) EffectiveBranchName(name, gitUsername string) string {
	var prefix string
	if c.BranchPrefix != nil {
		prefix = *c.BranchPrefix
	} else {
		prefix = gitUsername
	}
	if prefix == "" {
		return name
	}
	return prefix + "/" + name
}

// EffectiveStaleThreshold returns the stale threshold in days, defaulting to 7.
func (c *Config) EffectiveStaleThreshold() int {
	if c.StaleThreshold > 0 {
		return c.StaleThreshold
	}
	return 7
}

// EffectiveWorktreeDir builds the worktree directory name.
// Default pattern: "wt-<repo>-<name>". If WorktreePrefix is explicitly set
// (even to ""), uses that value instead of the default pattern.
func (c *Config) EffectiveWorktreeDir(repoName, name string) string {
	if c.WorktreePrefix != nil {
		return *c.WorktreePrefix + name
	}
	return "wt-" + repoName + "-" + name
}
