```
                  █████
                 ▒▒███
 █████ ███ █████ ███████
▒▒███ ▒███▒▒███ ▒▒▒███▒
 ▒███ ▒███ ▒███   ▒███
 ▒▒███████████    ▒███ ███
  ▒▒████▒████     ▒▒█████
   ▒▒▒▒ ▒▒▒▒       ▒▒▒▒▒
```

*Worktrees without the irk thees.*

Git worktrees are the best way to work on multiple branches at the same time. No stashing, no context-switching, no waiting for dependencies to reinstall. Every branch gets its own directory, ready to go.

But the built-in commands make it hard to love them. Creating a worktree means juggling `git worktree add`, `git checkout -b`, and a path you have to remember. Switching between them means typing out full directory paths. Figuring out which ones still have open PRs, or which ones were merged three weeks ago and are just sitting there, means checking GitHub manually.

`wt` handles all of that. Create a worktree and branch in one command, or wrap an existing branch with `--from`. Switch without typing paths. Rebase and push with `wt submit`. PR status, CI checks, and code review show up right in your terminal, and when a PR merges, `wt prune` cleans everything up.

It's a thin layer over git. No special repo format, no lock-in. Your worktrees and branches are normal git. `wt` just makes them easier to manage. Remove it and everything still works.

Works without config. `wt list` pulls in PR status, CI, and code review from GitHub. Shell `cd` just works when you switch worktrees.

## How It Works

```sh
wt new sidebar-card       # Create worktree + branch, cd into it
wt init                   # Copy files, run commands from .wt.toml
# ...work on your feature...
wt submit                 # Rebase onto main + push (offers to watch PR)
wt list                   # See all worktrees with PR/CI/review status
# ...PR gets merged on GitHub...
wt prune                  # Clean up merged worktrees
```

## Use Cases

<details open>
<summary><b>Run multiple agent sessions in parallel</b></summary>

You want two agents working on different tasks at the same time. If they share a branch, they'll overwrite each other's files and fight over `git status`.

```sh
wt new auth-refactor --init    # Agent 1 gets its own worktree
wt new fix-pagination --init   # Agent 2 gets a separate one
```

Each agent works in complete isolation — its own directory, its own branch, its own node_modules. `--init` copies `.env` files, installs dependencies, and sets up AI config (`.claude`, `.cursorrules`) automatically. When they're done, push each independently, then `wt watch` for updates as CI runs.

</details>

<details>
<summary><b>Fix a bug without derailing your current work</b></summary>

You're deep in changes when you find an unrelated bug. The old way: stash your changes, make a new branch, fix the bug, switch back, pop the stash, and hope nothing broke (except your concentration). Or more likely, ignore it and tell yourself you'll fix it later.

```sh
wt new hotfix                  # New worktree from main — your feature branch is untouched
# ...fix the bug...
wt submit                     # Rebase + push the fix
wt switch -                   # Back to your feature, exactly where you left off (or even better, farm this out to an agent to preserve your focus too)
```

Same thing works for addressing PR review comments on a different branch — `wt switch` to it, make the changes, `wt submit`, `wt switch -` back.

</details>

<details>
<summary><b>Move work you started on the wrong branch</b></summary>

You've been coding for 20 minutes before realizing you're on `main`. Or you started on `feature-a` but this really belongs in its own branch.

```sh
wt move new-feature            # Moves all uncommitted changes to another worktree
```

If the target doesn't exist, `wt move` offers to create it. It detects conflicts with files already modified in the target, cleans up the source after a successful move, and creates any missing directories. The manual alternative — copying files, checking for conflicts, running `git checkout .` and `git clean` — is tedious and error-prone.

</details>

<details>
<summary><b>Set up a new worktree without the boilerplate</b></summary>

A new worktree is a bare directory. No `node_modules`, no `.env`, no generated files. Getting it into a working state means copying environment files, running the right install command, generating Prisma schemas, and setting up AI tool configs. Lots of manual effort relative to `git checkout -b`.

```sh
wt init                        # Auto-detects and handles everything
```

With zero configuration, `wt init` detects your package manager from lockfiles, copies `.env` files from main, copies AI config directories, and runs install with the right frozen-lockfile flags. Support out of the box for `pnpm`, `yarn`, `bun`, `npm`, `Go`, `Cargo`, `Bundler`, and `pip`. If you use Prisma, it finds your schema and generates. All of this is customizable in `.wt.toml` if the defaults aren't right.

</details>

<details>
<summary><b>Push and stop refreshing GitHub</b></summary>

