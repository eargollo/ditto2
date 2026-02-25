# Ditto — Requirements Document

**Date:** 2026-02-25
**Status:** Draft

---

## 1. Overview

Ditto is a duplicate file finder designed to run on a Synology NAS (or any Docker host). It periodically scans one or more directories, identifies duplicate files, and provides a web UI for the user to review and clean up duplicates.

---

## 2. Goals

- Automatically find duplicate files on a large NAS library (1M+ files)
- Present duplicates in a UX optimized for efficient cleanup decisions
- Allow safe deletion with a recoverable trash mechanism
- Run periodically without user intervention; also support manual triggers
- Be portable: deployed as a single Docker container, no external services required

---

## 3. Non-Goals

- Multi-user access control (single trusted home network user)
- Cloud sync or remote NAS support
- Deduplication at the filesystem level (e.g. hardlinks/symlinks) — not in v1

---

## 4. Architecture

### 4.1 Deployment

- Single Docker container
- Mounts one or more NAS volumes as read/write paths
- Exposes a web UI on a configurable port (default: 8080)
- No external services (no separate database server, no message broker)

### 4.2 Internal Components

```
[Scheduler (internal cron)]
        │
        ▼
[Scan Pipeline]                         (see §5 for full design)
  ├── Walker pool (N goroutines)         — parallel directory traversal
  ├── Size accumulator (1 goroutine)     — streams candidates as pairs are found
  ├── Cache check (1 goroutine)          — skips already-hashed unchanged files
  ├── Partial hash pool (N goroutines)   — SHA256 of first 64KB per file
  ├── Partial hash grouper (1 goroutine) — filters to confirmed collision candidates
  ├── Full hash pool (N goroutines)      — SHA256 of full file content
  └── DB writer (1 goroutine)            — batched inserts via fan-in channel
        │
        ▼
   [SQLite DB (WAL mode)]
        │
        ▼
   [Web Server]               — serves UI, REST API for actions
```

### 4.3 Tech Stack

| Layer | Choice | Rationale |
|---|---|---|
| Language | Go | Single binary, excellent concurrency, fast file I/O |
| Database | SQLite (WAL mode) | No extra service, single file, sufficient for 1M+ files with batched writes |
| Web UI | Go templates + HTMX | Server-side rendering, minimal JS, responsive without a full SPA |
| CSS | Tailwind CSS | Utility-first, responsive by default, easy to keep consistent |
| Scheduling | Internal cron (e.g. `robfig/cron`) | No OS dependency, configurable from UI |
| REST API | JSON over HTTP | Powers the web UI and enables black-box regression testing |

---

## 5. Scan Pipeline

Walking, hashing, and writing all run **concurrently**. Hashing begins as soon as the first duplicate-size pair is found — no need to wait for the walk to complete.

### 5.1 Full Pipeline

```
[Walker Pool — N goroutines]
  Parallel directory traversal via unbounded work queue (see §5.2)
        │
        ▼  chan FileInfo  (buffer: 10,000)
[Size Accumulator — 1 goroutine]
  Maintains map[size][]FileInfo in memory.
  ├── 1st file of a size  → hold (might be unique)
  ├── 2nd file of a size  → dispatch BOTH immediately
  └── 3rd+ file of a size → dispatch immediately
        │
        ▼  chan FileInfo  (buffer: 1,000)  candidates only
[Cache Check — 1 goroutine]
  Batch-queries SQLite: (path, size, mtime) → known hash?
  ├── HIT  → inject cached hash directly to DB writer ──────────┐
  └── MISS → forward to partial hash pool                       │
        │                                                        │
        ▼  chan FileInfo  (buffer: 500)                          │
[Partial Hash Pool — N goroutines]                              │
  Read first 64KB, compute SHA256 prefix hash.                  │
        │                                                        │
        ▼  chan HashedFile  (buffer: 500)                        │
[Partial Hash Grouper — 1 goroutine]                            │
  Maintains map[size+partialHash][]FileInfo.                    │
  ├── unique in group → discard (not a duplicate)               │
  └── collision       → dispatch to full hash pool              │
        │                                                        │
        ▼  chan FileInfo  (buffer: 100)                          │
[Full Hash Pool — N goroutines]                                 │
  Read entire file, compute SHA256.                             │
        │                                                        │
        ▼  chan HashedFile  (buffer: 500)                        │
[DB Writer — 1 goroutine] ◄─────────────────────────────────────┘
  Batched inserts (1,000 rows/tx):
  ├── update file_cache table (path, size, mtime, hash)
  └── write duplicate_groups table
```

