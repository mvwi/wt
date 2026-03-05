package cmd

import "path/filepath"

// lockfileMapping maps a lockfile name to its install command.
type lockfileMapping struct {
	file    string
	command string
	exec    string // exec runner for post-install codegen (JS-only)
}

// knownLockfiles is the ordered list of lockfile→install-command mappings.
// Order matters: first match wins (pnpm preferred over npm, etc.).
var knownLockfiles = []lockfileMapping{
	{"pnpm-lock.yaml", "pnpm install --frozen-lockfile", "pnpm exec"},
	{"yarn.lock", "yarn install --frozen-lockfile", "yarn"},
	{"bun.lockb", "bun install --frozen-lockfile", "bunx"},
	{"package-lock.json", "npm ci", "npx"},
	{"go.sum", "go mod download", ""},
	{"Gemfile.lock", "bundle install", ""},
	{"Cargo.lock", "cargo fetch", ""},
	{"requirements.txt", "pip install -r requirements.txt", ""},
	{"pyproject.toml", "pip install -e .", ""},
}

// detectInstallCommand checks a directory for known lockfiles and returns
// the lockfile name and install command. Returns ("", "") if none found.
func detectInstallCommand(dir string) (lockfile, command string) {
	for _, lf := range knownLockfiles {
		if fileExists(filepath.Join(dir, lf.file)) {
			return lf.file, lf.command
		}
	}
	return "", ""
}
