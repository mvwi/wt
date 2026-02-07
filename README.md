# wt — Git Worktree Manager

A streamlined CLI for managing git worktrees. Create feature branches, sync with your base branch, track PR status, and clean up when done.

## Install

**Homebrew:**
```sh
brew install mvwi/tap/wt
```

**Go:**
```sh
go install github.com/mvwi/wt@latest
```

**Binary:** Download from [Releases](https://github.com/mvwi/wt/releases).

## Shell Setup

`wt` needs a thin shell wrapper to change directories (e.g., when switching worktrees). Add to your shell config:

**Fish** (`~/.config/fish/config.fish`):
```fish
eval (wt init-shell fish)
wt completion fish | source
```

**Bash** (`~/.bashrc`):
```bash
eval "$(wt init-shell bash)"
eval "$(wt completion bash)"
```

**Zsh** (`~/.zshrc`):
```zsh
eval "$(wt init-shell zsh)"
eval "$(wt completion zsh)"
```

## Quick Start

```sh
wt new sidebar-card       # Create worktree + branch from base branch
wt init                   # Install deps, copy .env, etc.
# ...work on feature...
wt submit                 # Rebase + push
# ...merge PR on GitHub...
wt prune                  # Clean up merged worktrees
```

## Commands

| Command | Aliases | Description |
|---------|---------|-------------|
| `wt new <name>` | `create` | Create worktree with feature branch |
| `wt init` | | Initialize worktree (deps, .env, prisma) |
| `wt list` | `ls` | Show all worktrees with PR status |
| `wt switch [name]` | `sw`, `cd`, `co` | Switch to a worktree (fzf picker if no args) |
| `wt rebase` | | Rebase current branch onto base branch |
| `wt submit` | | Rebase + push to remote |
| `wt move <name>` | `mv`, `tp` | Move uncommitted changes to another worktree |
| `wt close [name]` | `rm` | Close and clean up a worktree |
| `wt rename <name>` | `rn` | Rename worktree, branch, and remote |
| `wt prune` | | Clean up stale worktrees (merged/closed PRs) |

Run `wt <command> --help` for detailed usage of any command.

## Configuration

Drop a `.wt.toml` in your repo root to customize behavior. All fields are optional — sensible defaults work out of the box.

```toml
# Base branch to create worktrees from and rebase onto.
# Default: "main". Common alternatives: "staging", "develop"
base_branch = "staging"

# Git remote name. Default: "origin"
remote = "origin"

# Prefix for new branch names: "<prefix>/<name>".
# Default: your git username's first name (e.g., "michael").
# Set to "" to disable prefixing entirely.
branch_prefix = "michael"

# Worktree directory naming: "<prefix><name>".
# Default: "wt-<repo>-" (e.g., "wt-myapp-sidebar").
# Set to customize (e.g., "wt-" for shorter names).
worktree_prefix = ""

[init]
# .env files to copy from main worktree during init.
# Default: [".env", ".env.local", ".env.development.local"]
env_files = [".env", ".env.local"]

# Override package manager auto-detection.
# Values: "pnpm", "yarn", "npm", "bun", or "" for auto-detect.
package_manager = ""

# Commands to run after init (replaces .wt-init scripts).
post_commands = [
  "npx prisma generate",
  "npx prisma db push",
]
```

### What Gets Configured

| Setting | Default | Effect |
|---------|---------|--------|
| `base_branch` | `"main"` | Branch used for `wt new`, `wt rebase`, `wt submit` |
| `remote` | `"origin"` | Remote for fetch/push operations |
| `branch_prefix` | git username | New branches: `<prefix>/<name>` |
| `worktree_prefix` | `"wt-<repo>-"` | Directory naming: `<prefix><name>` |
| `init.env_files` | `.env`, `.env.local`, `.env.development.local` | Files copied from main repo |
| `init.package_manager` | auto-detect | Force specific package manager |
| `init.post_commands` | `[]` | Custom init steps |

## Dependencies

- **git** — required
- **gh** (GitHub CLI) — optional, enables PR status in `wt list`, safety checks in `wt close`, and remote rename in `wt rename`
- **fzf** — optional, enables interactive picker in `wt switch`

## Migration from Fish Script

If you were using the fish function version of `wt`:

1. Remove `~/.config/fish/functions/wt.fish`
2. Install the Go binary (see Install above)
3. Add shell integration (see Shell Setup above)
4. If your repo uses `staging` as base branch, add `.wt.toml`:
   ```toml
   base_branch = "staging"
   ```
5. All commands and aliases work identically

## License

MIT
