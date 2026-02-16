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
    list.go                  Show worktrees + PR/review/CI status
    switch.go                Switch worktree (fzf picker or fuzzy match)
    rebase.go                Rebase onto base branch (--all, --continue, --abort)
    submit.go                Rebase + push
    move.go                  Move uncommitted changes between worktrees
    close.go                 Close + clean up worktree
    prune.go                 Remove stale worktrees (merged/closed PRs)
    rename.go                Rename branch + directory + remote
    pr.go                    Checkout a GitHub PR into a worktree
    open.go                  Open PR in browser
    watch.go                 Poll PR until mergeable or blocked
    feedback.go              Open GitHub issue for feedback/bugs
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

### Git operations: shell out, don't use go-git
We call the `git` CLI via `exec.Command`. go-git has poor worktree support and divergent behavior. The `git.Run()` / `git.RunIn()` helpers capture output; `git.RunPassthrough()` streams to terminal for interactive commands (rebase, push). `git.RunSilent()` discards output.

### GitHub integration: graceful degradation
`github.IsAvailable()` checks if `gh` is on PATH. All GitHub features (PR status in list, safety checks in close, remote rename) are skipped silently when `gh` isn't installed. JSON is parsed with `encoding/json` — no `jq` dependency.

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
- Commands should call `newContext()` first, then use `ctx.*` helpers
- User-facing errors: return `fmt.Errorf(...)` — Cobra prints them
- User-facing output: use `ui.Success()`, `ui.Error()`, `ui.Warn()`, `fmt.Println()`
- Don't hardcode "staging", "main", or "origin" — use `ctx.Config.BaseBranch`, `ctx.Config.Remote`
- Git operations go through `internal/git/`, not raw `exec.Command`
- GitHub operations go through `internal/github/`, always check `IsAvailable()` first
- Interactive prompts use `ui.Confirm(message, defaultYes)` — defaultYes=true for safe operations (Y/n), false for destructive (y/N)

## Maintaining Documentation

When adding features, establishing new patterns, or making architectural decisions that future work should follow, update this file to reflect them. Examples: new conventions (e.g. how errors are displayed), new command files, new helpers in `internal/`, config fields, or external tool integrations. This file is the source of truth for how the codebase works — keep it accurate as things change.

Also keep the README updated when user-facing behavior changes — new commands, changed flags, new config options, updated installation steps, or revised usage examples.

## Distribution

- GoReleaser builds darwin/linux × amd64/arm64
- Homebrew formula auto-published to `mvwi/homebrew-tap` on tag push
- CI runs on push to main and PRs: test (ubuntu + macos), lint, build
