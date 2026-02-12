package git

import "testing"

func TestShortenAge(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"2 hours ago", "2h"},
		{"3 days ago", "3d"},
		{"3 months ago", "3mo"},
		{"1 year ago", "1y"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := shortenAge(tt.input)
			if got != tt.want {
				t.Errorf("shortenAge(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
