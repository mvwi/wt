package github

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
)

var (
	ghOnce      sync.Once
	ghInstalled bool
)

// IsAvailable returns true if `gh` CLI is on PATH.
func IsAvailable() bool {
	ghOnce.Do(func() {
		_, err := exec.LookPath("gh")
		ghInstalled = err == nil
	})
	return ghInstalled
}

// ResetAvailability resets the cached gh availability check.
// Intended for testing only.
func ResetAvailability() {
	ghOnce = sync.Once{}
	ghInstalled = false
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
	Author struct {
		Login string `json:"login"`
	} `json:"author"`
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
	return getReviewSummary(pr.ReviewRequests, pr.LatestReviews)
}

// GetCISummary computes CI check state counts.
func (pr *PR) GetCISummary() CISummary {
	return getCISummary(pr.StatusChecks)
}

// getReviewSummary computes review state counts from requests and reviews.
// A pending re-review request supersedes a stale completed review.
func getReviewSummary(requests []ReviewRequest, reviews []Review) ReviewSummary {
	reRequested := make(map[string]bool)
	for _, rr := range requests {
		reRequested[rr.Login] = true
	}

	var s ReviewSummary
	seen := make(map[string]bool)
	for _, r := range reviews {
		login := r.Author.Login
		if login == "" {
			continue
		}
		seen[login] = true
		if reRequested[login] {
			s.Pending++
			continue
		}
		switch r.State {
		case "APPROVED":
			s.Approved++
		case "CHANGES_REQUESTED":
			s.Changes++
		}
	}
	for _, rr := range requests {
		if !seen[rr.Login] {
			s.Pending++
		}
	}
	return s
}

