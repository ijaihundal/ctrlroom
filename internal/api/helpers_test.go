package api

import (
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// authedClient returns an HTTP client whose cookie jar holds the given cookie
// for the test server's domain, so all subsequent requests are authenticated.
func authedClient(t *testing.T, ts *testServer, cookie *http.Cookie) *http.Client {
	t.Helper()
	u, err := url.Parse(ts.server.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("new cookie jar: %v", err)
	}
	jar.SetCookies(u, []*http.Cookie{cookie})
	return &http.Client{Jar: jar}
}

// tempRepo creates a temporary git repository with one initial commit on the
// "main" branch. Local copy of testutil.TempRepo to avoid an import cycle
// (testutil imports api).
func tempRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGitIn(t, dir, "init", "-b", "main")
	runGitIn(t, dir, "config", "user.email", "test@ctrlroom.local")
	runGitIn(t, dir, "config", "user.name", "CtrlRoom Test")
	runGitIn(t, dir, "config", "commit.gpgsign", "false")
	readme := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readme, []byte("# test repo\n"), 0o600); err != nil {
		t.Fatalf("write readme: %v", err)
	}
	runGitIn(t, dir, "add", "README.md")
	runGitIn(t, dir, "commit", "-m", "initial")
	return dir
}

func runGitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
}
