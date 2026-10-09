package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mvwi/wt/internal/git"
	"github.com/mvwi/wt/internal/github"
	"github.com/mvwi/wt/internal/ui"
	"github.com/mvwi/wt/internal/update"
	"github.com/spf13/cobra"
)

// errSilent is returned when the error has already been printed and
// Execute should exit non-zero without printing anything extra.
var errSilent = errors.New("")

// cwdOverride is set by the persistent -C/--directory flag and applied
// in PersistentPreRunE before any subcommand reads cwd.
var cwdOverride string

// Version is set at build time via -ldflags.
var Version = "dev"

var rootCmd = &cobra.Command{
	Use:   "wt",
	Short: "Git worktree manager",
	Long: ui.Bold("wt") + ` — git worktree manager

A streamlined workflow for managing git worktrees. Create feature branches,
sync with your base branch, track PR status, and clean up when done.

` + ui.Bold("TYPICAL WORKFLOW") + `
  wt new feature        Create worktree + branch
  wt init               Initialize worktree (see .wt.toml)
  ...work on feature...
  wt submit             Rebase on base branch + push
  ...merge PR...
  wt prune              Clean up merged worktrees

` + ui.Bold("CONFIGURATION") + `
  Global:    ~/.config/wt/config.toml
  Per-repo:  .wt.toml in repo root`,
	SilenceUsage:               true,
	SilenceErrors:              true,
	SuggestionsMinimumDistance: 2,
	Version:                    Version,
	RunE:                       runStatus,
}

// Command group IDs
const (
	groupWorkflow = "workflow"
	groupSync     = "sync"
	groupManage   = "manage"
)

// Execute runs the root command.
func Execute() {
	update.CheckInBackground()

	if err := rootCmd.Execute(); err != nil {
		if !errors.Is(err, errSilent) {
			// Cobra includes "Did you mean this?" in the error message
			// when SuggestionsMinimumDistance is set. Since we silence
			// errors to control output, we print the error ourselves.
			fmt.Fprintf(os.Stderr, "%s %s\n", ui.Red(ui.Fail), err)
		}
		os.Exit(1)
	}

	// Suppress update banner for shell-infrastructure commands that run
	// during shell init (their stderr is visible even though stdout is piped).
	if !isShellInitCommand() {
		if info := update.GetUpdateInfo(Version); info != nil {
			fmt.Fprintf(os.Stderr, "\n%s %s → %s\n",
				ui.Cyan(ui.PushUp+" Update available:"),
				ui.Dim(info.Current),
				ui.Cyan(info.Latest),
			)
			fmt.Fprintf(os.Stderr, "  %s\n", ui.Dim("brew upgrade wt"))
		}
	}
}

func init() {
	rootCmd.SetVersionTemplate("wt version {{.Version}}\n")
	rootCmd.CompletionOptions.HiddenDefaultCmd = true
	rootCmd.PersistentFlags().BoolVarP(&ui.YesFlag, "yes", "y", false, "skip confirmation prompts (answer yes to all)")
	rootCmd.PersistentFlags().StringVarP(&cwdOverride, "directory", "C", "", "run as if started in the given directory (like git -C)")
	_ = rootCmd.MarkPersistentFlagDirname("directory")

	rootCmd.PersistentPreRunE = applyCwdOverride

	rootCmd.AddGroup(
		&cobra.Group{ID: groupWorkflow, Title: "WORKFLOW"},
		&cobra.Group{ID: groupSync, Title: "SYNC"},
		&cobra.Group{ID: groupManage, Title: "MANAGE"},
	)

	cobra.AddTemplateFunc("bold", ui.Bold)
	cobra.AddTemplateFunc("cyan", ui.Cyan)
	cobra.AddTemplateFunc("icon", icon)
	cobra.AddTemplateFunc("helpLine", helpLine)
	rootCmd.SetHelpTemplate(helpTemplate)
	rootCmd.SetUsageTemplate(usageTemplate)
}

// commandIcons decorates the command list in help output. Avoid emoji that
// need a variation selector (e.g. ✏️): terminals disagree on their width.
var commandIcons = map[string]string{
	"new":        "🌱",
	"init":       "🧰",
	"list":       "📋",
	"dash":       "📊",
	"switch":     "🔁",
	"open":       "🌐",
	"pr":         "🔀",
	"pull":       "📥",
	"rebase":     "🔃",
	"submit":     "🚀",
	"watch":      "👀",
	"close":      "🚪",
	"move":       "🚚",
	"prune":      "🧹",
	"rename":     "📝",
	"completion": "🧩",
	"init-shell": "🐚",
	"feedback":   "💬",
}

// helpLine renders one command row: full usage (with args), icon, summary.
func helpLine(c *cobra.Command) string {
	pad := 0
	for _, sib := range c.Parent().Commands() {
		if sib.IsAvailableCommand() {
			pad = max(pad, len(sib.Parent().CommandPath()+" "+sib.Use))
		}
	}
	return fmt.Sprintf("%-*s  %s %s", pad, c.Parent().CommandPath()+" "+c.Use, icon(c), c.Short)
}

func icon(c *cobra.Command) string {
	if i := commandIcons[c.Name()]; i != "" {
		return i
	}
	return "  "
}

// helpTemplate opens subcommand help with the command line and its icon,
// as wrangler does, so usageTemplate skips USAGE for them.
const helpTemplate = `{{if .HasParent}}{{bold .UseLine}}

{{icon .}} {{.Short}}{{with .Long}}

{{. | trimTrailingWhitespaces}}{{end}}{{else}}{{.Long | trimTrailingWhitespaces}}{{end}}

{{.UsageString}}`