// getCISummary computes CI check state counts from status checks.
// Checks with empty names (GitHub summary checks) are excluded.
func getCISummary(checks []StatusCheckRun) CISummary {
	var s CISummary
	for _, c := range checks {
		if c.Name == "" {
			continue
		}
		s.Total++
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

	out, err := runGH("pr", "list", "--state", state, "--json", fields, "--limit", "50")
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
// Returns (nil, nil) if gh is not available or no PR exists.
func GetPRForBranch(branch string) (*PR, error) {
	if !IsAvailable() {
		return nil, nil
	}
	out, err := runGH("pr", "list", "--head", branch, "--json", "number,state", "--limit", "1")
	if err != nil {
		return nil, err
	}
	var prs []PR
	if err := json.Unmarshal([]byte(out), &prs); err != nil {
		return nil, err
	}
	if len(prs) == 0 {
		return nil, nil
	}
	return &prs[0], nil
}

// RepoSlug returns the "owner/repo" string.
func RepoSlug() (string, error) {
	if !IsAvailable() {
		return "", fmt.Errorf("gh not installed")
	}
	return runGH("repo", "view", "--json", "nameWithOwner", "-q", ".nameWithOwner")
}

// PRLabel represents a label on a PR.
type PRLabel struct {
	Name string `json:"name"`
}

// PRDetails holds full PR metadata for recreation after branch rename.
type PRDetails struct {
	Number      int       `json:"number"`
	Title       string    `json:"title"`
	Body        string    `json:"body"`
	BaseRefName string    `json:"baseRefName"`
	IsDraft     bool      `json:"isDraft"`
	Labels      []PRLabel `json:"labels"`
}

// GetPRDetails fetches full details for the open PR on a branch.
func GetPRDetails(branch string) (*PRDetails, error) {
	if !IsAvailable() {
		return nil, nil
	}
	out, err := runGH("pr", "list", "--head", branch, "--state", "open",
		"--json", "number,title,body,baseRefName,isDraft,labels", "--limit", "1")
	if err != nil {
		return nil, err
	}
	var prs []PRDetails
	if err := json.Unmarshal([]byte(out), &prs); err != nil {
		return nil, err
	}
	if len(prs) == 0 {
		return nil, nil
	}
	return &prs[0], nil
}

// WatchStatus holds detailed PR state from `gh pr view`, including fields
// not available from `gh pr list` (mergeStateStatus, mergeable).
type WatchStatus struct {
	Number           int              `json:"number"`
	Title            string           `json:"title"`
	State            string           `json:"state"`            // OPEN, CLOSED, MERGED
	HeadRefName      string           `json:"headRefName"`
	MergeStateStatus string           `json:"mergeStateStatus"` // CLEAN, BLOCKED, BEHIND, DRAFT, DIRTY, UNKNOWN
	Mergeable        string           `json:"mergeable"`        // MERGEABLE, CONFLICTING, UNKNOWN
	ReviewDecision   string           `json:"reviewDecision"`   // APPROVED, CHANGES_REQUESTED, REVIEW_REQUIRED, ""
	ReviewRequests   []ReviewRequest  `json:"reviewRequests"`
	LatestReviews    []Review         `json:"latestReviews"`
	StatusChecks     []StatusCheckRun `json:"statusCheckRollup"`
}

// GetReviewSummary computes review state counts for a WatchStatus.
func (ws *WatchStatus) GetReviewSummary() ReviewSummary {
	return getReviewSummary(ws.ReviewRequests, ws.LatestReviews)
}

// GetCISummary computes CI check state counts for a WatchStatus.
func (ws *WatchStatus) GetCISummary() CISummary {
	return getCISummary(ws.StatusChecks)
}

// FailedCheckNames returns the names of checks that failed.
func (ws *WatchStatus) FailedCheckNames() []string {
	var names []string
	for _, c := range ws.StatusChecks {
		switch {
		case c.Conclusion == "FAILURE" || c.Conclusion == "TIMED_OUT" || c.Conclusion == "CANCELLED" || c.State == "FAILURE" || c.State == "ERROR":
			names = append(names, c.Name)
		}
	}
	return names
}

// ChecksByStatus returns checks sorted into pass, fail, and pending buckets.
// Checks with empty names (GitHub summary checks) are excluded.
func (ws *WatchStatus) ChecksByStatus() (pass, fail, pending []StatusCheckRun) {
	for _, c := range ws.StatusChecks {
		if c.Name == "" {
			continue
		}
		switch {
		case c.Conclusion == "SUCCESS" || c.Conclusion == "SKIPPED" || c.State == "SUCCESS":
			pass = append(pass, c)
		case c.Conclusion == "FAILURE" || c.Conclusion == "TIMED_OUT" || c.Conclusion == "CANCELLED" || c.State == "FAILURE" || c.State == "ERROR":
			fail = append(fail, c)
		default:
			pending = append(pending, c)
		}
	}
	return
}

// ReviewItem represents a unified review entry (completed or pending).
type ReviewItem struct {
	Login string
	State string // "approved", "changes_requested", "pending"
}

// ReviewItems merges LatestReviews and ReviewRequests into a unified list.
// A pending re-review request supersedes any previous completed review
// (the old approval is stale after new commits are pushed).
func (ws *WatchStatus) ReviewItems() []ReviewItem {
	// Build set of logins with pending re-review requests
	reRequested := make(map[string]bool)
	for _, rr := range ws.ReviewRequests {
		if rr.Login != "" {
			reRequested[rr.Login] = true
		}
	}

	seen := make(map[string]bool)
	var items []ReviewItem

	for _, r := range ws.LatestReviews {
		if r.State == "COMMENTED" {
			continue
		}
		login := r.Author.Login
		if login == "" {
			continue
		}
		seen[login] = true

		// Re-review request supersedes the stale completed review
		if reRequested[login] {
			items = append(items, ReviewItem{Login: login, State: "pending"})
			continue
		}

		state := "approved"
		if r.State == "CHANGES_REQUESTED" {
			state = "changes_requested"
		}
		items = append(items, ReviewItem{Login: login, State: state})
	}

	// Add reviewers who were requested but haven't reviewed yet
	for _, rr := range ws.ReviewRequests {
		if rr.Login != "" && !seen[rr.Login] {
			items = append(items, ReviewItem{Login: rr.Login, State: "pending"})
		}
	}
	return items
}

// GetWatchStatus fetches detailed PR status for a branch using `gh pr view`.
func GetWatchStatus(branch string) (*WatchStatus, error) {
	if !IsAvailable() {
		return nil, fmt.Errorf("gh not installed")
	}

	fields := "number,title,state,headRefName,mergeStateStatus,mergeable,reviewDecision,reviewRequests,latestReviews,statusCheckRollup"
	out, err := runGH("pr", "view", branch, "--json", fields)
	if err != nil {
		return nil, err
	}

	var ws WatchStatus
	if err := json.Unmarshal([]byte(out), &ws); err != nil {
		return nil, err
	}
	return &ws, nil
}

// CreatePR creates a new pull request and returns the PR URL.
func CreatePR(head, base, title, body string, draft bool, labels []string) (string, error) {
	if !IsAvailable() {
		return "", fmt.Errorf("gh not installed")
	}
	args := []string{"pr", "create", "--head", head, "--base", base, "--title", title, "--body", body}
	if draft {
		args = append(args, "--draft")
	}
	for _, l := range labels {
		args = append(args, "--label", l)
	}
	return runGH(args...)
}
