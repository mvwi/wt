package cmd

import (
	"fmt"
	"net/url"
	"os/exec"
	"runtime"

	"github.com/mvwi/wt/internal/github"
	"github.com/spf13/cobra"
)

const feedbackRepo = "mvwi/wt"

var feedbackCmd = &cobra.Command{
	Use:   "feedback [message]",
	Short: "Open a GitHub issue to share feedback or report a bug",
	Long: `Open a new GitHub issue on the wt repository.

If a message is provided, it will be used as the issue title.
The issue form opens in your default browser.

Examples:
  wt feedback
  wt feedback "rebase --all should show progress"`,
	Args: cobra.MaximumNArgs(1),
	RunE: runFeedback,
}

func init() {
	rootCmd.AddCommand(feedbackCmd)
}

func runFeedback(cmd *cobra.Command, args []string) error {
	var title string
	if len(args) > 0 {
		title = args[0]
	}

	fmt.Println("Opening GitHub issue form...")

	if github.IsAvailable() {
		return feedbackViaGH(title)
	}
	return feedbackViaBrowser(title)
}

func feedbackViaGH(title string) error {
	args := []string{"issue", "create", "--web", "--repo", feedbackRepo}
	if title != "" {
		args = append(args, "--title", title)
	}
	cmd := exec.Command("gh", args...)
	return cmd.Run()
}

func feedbackViaBrowser(title string) error {
	issueURL := "https://github.com/" + feedbackRepo + "/issues/new"
	if title != "" {
		issueURL += "?title=" + url.QueryEscape(title)
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", issueURL)
	case "linux":
		cmd = exec.Command("xdg-open", issueURL)
	default:
		return fmt.Errorf("cannot open browser on %s — visit %s", runtime.GOOS, issueURL)
	}
	return cmd.Run()
}
