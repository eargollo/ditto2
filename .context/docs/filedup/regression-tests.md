# Ditto — Regression Test Coverage

**Location:** `tests/regression/`
**Language:** Go (`package regression_test`)
**Run requirement:** a live Ditto server (default `http://localhost:8080`,
overridable with `DITTO_TEST_URL`)

---

## How the tests work

Each test calls `newTestServer(t)` which checks that the server is reachable. If
it is not, the test is **skipped** (not failed). This lets the regression suite
be included in `go test ./...` without breaking CI when no server is running.

---

## Test files

### `helpers_test.go`

Shared infrastructure — not a test file by itself.

| Symbol | Purpose |
|---|---|
| `testServer` | Wraps `http.Client` + base URL |
| `newTestServer(t)` | Resolves `DITTO_TEST_URL`, skips if unreachable |
| `get(t, path)` | HTTP GET helper |
| `post(t, path, body)` | HTTP POST helper |
| `patch(t, path, body)` | HTTP PATCH helper |
| `requireStatus(t, resp, want)` | Asserts HTTP status code |
| `requireContentType(t, resp, want)` | Asserts `Content-Type` prefix |
| `decodeJSON(t, resp, v)` | Decodes JSON response body |

---

### `status_test.go`

Tests `GET /api/status`. These **pass immediately** after scaffolding.

| Test | What it verifies |
|---|---|
| `TestStatus_ReturnsOK` | The endpoint responds with HTTP 200 |
| `TestStatus_ContentTypeJSON` | `Content-Type` is `application/json` |
| `TestStatus_Shape` | Response has `schedule.cron` (non-empty string), `active_scan` key, and `last_completed_scan` key |

**Design intent:** Smoke-test that the server is up and the status contract is
satisfied before running slower scan tests.

---

### `scan_test.go`

Tests the full duplicate-detection pipeline end-to-end via the REST API.

#### `TestManualScan_StartsAndCompletes`

| Step | Verification |
|---|---|
| `POST /api/scans` | Returns HTTP 202 with `status: "running"` |
| Poll `GET /api/status` every 2 s | `active_scan` transitions from non-null to `null` within 2 minutes |

**What this proves:** A manual scan can be triggered and runs to completion
(or at least to a terminal state). The configured `scan_paths` need not contain
any files — the test only checks that the scan finishes.

**Expected result:** PASS (scan over non-existent or empty paths completes
immediately with 0 files).

---

#### `TestScan_FindsDuplicates`

The main correctness test for the pipeline. Creates real files on disk, runs a
targeted scan, and asserts the API reports the expected duplicates.

| Step | Verification |
|---|---|
| Create `t.TempDir()` with `file_a.txt` and `file_b.txt` (identical content) plus `unique.txt` (distinct content) | Fixture on local filesystem |
| `PATCH /api/config` `{"scan_paths": [tempDir]}` | Returns HTTP 200 (config updated) |
| `POST /api/scans` | Returns HTTP 202 |
| Poll `GET /api/status` | `active_scan` becomes `null` (scan finished) |
| `GET /api/groups` | `total >= 1` and at least one group has `file_count >= 2` and `reclaimable_bytes > 0` |

**What this proves end-to-end:**

1. **Walker** correctly traverses the directory and discovers all three files.
2. **Size accumulator** identifies `file_a` and `file_b` as size-duplicate
   candidates (same byte count).
3. **Cache check** misses both (first scan, no cache entries) and forwards them
   for hashing.
4. **Partial hasher** computes the same partial SHA-256 for both identical files.
5. **Partial hash grouper** emits both as a probable-duplicate pair.
6. **Full hasher** computes the same full SHA-256, confirming they are true
   duplicates.
7. **DB writer** inserts one `duplicate_groups` row and two `duplicate_files`
   rows.
8. **`GET /api/groups` handler** reads from the DB and returns the group in the
   paginated response.
9. **`PATCH /api/config`** correctly updates the manager's scan roots for the
   next scan.

`unique.txt` must **not** appear as a duplicate (different size → excluded by
accumulator).

---

## Running the tests

```bash
# 1. Start the server
./ditto --config config.yaml.example

# 2. In another terminal, run the full suite
go test ./tests/regression/... -v -timeout 5m

# 3. Or point at a different instance
DITTO_TEST_URL=http://nas.local:8080 go test ./tests/regression/... -v
```

## Expected results after pipeline implementation

| Test | Expected |
|---|---|
| `TestStatus_ReturnsOK` | PASS |
| `TestStatus_ContentTypeJSON` | PASS |
| `TestStatus_Shape` | PASS |
| `TestManualScan_StartsAndCompletes` | PASS |
| `TestScan_FindsDuplicates` | PASS |
