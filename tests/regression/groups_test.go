package regression_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// maxGroupID returns the highest group ID currently in the database (0 if none).
func maxGroupID(t *testing.T, ts *testServer) int64 {
	t.Helper()
	resp := ts.get(t, "/api/groups?status=all&limit=1")
	requireStatus(t, resp, 200)
	// Groups are sorted by reclaimable_bytes DESC, not ID — use a workaround:
	// fetch all and take the max.
	var body struct {
		Items []struct {
			ID int64 `json:"id"`
		} `json:"items"`
		Total int `json:"total"`
	}
	decodeJSON(t, resp, &body)
	if body.Total == 0 {
		return 0
	}
	// Re-fetch all to find the maximum ID.
	resp2 := ts.get(t, fmt.Sprintf("/api/groups?status=all&limit=%d", body.Total+10))
	requireStatus(t, resp2, 200)
	var body2 struct {
		Items []struct {
			ID int64 `json:"id"`
		} `json:"items"`
	}
	decodeJSON(t, resp2, &body2)
	var max int64
	for _, item := range body2.Items {
		if item.ID > max {
			max = item.ID
		}
	}
	return max
}

// waitForScan triggers a scan on the given temp dir and waits for it to complete.
func waitForScan(t *testing.T, ts *testServer, dir string) {
	t.Helper()

	// Update scan_paths to the temp dir.
	ts.patch(t, "/api/config", strings.NewReader(fmt.Sprintf(`{"scan_paths":[%q]}`, dir)))

	// Trigger scan.
	resp := ts.post(t, "/api/scans", strings.NewReader(""))
	requireStatus(t, resp, 202)
	resp.Body.Close()

	// Poll until scan finishes.
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		time.Sleep(500 * time.Millisecond)
		statusResp := ts.get(t, "/api/status")
		requireStatus(t, statusResp, 200)
		var body struct {
			ActiveScan interface{} `json:"active_scan"`
		}
		decodeJSON(t, statusResp, &body)
		if body.ActiveScan == nil {
			return
		}
	}
	t.Fatal("scan did not complete within timeout")
}

// firstNewGroup returns the first group created after minID from GET /api/groups
// (using status=all), or fails the test if none found.
func firstNewGroup(t *testing.T, ts *testServer, minID int64) (id int64, fileCount int, status string) {
	t.Helper()
	resp := ts.get(t, "/api/groups?status=all&limit=100")
	requireStatus(t, resp, 200)
	var body struct {
		Items []struct {
			ID        int64  `json:"id"`
			FileCount int    `json:"file_count"`
			Status    string `json:"status"`
		} `json:"items"`
		Total int `json:"total"`
	}
	decodeJSON(t, resp, &body)
	for _, item := range body.Items {
		if item.ID > minID {
			return item.ID, item.FileCount, item.Status
		}
	}
	t.Fatalf("expected at least one group with ID > %d, got none (total=%d)", minID, body.Total)
	return 0, 0, ""
}

// TestGroupDelete_NoKeeper verifies that deleting all files in a group is rejected.
func TestGroupDelete_NoKeeper(t *testing.T) {
	ts := newTestServer(t)

	dir := t.TempDir()
	content := []byte("duplicate content for no-keeper test")
	os.WriteFile(filepath.Join(dir, "file_a.txt"), content, 0o644)
	os.WriteFile(filepath.Join(dir, "file_b.txt"), content, 0o644)

	prevMax := maxGroupID(t, ts)
	waitForScan(t, ts, dir)
	groupID, fileCount, _ := firstNewGroup(t, ts, prevMax)
	if fileCount < 2 {
		t.Fatalf("expected group with ≥2 files, got %d", fileCount)
	}

	// Get file IDs.
	resp := ts.get(t, fmt.Sprintf("/api/groups/%d", groupID))
	requireStatus(t, resp, 200)
	var detail struct {
		Files []struct{ ID int64 `json:"id"` } `json:"files"`
	}
	decodeJSON(t, resp, &detail)

	// Submit all file IDs — should be rejected.
	ids := make([]int64, len(detail.Files))
	for i, f := range detail.Files {
		ids[i] = f.ID
	}
	body := fmt.Sprintf(`{"delete_file_ids":[%d,%d]}`, ids[0], ids[1])
	delResp := ts.post(t, fmt.Sprintf("/api/groups/%d/delete", groupID), strings.NewReader(body))
	requireStatus(t, delResp, 400)
	var errBody struct {
		Error struct{ Code string `json:"code"` } `json:"error"`
	}
	decodeJSON(t, delResp, &errBody)
	if errBody.Error.Code != "NO_KEEPER" {
		t.Errorf("expected NO_KEEPER, got %q", errBody.Error.Code)
	}
}

