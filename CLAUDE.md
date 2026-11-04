# wt — Git Worktree Manager

## What This Is

A Go CLI that manages git worktrees with an opinionated workflow: create feature branches, sync with a base branch, track GitHub PR status, and clean up when done. Ported from a fish shell script — the Go version is distributable via Homebrew and works with any shell.

## Architecture

```
cmd/wt/main.go              Entry point — just calls cmd.Execute()
internal/
  cmd/                       Cobra commands (one file per command)
    context.go               Shared cmdContext: config + repo info, built once per command
    root.go                  Root command, version flag
    new.go                   Create worktree + branch
    init.go                  Initialize worktree (copy files, run commands)
    install.go               Shared lockfile detection + install command helpers
    list.go                  Show worktrees + PR/review/CI status
    dash.go                  Live worktree dashboard (launches tui.RunDashboard)
    switch.go                Switch worktree (fzf picker or fuzzy match)
    rebase.go                Rebase onto base branch (--all, --continue, --abort)
    submit.go                Rebase + push
    move.go                  Move uncommitted changes between worktrees
    close.go                 Close + clean up worktree
    prune.go                 Remove stale worktrees (merged/closed PRs)
    rename.go                Rename branch + directory + remote
    pull.go                  Pull a remote branch into a new worktree
    pr.go                    Checkout a GitHub PR into a worktree
    open.go                  Open PR in browser
    watch.go                 Poll PR until mergeable or blocked
    feedback.go              Open GitHub issue for feedback/bugs
    edit.go                  Open a worktree in the user's editor
    prompt.go                Prompt segment from local refs + PR cache (no network)
    skill.go                 Print SKILL.md for AI agents, built from the command tree
    output.go                Shared --output json flag: addOutputFlag, startOutput, printJSON
    hooks.go                 finishNew (end of every create flow), [hooks], WT_INDEX/WT_PORT, [init] env
    clone_darwin.go          clone_dirs via APFS clonefile (clone_other.go: cp --reflink=always)
    shell.go                 Shell wrapper output (init-shell fish|bash|zsh)
    completion.go            Shell completion generation
  git/                       Wraps `git` CLI via exec.Command
    git.go                   Run/RunIn/RunPassthrough/RunSilent helpers
    worktree.go              List, Add, Remove, Move worktrees
    branch.go                Branch operations + ahead/behind calculation
    repo.go                  RepoName, MainWorktree, Username, TopLevel
    status.go                HasChanges, StatusPorcelain, UnpushedCount
    stash.go                 StashPush, StashPop
    rebase.go                Rebase, MergeFF, Push, state file management
  github/                    Wraps `gh` CLI — degrades gracefully if not installed
    github.go                PR listing, review/CI summaries, branch rename via API
  tui/                       Bubble Tea screens; take plain data, return the user's choice
    dash.go                  `wt dash` dashboard model
    prune.go                 `wt prune` stale-worktree picker
  ui/                        Terminal output helpers
    ui.go                    Colors, prompts, glyphs, cd hints, Truncate
    spinner.go               Animated spinner for long-running operations
  config/                    Configuration from .wt.toml
    config.go                Load config with defaults, Effective* methods
  update/                    Version update checking
    check.go                 Daily update check + banner display
```

## Key Patterns

### cmdContext — shared per-command context
Every command starts with `ctx, err := newContext()`. This reads `.wt.toml` once and resolves repo info. Use `ctx.branchName("feat")`, `ctx.worktreePath("feat")`, `ctx.baseRef()` instead of hardcoding values. All configurability flows through here.

### Shell cd via `WT_CD_FILE` side-channel
The Go binary can't change the parent shell's directory. The shell wrapper from `wt init-shell` creates a temp file and passes its path via `WT_CD_FILE`. Commands that need to cd (switch, close, rename) call `ui.PrintCdHint(path)` which writes the target path to that file. After the binary exits, the wrapper reads it and runs `cd`. Stdout is never redirected, so colors, prompts, piping, and real-time output all work naturally.

### Auto-install after rebase
When `wt rebase` detects that a lockfile changed (pnpm-lock.yaml, yarn.lock, etc.), it automatically runs the corresponding install command. This is on by default and can be disabled with `auto_install = false` in `.wt.toml`. The lockfile detection is shared with `wt init` via `detectInstallCommand()` in `install.go`. For `wt rebase --all`, no install runs — worktrees with lockfile changes get an annotation instead.

### Git operations: shell out, don't use go-git
We call the `git` CLI via `exec.Command`. go-git has poor worktree support and divergent behavior. The `git.Run()` / `git.RunIn()` helpers capture output; `git.RunPassthrough()` streams to terminal for interactive commands (rebase, push). `git.RunSilent()` discards output.

### GitHub integration: graceful degradation
`github.IsAvailable()` checks if `gh` is on PATH. All GitHub features (PR status in list, safety checks in close, remote rename) are skipped silently when `gh` isn't installed. JSON is parsed with `encoding/json` — no `jq` dependency.

