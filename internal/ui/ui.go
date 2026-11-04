package ui

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/fatih/color"
	"github.com/mattn/go-isatty"
)

// Color shortcuts — matches the fish script's set_color calls.
var (
	Green   = color.New(color.FgGreen).SprintFunc()
	Yellow  = color.New(color.FgYellow).SprintFunc()
	Red     = color.New(color.FgRed).SprintFunc()
	Blue    = color.New(color.FgBlue).SprintFunc()
	Cyan    = color.New(color.FgCyan).SprintFunc()
	Magenta = color.New(color.FgMagenta).SprintFunc()
	Dim     = color.New(color.FgHiBlack).SprintFunc()
	Bold    = color.New(color.Bold).SprintFunc()

	GreenF  = color.New(color.FgGreen).PrintfFunc()
	YellowF = color.New(color.FgYellow).PrintfFunc()
	BlueF   = color.New(color.FgBlue).PrintfFunc()
	CyanF   = color.New(color.FgCyan).PrintfFunc()
	DimF    = color.New(color.FgHiBlack).PrintfFunc()
)

// Glyphs used throughout the UI.
const (
	Current   = "●"
	Pending   = "◐"
	Pass      = "✓"
	Fail      = "✗"
	ArrowDown = "⇣"
	ArrowUp   = "⇡"
	PushUp    = "⬆"
	Dash      = "—"
	NoReview  = "○"
)

// YesFlag is set by the root command's --yes persistent flag.
// When true, Confirm() skips the interactive prompt and returns true.
// It never answers ConfirmDiscard(); that takes --force.
var YesFlag bool

// Truncate shortens a string to max runes, adding "…" if truncated.
func Truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max <= 1 {
		return "…"
	}
	return string(runes[:max-1]) + "…"
}

// Confirm prompts the user with a y/n question.
// defaultYes: if true, pressing Enter means yes (Y/n); if false, means no (y/N).
// When stdin is not a terminal, returns defaultYes without prompting.
// Unrecognized input re-prompts until a valid answer is given.
func Confirm(message string, defaultYes bool) bool {
	if YesFlag {
		return true
	}
	if !IsTTY() {
		return defaultYes
	}
	return ask(message, defaultYes)
}

// ErrNeedsForce is returned by ConfirmDiscard when it may not ask.
var ErrNeedsForce = errors.New("won't discard work without --force\n   Re-run with --force to discard it")

// ConfirmDiscard asks before throwing away uncommitted changes or commits.
// Only force answers it: --yes does not, and with no terminal (or under
// --yes) it returns ErrNeedsForce so the command exits non-zero.
func ConfirmDiscard(message string, force bool) (bool, error) {
	if force {
		return true, nil
	}
	if YesFlag || !IsTTY() {
		return false, ErrNeedsForce
	}
	return ask(message, false), nil
}

func ask(message string, defaultYes bool) bool {
	hint := "y/N"
	if defaultYes {
		hint = "Y/n"
	}

	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Printf("%s [%s] ", message, hint)
		input, err := reader.ReadString('\n')
		if err != nil {
			return defaultYes
		}
		input = strings.TrimSpace(strings.ToLower(input))

		switch input {
		case "":
			return defaultYes
		case "y", "yes":
			return true
		case "n", "no":
			return false
		default:
			fmt.Println("Answer y or n.")
		}
	}
}

// PrintCdHint tells the shell wrapper to cd into the given path.
// When WT_CD_FILE is set (by the shell wrapper), writes the path to that file.
// Otherwise prints a hint so the user knows to cd manually.
func PrintCdHint(path string) {
	if cdFile := os.Getenv("WT_CD_FILE"); cdFile != "" {
		if err := os.WriteFile(cdFile, []byte(path), 0600); err != nil {
			fmt.Printf("  → cd %s\n", shellQuote(path))
		}
		return
	}
	fmt.Printf("  → cd %s\n", shellQuote(path))
	PrintWrapperHint()
}

var wrapperHintShown bool

// PrintWrapperHint tells a human at a terminal how to load the shell wrapper,
// once per run. It stays quiet when the wrapper is loaded or stdout is not a TTY.
func PrintWrapperHint() {
	if wrapperHintShown || os.Getenv("WT_CD_FILE") != "" || !IsTTY() {
		return
	}
	wrapperHintShown = true
	DimF("  wt can't cd for you: the shell wrapper isn't loaded. %s\n", wrapperSetup(os.Getenv("SHELL")))
}

// wrapperSetup returns the line that loads the wrapper for the given $SHELL.
func wrapperSetup(shell string) string {
	switch filepath.Base(shell) {
	case "fish":
		return "Add to ~/.config/fish/config.fish:\n    wt init-shell fish | source"
	case "zsh":
		return "Add to ~/.zshrc:\n    eval \"$(wt init-shell zsh)\""
	case "bash":
		return "Add to ~/.bashrc:\n    eval \"$(wt init-shell bash)\""
	}
	return "See: wt init-shell --help"
}

// shellQuote wraps a path in single quotes if it contains shell-special characters.
func shellQuote(s string) string {
	if !strings.ContainsAny(s, " \t\n'\"\\$`|&;<>(){}[]!*?~") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Error prints an error message with ✗ prefix to stderr.
func Error(format string, args ...any) {
	fmt.Fprintf(os.Stderr, Red("✗")+" "+format+"\n", args...)
}

// Success prints a success message with ✓ prefix.
func Success(format string, args ...any) {
	fmt.Printf(Green("✓")+" "+format+"\n", args...)
}

// Warn prints a warning message to stderr.
func Warn(format string, args ...any) {
	fmt.Fprintf(os.Stderr, Yellow("⚠")+"  "+format+"\n", args...)
}

// Info prints a regular message.
func Info(format string, args ...any) {
	fmt.Printf(format+"\n", args...)
}

// IsTTY reports whether stdout is connected to a terminal.
func IsTTY() bool {
	return isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd())
}

// ClearLines moves the cursor up n lines and clears each one.
// Used by wt watch to redraw the live status table in-place.
// No-op when stdout is not a terminal.
func ClearLines(n int) {
	if !IsTTY() {
		return
	}
	for range n {
		fmt.Print("\033[A\033[K")
	}
}

// Header prints a dim section header (like "WORKTREES").
func Header(text string) {
	fmt.Println()
	DimF("  %s\n", text)
}

// PrintCTA prints machine-readable call-to-action suggestions for agent consumers.
// Only emits output when stdout is not a TTY — human terminal output is unaffected.
// Format: "cta: cmd1 | cmd2"
func PrintCTA(cmds ...string) {
	if len(cmds) == 0 || IsTTY() {
		return
	}
	fmt.Printf("cta: %s\n", strings.Join(cmds, " | "))
}

// CISummary condenses CI check counts into one short cell: "✗ 2/36" when any
// fail, "◐ 3/36" when any are pending, otherwise "✓ 36". Returns "" when
// there are no checks. Callers pick the color.
func CISummary(pass, fail, pending, total int) string {
	switch {
	case total == 0:
		return ""
	case fail > 0:
		return fmt.Sprintf("%s %d/%d", Fail, fail, total)
	case pending > 0:
		return fmt.Sprintf("%s %d/%d", Pending, pending, total)
	}
	return fmt.Sprintf("%s %d", Pass, pass)
}
