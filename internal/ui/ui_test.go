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
