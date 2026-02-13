package github

import "testing"

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
			review("eve", "COMMENTED"),  // should not count as approved or changes
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
