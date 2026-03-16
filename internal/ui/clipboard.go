package ui

import (
	"os/exec"
	"strings"
)

// CopyToClipboard copies text to the system clipboard.
// Supports macOS (pbcopy) and Linux (xclip, xsel).
func CopyToClipboard(text string) error {
	name, args := clipboardCmd()
	cmd := exec.Command(name, args...)
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}

// ClipboardAvailable reports whether a clipboard command is on PATH.
func ClipboardAvailable() bool {
	name, _ := clipboardCmd()
	_, err := exec.LookPath(name)
	return err == nil
}

func clipboardCmd() (string, []string) {
	if _, err := exec.LookPath("pbcopy"); err == nil {
		return "pbcopy", nil
	}
	if _, err := exec.LookPath("xclip"); err == nil {
		return "xclip", []string{"-selection", "clipboard"}
	}
	return "xsel", []string{"--clipboard", "--input"}
}
