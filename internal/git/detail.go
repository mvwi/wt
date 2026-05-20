package git

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// LogEntry is one commit summary used by the dashboard detail panel.
type LogEntry struct {
	Hash    string // short
	Subject string
	Age     string // already shortened, e.g. "2h"
}

// LogOnelineIn returns up to n commits reachable from HEAD but not from base,
// most recent first. Returns nil (not error) if the range is empty.
func LogOnelineIn(dir, base string, n int) ([]LogEntry, error) {
	if n <= 0 {
		return nil, nil
	}
	out, err := RunIn(dir, "log",
		fmt.Sprintf("-%d", n),
		"--format=%h\t%s\t%ct",
		base+"..HEAD",
	)
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	lines := strings.Split(out, "\n")
	entries := make([]LogEntry, 0, len(lines))
	for _, line := range lines {
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 3 {
			continue
		}
		age := ""
		if ts, err := strconv.ParseInt(parts[2], 10, 64); err == nil {
			age = formatRelativeAge(time.Since(time.Unix(ts, 0)))
		}
		entries = append(entries, LogEntry{
			Hash:    parts[0],
			Subject: parts[1],
			Age:     age,
		})
	}
	return entries, nil
}

// DiffStats summarises a diff between base and HEAD.
type DiffStats struct {
	Commits   int
	Files     int
	Additions int
	Deletions int
}

// DiffStatsIn returns commits-ahead and shortstat (files/additions/deletions)
// for HEAD vs base. Empty range yields zeroed struct, not an error.
func DiffStatsIn(dir, base string) (DiffStats, error) {
	stats := DiffStats{}

	commitsStr, err := RunIn(dir, "rev-list", "--count", base+"..HEAD")
	if err != nil {
		return stats, err
	}
	stats.Commits, _ = strconv.Atoi(strings.TrimSpace(commitsStr))

	if stats.Commits == 0 {
		return stats, nil
	}

	// `git diff --shortstat` output: " 14 files changed, 247 insertions(+), 82 deletions(-)"
	shortstat, err := RunIn(dir, "diff", "--shortstat", base+"..HEAD")
	if err != nil {
		return stats, err
	}
	stats.Files, stats.Additions, stats.Deletions = parseShortstat(shortstat)
	return stats, nil
}

func parseShortstat(s string) (files, adds, dels int) {
	for part := range strings.SplitSeq(s, ",") {
		part = strings.TrimSpace(part)
		fields := strings.Fields(part)
		if len(fields) < 2 {
			continue
		}
		n, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		switch {
		case strings.HasPrefix(fields[1], "file"):
			files = n
		case strings.HasPrefix(fields[1], "insertion"):
			adds = n
		case strings.HasPrefix(fields[1], "deletion"):
			dels = n
		}
	}
	return
}

// DiffFile is one entry from `git diff --name-status`.
type DiffFile struct {
	Status string // "M", "A", "D", "R", "C", "T", etc.
	Path   string
}

// CommitDetail is the full info shown for one commit in the dashboard's
// commit-detail panel.
type CommitDetail struct {
	Hash    string
	Author  string
	Email   string
	Date    string // ISO-8601 from %ai, already formatted
	Subject string
	Body    string
	Files   []CommitFile
}

// CommitFile is one file changed by a commit, with line counts.
// Status is derived from the numstat counts:
//
//	A (added)    when Dels == 0 && Adds > 0
//	D (deleted)  when Adds == 0 && Dels > 0
//	M (modified) when both are non-zero
//	B (binary)   when numstat reports "-" for either
type CommitFile struct {
	Status string
	Path   string
	Adds   int
	Dels   int
}

// CommitDetailIn fetches the metadata and per-file change list for a commit.
// Two git calls — both fast — combined into a single struct.
func CommitDetailIn(dir, hash string) (CommitDetail, error) {
	var d CommitDetail

	// Metadata: null-separated to survive any character that might appear in
	// subject/body (newlines, tabs, unicode, etc.).
	metaOut, err := RunIn(dir, "show", "-s",
		"--format=%H%x00%an%x00%ae%x00%ai%x00%s%x00%b", hash)
	if err != nil {
		return d, err
	}
	parts := strings.SplitN(metaOut, "\x00", 6)
	if len(parts) < 6 {
		return d, fmt.Errorf("unexpected git show output for %s", hash)
	}
	d.Hash = parts[0]
	d.Author = parts[1]
	d.Email = parts[2]
	d.Date = parts[3]
	d.Subject = parts[4]
	d.Body = strings.TrimSpace(parts[5])

	// Per-file changes via numstat.
	numOut, err := RunIn(dir, "show", "--numstat", "--format=", hash)
	if err != nil {
		// Metadata is still useful; return what we have.
		return d, nil
	}
	for line := range strings.SplitSeq(numOut, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		addsStr, delsStr := fields[0], fields[1]
		path := fields[len(fields)-1]

		f := CommitFile{Path: path}
		switch {
		case addsStr == "-" || delsStr == "-":
			f.Status = "B"
		default:
			f.Adds, _ = strconv.Atoi(addsStr)
			f.Dels, _ = strconv.Atoi(delsStr)
			switch {
			case f.Adds > 0 && f.Dels == 0:
				f.Status = "A"
			case f.Adds == 0 && f.Dels > 0:
				f.Status = "D"
			default:
				f.Status = "M"
			}
		}
		d.Files = append(d.Files, f)
	}

	return d, nil
}

// DiffFilesIn returns per-file change status between base and HEAD.
func DiffFilesIn(dir, base string) ([]DiffFile, error) {
	out, err := RunIn(dir, "diff", "--name-status", base+"..HEAD")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	lines := strings.Split(out, "\n")
	changes := make([]DiffFile, 0, len(lines))
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		// Rename/copy entries have an extra "score" suffix on the status (e.g. "R100")
		// and a second path; we only surface the first letter and the destination path.
		status := string(fields[0][0])
		path := fields[len(fields)-1]
		changes = append(changes, DiffFile{Status: status, Path: path})
	}
	return changes, nil
}
