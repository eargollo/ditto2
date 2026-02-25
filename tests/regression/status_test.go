package regression_test

import (
	"testing"
)

// TestStatus_ReturnsOK verifies that GET /api/status returns 200.
func TestStatus_ReturnsOK(t *testing.T) {
	ts := newTestServer(t)
	resp := ts.get(t, "/api/status")
	defer resp.Body.Close()
	requireStatus(t, resp, 200)
}

// TestStatus_ContentTypeJSON verifies Content-Type is application/json.
func TestStatus_ContentTypeJSON(t *testing.T) {
	ts := newTestServer(t)
	resp := ts.get(t, "/api/status")
	defer resp.Body.Close()
	requireContentType(t, resp, "application/json")
}

// TestStatus_Shape verifies the response has the expected top-level keys.
func TestStatus_Shape(t *testing.T) {
	ts := newTestServer(t)
	resp := ts.get(t, "/api/status")

	var body struct {
		Schedule struct {
			Cron   string `json:"cron"`
			Paused bool   `json:"paused"`
		} `json:"schedule"`
		ActiveScan        interface{} `json:"active_scan"`
		LastCompletedScan interface{} `json:"last_completed_scan"`
	}
	decodeJSON(t, resp, &body)

	if body.Schedule.Cron == "" {
		t.Error("expected schedule.cron to be non-empty")
	}
}