// TestGroupDelete_Success verifies that deleting one file trashes it and resolves the group.
func TestGroupDelete_Success(t *testing.T) {
	ts := newTestServer(t)

	dir := t.TempDir()
	content := []byte("duplicate content for delete-success test")
	os.WriteFile(filepath.Join(dir, "file_a.txt"), content, 0o644)
	os.WriteFile(filepath.Join(dir, "file_b.txt"), content, 0o644)

	prevMax := maxGroupID(t, ts)
	waitForScan(t, ts, dir)
	groupID, _, _ := firstNewGroup(t, ts, prevMax)

	// Get file IDs.
	resp := ts.get(t, fmt.Sprintf("/api/groups/%d", groupID))
	requireStatus(t, resp, 200)
	var detail struct {
		Files []struct {
			ID   int64  `json:"id"`
			Path string `json:"path"`
		} `json:"files"`
	}
	decodeJSON(t, resp, &detail)
	if len(detail.Files) < 2 {
		t.Fatalf("expected ≥2 files, got %d", len(detail.Files))
	}

	// Delete just the first file.
	deleteID := detail.Files[0].ID
	body := fmt.Sprintf(`{"delete_file_ids":[%d]}`, deleteID)
	delResp := ts.post(t, fmt.Sprintf("/api/groups/%d/delete", groupID), strings.NewReader(body))
	requireStatus(t, delResp, 200)

	var result struct {
		Trashed []struct {
			FileID  int64 `json:"file_id"`
			TrashID int64 `json:"trash_id"`
		} `json:"trashed"`
		Group struct {
			FileCount int    `json:"file_count"`
			Status    string `json:"status"`
		} `json:"group"`
	}
	decodeJSON(t, delResp, &result)

	if len(result.Trashed) != 1 {
		t.Errorf("expected 1 trashed item, got %d", len(result.Trashed))
	}
	if result.Trashed[0].FileID != deleteID {
		t.Errorf("expected trashed file_id %d, got %d", deleteID, result.Trashed[0].FileID)
	}
	if result.Trashed[0].TrashID == 0 {
		t.Error("expected non-zero trash_id")
	}
	if result.Group.FileCount != 1 {
		t.Errorf("expected group file_count=1, got %d", result.Group.FileCount)
	}
	if result.Group.Status != "resolved" {
		t.Errorf("expected group status=resolved, got %q", result.Group.Status)
	}

	// Verify the trashed file appears in GET /api/trash.
	trashResp := ts.get(t, "/api/trash")
	requireStatus(t, trashResp, 200)
	var trashBody struct {
		Total int `json:"total"`
	}
	decodeJSON(t, trashResp, &trashBody)
	if trashBody.Total < 1 {
		t.Error("expected at least 1 item in trash after delete")
	}
}

