package cmd

import "testing"

func TestEveryCommandHasHelpIcon(t *testing.T) {
	for _, c := range rootCmd.Commands() {
		if c.IsAvailableCommand() && commandIcons[c.Name()] == "" {
			t.Errorf("command %q has no entry in commandIcons", c.Name())
		}
	}
}
