package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectInit(t *testing.T) {
	t.Run("detects pnpm and env files", func(t *testing.T) {
		dir := t.TempDir()
		touch(t, dir, "pnpm-lock.yaml")
		touch(t, dir, ".env")
		touch(t, dir, ".env.development")

		copyFiles, commands := detectInit(dir)

		if len(copyFiles) != 2 || copyFiles[0] != ".env" || copyFiles[1] != ".env.development" {
			t.Errorf("copyFiles = %v, want [.env .env.development]", copyFiles)
		}
		if len(commands) != 1 || commands[0] != "pnpm install --frozen-lockfile" {
			t.Errorf("commands = %v, want [pnpm install --frozen-lockfile]", commands)
		}
	})

	t.Run("detects yarn", func(t *testing.T) {
		dir := t.TempDir()
		touch(t, dir, "yarn.lock")

		_, commands := detectInit(dir)

		if len(commands) != 1 || commands[0] != "yarn install --frozen-lockfile" {
			t.Errorf("commands = %v, want [yarn install --frozen-lockfile]", commands)
		}
	})

	t.Run("detects go module", func(t *testing.T) {
		dir := t.TempDir()
		touch(t, dir, "go.sum")

		_, commands := detectInit(dir)

		if len(commands) != 1 || commands[0] != "go mod download" {
			t.Errorf("commands = %v, want [go mod download]", commands)
		}
	})

	t.Run("first lockfile wins", func(t *testing.T) {
		dir := t.TempDir()
		touch(t, dir, "pnpm-lock.yaml")
		touch(t, dir, "package-lock.json")

		_, commands := detectInit(dir)

		if len(commands) != 1 || commands[0] != "pnpm install --frozen-lockfile" {
			t.Errorf("commands = %v, want [pnpm install --frozen-lockfile] (first lockfile should win)", commands)
		}
	})

	t.Run("nothing detected in empty dir", func(t *testing.T) {
		dir := t.TempDir()

		copyFiles, commands := detectInit(dir)

		if len(copyFiles) != 0 {
			t.Errorf("copyFiles = %v, want empty", copyFiles)
		}
		if len(commands) != 0 {
			t.Errorf("commands = %v, want empty", commands)
		}
	})

	t.Run("only env files no lockfile", func(t *testing.T) {
		dir := t.TempDir()
		touch(t, dir, ".env")
		touch(t, dir, ".env.test")

		copyFiles, commands := detectInit(dir)

		if len(copyFiles) != 2 {
			t.Errorf("copyFiles = %v, want [.env .env.test]", copyFiles)
		}
		if len(commands) != 0 {
			t.Errorf("commands = %v, want empty", commands)
		}
	})

	t.Run("detects .claude directory", func(t *testing.T) {
		dir := t.TempDir()
		mkdir(t, dir, ".claude")

		copyFiles, _ := detectInit(dir)

		if !contains(copyFiles, ".claude") {
			t.Errorf("copyFiles = %v, want to contain .claude", copyFiles)
		}
	})

	t.Run("detects .cursorrules file", func(t *testing.T) {
		dir := t.TempDir()
		touch(t, dir, ".cursorrules")

		copyFiles, _ := detectInit(dir)

		if !contains(copyFiles, ".cursorrules") {
			t.Errorf("copyFiles = %v, want to contain .cursorrules", copyFiles)
		}
	})

	t.Run("detects .cursor/rules directory", func(t *testing.T) {
		dir := t.TempDir()
		mkdir(t, dir, ".cursor")
		mkdir(t, dir, ".cursor/rules")

		copyFiles, _ := detectInit(dir)

		if !contains(copyFiles, ".cursor/rules") {
			t.Errorf("copyFiles = %v, want to contain .cursor/rules", copyFiles)
		}
	})

	t.Run("absent AI configs not detected", func(t *testing.T) {
		dir := t.TempDir()
		touch(t, dir, ".env")

		copyFiles, _ := detectInit(dir)

		for _, f := range copyFiles {
			if f == ".claude" || f == ".cursorrules" || f == ".cursor/rules" {
				t.Errorf("copyFiles = %v, should not contain AI configs when absent", copyFiles)
			}
		}
	})
}

func TestCopyDirRecursive(t *testing.T) {
	t.Run("copies nested files preserving structure", func(t *testing.T) {
		src := t.TempDir()
		dst := filepath.Join(t.TempDir(), "dest")

		// Create nested structure
		mkdir(t, src, "sub")
		mkdir(t, src, "sub/deep")
		os.WriteFile(filepath.Join(src, "root.txt"), []byte("root"), 0644)
		os.WriteFile(filepath.Join(src, "sub", "mid.txt"), []byte("mid"), 0644)
		os.WriteFile(filepath.Join(src, "sub", "deep", "leaf.txt"), []byte("leaf"), 0644)

		if err := copyDirRecursive(src, dst); err != nil {
			t.Fatalf("copyDirRecursive failed: %v", err)
		}

		// Verify all files exist with correct content
		for _, tc := range []struct {
			path    string
			content string
		}{
			{"root.txt", "root"},
			{"sub/mid.txt", "mid"},
			{"sub/deep/leaf.txt", "leaf"},
		} {
			data, err := os.ReadFile(filepath.Join(dst, tc.path))
			if err != nil {
				t.Errorf("missing %s: %v", tc.path, err)
				continue
			}
			if string(data) != tc.content {
				t.Errorf("%s = %q, want %q", tc.path, data, tc.content)
			}
		}
	})

	t.Run("preserves file permissions", func(t *testing.T) {
		src := t.TempDir()
		dst := filepath.Join(t.TempDir(), "dest")

		os.WriteFile(filepath.Join(src, "exec.sh"), []byte("#!/bin/sh"), 0755)

		if err := copyDirRecursive(src, dst); err != nil {
			t.Fatalf("copyDirRecursive failed: %v", err)
		}

		info, err := os.Stat(filepath.Join(dst, "exec.sh"))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0111 == 0 {
			t.Errorf("exec.sh lost execute permission: %v", info.Mode())
		}
	})
}

func touch(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), nil, 0644); err != nil {
		t.Fatal(err)
	}
}

func mkdir(t *testing.T, parts ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(parts...), 0755); err != nil {
		t.Fatal(err)
	}
}

func contains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
