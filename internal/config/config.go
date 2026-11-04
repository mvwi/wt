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

	// AutoInstall controls whether `wt rebase` runs the package manager
	// install command when the lockfile changes. Default: true.
	// Pointer to distinguish "not set" (nil → true) from "explicitly false".
	AutoInstall *bool `toml:"auto_install"`

	// PortBase is added to a worktree's index to give WT_PORT. Default: 3000.
	PortBase int `toml:"port_base"`

	// Init configures the `wt init` command behavior.
	Init InitConfig `toml:"init"`

	// Hooks are shell commands run at points in a worktree's life. Only the
	// global config may set them; see RepoHooksIgnored.
	Hooks HooksConfig `toml:"hooks"`

	// RepoHooksIgnored is set when .wt.toml has [hooks]. Hooks run without being
	// asked for, so a repo you clone must not be able to set them.
	RepoHooksIgnored bool `toml:"-"`
}

// HooksConfig lists shell commands per lifecycle event. Configure under [hooks].
type HooksConfig struct {
	// PostNew runs in the new worktree after new, pr and pull (after init, if it ran).
	PostNew []string `toml:"post_new"`

	// PreClose runs in the worktree before close removes it. A failure aborts the close.
	PreClose []string `toml:"pre_close"`

	// PostClose runs in the main worktree after close removes the worktree.
	PostClose []string `toml:"post_close"`
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

	// CloneDirs are copy-on-write cloned from the main worktree if missing
	// (e.g. node_modules), before commands run.
	CloneDirs []string `toml:"clone_dirs"`

	// Env holds KEY = "template" lines upserted into EnvFile. Templates may use
	// {index}, {port}, {name}, {branch} and {repo}.
	Env map[string]string `toml:"env"`

	// EnvFile is the file Env writes to, relative to the worktree. Default: ".env".
	EnvFile string `toml:"env_file"`
}

// Load reads config with layered precedence:
//  1. Hardcoded defaults (base_branch="main", remote="origin")
//  2. Global defaults (~/.config/wt/config.toml top-level fields)
//  3. Global per-repo ([repos.<repoName>] section)
//  4. Repo-local (.wt.toml in the main worktree root), except [hooks]
//
// Each layer only overrides fields it explicitly sets.
func Load(dir, repoName string) (*Config, error) {
	cfg := &Config{
		BaseBranch: "main",
		Remote:     "origin",
	}

	// Layer 1+2: global defaults + per-repo overrides
	if globalPath, err := globalConfigPath(); err == nil {
		data, readErr := os.ReadFile(globalPath)
		if readErr == nil {
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
		} else if !os.IsNotExist(readErr) {
			return nil, fmt.Errorf("global config (%s): %w", globalPath, readErr)
		}
	}

	// Layer 3: repo-local .wt.toml (overrides non-zero fields, same as other layers)
	repoPath := filepath.Join(dir, ".wt.toml")
	if data, err := os.ReadFile(repoPath); err == nil {
		var repoCfg Config
		if err := toml.Unmarshal(data, &repoCfg); err != nil {
			return nil, fmt.Errorf("repo config (%s): %w", repoPath, err)
		}
		if h := repoCfg.Hooks; len(h.PostNew)+len(h.PreClose)+len(h.PostClose) > 0 {
			cfg.RepoHooksIgnored = true
			repoCfg.Hooks = HooksConfig{}
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
	if src.AutoInstall != nil {
		dst.AutoInstall = src.AutoInstall
	}
	if len(src.Init.CopyFiles) > 0 {
		dst.Init.CopyFiles = src.Init.CopyFiles
	}
	if len(src.Init.Commands) > 0 {
		dst.Init.Commands = src.Init.Commands
	}
	if len(src.Init.CloneDirs) > 0 {
		dst.Init.CloneDirs = src.Init.CloneDirs
	}
	if len(src.Init.Env) > 0 {
		dst.Init.Env = src.Init.Env
	}
	if src.Init.EnvFile != "" {
		dst.Init.EnvFile = src.Init.EnvFile
	}
	if src.PortBase > 0 {
		dst.PortBase = src.PortBase
	}
	if len(src.Hooks.PostNew) > 0 {
		dst.Hooks.PostNew = src.Hooks.PostNew
	}
	if len(src.Hooks.PreClose) > 0 {
		dst.Hooks.PreClose = src.Hooks.PreClose
	}
	if len(src.Hooks.PostClose) > 0 {
		dst.Hooks.PostClose = src.Hooks.PostClose
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

// EffectiveAutoInstall returns whether auto-install is enabled (default: true).
func (c *Config) EffectiveAutoInstall() bool {
	if c.AutoInstall != nil {
		return *c.AutoInstall
	}
	return true
}

// EffectivePortBase returns the port base, defaulting to 3000.
func (c *Config) EffectivePortBase() int {
	if c.PortBase > 0 {
		return c.PortBase
	}
	return 3000
}

// EffectiveEnvFile returns the file [init] env writes to, defaulting to ".env".
func (c *Config) EffectiveEnvFile() string {
	if c.Init.EnvFile != "" {
		return c.Init.EnvFile
	}
	return ".env"
}

// EffectiveWorktreeDir builds the worktree directory name.
// Default pattern: "wt-<repo>/<name>" (nested under a per-project folder).
// If WorktreePrefix is explicitly set (even to ""), uses flat layout instead.
func (c *Config) EffectiveWorktreeDir(repoName, name string) string {
	if c.WorktreePrefix != nil {
		return *c.WorktreePrefix + name
	}
	return filepath.Join("wt-"+repoName, name)
}
