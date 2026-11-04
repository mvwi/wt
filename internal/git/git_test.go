package git

import "testing"

func TestFilterGitNoise(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"empty", "", ""},
		{
			"drops hints and the sequencer echo",
			"CONFLICT (content): Merge conflict in f\n" +
				"error: could not apply b7f6417... feat: b\n" +
				"hint: To abort and get back to the state before \"git rebase\", run \"git rebase --\n" +
				"hint: abort\".\n" +
				"Could not apply b7f6417... # feat: b\n",
			"CONFLICT (content): Merge conflict in f\nerror: could not apply b7f6417... feat: b\n",
		},
		{"drops progress before carriage return", "Rebasing (1/1)\rerror: could not apply x... y\n", "error: could not apply x... y\n"},
		{"keeps real errors", "error: cannot rebase: You have unstaged changes.\n", "error: cannot rebase: You have unstaged changes.\n"},
		{"keeps last line without newline", "fatal: bad", "fatal: bad"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := filterGitNoise(tt.in); got != tt.want {
				t.Errorf("filterGitNoise() = %q, want %q", got, tt.want)
			}
		})
	}
}
