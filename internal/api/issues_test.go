package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

func createIssueViaAPI(t *testing.T, ts *testServer, client *http.Client, projectID, title string) issueResponse {
	t.Helper()
	resp := doJSON(t, client, http.MethodPost, ts.server.URL+"/api/projects/"+projectID+"/issues",
		map[string]any{"title": title, "description": "d", "tags": []string{"x"}})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create issue status = %d", resp.StatusCode)
	}
	var got issueResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return got
}

// setupIssueTest returns a logged-in client + a freshly created project ID.
// Pulled out so each issue test starts from a clean slate.
func setupIssueTest(t *testing.T) (*testServer, *http.Client, string) {
	t.Helper()
	ts := setup(t)
	cookie := loginAdmin(t, ts)
	client := authedClient(t, ts, cookie)
	pid := createProjectViaAPI(t, ts, client)
	return ts, client, pid
}

func TestCreateIssue_SortOrderSequential(t *testing.T) {
	ts, client, pid := setupIssueTest(t)

	first := createIssueViaAPI(t, ts, client, pid, "first")
	if first.SortOrder != 0 {
		t.Errorf("first SortOrder = %d, want 0", first.SortOrder)
	}
	second := createIssueViaAPI(t, ts, client, pid, "second")
	if second.SortOrder != 1 {
		t.Errorf("second SortOrder = %d, want 1", second.SortOrder)
	}
}

func TestCreateIssue_EmptyTagsReturnedAsArray(t *testing.T) {
	ts, client, pid := setupIssueTest(t)

	resp := doJSON(t, client, http.MethodPost, ts.server.URL+"/api/projects/"+pid+"/issues",
		map[string]any{"title": "no-tags"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatalf("decode raw: %v", err)
	}
	tags, ok := raw["tags"]
	if !ok {
		t.Fatal("missing 'tags' key")
	}
	if string(tags) != "[]" {
		t.Errorf("tags = %s, want `[]`", string(tags))
	}
}

func TestCreateIssue_MissingTitle(t *testing.T) {
	ts, client, pid := setupIssueTest(t)

	resp := doJSON(t, client, http.MethodPost, ts.server.URL+"/api/projects/"+pid+"/issues",
		map[string]any{"description": "no title"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
	env := decodeEnvelope(t, resp.Body)
	if env.Error.Code != "missing_title" {
		t.Errorf("code = %q, want %q", env.Error.Code, "missing_title")
	}
}

func TestListIssues_Default(t *testing.T) {
	ts, client, pid := setupIssueTest(t)

	createIssueViaAPI(t, ts, client, pid, "a")
	createIssueViaAPI(t, ts, client, pid, "b")

	resp := doJSON(t, client, http.MethodGet, ts.server.URL+"/api/projects/"+pid+"/issues", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var got issueListResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Issues) != 2 {
		t.Fatalf("len(Issues) = %d, want 2", len(got.Issues))
	}
	if got.Issues[0].SortOrder > got.Issues[1].SortOrder {
		t.Errorf("issues not in sort_order: [%d, %d]",
			got.Issues[0].SortOrder, got.Issues[1].SortOrder)
	}
}

func TestListIssues_StatusFilter(t *testing.T) {
	ts, client, pid := setupIssueTest(t)

	a := createIssueViaAPI(t, ts, client, pid, "a")
	createIssueViaAPI(t, ts, client, pid, "b")

	// Move 'a' to in_progress.
	patch := doJSON(t, client, http.MethodPatch, ts.server.URL+"/api/issues/"+a.ID,
		map[string]any{"status": "in_progress"})
	if patch.StatusCode != http.StatusOK {
		t.Fatalf("patch status = %d", patch.StatusCode)
	}
	patch.Body.Close()

	resp := doJSON(t, client, http.MethodGet,
		ts.server.URL+"/api/projects/"+pid+"/issues?status=in_progress", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var got issueListResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Issues) != 1 {
		t.Fatalf("len(Issues) = %d, want 1 (in_progress filter)", len(got.Issues))
	}
	if got.Issues[0].Status != "in_progress" {
		t.Errorf("status = %q, want in_progress", got.Issues[0].Status)
	}
}

func TestListIssues_InvalidStatusFilter(t *testing.T) {
	ts, client, pid := setupIssueTest(t)

	resp := doJSON(t, client, http.MethodGet,
		ts.server.URL+"/api/projects/"+pid+"/issues?status=bogus", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
	env := decodeEnvelope(t, resp.Body)
	if env.Error.Code != "invalid_status" {
		t.Errorf("code = %q, want %q", env.Error.Code, "invalid_status")
	}
}

func TestUpdateIssue_Status(t *testing.T) {
	ts, client, pid := setupIssueTest(t)
	a := createIssueViaAPI(t, ts, client, pid, "a")

	resp := doJSON(t, client, http.MethodPatch, ts.server.URL+"/api/issues/"+a.ID,
		map[string]any{"status": "done"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var got issueResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Status != "done" {
		t.Errorf("status = %q, want done", got.Status)
	}
}

func TestUpdateIssue_InvalidStatus(t *testing.T) {
	ts, client, pid := setupIssueTest(t)
	a := createIssueViaAPI(t, ts, client, pid, "a")

	resp := doJSON(t, client, http.MethodPatch, ts.server.URL+"/api/issues/"+a.ID,
		map[string]any{"status": "bogus"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
	env := decodeEnvelope(t, resp.Body)
	if env.Error.Code != "invalid_status" {
		t.Errorf("code = %q, want %q", env.Error.Code, "invalid_status")
	}
}

func TestReorderIssues_NewOrder(t *testing.T) {
	ts, client, pid := setupIssueTest(t)
	a := createIssueViaAPI(t, ts, client, pid, "a")
	b := createIssueViaAPI(t, ts, client, pid, "b")
	c := createIssueViaAPI(t, ts, client, pid, "c")

	// Reverse the order.
	resp := doJSON(t, client, http.MethodPost, ts.server.URL+"/api/projects/"+pid+"/issues/reorder",
		map[string]any{"ids": []string{c.ID, b.ID, a.ID}})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var got issueListResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Issues) != 3 {
		t.Fatalf("len = %d, want 3", len(got.Issues))
	}
	want := []string{c.ID, b.ID, a.ID}
	for i, w := range want {
		if got.Issues[i].ID != w {
			t.Errorf("Issues[%d].ID = %q, want %q", i, got.Issues[i].ID, w)
		}
		if got.Issues[i].SortOrder != i {
			t.Errorf("Issues[%d].SortOrder = %d, want %d", i, got.Issues[i].SortOrder, i)
		}
	}
}

func TestDeleteIssue_Success(t *testing.T) {
	ts, client, pid := setupIssueTest(t)
	a := createIssueViaAPI(t, ts, client, pid, "a")

	resp := doJSON(t, client, http.MethodDelete, ts.server.URL+"/api/issues/"+a.ID, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNoContent)
	}

	list := doJSON(t, client, http.MethodGet, ts.server.URL+"/api/projects/"+pid+"/issues", nil)
	defer list.Body.Close()
	var got issueListResponse
	_ = json.NewDecoder(list.Body).Decode(&got)
	for _, i := range got.Issues {
		if i.ID == a.ID {
			t.Errorf("deleted issue %q still present in list", a.ID)
		}
	}
}

func TestDeleteIssue_NotFound(t *testing.T) {
	ts, client, _ := setupIssueTest(t)

	resp := doJSON(t, client, http.MethodDelete, ts.server.URL+"/api/issues/nope", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}
