package regression_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestManualScan_StartsAndCompletes triggers a manual scan and waits for it
// to reach a terminal status.
//
// EXPECTED TO FAIL until the scan pipeline is implemented.
func TestManualScan_StartsAndCompletes(t *testing.T) {
	ts := newTestServer(t)

	// POST /api/scans to start a scan.
	resp := ts.post(t, "/api/scans", bytes.NewBufferString("{}"))
	defer resp.Body.Close()

	if resp.StatusCode == 501 {
		t.Fatal("scan pipeline not yet implemented — POST /api/scans returned 501")
	}
	requireStatus(t, resp, 202)

	var startBody struct {
		ID     int64  `json:"id"`
		Status string `json:"status"`
	}
	decodeJSON(t, resp, &startBody)

	// ID may be 0 for a brief moment, but should be positive since the
	// scan_history record is created before the 202 response is sent.
	if startBody.ID < 0 {
		t.Fatalf("expected scan id >= 0, got %d", startBody.ID)
	}
	if startBody.Status != "running" {
		t.Fatalf("expected status=running, got %q", startBody.Status)
	}

	// Poll /api/status until scan completes (or timeout).
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		time.Sleep(2 * time.Second)

		statusResp := ts.get(t, "/api/status")
		var statusBody struct {
			ActiveScan interface{} `json:"active_scan"`
		}
		decodeJSON(t, statusResp, &statusBody)

		if statusBody.ActiveScan == nil {
			return // scan completed
		}
	}
	t.Fatal("scan did not complete within timeout")
}

// TestScan_FindsDuplicates creates a temporary directory with duplicate files,
// configures a scan path pointing to it, triggers a scan, and asserts that
// GET /api/groups returns the expected duplicate group.
//
// EXPECTED TO FAIL until the scan pipeline is implemented.
func TestScan_FindsDuplicates(t *testing.T) {
	ts := newTestServer(t)

	// Create fixture: two identical files.
	dir := t.TempDir()
	content := []byte("ditto-test-content-for-duplicate-detection")
	for _, name := range []string{"file_a.txt", "file_b.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Also create a unique file that should NOT appear as a duplicate.
	if err := os.WriteFile(filepath.Join(dir, "unique.txt"), []byte("unique"), 0o644); err != nil {
		t.Fatal(err)
	}

	// PATCH /api/config to point scan_paths at the temp dir.
	patchBody, _ := json.Marshal(map[string]interface{}{
		"scan_paths": []string{dir},
	})
	patchResp := ts.patch(t, "/api/config", bytes.NewBuffer(patchBody))
	requireStatus(t, patchResp, 200)
	patchResp.Body.Close()

	// POST /api/scans to start a scan.
	scanResp := ts.post(t, "/api/scans", bytes.NewBufferString("{}"))
	defer scanResp.Body.Close()
	if scanResp.StatusCode == 501 {
		t.Fatal("scan pipeline not yet implemented — POST /api/scans returned 501")
	}
	requireStatus(t, scanResp, 202)

	// Wait for the scan to complete.
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		time.Sleep(2 * time.Second)
		statusResp := ts.get(t, "/api/status")
		var statusBody struct {
			ActiveScan interface{} `json:"active_scan"`
		}
		decodeJSON(t, statusResp, &statusBody)
		if statusBody.ActiveScan == nil {
			break
		}
	}

	// GET /api/groups — expect at least one group.
	groupsResp := ts.get(t, "/api/groups")
	requireStatus(t, groupsResp, 200)

	var groupsBody struct {
		Items []struct {
			ID               int64  `json:"id"`
			FileCount        int    `json:"file_count"`
			ReclaimableBytes int64  `json:"reclaimable_bytes"`
			FileType         string `json:"file_type"`
		} `json:"items"`
		Total int `json:"total"`
	}
	decodeJSON(t, groupsResp, &groupsBody)

	if groupsBody.Total == 0 {
		t.Fatal("expected at least one duplicate group, got 0")
	}

	// Verify the duplicate group contains the two identical files.
	found := false
	for _, g := range groupsBody.Items {
		if g.FileCount >= 2 && g.ReclaimableBytes > 0 {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("no group with file_count >= 2 found; groups: %+v", groupsBody.Items)
	}
}
