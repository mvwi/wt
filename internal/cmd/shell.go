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

The wrapper intercepts __WT_CD__:/path markers from wt output
and translates them into actual directory changes.`,
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
    set -l tmpfile (mktemp)
    command wt $argv > $tmpfile
    set -l exit_code $status
    while read -l line
        if string match -q "__WT_CD__:*" $line
            cd (string replace "__WT_CD__:" "" $line)
        else
            echo $line
        end
    end < $tmpfile
    rm -f $tmpfile
    return $exit_code
end

# Completions are handled by cobra's built-in fish completion.
# Generate with: wt completion fish | source
`

const bashWrapper = `# wt shell integration (bash)
# Add to ~/.bashrc:
#   eval "$(wt init-shell bash)"

wt() {
    local tmpfile
    tmpfile=$(mktemp)
    command wt "$@" > "$tmpfile"
    local exit_code=$?

    while IFS= read -r line; do
        if [[ "$line" == __WT_CD__:* ]]; then
            cd "${line#__WT_CD__:}" || true
        else
            echo "$line"
        fi
    done < "$tmpfile"

    rm -f "$tmpfile"
    return $exit_code
}

# Generate completions with: eval "$(wt completion bash)"
`

const zshWrapper = `# wt shell integration (zsh)
# Add to ~/.zshrc:
#   eval "$(wt init-shell zsh)"

wt() {
    local tmpfile
    tmpfile=$(mktemp)
    command wt "$@" > "$tmpfile"
    local exit_code=$?

    while IFS= read -r line; do
        if [[ "$line" == __WT_CD__:* ]]; then
            cd "${line#__WT_CD__:}" || true
        else
            echo "$line"
        fi
    done < "$tmpfile"

    rm -f "$tmpfile"
    return $exit_code
}

# Generate completions with: eval "$(wt completion zsh)"
`
