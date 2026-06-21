package git

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

type Client struct {
	bin string
}

// New resolves the git binary via exec.LookPath and returns a Client.
// Returns ErrGitNotInstalled if not found.
func New() (*Client, error) {
	path, err := exec.LookPath("git")
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrGitNotInstalled, err)
	}
	return &Client{bin: path}, nil
}

// NewWithPath returns a Client using the given binary path. Useful for tests
// or when CTRLROOM_GIT_BIN points somewhere unusual. Does not check existence.
func NewWithPath(path string) *Client {
	return &Client{bin: path}
}

// run executes git -C <repoPath> <args...> and returns stdout.
// On non-zero exit, returns an error wrapping stderr (last ~2KB).
//
//nolint:gosec // G204 intentional: CLI wrapper by design.
func (c *Client) run(repoPath string, args ...string) ([]byte, error) {
	fullArgs := make([]string, 0, len(args)+2)
	fullArgs = append(fullArgs, "-C", repoPath)
	fullArgs = append(fullArgs, args...)
	cmd := exec.Command(c.bin, fullArgs...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git %s: %w: %s",
			strings.Join(args, " "), err, strings.TrimSpace(stderr.String()),
		)
	}
	return stdout.Bytes(), nil
}

// runNoRepo executes git without -C (used for global commands like --version).
//
//nolint:gosec // G204 intentional: CLI wrapper by design.
func (c *Client) runNoRepo(args ...string) ([]byte, error) {
	cmd := exec.Command(c.bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git %s: %w: %s",
			strings.Join(args, " "), err, strings.TrimSpace(stderr.String()),
		)
	}
	return stdout.Bytes(), nil
}

// Version returns `git --version` output, useful for diagnostics / startup logs.
func (c *Client) Version() (string, error) {
	out, err := c.runNoRepo("--version")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
