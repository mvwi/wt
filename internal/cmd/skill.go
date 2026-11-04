package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var skillCmd = &cobra.Command{
	Use:   "skill",
	Short: "Print a SKILL.md that teaches AI agents to use wt",
	Long: `Print a SKILL.md for AI coding agents such as Claude Code. The command
list comes from this build of wt, so it always matches.`,
	Example: `  mkdir -p ~/.claude/skills/wt && wt skill > ~/.claude/skills/wt/SKILL.md`,
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Print(skillMarkdown(rootCmd))
		return nil
	},
}

func init() {
	rootCmd.AddCommand(skillCmd)
}

const skillHead = `---
name: wt
description: Manage git worktrees with wt. Use when starting work on a new branch, running tasks in parallel, checking out a PR, rebasing on the base branch, pushing, checking PR and CI status, or cleaning up finished work.
---

# wt: git worktree manager

Use wt instead of raw ` + "`git worktree`, `git checkout -b`, `git rebase` and `git push`" + `.
Each task gets its own directory and branch, so parallel work never collides.

## Commands

`

const skillTail = `
Run ` + "`wt <command> --help`" + ` for flags and examples.

## Reading results

- ` + "`--output json`" + ` on new, pr, pull, switch, close, submit and list prints one JSON
  object on stdout. Everything else goes to stderr, and nothing prompts.
- The ` + "`cta`" + ` field lists the commands that usually come next. Without --output json,
  piped output ends with a ` + "`cta: cmd1 | cmd2`" + ` line instead.
- Your shell does not follow ` + "`wt switch`" + `. Take ` + "`path`" + ` from the JSON and run
  commands there, or pass ` + "`-C <path>`" + ` to wt.
- ` + "`wt new <name> --init --run \"<cmd>\"`" + ` creates a worktree, sets it up, and runs
  the command inside it.

## Safety

- ` + "`-y`/`--yes`" + ` answers yes to prompts that lose no work.
- Only ` + "`--force`" + ` discards uncommitted changes or unpushed commits (close, move).
  Never pass it unless the user asked to throw that work away.
- A prompt that would discard work fails when no one can answer it. Report the
  error to the user; don't retry with --force.
`

// skillMarkdown builds SKILL.md, listing the grouped commands of root.
func skillMarkdown(root *cobra.Command) string {
	var b strings.Builder
	b.WriteString(skillHead)
	for _, g := range root.Groups() {
		for _, c := range root.Commands() {
			if c.GroupID == g.ID && c.IsAvailableCommand() {
				fmt.Fprintf(&b, "- `wt %s`: %s\n", c.Use, c.Short)
			}
		}
	}
	b.WriteString(skillTail)
	return b.String()
}