### Create flows end in one place
`new`, `pr` and `pull` all end in `finishCreate` (new.go), which calls `finishNew` (hooks.go) for init, `post_new` hooks and the next-step hint, then handles `--output json`, `--edit` and `--run`. Add new create-time behaviour there, not in each command.

### Hooks come only from global config
`[hooks]` run without being asked for, so `config.Load` drops them from a repo's `.wt.toml` (sets `RepoHooksIgnored`, and `warnRepoHooks` says so). Never let repo-local config run commands implicitly; `[init] commands` are fine because the user runs `wt init` or passes `-i`.

### Discarding work needs --force
`-y` answers prompts but never discards work. Prompts that would lose uncommitted changes, unpushed commits or remote commits use `ui.ConfirmDiscard(msg, force)`, which returns `ui.ErrNeedsForce` (exit 1) under `-y` or without a TTY. A declined prompt in an interactive terminal prints "Cancelled" and exits 0.

### Machine-readable output
Commands that agents call take `--output json` via `addOutputFlag`/`startOutput`/`printJSON` (output.go). In JSON mode `ui.JSON` is set: stdout gets only the JSON object, `IsTTY()` reports false so there are no prompts or spinners, and a needed confirmation becomes an error. `fetchPRData` (list.go) and `watch` write the PR cache that `wt prompt` reads.

### Configuration: zero-config with full override
`.wt.toml` is optional. Defaults: `base_branch = "main"`, `remote = "origin"`, `branch_prefix` = git username. Config is loaded from the main worktree root (not cwd). See `config.go` for all fields.

## External Dependencies

- **git** — required, called via `exec.Command`
- **gh** (GitHub CLI) — optional, enables PR status/safety checks
- **fzf** — optional, enables interactive picker in `wt switch`

## Go Dependencies

- `github.com/spf13/cobra` — CLI framework + shell completions
- `github.com/fatih/color` — terminal colors (respects NO_COLOR)
- `github.com/BurntSushi/toml` — config parsing
- `charm.land/bubbletea/v2` + `charm.land/lipgloss/v2` — TUI screens in `internal/tui/` (v2 API: `View()` returns `tea.View`, keys arrive as `tea.KeyPressMsg`)

## Build & Test

```sh
make build        # Build ./wt binary
make install      # go install to $GOPATH/bin
make test         # go test ./...
make lint         # golangci-lint
go vet ./...      # Static analysis
```

## Conventions

- One Cobra command per file in `internal/cmd/`
- New commands need a help icon in `commandIcons` (`root.go`); help styling lives in `usageTemplate` there. Pick emoji without a variation selector so columns stay aligned
- Commands should call `newContext()` first, then use `ctx.*` helpers
- User-facing errors: return `fmt.Errorf(...)` — Cobra prints them
- User-facing output: use `ui.Success()`, `ui.Error()`, `ui.Warn()`, `fmt.Println()`
- Agent call-to-action hints: use `ui.PrintCTA("wt next-cmd", ...)` — emits `cta: cmd1 | cmd2` only when stdout is non-TTY; no-ops in human terminal sessions
- Don't hardcode "staging", "main", or "origin" — use `ctx.Config.BaseBranch`, `ctx.Config.Remote`
- Git operations go through `internal/git/`, not raw `exec.Command`
- GitHub operations go through `internal/github/`, always check `IsAvailable()` first
- Interactive prompts use `ui.Confirm(message, defaultYes)` — defaultYes=true for safe operations (Y/n), false for destructive (y/N). Prompts that throw away work use `ui.ConfirmDiscard` instead (see "Discarding work needs --force")
- Parse `git status --porcelain` through `git.StatusPorcelainIn`, never through `Run`/`RunIn`: those trim output, which eats the leading space of the first status line
- Spinners draw nothing when stdout isn't a TTY; never print progress text that would leak into pipes
- TUI tests drive the model directly: `newDashModel(cfg)`, then `Update` with `tea.WindowSizeMsg` / `tea.KeyPressMsg`, and assert on `View().Content` (see `internal/tui/dash_test.go`)
- Auto-install uses `detectInstallCommand()` from `install.go` — update `knownLockfiles` there when adding new package managers

## Maintaining Documentation

When adding features, establishing new patterns, or making architectural decisions that future work should follow, update this file to reflect them. Examples: new conventions (e.g. how errors are displayed), new command files, new helpers in `internal/`, config fields, or external tool integrations. This file is the source of truth for how the codebase works — keep it accurate as things change.

Also keep the README updated when user-facing behavior changes — new commands, changed flags, new config options, updated installation steps, or revised usage examples.

## Distribution

- GoReleaser builds darwin/linux × amd64/arm64
- Homebrew formula auto-published to `mvwi/homebrew-tap` on tag push
- CI runs on push to main and PRs: test (ubuntu + macos), lint, build
