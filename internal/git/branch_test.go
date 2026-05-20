package git

import (
	"testing"
	"time"
)

func TestFormatRelativeAge(t *testing.T) {
	tests := []struct {
		name string
		d    time.Duration
		want string
	}{
		{"zero", 0, "0s"},
		{"negative clamps to zero", -5 * time.Second, "0s"},
		{"seconds", 42 * time.Second, "42s"},
		{"minutes", 5 * time.Minute, "5m"},
		{"hours", 3 * time.Hour, "3h"},
		{"days", 5 * 24 * time.Hour, "5d"},
		{"two weeks switches to weeks", 14 * 24 * time.Hour, "2w"},
		{"months", 90 * 24 * time.Hour, "3mo"},
		{"years", 2 * 365 * 24 * time.Hour, "2y"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatRelativeAge(tt.d)
			if got != tt.want {
				t.Errorf("formatRelativeAge(%v) = %q, want %q", tt.d, got, tt.want)
			}
		})
	}
}

func TestParseReflogTime(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		wantTs  int64
		wantOk  bool
	}{
		{
			name:   "creation entry without reason",
			line:   "0000000000000000000000000000000000000000 29abadac3894da3ff8b3fa1c95631fc3832a2896 Michael Williams <m@example.com> 1778679002 -0300",
			wantTs: 1778679002,
			wantOk: true,
		},
		{
			name:   "entry with tab-separated reason",
			line:   "abc def Name <n@example.com> 1700000000 +0000\tcommit: hello",
			wantTs: 1700000000,
			wantOk: true,
		},
		{
			name:   "malformed line",
			line:   "garbage",
			wantOk: false,
		},
		{
			name:   "non-numeric timestamp",
			line:   "abc def Name <n@example.com> notanumber +0000",
			wantOk: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseReflogTime(tt.line)
			if ok != tt.wantOk {
				t.Fatalf("parseReflogTime(%q) ok = %v, want %v", tt.line, ok, tt.wantOk)
			}
			if ok && got.Unix() != tt.wantTs {
				t.Errorf("parseReflogTime(%q) ts = %d, want %d", tt.line, got.Unix(), tt.wantTs)
			}
		})
	}
}
