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

When [init] is configured in .wt.toml, runs exactly those steps:

  copy_files — files copied from the main worktree (if missing)
  commands   — shell commands run sequentially

When no [init] section exists, auto-detects common patterns:

  • Copies .env files found in the main worktree
  • Detects package manager from lockfile and runs install

Example config (overrides auto-detection):

  [init]
  copy_files = [".env", ".env.local"]
  commands = ["pnpm install", "npx prisma generate"]`,
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

	copyFiles := ctx.Config.Init.CopyFiles
	commands := ctx.Config.Init.Commands
	configured := len(copyFiles) > 0 || len(commands) > 0

	// Auto-detect when no [init] section is configured
	if !configured {
		copyFiles, commands = detectInit(ctx.MainWorktree)
		if len(copyFiles) == 0 && len(commands) == 0 {
			fmt.Println("Nothing to initialize — no [init] config and nothing detected.")
			fmt.Println()
			fmt.Println("Add an [init] section to .wt.toml to configure setup:")
			fmt.Println()
			fmt.Println("  [init]")
			fmt.Println("  copy_files = [\".env\"]")
			fmt.Println("  commands = [\"pnpm install\"]")
			return nil
		}
	}

	fmt.Println("Initializing worktree...")
	fmt.Println()

	// Step 1: Copy files from main worktree
	for _, file := range copyFiles {
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
	for _, command := range commands {
		fmt.Printf("Running: %s\n", command)
		if err := runShellString(dir, command); err != nil {
			ui.Warn("Command failed: %v", err)
		}
	}

	fmt.Println()
	ui.Success("Worktree initialized!")

	if !configured {
		fmt.Println()
		fmt.Println(ui.Dim("Tip: add [init] to .wt.toml to customize this behavior"))
	}

	return nil
}

// detectInit auto-detects initialization steps from the main worktree.
// Returns env files to copy and install commands to run.
func detectInit(mainWorktree string) (copyFiles []string, commands []string) {
	// Detect .env files
	envFiles := []string{".env", ".env.local", ".env.development", ".env.test"}
	for _, f := range envFiles {
		if fileExists(filepath.Join(mainWorktree, f)) {
			copyFiles = append(copyFiles, f)
		}
	}

	// Detect package manager from lockfile
	lockfiles := []struct {
		file    string
		command string
	}{
		{"pnpm-lock.yaml", "pnpm install"},
		{"yarn.lock", "yarn install"},
		{"bun.lockb", "bun install"},
		{"package-lock.json", "npm install"},
		{"go.sum", "go mod download"},
		{"Gemfile.lock", "bundle install"},
		{"Cargo.lock", "cargo fetch"},
		{"requirements.txt", "pip install -r requirements.txt"},
		{"pyproject.toml", "pip install -e ."},
	}

	for _, lf := range lockfiles {
		if fileExists(filepath.Join(mainWorktree, lf.file)) {
			commands = append(commands, lf.command)
			break
		}
	}

	return
}

func runShellString(dir, command string) error {
	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
