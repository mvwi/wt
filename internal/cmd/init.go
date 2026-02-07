package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/mvwi/wt/internal/git"
	"github.com/mvwi/wt/internal/ui"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize worktree (install deps, copy .env, etc.)",
	Long: `Initialize a worktree for development.

Steps:
  1. Copy .env files from main repo (configurable in .wt.toml)
  2. Install dependencies (auto-detects pnpm/yarn/npm/bun)
  3. Generate Prisma client (if prisma/schema.prisma exists)
  4. Run custom init commands (from .wt.toml or .wt-init script)`,
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
	cwd := dir
	if cwd == ctx.MainWorktree || isSubpath(cwd, ctx.MainWorktree) {
		return fmt.Errorf("you're in the main repo\n   Switch to a worktree first: wt switch <name>")
	}

	fmt.Println("Initializing worktree...")
	fmt.Println()

	didSomething := false

	// Step 1: Copy .env files
	for _, envFile := range ctx.Config.EffectiveEnvFiles() {
		src := filepath.Join(ctx.MainWorktree, envFile)
		dst := filepath.Join(cwd, envFile)
		if git.FileExists(src) && !git.FileExists(dst) {
			fmt.Printf("Copying %s from main repo...\n", envFile)
			data, err := os.ReadFile(src)
			if err == nil {
				_ = os.WriteFile(dst, data, 0644)
				didSomething = true
			}
		}
	}

	// Step 2: Install dependencies
	pm := detectPackageManager(cwd, ctx.Config.Init.PackageManager)
	if pm != "" {
		fmt.Printf("Installing dependencies (%s)...\n", pm)
		if err := runShellCmd(cwd, pm, "install"); err != nil {
			ui.Warn("Install failed: %v", err)
		} else {
			didSomething = true
		}
	}

	// Step 3: Prisma generate
	if git.FileExists(filepath.Join(cwd, "prisma/schema.prisma")) {
		fmt.Println("Generating Prisma client...")
		if err := runShellCmd(cwd, "npx", "prisma", "generate"); err != nil {
			ui.Warn("Prisma generate failed: %v", err)
		} else {
			didSomething = true
		}
	}

	// Step 4: Custom commands from config
	for _, cmd := range ctx.Config.Init.PostCommands {
		fmt.Printf("Running: %s\n", cmd)
		if err := runShellString(cwd, cmd); err != nil {
			ui.Warn("Command failed: %v", err)
		} else {
			didSomething = true
		}
	}

	// Step 4b: Legacy .wt-init script support
	if len(ctx.Config.Init.PostCommands) == 0 {
		for _, script := range []string{".wt-init.fish", ".wt-init"} {
			scriptPath := filepath.Join(cwd, script)
			if git.FileExists(scriptPath) {
				fmt.Printf("Running custom init script (%s)...\n", script)
				if err := runShellString(cwd, "fish "+scriptPath); err != nil {
					// Try sh if fish fails
					_ = runShellString(cwd, "sh "+scriptPath)
				}
				didSomething = true
				break
			}
		}
	}

	if !didSomething {
		fmt.Println("Nothing to initialize (no package.json, prisma, or .env files found)")
	} else {
		fmt.Println()
		ui.Success("Worktree initialized!")
	}
	return nil
}

func detectPackageManager(dir, override string) string {
	if override != "" {
		return override
	}
	if !git.FileExists(filepath.Join(dir, "package.json")) {
		return ""
	}
	switch {
	case git.FileExists(filepath.Join(dir, "bun.lockb")):
		return "bun"
	case git.FileExists(filepath.Join(dir, "pnpm-lock.yaml")):
		return "pnpm"
	case git.FileExists(filepath.Join(dir, "yarn.lock")):
		return "yarn"
	case git.FileExists(filepath.Join(dir, "package-lock.json")):
		return "npm"
	default:
		return "pnpm"
	}
}

func runShellCmd(dir string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func runShellString(dir, command string) error {
	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func isSubpath(child, parent string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel != ".." && !filepath.IsAbs(rel) && rel[0] != '.'
}
