package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/fatih/color"
	"github.com/mvwi/wt/internal/ui"
	"github.com/spf13/cobra"
)

// outputFormat backs --output on new, pr, pull, switch, close and submit.
// Only one command runs per process, so they share it.
var outputFormat string

// jsonOut is the real stdout while --output json is on, nil otherwise.
var jsonOut io.Writer

func addOutputFlag(c *cobra.Command) {
	c.Flags().StringVar(&outputFormat, "output", "", "output format: json")
}

// startOutput applies --output. For json it sends every other line to
// stderr and turns off prompts and spinners, so stdout holds only the JSON.
func startOutput() error {
	switch outputFormat {
	case "":
		return nil
	case "json":
	default:
		return fmt.Errorf("unknown output format %q (want json)", outputFormat)
	}
	jsonOut = os.Stdout
	os.Stdout = os.Stderr
	color.Output = os.Stderr
	ui.JSON = true
	return nil
}

func printJSON(v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(jsonOut, string(data))
	return err
}

// worktreeJSON is what new, pr, pull and switch print with --output json.
type worktreeJSON struct {
	Name   string   `json:"name"`
	Path   string   `json:"path"`
	Branch string   `json:"branch"`
	Base   string   `json:"base"`
	PR     int      `json:"pr,omitempty"`
	CTA    []string `json:"cta,omitempty"`
}