### 5.2 Parallel Directory Walker

Uses a fixed goroutine pool with an **unbounded dynamic queue** to avoid deadlocks inherent to fixed-size channels when workers are both producers and consumers of the same queue.

**Termination via pending counter:**
- `Push(dir)` increments a `pending` counter before adding to queue
- Worker calls `Done()` after enqueuing all child directories (never before)
- When `pending == 0` and queue is empty → all workers exit via `cond.Broadcast()`

```
[Root dir]
   │  Push(root) → pending=1
   ▼
[DirQueue]  ←──── unbounded (mutex + slice + sync.Cond)
   │
   ├── Worker 1: Pop() → reads dir A → Push(sub1), Push(sub2) → Done()
   ├── Worker 2: Pop() → reads dir B → Push(sub3) → Done()
   ├── Worker 3: Pop() → reads sub1 → no subdirs → Done()
   └── ...
        │
        ▼  (files, not dirs)
   chan FileInfo → Size Accumulator
```

Each worker: reads directory entries, pushes all subdirectories to queue, emits files to the `FileInfo` channel, then calls `Done()`.

### 5.3 Worker Pool Sizing

| Stage | Default workers | Rationale |
|---|---|---|
| Walker | 4 | Metadata reads; more workers = faster traversal of deep trees |
| Partial hashers | 4 | 64KB reads; SSD cache absorbs most; safe for HDD |
| Full hashers | 2 | Large sequential reads; low count prevents HDD seek thrashing |
| DB writer | 1 | Fan-in; batched inserts are fast enough |

All pool sizes are configurable in `config.yaml`:

```yaml
scan_workers:
  walkers: 4
  partial_hashers: 4
  full_hashers: 2
```

### 5.4 Incremental Scanning

- SQLite `file_cache` table stores `(path, size, mtime) → hash`
- Cache check stage queries this before dispatching any file to the hash pools
- Files whose path + size + mtime are unchanged reuse the cached hash — no I/O
- On subsequent weekly scans of a stable library, only new or modified files are hashed

### 5.5 Termination Cascade

Each stage closes its output channel when done, naturally signalling the next stage:

```
Walker pool done         → close(filesOut)        [walk complete]
Size accumulator done    → close(candidatesOut)   [flush size-singletons to DB as unique]
Cache check done         → close(toHashOut)
Partial hash pool done   → close(partialOut)
Partial grouper done     → close(toFullHashOut)   [flush partial-singletons — not duplicates]
Full hash pool done      → close(fullHashOut)
DB writer done           → scan complete
```

### 5.6 Progress Tracking

A reporter goroutine reads atomic counters every second and writes a snapshot to SQLite for the web UI to poll:

| Counter | Description |
|---|---|
| `files_discovered` | Total files seen by walker |
| `candidates_found` | Files dispatched for hashing |
| `partial_hashed` | Files through partial hash stage |
| `full_hashed` | Files through full hash stage |
| `bytes_read` | Total bytes read for hashing |

### 5.7 Cancellation

Every worker receives a `context.Context`. Cancelling it (e.g. user stops scan from UI) causes all workers to exit after their current file. In-flight hashes are discarded; the DB writer flushes any accumulated batch before exiting.

---

## 6. Configuration

### 6.1 Config File (`config.yaml`)

Mounted into the container. Defines:

```yaml
scan_paths:
  - /volume1/photos
  - /volume1/documents
exclude_paths:
  - /volume1/photos/originals
schedule: "0 2 * * 0"   # weekly, Sunday 2am
data_dir: /data          # where SQLite DB and trash live
trash_retention_days: 30
```

### 6.2 UI Overrides

From the web UI, users can:
- Trigger a manual scan
- Pause / resume the schedule
- View the next scheduled run time

---

## 7. Web UI

Built with Go templates + HTMX + Tailwind CSS. The UI must be responsive (usable on a tablet from the couch) and visually clean.

### 7.1 Dashboard

**Stat cards** (large, prominent):
- Duplicate groups found
- Reclaimable space (current scan)
- Files deleted all-time
- Bytes reclaimed all-time
- Files deleted / bytes reclaimed in the last 30 days

**Active scan state**: when a scan is running the dashboard replaces the "Scan Now" button with a live progress indicator (HTMX poll every 2s) showing per-stage counters — Walk, Partial Hash, Full Hash — and a progress bar.

