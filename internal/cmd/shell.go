package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var shellCmd = &cobra.Command{
	Use:   "init-shell <fish|bash|zsh>",
	Short: "Print shell integration wrapper",
	Long: `Print a shell wrapper function that enables 'cd' integration.

Add to your shell config:
  Fish:  wt init-shell fish | source
  Bash:  eval "$(wt init-shell bash)"
  Zsh:   eval "$(wt init-shell zsh)"

The wrapper passes a temp file path via WT_CD_FILE. Commands that
need to change directory write the target path to that file, and
the wrapper reads it after the command exits.`,
	Args:      cobra.ExactArgs(1),
	ValidArgs: []string{"fish", "bash", "zsh"},
	RunE:      runShell,
}

func init() {
	rootCmd.AddCommand(shellCmd)
}

func runShell(cmd *cobra.Command, args []string) error {
	switch args[0] {
	case "fish":
		fmt.Print(fishWrapper)
	case "bash":
		fmt.Print(bashWrapper)
	case "zsh":
		fmt.Print(zshWrapper)
	default:
		return fmt.Errorf("unsupported shell: %s (use fish, bash, or zsh)", args[0])
	}
	return nil
}

const fishWrapper = `# wt shell integration (fish)
# Add to ~/.config/fish/config.fish:
#   wt init-shell fish | source

function wt --wraps wt --description "Git worktree manager"
    set -l cdfile (mktemp -t wt-cd.XXXXXXXX)
    or begin
        command wt $argv
        return $status
    end
    WT_CD_FILE=$cdfile command wt $argv
    set -l exit_code $status
    if test -s $cdfile
        read -l target < $cdfile
        and cd $target
    end
    rm -f $cdfile
    return $exit_code
end

# Completions are handled by cobra's built-in fish completion.
# Generate with: wt completion fish | source
`

const bashWrapper = `# wt shell integration (bash)
# Add to ~/.bashrc:
#   eval "$(wt init-shell bash)"

wt() {
    local cdfile _wt_prev_trap
    cdfile=$(mktemp -t wt-cd.XXXXXXXX) || {
        command wt "$@"
        return $?
    }
    _wt_prev_trap=$(trap -p EXIT 2>/dev/null)
    trap 'rm -f "$cdfile"' EXIT
    WT_CD_FILE="$cdfile" command wt "$@"
    local exit_code=$?
    if [ -s "$cdfile" ]; then
        cd "$(cat "$cdfile")" || true
    fi
    rm -f "$cdfile"
    if [ -n "$_wt_prev_trap" ]; then
        eval "$_wt_prev_trap"
    else
        trap - EXIT
    fi
    return $exit_code
}

# Generate completions with: eval "$(wt completion bash)"
`

const zshWrapper = `# wt shell integration (zsh)
# Add to ~/.zshrc:
#   eval "$(wt init-shell zsh)"

wt() {
    local cdfile _wt_prev_trap
    cdfile=$(mktemp -t wt-cd.XXXXXXXX) || {
        command wt "$@"
        return $?
    }
    _wt_prev_trap=$(trap -p EXIT 2>/dev/null)
    trap 'rm -f "$cdfile"' EXIT
    WT_CD_FILE="$cdfile" command wt "$@"
    local exit_code=$?
    if [ -s "$cdfile" ]; then
        cd "$(cat "$cdfile")" || true
    fi
    rm -f "$cdfile"
    if [ -n "$_wt_prev_trap" ]; then
        eval "$_wt_prev_trap"
    else
        trap - EXIT
    fi
    return $exit_code
}

# Generate completions with: eval "$(wt completion zsh)"
`
