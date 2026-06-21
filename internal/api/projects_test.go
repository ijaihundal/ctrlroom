package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func loginAdmin(t *testing.T, ts *testServer) *http.Cookie {
	t.Helper()
	resp := doJSON(t, newClient(), http.MethodPost, ts.server.URL+"/api/auth/login",
		map[string]string{"username": "admin", "password": ts.password})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status %d, want 200", resp.StatusCode)
	}
	return extractSessionCookie(t, resp)
}

func TestCreateProject_Success(t *testing.T) {
	ts := setup(t)
	cookie := loginAdmin(t, ts)
	client := authedClient(t, ts, cookie)
	repo := tempRepo(t)

	resp := doJSON(t, client, http.MethodPost, ts.server.URL+"/api/projects",
		map[string]any{
			"name":        "My Project",
			"repo_path":   repo,
			"description": "a test project",
		})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}
	var got projectResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ID == "" {
		t.Error("ID is empty")
	}
	if got.Name != "My Project" {
		t.Errorf("Name = %q, want %q", got.Name, "My Project")
	}
	if got.DefaultBranch != "main" {
		t.Errorf("DefaultBranch = %q, want %q", got.DefaultBranch, "main")
	}
	if got.ApprovalMode != "autonomous" {
		t.Errorf("ApprovalMode = %q, want %q", got.ApprovalMode, "autonomous")
	}
}

func TestCreateProject_MissingName(t *testing.T) {
	ts := setup(t)
	cookie := loginAdmin(t, ts)
	client := authedClient(t, ts, cookie)
	repo := tempRepo(t)

	resp := doJSON(t, client, http.MethodPost, ts.server.URL+"/api/projects",
		map[string]any{"repo_path": repo})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
	env := decodeEnvelope(t, resp.Body)
	if env.Error.Code != "missing_name" {
		t.Errorf("code = %q, want %q", env.Error.Code, "missing_name")
	}
}

func TestCreateProject_NonexistentRepoPath(t *testing.T) {
	ts := setup(t)
	cookie := loginAdmin(t, ts)
	client := authedClient(t, ts, cookie)
	missing := filepath.Join(t.TempDir(), "does-not-exist")

	resp := doJSON(t, client, http.MethodPost, ts.server.URL+"/api/projects",
		map[string]any{"name": "P", "repo_path": missing})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
	env := decodeEnvelope(t, resp.Body)
	if env.Error.Code != "invalid_repo_path" {
		t.Errorf("code = %q, want %q", env.Error.Code, "invalid_repo_path")
	}
}

func TestCreateProject_NonGitDirectory(t *testing.T) {
	ts := setup(t)
	cookie := loginAdmin(t, ts)
	client := authedClient(t, ts, cookie)
	dir := t.TempDir()

	resp := doJSON(t, client, http.MethodPost, ts.server.URL+"/api/projects",
		map[string]any{"name": "P", "repo_path": dir})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
	env := decodeEnvelope(t, resp.Body)
	if env.Error.Code != "invalid_repo_path" {
		t.Errorf("code = %q, want %q", env.Error.Code, "invalid_repo_path")
	}
}

func TestCreateProject_InvalidApprovalMode(t *testing.T) {
	ts := setup(t)
	cookie := loginAdmin(t, ts)
	client := authedClient(t, ts, cookie)
	repo := tempRepo(t)

	resp := doJSON(t, client, http.MethodPost, ts.server.URL+"/api/projects",
		map[string]any{"name": "P", "repo_path": repo, "approval_mode": "bogus"})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
	env := decodeEnvelope(t, resp.Body)
	if env.Error.Code != "invalid_approval_mode" {
		t.Errorf("code = %q, want %q", env.Error.Code, "invalid_approval_mode")
	}
}

func TestListProjects_ReturnsArray(t *testing.T) {
	ts := setup(t)
	cookie := loginAdmin(t, ts)
	client := authedClient(t, ts, cookie)
	repo := tempRepo(t)

	create := doJSON(t, client, http.MethodPost, ts.server.URL+"/api/projects",
		map[string]any{"name": "Seeded", "repo_path": repo})
	if create.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", create.StatusCode)
	}
	create.Body.Close()

	resp := doJSON(t, client, http.MethodGet, ts.server.URL+"/api/projects", nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var got projectListResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Projects) == 0 {
		t.Fatal("Projects is empty, want at least 1")
	}
	if got.Projects[0].ID == "" {
		t.Error("Projects[0].ID is empty")
	}
}

func TestListProjects_EmptyReturnsArrayNotNull(t *testing.T) {
	ts := setup(t)
	cookie := loginAdmin(t, ts)
	client := authedClient(t, ts, cookie)

	resp := doJSON(t, client, http.MethodGet, ts.server.URL+"/api/projects", nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatalf("decode raw: %v", err)
	}
	arr, ok := raw["projects"]
	if !ok {
		t.Fatal("missing 'projects' key")
	}
	if string(arr) != "[]" {
		t.Errorf("projects = %s, want `[]`", string(arr))
	}
}

