package ui

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/fatih/color"
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

	GreenF   = color.New(color.FgGreen).PrintfFunc()
	YellowF  = color.New(color.FgYellow).PrintfFunc()
	RedF     = color.New(color.FgRed).PrintfFunc()
	BlueF    = color.New(color.FgBlue).PrintfFunc()
	CyanF    = color.New(color.FgCyan).PrintfFunc()
	MagentaF = color.New(color.FgMagenta).PrintfFunc()
	DimF     = color.New(color.FgHiBlack).PrintfFunc()
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

// Truncate shortens a string to max length, adding "…" if truncated.
func Truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 1 {
		return "…"
	}
	return s[:max-1] + "…"
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
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(strings.ToLower(input))

	if input == "" {
		return defaultYes
	}
	return input == "y" || input == "yes"
}

// CdHintPrefix is the special marker that shell wrappers look for.
const CdHintPrefix = "__WT_CD__:"

// PrintCdHint outputs a cd hint that the shell wrapper will intercept.
func PrintCdHint(path string) {
	fmt.Println(CdHintPrefix + path)
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

// Header prints a dim section header (like "WORKTREES").
func Header(text string) {
	fmt.Println()
	DimF("  %s\n", text)
}