**Trend charts** (shown once ≥ 3 historical scans exist):
- Duplicate groups over time
- Reclaimable space over time
- Cumulative bytes reclaimed over time

**Recent scan history table**: one row per scan showing date, duration, files scanned, groups found, reclaimable space, error count (expandable to error list).

### 7.2 Duplicate Groups List

Default sort: **reclaimable space descending** — user captures 80% of savings in the first 20% of effort.

**Filter bar**:
- File type pill filters: All / Images / Video / Documents / Other
- Minimum reclaimable space slider
- Status filter: Unresolved / Ignored / Resolved

**Each group row shows**:
- Lazy-loaded thumbnail (images/video poster frame)
- File type badge (color-coded pill: blue=image, purple=video, gray=document)
- Number of copies
- File size
- Reclaimable space (highlighted in amber for large values)
- Quick-action menu: Ignore group, Exclude directory

**Session progress bar** pinned to top of list: `47 / 1,204 groups resolved · 14.2 GB freed this session` — the single most motivating element for large library cleanup.

### 7.3 Group Detail View

Each copy is rendered as a **card**. Cards use a radio-button "KEEP" model rather than checkboxes — one click selects which copy to keep; all others are implicitly queued for deletion. This reduces N−1 clicks to 1 click for any group size.

**Card anatomy**:
- Large thumbnail / video poster (clickable to full-screen lightbox)
- Full file path
- File size, last modified date, dimensions (images/video)
- **Delta values** shown in amber for copies that are smaller or older than the largest/newest: `−2.1 MB`, `3 days older` — borrowed from dupeGuru, helps decide which copy is inferior
- KEEP card: `ring-2 ring-green-500 bg-green-50`
- DELETE cards: `ring-1 ring-red-200 opacity-80`

**Image/video comparison**: a "Compare two" button opens a full-screen side-by-side slider modal for any two selected cards. Power-user feature; not forced on everyone.

**Keyboard shortcuts** for fast review:
- `1` / `2` / `3` … — select which copy to keep
- `D` — confirm deletion and advance to next group
- `→` / `←` — skip to next / previous group
- `I` — ignore this group

**Action bar**:
- "Delete selected" → triggers pre-deletion validation (§8.1) then moves to trash
- "Ignore group" / "Ignore this content hash" / "Exclude directory" (§7.4)

### 7.4 Whitelist Controls

Per group detail, the user can:
- **Ignore this group** — suppress this exact set of file paths
- **Ignore this content** — suppress all files with this hash (never flag again)
- **Exclude a directory** — add a path to the scan exclusion list permanently

### 7.5 Trash View

- List of files pending permanent deletion
- "Expires in" column color-coded: green (> 14 days), amber (7–14 days), red (< 7 days)
- "Restore" button per file
- "Purge all now" button — requires confirmation dialog explaining irreversibility
- Auto-purge runs daily; deletes files older than 30 days

---

## 8. Deletion Workflow

### 8.1 Pre-deletion Validation

Before any file is moved to trash, the system re-validates the entire duplicate group to ensure no copy has changed or disappeared since the scan. This prevents accidentally deleting the last remaining copy of a file.

**For each file selected for deletion:**
- Re-check current `(size, mtime)` against the scan snapshot
- If changed: abort, mark group as stale, prompt user to re-scan

**For each file designated to be kept:**
- Verify the file still exists on disk
- Re-check current `(size, mtime)` against the scan snapshot
- If missing or changed: abort with an explicit error — "the file you intended to keep no longer exists or has changed"

**On validation failure:**
- No files are deleted (all-or-nothing per deletion action)
- The affected group is flagged as `stale` in the UI
- User is shown which file(s) triggered the failure and prompted to re-scan

### 8.2 Deletion Steps

1. User selects files to delete in the UI (must keep at least one — enforced client-side)
2. Server runs pre-deletion validation (§8.1)
3. On success: files are **moved to a managed trash folder** (`/data/trash/`)
4. Original paths, file size, and scan hash are recorded in SQLite
5. Group is updated: trashed files removed from active duplicate view
6. After 30 days, the trash auto-purge job permanently deletes them
7. User can restore any file from the Trash view within the retention window
8. DB is updated after any delete/restore/purge action

---

## 9. Statistics & Scan History

### 9.1 Per-Scan Record

Every completed (or failed) scan writes a record to the `scan_history` table:

