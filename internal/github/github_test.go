package github

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestHeadQueries(t *testing.T) {
	if got := headQueries(nil); len(got) != 0 {
		t.Errorf("headQueries(nil) = %v, want none", got)
	}
	if got := headQueries([]string{"a/x", "b"}); len(got) != 1 || got[0] != "head:a/x head:b" {
		t.Errorf("headQueries = %v, want [head:a/x head:b]", got)
	}

	long := make([]string, 30)
	for i := range long {
		long[i] = "mike/some-long-branch-name"
	}
	got := headQueries(long)
	if len(got) < 2 {
		t.Fatalf("want several queries, got %d", len(got))
	}
	n := 0
	for _, q := range got {
		if len(q) > maxHeadQuery {
			t.Errorf("query too long (%d): %s", len(q), q)
		}
		n += len(strings.Fields(q))
	}
	if n != len(long) {
		t.Errorf("terms = %d, want %d", n, len(long))
	}
}

func review(login, state string) Review {
	return Review{
		Author: struct {
			Login string `json:"login"`
		}{Login: login},
		State: state,
	}
}

func TestPRGetReviewSummary(t *testing.T) {
	pr := &PR{
		ReviewRequests: []ReviewRequest{{Login: "alice"}, {Login: "bob"}},
		LatestReviews: []Review{
			review("charlie", "APPROVED"),
			review("dave", "CHANGES_REQUESTED"),
			review("eve", "COMMENTED"), // should not count as approved or changes
			review("frank", "APPROVED"),
		},
	}

	s := pr.GetReviewSummary()

	if s.Pending != 2 {
		t.Errorf("Pending = %d, want 2", s.Pending)
	}
	if s.Approved != 2 {
		t.Errorf("Approved = %d, want 2", s.Approved)
	}
	if s.Changes != 1 {
		t.Errorf("Changes = %d, want 1", s.Changes)
	}
}

func TestReviewSummaryReRequestedSupersedes(t *testing.T) {
	pr := &PR{
		ReviewRequests: []ReviewRequest{{Login: "alice"}},
		LatestReviews: []Review{
			review("alice", "APPROVED"), // stale — re-review requested
		},
	}

	s := pr.GetReviewSummary()

	if s.Approved != 0 {
		t.Errorf("Approved = %d, want 0 (re-review supersedes)", s.Approved)
	}
	if s.Pending != 1 {
		t.Errorf("Pending = %d, want 1", s.Pending)
	}
}

func TestReviewSummaryDeduplicatesRequests(t *testing.T) {
	// Duplicate entries in ReviewRequests (team + individual request for same user)
	pr := &PR{
		ReviewRequests: []ReviewRequest{
			{Login: "alice"},
			{Login: "bob"},
			{Login: "alice"}, // duplicate
		},
	}

	s := pr.GetReviewSummary()

	if s.Pending != 2 {
		t.Errorf("Pending = %d, want 2 (alice should be counted once)", s.Pending)
	}
}

func TestReviewItemsDeduplicatesRequests(t *testing.T) {
	// Duplicate entries in ReviewRequests (team + individual request for same user)
	ws := &WatchStatus{
		ReviewRequests: []ReviewRequest{
			{Login: "alice"},
			{Login: "bob"},
			{Login: "alice"}, // duplicate
		},
		LatestReviews: []Review{
			review("charlie", "APPROVED"),
		},
	}

	items := ws.ReviewItems()

	count := make(map[string]int)
	for _, item := range items {
		count[item.Login]++
	}
	if count["alice"] != 1 {
		t.Errorf("alice appears %d times, want 1", count["alice"])
	}
	if count["bob"] != 1 {
		t.Errorf("bob appears %d times, want 1", count["bob"])
	}
	if count["charlie"] != 1 {
		t.Errorf("charlie appears %d times, want 1", count["charlie"])
	}
	if len(items) != 3 {
		t.Errorf("len(items) = %d, want 3", len(items))
	}
}

func TestPRGetCISummary(t *testing.T) {
	pr := &PR{
		StatusChecks: []StatusCheckRun{
			{Name: "test", Conclusion: "SUCCESS"},
			{Name: "lint", Conclusion: "FAILURE"},
			{Name: "build", Conclusion: "SKIPPED"},
			{Name: "deploy", State: "PENDING"},
			{Name: "e2e", Conclusion: "TIMED_OUT"},
			{Name: "security", State: "SUCCESS"},
			{Name: "coverage", Conclusion: "CANCELLED"},
			{Name: "perf", State: "ERROR"},
		},
	}

	s := pr.GetCISummary()

	if s.Total != 8 {
		t.Errorf("Total = %d, want 8", s.Total)
	}
	if s.Pass != 3 { // SUCCESS, SKIPPED, state=SUCCESS
		t.Errorf("Pass = %d, want 3", s.Pass)
	}
	if s.Fail != 4 { // FAILURE, TIMED_OUT, CANCELLED, state=ERROR
		t.Errorf("Fail = %d, want 4", s.Fail)
	}
	if s.Pending != 1 { // state=PENDING
		t.Errorf("Pending = %d, want 1", s.Pending)
	}
}

func TestChecksUnmarshal(t *testing.T) {
	// A job listed twice (push + pull_request), a commit status with only a
	// context, and a nameless summary entry.
	data := `[
		{"name":"build","conclusion":"SUCCESS"},
		{"name":"build","conclusion":"FAILURE"},
		{"context":"codecov/patch","state":"FAILURE"},
		{"state":"SUCCESS"}
	]`
	var cs Checks
	if err := json.Unmarshal([]byte(data), &cs); err != nil {
		t.Fatal(err)
	}
	if len(cs) != 2 {
		t.Fatalf("got %d checks, want 2: %+v", len(cs), cs)
	}
	if cs[0].Name != "build" || cs[0].Result() != "fail" {
		t.Errorf("duplicate should keep the worst result: %+v", cs[0])
	}
	if cs[1].Name != "codecov/patch" || cs[1].Result() != "fail" {
		t.Errorf("status context should become the name: %+v", cs[1])
	}
	if s := getCISummary(cs); s.Total != 2 || s.Fail != 2 {
		t.Errorf("summary = %+v", s)
	}
}
