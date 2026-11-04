package cmd

import (
	"strings"
	"testing"

	"github.com/mvwi/wt/internal/github"
)

func TestBuildSpinnerMessage(t *testing.T) {
	pending := []github.StatusCheckRun{{Name: "test", State: "PENDING"}}
	tests := []struct {
		name string
		ws   github.WatchStatus
		want string
	}{
		{"blocked with no checks", github.WatchStatus{MergeStateStatus: "BLOCKED"}, "maintainer approval"},
		{"no checks yet", github.WatchStatus{MergeStateStatus: "UNKNOWN"}, "Waiting for status"},
		{"pending check", github.WatchStatus{MergeStateStatus: "BLOCKED", StatusChecks: pending}, "Waiting for 1 check"},
		{"review", github.WatchStatus{ReviewDecision: "REVIEW_REQUIRED", StatusChecks: pending}, "checks and review"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := buildSpinnerMessage(&tt.ws); !strings.Contains(got, tt.want) {
				t.Errorf("got %q, want it to contain %q", got, tt.want)
			}
		})
	}
}