func TestGetProject_Success(t *testing.T) {
	ts := setup(t)
	cookie := loginAdmin(t, ts)
	client := authedClient(t, ts, cookie)
	pid := createProjectViaAPI(t, ts, client)

	resp := doJSON(t, client, http.MethodGet, ts.server.URL+"/api/projects/"+pid, nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var got projectResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ID != pid {
		t.Errorf("ID = %q, want %q", got.ID, pid)
	}
}

func TestGetProject_NotFound(t *testing.T) {
	ts := setup(t)
	cookie := loginAdmin(t, ts)
	client := authedClient(t, ts, cookie)

	resp := doJSON(t, client, http.MethodGet, ts.server.URL+"/api/projects/nope", nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
	env := decodeEnvelope(t, resp.Body)
	if env.Error.Code != "not_found" {
		t.Errorf("code = %q, want %q", env.Error.Code, "not_found")
	}
}

func TestUpdateProject_Name(t *testing.T) {
	ts := setup(t)
	cookie := loginAdmin(t, ts)
	client := authedClient(t, ts, cookie)
	pid := createProjectViaAPI(t, ts, client)

	resp := doJSON(t, client, http.MethodPatch, ts.server.URL+"/api/projects/"+pid,
		map[string]any{"name": "Renamed"})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var got projectResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Name != "Renamed" {
		t.Errorf("Name = %q, want %q", got.Name, "Renamed")
	}
}

func TestUpdateProject_EmptyBodyTouchesUpdatedAt(t *testing.T) {
	ts := setup(t)
	cookie := loginAdmin(t, ts)
	client := authedClient(t, ts, cookie)
	pid := createProjectViaAPI(t, ts, client)

	before := doJSON(t, client, http.MethodGet, ts.server.URL+"/api/projects/"+pid, nil)
	var beforeResp projectResponse
	if err := json.NewDecoder(before.Body).Decode(&beforeResp); err != nil {
		t.Fatalf("decode before: %v", err)
	}
	before.Body.Close()

	resp := doJSON(t, client, http.MethodPatch, ts.server.URL+"/api/projects/"+pid,
		map[string]any{})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var got projectResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.UpdatedAt < beforeResp.UpdatedAt {
		t.Errorf("UpdatedAt = %q, want >= %q", got.UpdatedAt, beforeResp.UpdatedAt)
	}
}

func TestUpdateProject_NotFound(t *testing.T) {
	ts := setup(t)
	cookie := loginAdmin(t, ts)
	client := authedClient(t, ts, cookie)

	resp := doJSON(t, client, http.MethodPatch, ts.server.URL+"/api/projects/nope",
		map[string]any{"name": "X"})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

func TestDeleteProject_Success(t *testing.T) {
	ts := setup(t)
	cookie := loginAdmin(t, ts)
	client := authedClient(t, ts, cookie)
	pid := createProjectViaAPI(t, ts, client)

	resp := doJSON(t, client, http.MethodDelete, ts.server.URL+"/api/projects/"+pid, nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNoContent)
	}

	getResp := doJSON(t, client, http.MethodGet, ts.server.URL+"/api/projects/"+pid, nil)
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusNotFound {
		t.Errorf("get after delete: status = %d, want %d", getResp.StatusCode, http.StatusNotFound)
	}
}

func TestDeleteProject_NotFound(t *testing.T) {
	ts := setup(t)
	cookie := loginAdmin(t, ts)
	client := authedClient(t, ts, cookie)

	resp := doJSON(t, client, http.MethodDelete, ts.server.URL+"/api/projects/nope", nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

func TestDeleteProject_CleansUpWorkspaces(t *testing.T) {
	ts := setup(t)
	cookie := loginAdmin(t, ts)
	client := authedClient(t, ts, cookie)
	pid := createProjectViaAPI(t, ts, client)

	wsResp := doJSON(t, client, http.MethodPost, ts.server.URL+"/api/workspaces",
		map[string]any{"project_id": pid, "agent_type": "claude"})
	if wsResp.StatusCode != http.StatusCreated {
		t.Fatalf("create workspace status = %d", wsResp.StatusCode)
	}
	var ws workspaceResponse
	if err := json.NewDecoder(wsResp.Body).Decode(&ws); err != nil {
		t.Fatalf("decode ws: %v", err)
	}
	wsResp.Body.Close()
	wtPath := ws.WorktreePath
	if wtPath == "" {
		t.Fatal("WorktreePath empty after create")
	}
	if _, err := os.Stat(wtPath); err != nil {
		t.Fatalf("worktree not present: %v", err)
	}

	delResp := doJSON(t, client, http.MethodDelete, ts.server.URL+"/api/projects/"+pid, nil)
	defer delResp.Body.Close()
	if delResp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete project status = %d, want %d", delResp.StatusCode, http.StatusNoContent)
	}

	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Errorf("worktree after project delete: %v, want IsNotExist", err)
	}
}

func createProjectViaAPI(t *testing.T, ts *testServer, client *http.Client) string {
	t.Helper()
	repo := tempRepo(t)
	resp := doJSON(t, client, http.MethodPost, ts.server.URL+"/api/projects",
		map[string]any{"name": "P", "repo_path": repo})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create project status = %d", resp.StatusCode)
	}
	var got projectResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return got.ID
}
