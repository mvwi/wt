package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mvwi/wt/internal/config"
	"github.com/mvwi/wt/internal/git"
	"github.com/mvwi/wt/internal/github"
	"github.com/mvwi/wt/internal/ui"
	"github.com/spf13/cobra"
)

var promptCmd = &cobra.Command{
	Use:   "prompt",
	Short: "Print a short worktree status for your shell prompt",
	Long: `Print a one-line status of the current worktree for a shell prompt:

  login-form ⇡2 ⇣5 #123 ✓

⇡ and ⇣ count commits ahead of and behind the base branch, from local refs
(no fetch). The PR number and CI result come from a cache that wt list,
wt dash and wt watch refresh, so wt prompt never calls gh or the network.
Prints nothing outside a worktree or in the main checkout.`,
	Example: `  # starship.toml
  [custom.wt]
  command = "wt prompt"
  when = true
  format = "[$output]($style) "

  # fish_prompt
  echo -n (wt prompt)`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if s := promptSegment(); s != "" {
			fmt.Println(s)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(promptCmd)
}

// cachedPR is the open PR for a branch, as wt prompt shows it.
type cachedPR struct {
	Number int    `json:"number"`
	CI     string `json:"ci,omitempty"` // pass, fail, pending, or "" for no checks
}

// prCache maps main worktree path → branch → open PR.
type prCache map[string]map[string]cachedPR

// promptSegment builds the prompt text, or "" when there is nothing to show.
// It runs two git commands and reads two small files, nothing more.
func promptSegment() string {
	out, err := git.Run("rev-parse", "--path-format=absolute", "--show-toplevel", "--git-common-dir", "--abbrev-ref", "HEAD")
	lines := strings.Split(out, "\n")
	if err != nil || len(lines) != 3 {
		return ""
	}
	top, branch := lines[0], lines[2]
	main := filepath.Dir(lines[1])
	if top == main {
		return ""
	}

	repo := filepath.Base(main)
	cfg, err := config.Load(main, repo)
	if err != nil {
		return ""
	}
	ctx := &cmdContext{Config: cfg, RepoName: repo, MainWorktree: main, ParentDir: filepath.Dir(main)}

	var ahead, behind int
	if counts, err := git.Run("rev-list", "--left-right", "--count", "HEAD..."+ctx.baseRef()); err == nil {
		if a, b, ok := strings.Cut(counts, "\t"); ok {
			ahead, _ = strconv.Atoi(a)
			behind, _ = strconv.Atoi(b)
		}
	}
	return formatPrompt(ctx.shortName(top), ahead, behind, readPRCache()[main][branch])
}

func formatPrompt(name string, ahead, behind int, pr cachedPR) string {
	parts := []string{name}
	if ahead > 0 {
		parts = append(parts, fmt.Sprintf("%s%d", ui.ArrowUp, ahead))
	}
	if behind > 0 {
		parts = append(parts, fmt.Sprintf("%s%d", ui.ArrowDown, behind))
	}
	if pr.Number > 0 {
		parts = append(parts, fmt.Sprintf("#%d", pr.Number))
		switch pr.CI {
		case "pass":
			parts = append(parts, ui.Pass)
		case "fail":
			parts = append(parts, ui.Fail)
		case "pending":
			parts = append(parts, ui.Pending)
		}
	}
	return strings.Join(parts, " ")
}

func ciState(cs github.CISummary) string {
	switch {
	case cs.Fail > 0:
		return "fail"
	case cs.Pending > 0:
		return "pending"
	case cs.Pass > 0:
		return "pass"
	}
	return ""
}

func prCachePath() (string, error) {
	dir, err := os.UserCacheDir()
	return filepath.Join(dir, "wt", "prompt.json"), err
}

func readPRCache() prCache {
	c := prCache{}
	if path, err := prCachePath(); err == nil {
		if data, err := os.ReadFile(path); err == nil {
			_ = json.Unmarshal(data, &c)
		}
	}
	return c
}

// cacheOpenPRs records the open PRs among branches for wt prompt and drops
// cached PRs for the branches that have none now. The cache is only a
// convenience, so errors are ignored.
func cacheOpenPRs(branches []string, open []github.PR) {
	found := map[string]cachedPR{}
	for _, pr := range open {
		found[pr.HeadRefName] = cachedPR{Number: pr.Number, CI: ciState(pr.GetCISummary())}
	}
	writePRCache(branches, found)
}

// cacheWatchStatus records the PR that wt watch polled.
func cacheWatchStatus(ws *github.WatchStatus) {
	found := map[string]cachedPR{}
	if ws.State == "OPEN" {
		found[ws.HeadRefName] = cachedPR{Number: ws.Number, CI: ciState(ws.GetCISummary())}
	}
	writePRCache([]string{ws.HeadRefName}, found)
}

func writePRCache(branches []string, found map[string]cachedPR) {
	main, err := git.MainWorktree()
	if err != nil {
		return
	}
	path, err := prCachePath()
	if err != nil {
		return
	}
	c := readPRCache()
	if c[main] == nil {
		c[main] = map[string]cachedPR{}
	}
	for _, b := range branches {
		if pr, ok := found[b]; ok {
			c[main][b] = pr
		} else {
			delete(c[main], b)
		}
	}
	data, err := json.Marshal(c)
	if err != nil || os.MkdirAll(filepath.Dir(path), 0o755) != nil {
		return
	}
	// Write then rename, so a prompt never reads half a file.
	tmp := path + ".tmp"
	if os.WriteFile(tmp, data, 0o644) == nil {
		_ = os.Rename(tmp, path)
	}
}
