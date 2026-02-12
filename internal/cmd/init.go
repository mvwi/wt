package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/mvwi/wt/internal/ui"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:     "init",
	GroupID: groupWorkflow,
	Short:   "Initialize worktree (copy files, run commands)",
	Long: `Initialize a worktree for development.

Runs the steps defined in .wt.toml under [init]:

  copy_files — files copied from the main worktree (if missing)
  commands   — shell commands run sequentially

Example config:

  [init]
  copy_files = [".env", ".env.local"]
  commands = ["pnpm install", "npx prisma generate"]

If no [init] section is configured, prints a hint and exits.`,
	RunE: runInit,
}

func init() {
	rootCmd.AddCommand(initCmd)
}

func runInit(cmd *cobra.Command, args []string) error {
	ctx, err := newContext()
	if err != nil {
		return err
	}

	cwd, _ := os.Getwd()
	return runInitIn(cwd, ctx)
}

func runInitIn(dir string, ctx *cmdContext) error {
	// Check if we're in the main repo
	if dir == ctx.MainWorktree || isSubpath(dir, ctx.MainWorktree) {
		return fmt.Errorf("you're in the main repo\n   Switch to a worktree first: wt switch <name>")
	}

	hasCopyFiles := len(ctx.Config.Init.CopyFiles) > 0
	hasCommands := len(ctx.Config.Init.Commands) > 0

	if !hasCopyFiles && !hasCommands {
		fmt.Println("Nothing to initialize — no [init] section in .wt.toml")
		fmt.Println()
		fmt.Println("Add an [init] section to configure worktree setup:")
		fmt.Println()
		fmt.Println("  [init]")
		fmt.Println("  copy_files = [\".env\"]")
		fmt.Println("  commands = [\"pnpm install\"]")
		return nil
	}

	fmt.Println("Initializing worktree...")
	fmt.Println()

	// Step 1: Copy files from main worktree
	for _, file := range ctx.Config.Init.CopyFiles {
		src := filepath.Join(ctx.MainWorktree, file)
		dst := filepath.Join(dir, file)
		if fileExists(src) && !fileExists(dst) {
			fmt.Printf("Copying %s from main repo...\n", file)
			data, err := os.ReadFile(src)
			if err == nil {
				if err := os.WriteFile(dst, data, 0644); err != nil {
					ui.Warn("Failed to write %s: %v", file, err)
				}
			}
		}
	}

	// Step 2: Run commands
	for _, command := range ctx.Config.Init.Commands {
		fmt.Printf("Running: %s\n", command)
		if err := runShellString(dir, command); err != nil {
			ui.Warn("Command failed: %v", err)
		}
	}

	fmt.Println()
	ui.Success("Worktree initialized!")
	return nil
}

func runShellString(dir, command string) error {
	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
