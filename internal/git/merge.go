package git

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

type MergeResult struct {
	Clean         bool
	MergedTreeSHA string
	Conflicts     []string
	Base          string
	Target        string
	Branch        string
}

// MergeTree runs `git merge-tree --write-tree --name-only <target> <branch>`.
//
// Output format (git >= 2.38):
//   - Clean merge: a single line containing the merged tree SHA. Exit 0.
//   - Conflicted merge: the merged tree SHA on the first line, followed by
//     conflicted file paths (one per line). Exit 1.
//
// ErrMergeConflict is NOT returned here; conflict is reported in MergeResult.
func (c *Client) MergeTree(repoPath, target, branch string) (MergeResult, error) {
	targetSHA, err := c.RevParse(repoPath, target)
	if err != nil {
		return MergeResult{}, fmt.Errorf("resolve target: %w", err)
	}
	branchSHA, err := c.RevParse(repoPath, branch)
	if err != nil {
		return MergeResult{}, fmt.Errorf("resolve branch: %w", err)
	}
	base, err := c.run(repoPath, "merge-base", target, branch)
	if err != nil {
		return MergeResult{}, fmt.Errorf("merge-base: %w", err)
	}

	out, conflicted, runErr := c.runMergeTree(repoPath, target, branch)
	if runErr != nil {
		return MergeResult{}, fmt.Errorf("merge-tree: %w", runErr)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 0 || lines[0] == "" {
		return MergeResult{}, errors.New("empty merge-tree output")
	}

	result := MergeResult{
		Base:   strings.TrimSpace(string(base)),
		Target: targetSHA,
		Branch: branchSHA,
	}

	if !conflicted {
		result.Clean = true
		result.MergedTreeSHA = lines[0]
		return result, nil
	}

	result.Clean = false
	result.MergedTreeSHA = lines[0]
	if len(lines) > 1 {
		result.Conflicts = make([]string, 0, len(lines)-1)
		for _, l := range lines[1:] {
			if l != "" {
				result.Conflicts = append(result.Conflicts, l)
			}
		}
	}
	return result, nil
}

// runMergeTree invokes git merge-tree. The returned bool is true when git
// exited with status 1 (conflict). Other non-zero exits are real errors.
//
//nolint:gosec // G204 intentional: CLI wrapper by design.
func (c *Client) runMergeTree(repoPath, target, branch string) (stdout []byte, conflict bool, err error) {
	args := []string{"-C", repoPath, "merge-tree", "--write-tree", "--name-only", target, branch}
	cmd := exec.Command(c.bin, args...)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	if runErr := cmd.Run(); runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) && exitErr.ExitCode() == 1 && outBuf.Len() > 0 {
			return outBuf.Bytes(), true, nil
		}
		return nil, false, fmt.Errorf("git merge-tree: %w: %s", runErr, strings.TrimSpace(errBuf.String()))
	}
	return outBuf.Bytes(), false, nil
}

// UpdateBranch finalizes a clean merge by wrapping the merged tree in a commit
// and pointing targetBranch at it. The commit uses targetBranch's current tip
// as its (sole) parent.
//
// Spec deviation: the original design used `git update-ref refs/heads/<target>
// <tree-sha>` directly, but git rejects non-commit objects on refs/heads/*.
// We use commit-tree + update-ref instead.
func (c *Client) UpdateBranch(repoPath, targetBranch, treeSHA string) error {
	parent, err := c.RevParse(repoPath, "refs/heads/"+targetBranch)
	if err != nil {
		return fmt.Errorf("resolve parent for update: %w", err)
	}
	out, err := c.run(repoPath, "commit-tree", treeSHA, "-p", parent, "-m", "ctrlroom merge")
	if err != nil {
		return fmt.Errorf("commit-tree: %w", err)
	}
	commitSHA := strings.TrimSpace(string(out))
	if _, err := c.run(repoPath, "update-ref", "refs/heads/"+targetBranch, commitSHA); err != nil {
		return fmt.Errorf("update-ref %s: %w", targetBranch, err)
	}
	return nil
}
