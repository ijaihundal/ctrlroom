package git

import (
	"fmt"
	"strings"
)

// Diff returns a unified diff between baseRef and headRef (three-dot syntax:
// changes on headRef since the merge base with baseRef).
func (c *Client) Diff(repoPath, baseRef, headRef string) (string, error) {
	out, err := c.run(repoPath, "diff", baseRef+"..."+headRef)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// FileStat summarizes per-file line additions and deletions.
type FileStat struct {
	Path    string
	Added   int
	Deleted int
}

// DiffStat returns a per-file summary of changes between baseRef and headRef.
func (c *Client) DiffStat(repoPath, baseRef, headRef string) ([]FileStat, error) {
	out, err := c.run(repoPath, "diff", "--numstat", baseRef+"..."+headRef)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	stats := make([]FileStat, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 3 {
			continue
		}
		s := FileStat{Path: parts[2]}
		if parts[0] != "-" {
			_, _ = fmt.Sscanf(parts[0], "%d", &s.Added)
		}
		if parts[1] != "-" {
			_, _ = fmt.Sscanf(parts[1], "%d", &s.Deleted)
		}
		stats = append(stats, s)
	}
	return stats, nil
}
