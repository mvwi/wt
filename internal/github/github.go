package github

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// ghAvailable caches whether `gh` CLI is installed.
var ghChecked bool
var ghInstalled bool

// IsAvailable returns true if `gh` CLI is on PATH.
func IsAvailable() bool {
	if !ghChecked {
		_, err := exec.LookPath("gh")
		ghInstalled = err == nil
		ghChecked = true
	}
	return ghInstalled
}

// runGH executes a gh command and returns stdout.
func runGH(args ...string) (string, error) {
	cmd := exec.Command("gh", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("gh %s: %s", strings.Join(args, " "), strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// PR represents a GitHub pull request.
type PR struct {
	Number         int              `json:"number"`
	HeadRefName    string           `json:"headRefName"`
	State          string           `json:"state"`
	ReviewRequests []ReviewRequest  `json:"reviewRequests"`
	LatestReviews  []Review         `json:"latestReviews"`
	StatusChecks   []StatusCheckRun `json:"statusCheckRollup"`
}

// ReviewRequest represents a pending review request.
type ReviewRequest struct {
	Login string `json:"login"`
}

// Review represents a completed review.
type Review struct {
	State string `json:"state"` // APPROVED, CHANGES_REQUESTED, COMMENTED
}

// StatusCheckRun represents a CI check.
type StatusCheckRun struct {
	Name       string `json:"name"`
	State      string `json:"state"`      // SUCCESS, FAILURE, PENDING, etc.
	Conclusion string `json:"conclusion"` // SUCCESS, FAILURE, SKIPPED, TIMED_OUT, CANCELLED, ""
}

// ReviewSummary aggregates review counts.
type ReviewSummary struct {
	Approved int
	Changes  int
	Pending  int
}

// CISummary aggregates CI check counts.
type CISummary struct {
	Pass    int
	Fail    int
	Pending int
	Total   int
}

// GetReviewSummary computes review state counts.
func (pr *PR) GetReviewSummary() ReviewSummary {
	s := ReviewSummary{Pending: len(pr.ReviewRequests)}
	for _, r := range pr.LatestReviews {
		switch r.State {
		case "APPROVED":
			s.Approved++
		case "CHANGES_REQUESTED":
			s.Changes++
		}
	}
	return s
}

// GetCISummary computes CI check state counts.
func (pr *PR) GetCISummary() CISummary {
	s := CISummary{Total: len(pr.StatusChecks)}
	for _, c := range pr.StatusChecks {
		switch {
		case c.Conclusion == "SUCCESS" || c.Conclusion == "SKIPPED" || c.State == "SUCCESS":
			s.Pass++
		case c.Conclusion == "FAILURE" || c.Conclusion == "TIMED_OUT" || c.Conclusion == "CANCELLED" || c.State == "FAILURE" || c.State == "ERROR":
			s.Fail++
		default:
			s.Pending++
		}
	}
	return s
}

// ListPRs fetches PRs in a given state with full review/CI data.
func ListPRs(state string) ([]PR, error) {
	if !IsAvailable() {
		return nil, nil
	}

	fields := "number,headRefName"
	if state == "open" {
		fields = "number,headRefName,reviewRequests,latestReviews,statusCheckRollup"
	}

	out, err := runGH("pr", "list", "--state", state, "--json", fields, "--limit", "100")
	if err != nil {
		return nil, err
	}

	var prs []PR
	if err := json.Unmarshal([]byte(out), &prs); err != nil {
		return nil, err
	}
	return prs, nil
}

// FindPRForBranch returns the first PR matching the branch name and state.
func FindPRForBranch(prs []PR, branch string) *PR {
	for i := range prs {
		if prs[i].HeadRefName == branch {
			return &prs[i]
		}
	}
	return nil
}

// GetPRForBranch fetches the PR for a specific branch.
func GetPRForBranch(branch string) *PR {
	if !IsAvailable() {
		return nil
	}
	out, err := runGH("pr", "list", "--head", branch, "--json", "number,state", "--limit", "1")
	if err != nil {
		return nil
	}
	var prs []PR
	if err := json.Unmarshal([]byte(out), &prs); err != nil || len(prs) == 0 {
		return nil
	}
	return &prs[0]
}

// RepoSlug returns the "owner/repo" string.
func RepoSlug() (string, error) {
	if !IsAvailable() {
		return "", fmt.Errorf("gh not installed")
	}
	return runGH("repo", "view", "--json", "nameWithOwner", "-q", ".nameWithOwner")
}

// RenameBranch renames a branch on the remote via GitHub API (preserves PRs).
func RenameBranch(repoSlug, oldBranch, newBranch string) error {
	if !IsAvailable() {
		return fmt.Errorf("gh not installed")
	}
	// URL-encode slashes in the branch name
	encoded := strings.ReplaceAll(oldBranch, "/", "%2F")
	_, err := runGH("api", fmt.Sprintf("repos/%s/branches/%s/rename", repoSlug, encoded),
		"-f", "new_name="+newBranch)
	return err
}
