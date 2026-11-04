package ui

import "testing"

// Tests run without a terminal, so ConfirmDiscard never prompts here.
func TestConfirmDiscard(t *testing.T) {
	tests := []struct {
		name    string
		yes     bool
		force   bool
		want    bool
		wantErr bool
	}{
		{"force discards", false, true, true, false},
		{"--yes alone refuses", true, false, false, true},
		{"no terminal refuses", false, false, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			YesFlag = tt.yes
			defer func() { YesFlag = false }()
			got, err := ConfirmDiscard("Discard?", tt.force)
			if got != tt.want || (err != nil) != tt.wantErr {
				t.Errorf("ConfirmDiscard() = %v, %v; want %v, err=%v", got, err, tt.want, tt.wantErr)
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		name  string
		input string
		max   int
		want  string
	}{
		{"no truncation needed", "hello", 10, "hello"},
		{"truncates with ellipsis", "hello world", 8, "hello w…"},
		{"max 0 returns ellipsis", "hello", 0, "…"},
		{"empty string", "", 5, ""},
		{"multi-byte runes", "héllo wörld", 8, "héllo w…"},
		{"emoji", "🎉🎊🎈🎁", 3, "🎉🎊…"},
		{"cjk characters", "日本語テスト", 4, "日本語…"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Truncate(tt.input, tt.max)
			if got != tt.want {
				t.Errorf("Truncate(%q, %d) = %q, want %q", tt.input, tt.max, got, tt.want)
			}
		})
	}
}

func TestCISummary(t *testing.T) {
	tests := []struct {
		name                       string
		pass, fail, pending, total int
		want                       string
	}{
		{"no checks", 0, 0, 0, 0, ""},
		{"all pass", 36, 0, 0, 36, "✓ 36"},
		{"some fail", 33, 2, 1, 36, "✗ 2/36"},
		{"some pending", 33, 0, 3, 36, "◐ 3/36"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CISummary(tt.pass, tt.fail, tt.pending, tt.total); got != tt.want {
				t.Errorf("CISummary() = %q, want %q", got, tt.want)
			}
		})
	}
}