You push your branch and alt-tab to GitHub, refreshing until CI goes green and reviewers approve. If a check fails, you might not notice for 10 minutes.

```sh
wt submit                     # Rebase onto main + push (force-with-lease)
# "Watch PR status? [Y/n]"
wt watch                      # Live terminal dashboard, polls every 15s
```

`wt watch` shows CI checks and review status in a live-updating table. When everything resolves — or something fails — you get a desktop notification and a terminal bell. No more tab-switching.

</details>

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

### Shell Setup

`wt` needs a thin shell wrapper so it can `cd` into worktrees for you. Add one line to your shell config:

**Fish** (`~/.config/fish/config.fish`):
```fish
wt init-shell fish | source
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

## Commands

| Command | Aliases | Description |
|---------|---------|-------------|
| `wt` | | Show current worktree status (branch, sync, PR) |
| `wt new <name>` | `create` | Create worktree with feature branch |
| `wt init` | | Initialize worktree (auto-detects or uses config) |
| `wt list` | `ls` | Show all worktrees with PR status |
| `wt switch [name]` | `sw`, `cd`, `checkout`, `co` | Switch to a worktree (fzf picker if no args, `-` for previous) |
| `wt rebase` | | Rebase current branch onto base branch |
| `wt submit` | | Rebase + push to remote |
| `wt move <name>` | `mv`, `teleport`, `tp` | Move uncommitted changes to another worktree |
| `wt close [name]` | `rm` | Close and clean up a worktree |
| `wt rename <name>` | `rn` | Rename worktree, branch, and remote |
| `wt prune` | | Clean up stale worktrees (merged/closed PRs) |
| `wt pr <number>` | | Checkout a PR into a worktree |
| `wt open [name]` | | Open PR in browser |
| `wt watch [branch or PR]` | | Watch PR until mergeable or blocked |
| `wt feedback [message]` | | Open a GitHub issue for feedback |

Run `wt <command> --help` for detailed usage of any command.

## Configuration

All fields are optional. Config is layered: **defaults -> global -> repo**, each layer only overrides what it sets.

- **Global** (`~/.config/wt/config.toml`): applies to all repos
- **Repo** (`.wt.toml` in repo root): overrides global for that repo

<details>
<summary>Config reference and examples</summary>

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

# Days before a worktree with no open PR is flagged stale in `wt list`.
# Default: 7
stale_threshold = 14

[init]
# Files to copy from the main worktree (if missing in the new worktree).
# Without [init], auto-detected: .env, .env.local, .env.development, .env.test
copy_files = [".env", ".env.local"]

# Shell commands to run sequentially during init.
# Without [init], auto-detected from lockfile: pnpm/yarn/npm/go/cargo/bundler
commands = ["pnpm install", "npx prisma generate"]
```

### Global Config with Per-Repo Overrides

Set everything in `~/.config/wt/config.toml` so you don't need to commit config to repos:

```toml
# Defaults for all repos
base_branch = "main"

# Per-repo overrides (keyed by repo directory name)
[repos.platform]
base_branch = "staging"

[repos.legacy-app]
base_branch = "develop"
branch_prefix = "fix"
```

Precedence: **defaults -> global -> global per-repo -> .wt.toml**

A `.wt.toml` in a repo root always wins, but most users won't need one.

### Settings Reference

| Setting | Default | Effect |
|---------|---------|--------|
| `base_branch` | `"main"` | Branch used for `wt new`, `wt rebase`, `wt submit` |
| `remote` | `"origin"` | Remote for fetch/push operations |
| `branch_prefix` | git username | New branches: `<prefix>/<name>` |
| `worktree_prefix` | `"wt-<repo>-"` | Directory naming: `<prefix><name>` |
| `stale_threshold` | `7` | Days before worktree flagged stale in `wt list` |
| `init.copy_files` | `[]` | Files copied from main worktree if missing |
| `init.commands` | `[]` | Shell commands run during init |

</details>

## AI Tools

`wt` works well with AI coding agents (Claude Code, Cursor, Aider, etc.) — agents with shell access can run `wt` commands directly.

- Run `wt --help` or `wt <command> --help` for full usage
- `wt list --json` outputs machine-readable worktree and PR status
- `wt init` auto-copies AI config (`.claude`, `.cursorrules`, `.cursor/rules`) to new worktrees

## Dependencies

- **git** (required)
- **gh** (GitHub CLI, optional): PR status in `wt list`, safety checks in `wt close`, remote rename in `wt rename`
- **fzf** (optional): interactive picker in `wt switch`

`wt` checks for new versions once daily and shows a notification when an update is available.

## License

MIT