| Field | Description |
|---|---|
| `scan_id` | Unique identifier |
| `started_at` | Timestamp when scan began |
| `finished_at` | Timestamp when scan completed (null if in-progress or failed) |
| `status` | `running`, `completed`, `failed`, `cancelled` |
| `files_discovered` | Total files seen by walker |
| `files_hashed` | Total size-group candidates (`cache_hits` + `cache_misses`) |
| `cache_hits` | Candidates whose hash was reused from `file_cache` — no I/O |
| `cache_misses` | Candidates that required actual hashing (partial or full) |
| `duplicate_groups` | Number of duplicate groups found |
| `duplicate_files` | Total number of files that are duplicates |
| `reclaimable_bytes` | Total bytes reclaimable if one copy per group is kept |
| `errors` | Count of non-fatal errors during scan |
| `duration_seconds` | Wall-clock scan duration |

### 9.2 Scan Errors

Non-fatal errors (e.g. unreadable files, permission denied) are logged individually in a `scan_errors` table rather than aborting the scan:

| Field | Description |
|---|---|
| `scan_id` | FK to scan_history |
| `path` | File or directory that caused the error |
| `stage` | Pipeline stage: `walk`, `partial_hash`, `full_hash` |
| `error` | Error message string |
| `occurred_at` | Timestamp |

Errors are surfaced in the UI on the scan history row (expandable error list) and counted on the dashboard.

### 9.3 Deletion Tracking

Every file permanently purged from trash (whether by auto-purge or manual "Purge all") is recorded in a `deletion_log` table:

| Field | Description |
|---|---|
| `deleted_at` | Timestamp of permanent deletion |
| `original_path` | Path the file occupied before trash |
| `file_size` | Bytes freed |
| `content_hash` | SHA256 of the deleted file |
| `trigger` | `user` (manual purge) or `auto` (30-day TTL) |

This log is append-only and never pruned — it is the permanent record of what the tool has removed.

Aggregate stats derived from this log and surfaced on the dashboard:
- Total files deleted (all time)
- Total bytes reclaimed (all time)
- Files deleted in the last 30 days / bytes reclaimed in the last 30 days

### 9.4 Historical Trend Data

At the end of each completed scan, a snapshot is appended to `scan_snapshots` for charting:

| Field | Description |
|---|---|
| `scan_id` | FK to scan_history |
| `snapshot_at` | Timestamp |
| `duplicate_groups` | Groups at this point in time |
| `duplicate_files` | Files at this point in time |
| `reclaimable_bytes` | Reclaimable space at this point in time |
| `cumulative_deleted_files` | Total files deleted by tool up to this scan |
| `cumulative_reclaimed_bytes` | Total bytes freed by tool up to this scan |

These snapshots drive the trend charts on the dashboard (see §7.1).

---

## 10. Authentication

- None — assumed trusted home network
- The container port should not be exposed publicly (Synology firewall / local network only)

---

## 11. REST API & Testability

### 11.1 REST API

All web UI interactions are backed by a JSON REST API. The UI never holds authoritative state — it is a thin client over the API. This design enables black-box regression testing of the entire system without a browser.

Full request/response shapes, error codes, and pagination contract are defined in **`api-contract.md`**.