// usageTemplate is cobra's default, restyled after wrangler's help:
// bold uppercase headers, full command lines with icons, a short footer.
const usageTemplate = `{{if not .HasParent}}{{bold "USAGE"}}
  {{.UseLine}}
  {{.CommandPath}} [command]

{{end}}{{if gt (len .Aliases) 0}}{{bold "ALIASES"}}
  {{.NameAndAliases}}

{{end}}{{if .HasExample}}{{bold "EXAMPLES"}}
{{.Example}}

{{end}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{range $group := .Groups}}{{bold .Title}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) .IsAvailableCommand)}}
  {{helpLine .}}{{end}}{{end}}

{{end}}{{if not .AllChildCommandsHaveGroup}}{{bold "OTHER"}}{{range $cmds}}{{if (and (eq .GroupID "") .IsAvailableCommand)}}
  {{helpLine .}}{{end}}{{end}}

{{end}}{{end}}{{if .HasAvailableLocalFlags}}{{bold "FLAGS"}}
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

{{bold "GLOBAL FLAGS"}}
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableSubCommands}}

Run {{cyan "wt <command> -h"}} for command help. Report bugs with {{cyan "wt feedback"}}.{{end}}
`

// applyCwdOverride chdir's into the path passed via -C/--directory before any
// subcommand runs. All commands read cwd via os.Getwd() and run git from there,
// so a single chdir at startup propagates transparently to every command.
func applyCwdOverride(_ *cobra.Command, _ []string) error {
	if cwdOverride == "" {
		return nil
	}
	abs, err := filepath.Abs(cwdOverride)
	if err != nil {
		return fmt.Errorf("invalid directory %q: %w", cwdOverride, err)
	}
	if err := os.Chdir(abs); err != nil {
		return fmt.Errorf("cannot change to %s: %w", abs, err)
	}
	return nil
}

// isShellInitCommand returns true for commands that run during shell startup
// (init-shell, completion) where stderr output would be disruptive.
func isShellInitCommand() bool {
	if len(os.Args) < 2 {
		return false
	}
	switch os.Args[1] {
	case "init-shell", "completion":
		return true
	}
	return false
}

// runStatus is called when `wt` is run with no subcommand.
// Shows a quick status of the current worktree, or help if not in a repo.
func runStatus(cmd *cobra.Command, args []string) error {
	if !git.IsInsideWorkTree() {
		return cmd.Help()
	}

	ctx, err := newContext()
	if err != nil {
		return cmd.Help()
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("cannot determine current directory: %w", err)
	}
	branch, err := git.CurrentBranch()
	if err != nil {
		return cmd.Help()
	}

	// Worktree name
	short := ctx.shortName(cwd)
	isMain := cwd == ctx.MainWorktree || isSubpath(cwd, ctx.MainWorktree)

	fmt.Printf("%s %s", ui.Yellow(ui.Current), short)
	if branch != short {
		fmt.Printf("  %s", ui.Dim(branch))
	}
	fmt.Println()

	// Base/main branch: just show it
	if isMain || ctx.isBaseBranch(branch) {
		return nil
	}

	// Sync status
	ab, err := git.GetAheadBehind(ctx.baseRef())
	if err == nil {
		var parts []string
		if ab.Behind > 0 {
			parts = append(parts, ui.Yellow(fmt.Sprintf("%s%d behind %s", ui.ArrowDown, ab.Behind, ctx.Config.BaseBranch)))
		}
		if ab.Ahead > 0 {
			parts = append(parts, ui.Green(fmt.Sprintf("%s%d ahead", ui.ArrowUp, ab.Ahead)))
		}
		if len(parts) > 0 {
			fmt.Printf("  %s\n", strings.Join(parts, "  "))
		} else {
			fmt.Printf("  %s\n", ui.Green("up to date with "+ctx.Config.BaseBranch))
		}
	}

	// Uncommitted changes
	if git.HasChanges() {
		statusShort, _ := git.StatusShort()
		lines := strings.Split(strings.TrimSpace(statusShort), "\n")
		fmt.Printf("  %s\n", ui.Yellow(fmt.Sprintf("%d uncommitted change(s)", len(lines))))
	}

	// Unpushed commits
	unpushed := git.UnpushedCountIn(cwd)
	if unpushed > 0 {
		fmt.Printf("  %s\n", ui.Cyan(fmt.Sprintf("%s%d unpushed commit(s)", ui.PushUp, unpushed)))
	}

	// PR status
	if github.IsAvailable() {
		pr, _ := github.GetPRForBranch(branch)
		if pr != nil && pr.State == "OPEN" {
			rs := pr.GetReviewSummary()
			cs := pr.GetCISummary()

			prStr := fmt.Sprintf("PR #%d", pr.Number)
			var details []string
			if rs.Approved > 0 {
				details = append(details, fmt.Sprintf("%d approved", rs.Approved))
			}
			if rs.Changes > 0 {
				details = append(details, fmt.Sprintf("%d changes requested", rs.Changes))
			}
			if rs.Pending > 0 {
				details = append(details, fmt.Sprintf("%d pending", rs.Pending))
			}
			switch {
			case cs.Fail > 0:
				details = append(details, ui.Red("CI failing"))
			case cs.Pending > 0:
				details = append(details, ui.Yellow("CI pending"))
			case cs.Pass > 0:
				details = append(details, ui.Green("CI passing"))
			}

			if len(details) > 0 {
				fmt.Printf("  %s  %s\n", ui.Blue(prStr), strings.Join(details, ", "))
			} else {
				fmt.Printf("  %s\n", ui.Blue(prStr))
			}
		}
	}

	return nil
}
