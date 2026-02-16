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
}

func touch(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), nil, 0644); err != nil {
		t.Fatal(err)
	}
}
