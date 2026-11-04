package cmd

import (
	"testing"

	"github.com/mvwi/wt/internal/github"
)

func TestPRBranch(t *testing.T) {
	tests := []struct {
		pr   github.PRInfo
		want string
	}{
		{github.PRInfo{Number: 1882, HeadRefName: "fix/perf"}, "fix/perf"},
		{github.PRInfo{Number: 1889, HeadRefName: "perf/redraw", IsCrossRepository: true}, "pr/1889-perf/redraw"},
	}
	for _, tt := range tests {
		if got := prBranch(&tt.pr); got != tt.want {
			t.Errorf("prBranch(#%d) = %q, want %q", tt.pr.Number, got, tt.want)
		}
	}
}
