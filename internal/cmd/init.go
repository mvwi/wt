package cmd

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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
  • Copies AI config (.claude, .cursorrules, .cursor/rules)
  • Detects package manager from lockfile and runs install
  • Detects Prisma schema and runs prisma generate

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

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("cannot determine current directory: %w", err)
	}
	return runInitIn(cwd, ctx)
}

// initStep records the outcome of a single initialization step.
type initStep struct {
	label  string // e.g. "copy .env" or "pnpm install"
	status string // "ok", "skip", "fail"
	detail string // reason for skip/fail, empty on success
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

	var steps []initStep

	// Step 1: Copy files/directories from main worktree
	for _, file := range copyFiles {
		src := filepath.Join(ctx.MainWorktree, file)
		dst := filepath.Join(dir, file)

		if !fileExists(src) {
			steps = append(steps, initStep{"copy " + file, "skip", "not found in main worktree"})
			continue
		}
		if fileExists(dst) {
			steps = append(steps, initStep{"copy " + file, "skip", "already exists"})
			continue
		}

		if isDir(src) {
			if err := copyDirRecursive(src, dst); err != nil {
				steps = append(steps, initStep{"copy " + file, "fail", err.Error()})
			} else {
				steps = append(steps, initStep{"copy " + file, "ok", ""})
			}
		} else {
			data, err := os.ReadFile(src)
			if err != nil {
				steps = append(steps, initStep{"copy " + file, "fail", err.Error()})
			} else if err := os.WriteFile(dst, data, 0644); err != nil {
				steps = append(steps, initStep{"copy " + file, "fail", err.Error()})
			} else {
				steps = append(steps, initStep{"copy " + file, "ok", ""})
			}
		}
	}

	// Step 2: Run commands
	for _, command := range commands {
		fmt.Printf("  %s %s\n", ui.Dim("$"), command)
		if err := runShellString(dir, command); err != nil {
			steps = append(steps, initStep{"run " + command, "fail", err.Error()})
		} else {
			steps = append(steps, initStep{"run " + command, "ok", ""})
		}
		fmt.Println()
	}

	// Print summary
	hasFailures := false
	for _, s := range steps {
		switch s.status {
		case "ok":
			fmt.Printf("  %s  %s\n", ui.Green(ui.Pass), s.label)
		case "skip":
			fmt.Printf("  %s  %s %s\n", ui.Dim(ui.Dash), s.label, ui.Dim("("+s.detail+")"))
		case "fail":
			fmt.Printf("  %s  %s %s\n", ui.Red(ui.Fail), s.label, ui.Dim("("+s.detail+")"))
			hasFailures = true
		}
	}

	fmt.Println()
	if hasFailures {
		ui.Warn("Worktree initialized with errors")
	} else {
		ui.Success("Worktree initialized")
	}

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

	// Detect AI config files/directories
	aiConfigs := []string{".claude", ".cursorrules", ".cursor/rules"}
	for _, f := range aiConfigs {
		if fileExists(filepath.Join(mainWorktree, f)) {
			copyFiles = append(copyFiles, f)
		}
	}

	// Detect package manager from lockfile
	var execRunner string
	for _, lf := range knownLockfiles {
		if fileExists(filepath.Join(mainWorktree, lf.file)) {
			commands = append(commands, lf.command)
			execRunner = lf.exec
			break
		}
	}

	// Detect Prisma schema anywhere in the repo (requires a JS package manager)
	if execRunner != "" {
		if schemaPath := findPrismaSchema(mainWorktree); schemaPath != "" {
			commands = append(commands, execRunner+" prisma generate --schema "+shellQuoteInit(schemaPath))
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

// copyDirRecursive copies a directory tree from src to dst, preserving permissions.
func copyDirRecursive(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		if d.IsDir() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			return os.MkdirAll(target, info.Mode())
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode())
	})
}

// shellQuoteInit wraps a string in single quotes for safe shell interpolation.
func shellQuoteInit(s string) string {
	if !strings.ContainsAny(s, " \t\n'\"\\$`|&;<>(){}[]!*?~") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// findPrismaSchema walks the tree looking for a .prisma file, skipping
// node_modules and other heavy dirs. Returns the repo-relative path to
// the first schema found, or "" if none exists.
func findPrismaSchema(root string) string {
	skipDirs := map[string]bool{
		"node_modules": true,
		".git":         true,
		"dist":         true,
		".next":        true,
		".turbo":       true,
	}
	var result string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable paths
		}
		if result != "" {
			return fs.SkipAll
		}
		if d.IsDir() && skipDirs[d.Name()] {
			return fs.SkipDir
		}
		if !d.IsDir() && filepath.Ext(d.Name()) == ".prisma" {
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return nil
			}
			result = rel
			return fs.SkipAll
		}
		return nil
	})
	return result
}
