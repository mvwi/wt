package update

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mvwi/wt/internal/ui"
)

const (
	releasesURL = "https://api.github.com/repos/mvwi/wt/releases/latest"
	checkInterval = 24 * time.Hour
	httpTimeout   = 3 * time.Second
)

// cacheDir returns ~/.config/wt/, creating it if needed.
func cacheDir() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(configDir, "wt")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	return dir, nil
}

func cacheFile() (string, error) {
	dir, err := cacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "latest-version"), nil
}

// CheckInBackground spawns a goroutine to fetch the latest version from
// GitHub and write it to the cache file. Non-blocking — if the command
// exits before the fetch completes, the cache is simply not updated.
func CheckInBackground() {
	path, err := cacheFile()
	if err != nil {
		return
	}

	// Skip if cache is fresh
	info, err := os.Stat(path)
	if err == nil && time.Since(info.ModTime()) < checkInterval {
		return
	}

	// Skip in CI environments
	if os.Getenv("CI") != "" {
		return
	}

	go func() {
		client := &http.Client{Timeout: httpTimeout}
		resp, err := client.Get(releasesURL)
		if err != nil || resp.StatusCode != 200 {
			return
		}
		defer resp.Body.Close()

		var release struct {
			TagName string `json:"tag_name"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
			return
		}
		if release.TagName == "" {
			return
		}

		_ = os.WriteFile(path, []byte(release.TagName), 0644)
	}()
}

// PrintNoticeIfNewer reads the cached latest version and prints an update
// notice to stderr if it's newer than the current version.
func PrintNoticeIfNewer(currentVersion string) {
	path, err := cacheFile()
	if err != nil {
		return
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return
	}

	latest := strings.TrimSpace(string(data))
	if latest == "" {
		return
	}

	// Normalize: strip "v" prefix for comparison
	cur := strings.TrimPrefix(currentVersion, "v")
	lat := strings.TrimPrefix(latest, "v")

	if cur == "dev" || lat == cur {
		return
	}

	if !isNewer(lat, cur) {
		return
	}

	fmt.Fprintf(os.Stderr, "\n%s %s → %s\n",
		ui.Cyan(ui.PushUp+" Update available:"),
		ui.Dim(currentVersion),
		ui.Cyan(latest),
	)
	fmt.Fprintf(os.Stderr, "  %s\n", ui.Dim("brew upgrade wt"))
}

// isNewer returns true if version a is newer than b (simple semver comparison).
func isNewer(a, b string) bool {
	aParts := parseSemver(a)
	bParts := parseSemver(b)
	for i := 0; i < 3; i++ {
		if aParts[i] > bParts[i] {
			return true
		}
		if aParts[i] < bParts[i] {
			return false
		}
	}
	return false
}

// parseSemver extracts major.minor.patch as ints. Returns [0,0,0] on failure.
func parseSemver(v string) [3]int {
	var parts [3]int
	v = strings.TrimPrefix(v, "v")
	segments := strings.SplitN(v, ".", 3)
	for i, s := range segments {
		if i >= 3 {
			break
		}
		// Strip pre-release suffix (e.g., "1-rc1" → "1")
		if idx := strings.IndexAny(s, "-+"); idx >= 0 {
			s = s[:idx]
		}
		fmt.Sscanf(s, "%d", &parts[i])
	}
	return parts
}
