package regression_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"
)

const defaultTestURL = "http://localhost:8080"

// testServer wraps the base URL for a running Ditto instance.
type testServer struct {
	baseURL string
	client  *http.Client
}

// newTestServer returns a testServer pointing at the URL in DITTO_TEST_URL
// (default: http://localhost:8080). If the server is unreachable the test is
// skipped with a clear message.
func newTestServer(t *testing.T) *testServer {
	t.Helper()
	base := os.Getenv("DITTO_TEST_URL")
	if base == "" {
		base = defaultTestURL
	}
	ts := &testServer{
		baseURL: base,
		client:  &http.Client{Timeout: 10 * time.Second},
	}
	// Verify the server is reachable.
	resp, err := ts.client.Get(base + "/api/status")
	if err != nil {
		t.Skipf("ditto server not reachable at %s: %v", base, err)
	}
	resp.Body.Close()
	return ts
}

// get performs a GET request to path and returns the response.
func (ts *testServer) get(t *testing.T, path string) *http.Response {
	t.Helper()
	resp, err := ts.client.Get(ts.baseURL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	return resp
}

// post performs a POST request to path with the given JSON body.
func (ts *testServer) post(t *testing.T, path string, body io.Reader) *http.Response {
	t.Helper()
	resp, err := ts.client.Post(ts.baseURL+path, "application/json", body)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	return resp
}

// patch performs a PATCH request to path with the given JSON body.
func (ts *testServer) patch(t *testing.T, path string, body io.Reader) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPatch, ts.baseURL+path, body)
	if err != nil {
		t.Fatalf("build PATCH %s: %v", path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := ts.client.Do(req)
	if err != nil {
		t.Fatalf("PATCH %s: %v", path, err)
	}
	return resp
}

// requireStatus fails the test if the response status code != want.
func requireStatus(t *testing.T, resp *http.Response, want int) {
	t.Helper()
	if resp.StatusCode != want {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status %d, got %d\nbody: %s", want, resp.StatusCode, body)
	}
}

// decodeJSON decodes the response body into v, failing the test on error.
func decodeJSON(t *testing.T, resp *http.Response, v interface{}) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
}

// requireContentType fails if the Content-Type header doesn't contain want.
func requireContentType(t *testing.T, resp *http.Response, want string) {
	t.Helper()
	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		t.Fatalf("missing Content-Type header, expected %q", want)
	}
	// Check prefix only (ignores "; charset=utf-8" suffix)
	if len(ct) < len(want) || ct[:len(want)] != want {
		t.Fatalf("Content-Type: got %q, want prefix %q", ct, want)
	}
}

// notImplementedMsg is returned by stub handlers.
const notImplementedMsg = "not yet implemented"

// formatURL is a helper for building query-param URLs.
func formatURL(path string, params map[string]string) string {
	url := path
	sep := "?"
	for k, v := range params {
		url += fmt.Sprintf("%s%s=%s", sep, k, v)
		sep = "&"
	}
	return url
}