Key API surface:

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/status` | System status, active scan progress, next scheduled run |
| `POST` | `/api/scans` | Trigger a manual scan |
| `DELETE` | `/api/scans/current` | Cancel running scan |
| `GET` | `/api/scans` | Scan history list |
| `GET` | `/api/scans/:id` | Single scan details + error list |
| `GET` | `/api/groups` | Duplicate groups (filterable by status/type, paginated, sorted by reclaimable space) |
| `GET` | `/api/groups/:id` | Single group with all file copies |
| `GET` | `/api/groups/:id/thumbnail` | JPEG thumbnail for groups list |
| `POST` | `/api/groups/:id/delete` | Delete selected files (triggers pre-deletion validation, moves to trash) |
| `POST` | `/api/groups/:id/ignore` | Whitelist by hash, path pair, or directory |
| `GET` | `/api/files/:id/thumbnail` | Per-file JPEG thumbnail |
| `GET` | `/api/files/:id/preview` | Full file for lightbox (image/video) |
| `GET` | `/api/trash` | Active trash items |
| `POST` | `/api/trash/:id/restore` | Restore a file to its original path |
| `DELETE` | `/api/trash` | Purge all trash immediately (requires `confirm: true`) |
| `GET` | `/api/stats` | Historical trend snapshots + all-time deletion totals |
| `GET` | `/api/config` | Current effective configuration |
| `PATCH` | `/api/config` | Update runtime settings (schedule, workers, retention days) |

### 11.2 Black-Box Regression Testing

The REST API enables a full regression test suite that treats Ditto as a black box — tests interact only through HTTP, with a real filesystem mounted.

**Test strategy**:
- Tests construct a controlled directory tree on disk with known duplicate sets
- Start Ditto (or point it at the test tree) via the API
- Trigger a scan via `POST /api/scans` and poll `GET /api/status` until complete
- Assert on `GET /api/groups` response: expected groups, expected files, expected reclaimable space
- Execute delete actions and assert trash state
- Assert pre-deletion validation catches modified/deleted files correctly
- Assert historical stats accumulate correctly across multiple scans

**Test fixture design**:
- Fixtures are directories of files constructed programmatically (Go test helpers)
- Cover: exact duplicates, size-only collisions (different content), empty files, symlinks, permission-denied paths, very large files (via sparse files), files modified between scan and delete

This approach catches regressions in the pipeline, the API contract, the validation logic, and the DB schema — all without mocking internals.

---

## 12. Database Schema

See `schema.sql` for the full DDL. Summary of tables:

### Tables

| Table | Purpose |
|---|---|
| `scan_history` | One row per scan attempt; doubles as live progress record during scan |
| `scan_errors` | Non-fatal per-file errors per scan (expandable in UI) |
| `scan_snapshots` | One row per completed scan for historical trend charts |
| `file_cache` | `(path, size, mtime) → full_hash` incremental scan cache |
| `duplicate_groups` | Persistent groups by `content_hash`; user actions (ignore/resolve) survive re-scans |
| `duplicate_files` | Current file paths per group; replaced wholesale each scan |
| `trash` | Files pending auto-purge; status tracks `trashed → restored/purged` |
| `deletion_log` | Append-only permanent record of every purged file |
| `whitelist` | Suppression rules: by hash, by path pair, by directory |
| `settings` | Key-value store for UI overrides that persist across restarts |

### Key Design Decisions

**Groups are persistent, identified by `content_hash`** — `duplicate_groups` rows are created once (`INSERT OR IGNORE`) and updated on each re-scan. User actions (ignore, resolve) are never reset by a new scan.

**`duplicate_files` is replaced per scan** — at end of scan, old rows for touched groups are deleted and new rows inserted. Groups not seen in a scan retain their previous file list unchanged (handles temporarily unmounted volumes).

**Progress counters live on `scan_history`** — the active scan row has `progress_*` columns updated every second, avoiding a separate `scan_progress` table.

**`trash` rows are never deleted** — status (`trashed/restored/purged`) provides a full audit trail. The `deletion_log` is a separate append-only record written on purge.

**`whitelist.value` is type-discriminated** — one table, three types: `hash` (SHA-256 hex), `dir` (absolute path), `path_pair` (canonical JSON array of sorted paths).

**All timestamps are `INTEGER` Unix epoch** — avoids SQLite timezone ambiguity; fast integer range queries.

**All tables use SQLite `STRICT` mode** — enforces type affinity, catching application bugs at the DB boundary.

### Migration Execution Order

Tables must be created in this order due to FK dependencies:

1. `scan_history`
2. `file_cache`, `duplicate_groups`, `scan_errors`, `scan_snapshots`
3. `duplicate_files`, `trash`
4. `deletion_log`, `whitelist`, `settings` (no hard FKs; any order)

### Critical Indexes

| Index | Table | Serves |
|---|---|---|
| `(status, file_type, reclaimable_bytes DESC)` | `duplicate_groups` | Groups list — filter + sort, no filesort |
| `(path, size, mtime)` | `file_cache` | Cache check hot path |
| `(expires_at) WHERE status='trashed'` | `trash` | Auto-purge job (partial index) |
| `(status, trashed_at DESC)` | `trash` | Trash view |
| `(deleted_at DESC)` | `deletion_log` | 30-day stat filter |
| `(snapshot_at ASC)` | `scan_snapshots` | Trend chart query |

---

## 13. Open Questions / Future Work

- [ ] **Perceptual hashing** for near-duplicate images/videos (v2)
- [ ] **Hardlink/symlink mode** as an alternative to deletion (v2)
- [ ] **Email/notification** on scan completion with summary (v2)
- [ ] **Smart auto-selection rules** (e.g. always keep files in path X) (v2)
