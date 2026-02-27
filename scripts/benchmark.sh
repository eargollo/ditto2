#!/usr/bin/env bash
# benchmark.sh — trigger a scan against a running Ditto server, wait for
# completion, fetch telemetry, save a JSON report, and print a summary.
#
# Usage:
#   ./scripts/benchmark.sh [SERVER_URL]
#
# Environment variables:
#   DITTO_URL   — base URL of the Ditto server (default: http://localhost:8280)
#   BENCH_OUT   — directory for JSON reports (default: benchmarks/)
#
# Examples:
#   ./scripts/benchmark.sh
#   DITTO_URL=http://localhost:8080 ./scripts/benchmark.sh
set -euo pipefail

DITTO_URL="${DITTO_URL:-http://localhost:8280}"
BENCH_OUT="${BENCH_OUT:-benchmarks}"
POLL_INTERVAL=5   # seconds between status polls
TIMEOUT=7200      # max seconds to wait for scan completion (2 hours)

# ── helpers ───────────────────────────────────────────────────────────────────

die() { echo "ERROR: $*" >&2; exit 1; }

require() {
  for cmd in "$@"; do
    command -v "$cmd" >/dev/null 2>&1 || die "required command not found: $cmd"
  done
}

require curl jq

# ── check server reachable ─────────────────────────────────────────────────────

echo "==> Checking server at $DITTO_URL ..."
curl -sf "$DITTO_URL/api/status" >/dev/null || die "server not reachable at $DITTO_URL"
echo "    Server OK."

# ── trigger scan ──────────────────────────────────────────────────────────────

echo "==> Triggering scan ..."
SCAN_RESP=$(curl -sf -X POST "$DITTO_URL/api/scans" -H "Content-Type: application/json" -d '{}')
SCAN_ID=$(echo "$SCAN_RESP" | jq -r '.id // empty')
if [ -z "$SCAN_ID" ] || [ "$SCAN_ID" = "null" ] || [ "$SCAN_ID" = "0" ]; then
  # Scan may already be running — get the current scan ID from status
  echo "    (scan may already be running, checking status...)"
  STATUS_RESP=$(curl -sf "$DITTO_URL/api/status")
  SCAN_ID=$(echo "$STATUS_RESP" | jq -r '.active_scan.id // empty')
  if [ -z "$SCAN_ID" ] || [ "$SCAN_ID" = "null" ]; then
    die "could not start or find an active scan"
  fi
  echo "    Attached to existing scan ID=$SCAN_ID"
else
  echo "    Scan started: ID=$SCAN_ID"
fi

# ── poll until complete ────────────────────────────────────────────────────────

echo "==> Waiting for scan $SCAN_ID to complete (polling every ${POLL_INTERVAL}s, timeout ${TIMEOUT}s) ..."
elapsed=0
while true; do
  DETAIL=$(curl -sf "$DITTO_URL/api/scans/$SCAN_ID")
  STATUS=$(echo "$DETAIL" | jq -r '.status // "unknown"')

  if [ "$STATUS" = "completed" ]; then
    echo "    Scan completed."
    break
  elif [ "$STATUS" = "failed" ]; then
    die "scan $SCAN_ID failed"
  elif [ "$STATUS" = "cancelled" ]; then
    die "scan $SCAN_ID was cancelled"
  fi

  # Print a brief progress line.
  FILES=$(echo "$DETAIL" | jq -r '.files_discovered // 0')
  printf "    [%4ds] status=%-8s files_discovered=%s\n" "$elapsed" "$STATUS" "$FILES"

  if [ "$elapsed" -ge "$TIMEOUT" ]; then
    die "timed out after ${TIMEOUT}s waiting for scan to complete"
  fi
  sleep "$POLL_INTERVAL"
  elapsed=$((elapsed + POLL_INTERVAL))
done

# ── fetch telemetry ───────────────────────────────────────────────────────────

echo "==> Fetching telemetry for scan $SCAN_ID ..."
TELEMETRY=$(curl -sf "$DITTO_URL/api/scans/$SCAN_ID/telemetry")

# ── save report ───────────────────────────────────────────────────────────────

mkdir -p "$BENCH_OUT"
TIMESTAMP=$(date -u +"%Y-%m-%dT%H-%M-%SZ")
REPORT_FILE="${BENCH_OUT}/${TIMESTAMP}_scan${SCAN_ID}.json"
echo "$TELEMETRY" | jq '.' > "$REPORT_FILE"
echo "==> Report saved: $REPORT_FILE"

# ── print summary ─────────────────────────────────────────────────────────────

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo " Benchmark Summary — scan $SCAN_ID"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "$TELEMETRY" | jq -r '
  "  Duration:          \(.duration_seconds)s",
  "  Files discovered:  \(.files_discovered | tostring | gsub("(?<d>[0-9])(?=([0-9]{3})+$)"; "\(.d),")),
  "  Files/sec:         \(.files_per_sec | floor)",
  "  Candidate %:       \(.candidate_pct | floor)%  (size-duplicate matches)",
  "  Cache hit %:       \(.cache_hit_pct | floor)%",
  "  Duplicate groups:  \(.duplicate_groups)",
  "  Bytes hashed:      \(.bytes_read_mb | floor) MB",
  "  Hash throughput:   \(.hash_throughput_mbps | floor) MB/s",
  "  Disk I/O:          \(.disk_read_ms)ms  (\(.disk_pct | floor)% of timed work)",
  "  DB writes:         \(.db_write_ms)ms  (\(.db_write_pct | floor)% of timed work)",
  "  DB reads:          \(.db_read_ms)ms   (\(.db_read_pct | floor)% of timed work)",
  "  Errors:            \(.errors)"
'
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