// TestGroupIgnore_Hash verifies that ignoring by hash sets the group to ignored.
func TestGroupIgnore_Hash(t *testing.T) {
	ts := newTestServer(t)

	dir := t.TempDir()
	content := []byte("duplicate content for ignore-hash test")
	os.WriteFile(filepath.Join(dir, "file_a.txt"), content, 0o644)
	os.WriteFile(filepath.Join(dir, "file_b.txt"), content, 0o644)

	prevMax := maxGroupID(t, ts)
	waitForScan(t, ts, dir)
	groupID, _, _ := firstNewGroup(t, ts, prevMax)

	ignResp := ts.post(t, fmt.Sprintf("/api/groups/%d/ignore", groupID), strings.NewReader(`{"type":"hash"}`))
	requireStatus(t, ignResp, 200)

	var result struct {
		WhitelistID int64  `json:"whitelist_id"`
		Type        string `json:"type"`
		Group       struct {
			Status string `json:"status"`
		} `json:"group"`
	}
	decodeJSON(t, ignResp, &result)

	if result.WhitelistID == 0 {
		t.Error("expected non-zero whitelist_id")
	}
	if result.Type != "hash" {
		t.Errorf("expected type=hash, got %q", result.Type)
	}
	if result.Group.Status != "ignored" {
		t.Errorf("expected group status=ignored, got %q", result.Group.Status)
	}

	// The specific group should now be ignored — check it directly.
	groupResp := ts.get(t, fmt.Sprintf("/api/groups/%d", groupID))
	requireStatus(t, groupResp, 200)
	var groupDetail struct {
		Status string `json:"status"`
	}
	decodeJSON(t, groupResp, &groupDetail)
	if groupDetail.Status != "ignored" {
		t.Errorf("expected group %d status=ignored, got %q", groupID, groupDetail.Status)
	}

	// Should appear with status=ignored filter.
	listResp2 := ts.get(t, "/api/groups?status=ignored")
	requireStatus(t, listResp2, 200)
	var listBody2 struct {
		Items []struct {
			ID int64 `json:"id"`
		} `json:"items"`
		Total int `json:"total"`
	}
	decodeJSON(t, listResp2, &listBody2)
	found := false
	for _, item := range listBody2.Items {
		if item.ID == groupID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected group %d to appear under status=ignored filter", groupID)
	}
}

// TestGroupIgnore_PathPair verifies that path_pair watch sets the group to watching.
func TestGroupIgnore_PathPair(t *testing.T) {
	ts := newTestServer(t)

	dir := t.TempDir()
	content := []byte("duplicate content for path-pair test")
	os.WriteFile(filepath.Join(dir, "file_a.txt"), content, 0o644)
	os.WriteFile(filepath.Join(dir, "file_b.txt"), content, 0o644)

	prevMax := maxGroupID(t, ts)
	waitForScan(t, ts, dir)
	groupID, _, _ := firstNewGroup(t, ts, prevMax)

	ignResp := ts.post(t, fmt.Sprintf("/api/groups/%d/ignore", groupID), strings.NewReader(`{"type":"path_pair"}`))
	requireStatus(t, ignResp, 200)

	var result struct {
		WhitelistID int64  `json:"whitelist_id"`
		Type        string `json:"type"`
		Group       struct {
			Status string `json:"status"`
		} `json:"group"`
	}
	decodeJSON(t, ignResp, &result)

	if result.WhitelistID == 0 {
		t.Error("expected non-zero whitelist_id")
	}
	if result.Group.Status != "watching" {
		t.Errorf("expected group status=watching, got %q", result.Group.Status)
	}

	// The specific group should now be watching — check it directly.
	groupResp := ts.get(t, fmt.Sprintf("/api/groups/%d", groupID))
	requireStatus(t, groupResp, 200)
	var groupDetail struct {
		Status string `json:"status"`
	}
	decodeJSON(t, groupResp, &groupDetail)
	if groupDetail.Status != "watching" {
		t.Errorf("expected group %d status=watching, got %q", groupID, groupDetail.Status)
	}

	// Should appear with status=watching filter.
	listResp2 := ts.get(t, "/api/groups?status=watching")
	requireStatus(t, listResp2, 200)
	var listBody2 struct {
		Items []struct {
			ID int64 `json:"id"`
		} `json:"items"`
		Total int `json:"total"`
	}
	decodeJSON(t, listResp2, &listBody2)
	found := false
	for _, item := range listBody2.Items {
		if item.ID == groupID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected group %d to appear under status=watching filter", groupID)
	}
}
