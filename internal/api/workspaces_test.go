package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/ijaihundal/ctrlroom/internal/db"
	"github.com/ijaihundal/ctrlroom/internal/types"
)

func setupWorkspaceTest(t *testing.T) (*testServer, *http.Client, string) {
	t.Helper()
	ts := setup(t)
	cookie := loginAdmin(t, ts)
	client := authedClient(t, ts, cookie)
	pid := createProjectViaAPI(t, ts, client)
	return ts, client, pid
}

func createWorkspaceViaAPI(t *testing.T, ts *testServer, client *http.Client, pid string) workspaceResponse {
	t.Helper()
	resp := doJSON(t, client, http.MethodPost, ts.server.URL+"/api/workspaces",
		map[string]any{"project_id": pid, "agent_type": "claude"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create workspace status = %d", resp.StatusCode)
	}
	var got workspaceResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return got
}

func TestCreateWorkspace_PrepareSuccess(t *testing.T) {
	ts, client, pid := setupWorkspaceTest(t)

	ws := createWorkspaceViaAPI(t, ts, client, pid)

	if ws.Status != string(types.WorkspaceIdle) {
		t.Errorf("Status = %q, want idle", ws.Status)
	}
	if ws.WorktreePath == "" {
		t.Error("WorktreePath is empty")
	}
	if ws.Branch == "" {
		t.Error("Branch is empty")
	}
	if ws.TargetRef != "main" {
		t.Errorf("TargetRef = %q, want main", ws.TargetRef)
	}
}

func TestCreateWorkspace_InvalidAgentType(t *testing.T) {
	ts, client, pid := setupWorkspaceTest(t)

	resp := doJSON(t, client, http.MethodPost, ts.server.URL+"/api/workspaces",
		map[string]any{"project_id": pid, "agent_type": "bogus"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
	env := decodeEnvelope(t, resp.Body)
	if env.Error.Code != "invalid_agent_type" {
		t.Errorf("code = %q, want %q", env.Error.Code, "invalid_agent_type")
	}
}

func TestCreateWorkspace_MissingProjectID(t *testing.T) {
	ts, client, _ := setupWorkspaceTest(t)

	resp := doJSON(t, client, http.MethodPost, ts.server.URL+"/api/workspaces",
		map[string]any{"agent_type": "claude"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestCreateWorkspace_ProjectNotFound(t *testing.T) {
	ts, client, _ := setupWorkspaceTest(t)

	resp := doJSON(t, client, http.MethodPost, ts.server.URL+"/api/workspaces",
		map[string]any{"project_id": "nope", "agent_type": "claude"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

func TestGetWorkspace_Success(t *testing.T) {
	ts, client, pid := setupWorkspaceTest(t)
	ws := createWorkspaceViaAPI(t, ts, client, pid)

	resp := doJSON(t, client, http.MethodGet, ts.server.URL+"/api/workspaces/"+ws.ID, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var got workspaceResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ID != ws.ID {
		t.Errorf("ID = %q, want %q", got.ID, ws.ID)
	}
}

func TestGetWorkspace_NotFound(t *testing.T) {
	ts, client, _ := setupWorkspaceTest(t)

	resp := doJSON(t, client, http.MethodGet, ts.server.URL+"/api/workspaces/nope", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

func TestListWorkspacesByProject_ReturnsArray(t *testing.T) {
	ts, client, pid := setupWorkspaceTest(t)
	createWorkspaceViaAPI(t, ts, client, pid)

	resp := doJSON(t, client, http.MethodGet, ts.server.URL+"/api/projects/"+pid+"/workspaces", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var got workspaceListResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Workspaces) == 0 {
		t.Fatal("Workspaces is empty, want at least 1")
	}
}

func TestListWorkspacesByProject_EmptyReturnsArray(t *testing.T) {
	ts, client, pid := setupWorkspaceTest(t)

	resp := doJSON(t, client, http.MethodGet, ts.server.URL+"/api/projects/"+pid+"/workspaces", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatalf("decode raw: %v", err)
	}
	arr, ok := raw["workspaces"]
	if !ok {
		t.Fatal("missing 'workspaces' key")
	}
	if string(arr) != "[]" {
		t.Errorf("workspaces = %s, want `[]`", string(arr))
	}
}

func TestDiff_AfterCreateIsEmpty(t *testing.T) {
	ts, client, pid := setupWorkspaceTest(t)
	ws := createWorkspaceViaAPI(t, ts, client, pid)

	resp := doJSON(t, client, http.MethodGet, ts.server.URL+"/api/workspaces/"+ws.ID+"/diff", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var got diffResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Diff != "" {
		t.Errorf("Diff = %q, want empty", got.Diff)
	}
}

func TestStopWorkspace_Success(t *testing.T) {
	ts, client, pid := setupWorkspaceTest(t)
	ws := createWorkspaceViaAPI(t, ts, client, pid)
	wtPath := ws.WorktreePath

	resp := doJSON(t, client, http.MethodPost, ts.server.URL+"/api/workspaces/"+ws.ID+"/stop", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var got workspaceResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Status != string(types.WorkspaceCancelled) {
		t.Errorf("Status = %q, want cancelled", got.Status)
	}
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Errorf("worktree after stop: %v, want IsNotExist", err)
	}
}

func TestStopWorkspace_InvalidTransition(t *testing.T) {
	ts, client, pid := setupWorkspaceTest(t)
	ws := createWorkspaceViaAPI(t, ts, client, pid)

	// Cancel once.
	stop := doJSON(t, client, http.MethodPost, ts.server.URL+"/api/workspaces/"+ws.ID+"/stop", nil)
	if stop.StatusCode != http.StatusOK {
		t.Fatalf("first stop status = %d", stop.StatusCode)
	}
	stop.Body.Close()

	// Cancel again — already cancelled (terminal state) → 409.
	resp := doJSON(t, client, http.MethodPost, ts.server.URL+"/api/workspaces/"+ws.ID+"/stop", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusConflict)
	}
	env := decodeEnvelope(t, resp.Body)
	if env.Error.Code != "invalid_transition" {
		t.Errorf("code = %q, want %q", env.Error.Code, "invalid_transition")
	}
}

func TestStopWorkspace_NotFound(t *testing.T) {
	ts, client, _ := setupWorkspaceTest(t)

	resp := doJSON(t, client, http.MethodPost, ts.server.URL+"/api/workspaces/nope/stop", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

// markWorkspaceCompleted flips the workspace to completed via direct db access,
// since the REST API doesn't expose status transitions in Phase 2.
func markWorkspaceCompleted(t *testing.T, ts *testServer, wsID string) {
	t.Helper()
	completed := types.WorkspaceCompleted
	if _, err := db.UpdateWorkspace(context.Background(), ts.database, wsID,
		db.WorkspaceUpdatePatch{Status: &completed}); err != nil {
		t.Fatalf("set completed: %v", err)
	}
}

func TestMerge_InvalidStatus(t *testing.T) {
	ts, client, pid := setupWorkspaceTest(t)
	// Freshly created workspace is idle, not completed → 409 invalid_transition.
	ws := createWorkspaceViaAPI(t, ts, client, pid)

	resp := doJSON(t, client, http.MethodPost, ts.server.URL+"/api/workspaces/"+ws.ID+"/merge", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusConflict)
	}
	env := decodeEnvelope(t, resp.Body)
	if env.Error.Code != "invalid_transition" {
		t.Errorf("code = %q, want %q", env.Error.Code, "invalid_transition")
	}
}

func TestMerge_Clean(t *testing.T) {
	ts, client, pid := setupWorkspaceTest(t)
	ws := createWorkspaceViaAPI(t, ts, client, pid)

	// Make a commit on the workspace's branch.
	featureFile := filepath.Join(ws.WorktreePath, "feature.txt")
	if err := os.WriteFile(featureFile, []byte("feature\n"), 0o600); err != nil {
		t.Fatalf("write feature: %v", err)
	}
	runGitIn(t, ws.WorktreePath, "add", "feature.txt")
	runGitIn(t, ws.WorktreePath, "commit", "-m", "feature")

	markWorkspaceCompleted(t, ts, ws.ID)

	resp := doJSON(t, client, http.MethodPost, ts.server.URL+"/api/workspaces/"+ws.ID+"/merge", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var got mergeResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.Merged {
		t.Errorf("Merged = false, want true; conflicts=%v", got.Conflicts)
	}
	if got.Workspace.Status != string(types.WorkspaceMerged) {
		t.Errorf("Workspace.Status = %q, want merged", got.Workspace.Status)
	}
}

func TestMerge_Conflict(t *testing.T) {
	ts, client, pid := setupWorkspaceTest(t)
	ws := createWorkspaceViaAPI(t, ts, client, pid)

	// Conflicting change on branch.
	readme := filepath.Join(ws.WorktreePath, "README.md")
	if err := os.WriteFile(readme, []byte("# branch change\n"), 0o600); err != nil {
		t.Fatalf("write branch: %v", err)
	}
	runGitIn(t, ws.WorktreePath, "add", "README.md")
	runGitIn(t, ws.WorktreePath, "commit", "-m", "branch modifies README")

	// Look up the repo path via the project to make a conflicting commit on main.
	proj, err := db.GetProject(context.Background(), ts.database, pid)
	if err != nil {
		t.Fatalf("get project: %v", err)
	}
	mainReadme := filepath.Join(proj.RepoPath, "README.md")
	if err := os.WriteFile(mainReadme, []byte("# main change\n"), 0o600); err != nil {
		t.Fatalf("write main: %v", err)
	}
	runGitIn(t, proj.RepoPath, "add", "README.md")
	runGitIn(t, proj.RepoPath, "commit", "-m", "main modifies README")

	markWorkspaceCompleted(t, ts, ws.ID)

	resp := doJSON(t, client, http.MethodPost, ts.server.URL+"/api/workspaces/"+ws.ID+"/merge", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusConflict)
	}
	var got mergeResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Merged {
		t.Error("Merged = true, want false")
	}
	found := false
	for _, p := range got.Conflicts {
		if p == "README.md" {
			found = true
		}
	}
	if !found {
		t.Errorf("Conflicts = %v, want to contain README.md", got.Conflicts)
	}
}

func TestMessage_Phase2NotImplemented(t *testing.T) {
	ts, client, pid := setupWorkspaceTest(t)
	ws := createWorkspaceViaAPI(t, ts, client, pid)

	resp := doJSON(t, client, http.MethodPost, ts.server.URL+"/api/workspaces/"+ws.ID+"/message",
		map[string]any{"content": "hi"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNotImplemented)
	}
	env := decodeEnvelope(t, resp.Body)
	if env.Error.Code != "not_implemented" {
		t.Errorf("code = %q, want %q", env.Error.Code, "not_implemented")
	}
}
