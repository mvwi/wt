package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestShellWrappers loads each wrapper into its real shell with a stub wt
// that asks for a cd, then checks the cd happens, nothing leaks to stderr,
// and the caller's EXIT trap survives.
func TestShellWrappers(t *testing.T) {
	stubDir := t.TempDir()
	target := t.TempDir()
	stub := "#!/bin/sh\nprintf %s \"$TARGET\" > \"$WT_CD_FILE\"\n"
	if err := os.WriteFile(filepath.Join(stubDir, "wt"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		shell   string
		args    []string
		wrapper string
		script  string
		want    string
	}{
		{"bash", []string{"--norc", "--noprofile", "-c"}, bashWrapper,
			`eval "$(cat "$W")"; trap 'echo outer' EXIT; cd /; wt -v; pwd`, target + "\nouter\n"},
		{"zsh", []string{"-f", "-c"}, zshWrapper,
			`eval "$(cat "$W")"; trap 'echo outer' EXIT; cd /; wt -v; pwd`, target + "\nouter\n"},
		{"fish", []string{"--no-config", "-c"}, fishWrapper,
			`source $W; cd /; wt -v; pwd`, target + "\n"},
	}
	for _, tt := range tests {
		t.Run(tt.shell, func(t *testing.T) {
			if _, err := exec.LookPath(tt.shell); err != nil {
				t.Skipf("%s not installed", tt.shell)
			}
			w := filepath.Join(t.TempDir(), "wrapper")
			if err := os.WriteFile(w, []byte(tt.wrapper), 0o644); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(tt.shell, append(tt.args, tt.script)...)
			cmd.Env = []string{"PATH=" + stubDir + ":/usr/bin:/bin", "W=" + w, "TARGET=" + target, "HOME=" + t.TempDir()}
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			if err := cmd.Run(); err != nil {
				t.Fatalf("run: %v\nstderr: %s", err, stderr.String())
			}
			if stderr.Len() > 0 {
				t.Errorf("stderr = %q, want empty", stderr.String())
			}
			if stdout.String() != tt.want {
				t.Errorf("stdout = %q, want %q", stdout.String(), tt.want)
			}
		})
	}
}
