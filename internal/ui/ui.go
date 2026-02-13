package ui

import (
	"bufio"
	"fmt"
	"os"
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
	Current    = "●"
	Pending    = "◐"
	Pass       = "✓"
	Fail       = "✗"
	ArrowDown  = "⇣"
	ArrowUp    = "⇡"
	PushUp     = "⬆"
	Dash       = "—"
	NoReview   = "○"
	ErrorMark  = "✗"
)

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
func Confirm(message string, defaultYes bool) bool {
	hint := "y/N"
	if defaultYes {
		hint = "Y/n"
	}

	fmt.Printf("%s [%s] ", message, hint)
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return defaultYes
	}
	input = strings.TrimSpace(strings.ToLower(input))

	if input == "" {
		return defaultYes
	}
	return input == "y" || input == "yes"
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
}

// shellQuote wraps a path in single quotes if it contains shell-special characters.
func shellQuote(s string) string {
	if !strings.ContainsAny(s, " \t\n'\"\\$`|&;<>(){}[]!*?~") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Error prints an error message with ✗ prefix.
func Error(format string, args ...any) {
	fmt.Printf(Red("✗")+" "+format+"\n", args...)
}

// Success prints a success message with ✓ prefix.
func Success(format string, args ...any) {
	fmt.Printf(Green("✓")+" "+format+"\n", args...)
}

// Warn prints a warning message.
func Warn(format string, args ...any) {
	fmt.Printf(Yellow("⚠")+"  "+format+"\n", args...)
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
	for i := 0; i < n; i++ {
		fmt.Print("\033[A\033[K")
	}
}

// Header prints a dim section header (like "WORKTREES").
func Header(text string) {
	fmt.Println()
	DimF("  %s\n", text)
}
