# Ditto — REST API Contract

**Date:** 2026-02-25
**Status:** Draft
**Base path:** `/api`
**Content-Type:** `application/json` for all requests and responses

---

## 1. General Conventions

### 1.1 Timestamps

All timestamps in responses are **ISO 8601 strings** (e.g. `"2026-02-25T02:00:00Z"`).
All nullable timestamps are `null` when not yet set.

### 1.2 Byte values

All byte sizes are **integers** (not strings). The UI is responsible for formatting
(e.g. `14.2 GB`).

### 1.3 Pagination

List endpoints accept:
- `limit` — max items to return (default: 50, max: 200)
- `offset` — number of items to skip (default: 0)

All list responses include:

```json
{
  "items": [...],
  "total": 1204,
  "limit": 50,
  "offset": 0
}
```

### 1.4 Error Format

All errors return a JSON body with a machine-readable `code`:

```json
{
  "error": {
    "code": "SCAN_ALREADY_RUNNING",
    "message": "A scan is already in progress"
  }
}
```

For validation failures with per-file details, an additional `failures` array is included:

```json
{
  "error": {
    "code": "VALIDATION_FAILED",
    "message": "One or more files have changed since the last scan",
    "failures": [
      {
        "file_id": 456,
        "path": "/volume1/photos/IMG_001.jpg",
        "reason": "FILE_MODIFIED"
      }
    ]
  }
}
```

**Failure reasons:**

| Code | Meaning |
|---|---|
| `FILE_MODIFIED` | File to delete has different size or mtime than scan record |
| `FILE_MISSING` | File to delete no longer exists on disk |
| `KEEPER_MODIFIED` | File designated to keep has changed since scan |
| `KEEPER_MISSING` | File designated to keep no longer exists on disk |

### 1.5 HTTP Status Codes

| Code | When |
|---|---|
| `200 OK` | Successful read or action |
| `202 Accepted` | Scan started asynchronously |
| `400 Bad Request` | Malformed request body or invalid parameters |
| `404 Not Found` | Resource does not exist |
| `409 Conflict` | Business logic conflict (scan running, restore path exists, validation failed) |
| `500 Internal Server Error` | Unexpected server error |

---

## 2. Endpoints

---

### `GET /api/status`

Current system state. The primary polling endpoint during an active scan (poll every 2s).

**Response `200`:**

```json
{
  "active_scan": {
    "id": 42,
    "started_at": "2026-02-25T02:00:00Z",
    "triggered_by": "schedule",
    "progress": {
      "files_discovered": 842103,
      "candidates_found": 12450,
      "partial_hashed": 8200,
      "full_hashed": 3100,
      "bytes_read": 4831838208,
      "cache_hits": 11200,
      "cache_misses": 1250
    }
  },
  "schedule": {
    "cron": "0 2 * * 0",
    "paused": false,
    "next_run_at": "2026-03-04T02:00:00Z"
  },
  "last_completed_scan": {
    "id": 41,
    "finished_at": "2026-02-18T03:14:22Z",
    "duplicate_groups": 1204,
    "duplicate_files": 3891,
    "reclaimable_bytes": 15234567890,
    "cache_hits": 46891,
    "cache_misses": 1429,
    "cache_hit_rate": 0.97
  }
}
```

`active_scan` is `null` when no scan is running.
`last_completed_scan` is `null` on first run before any scan has completed.

---

### `POST /api/scans`

Trigger a manual scan.

**Request:** no body required.

**Response `202`:**

```json
{
  "id": 43,
  "status": "running",
  "started_at": "2026-02-25T10:30:00Z",
  "triggered_by": "manual"
}
```

**Response `409`** — scan already running:

```json
{
  "error": {
    "code": "SCAN_ALREADY_RUNNING",
    "message": "A scan is already in progress"
  }
}
```

---

### `DELETE /api/scans/current`

Cancel the currently running scan.

**Response `200`:**

```json
{
  "id": 43,
  "status": "cancelled",
  "started_at": "2026-02-25T10:30:00Z",
  "finished_at": "2026-02-25T10:31:45Z"
}
```

**Response `404`** — no scan running:

```json
{
  "error": {
    "code": "NO_ACTIVE_SCAN",
    "message": "No scan is currently running"
  }
}
```

---

### `GET /api/scans`

Scan history, newest first.

**Query params:** `limit`, `offset`

**Response `200`:**

```json
{
  "items": [
    {
      "id": 41,
      "started_at": "2026-02-18T02:00:00Z",
      "finished_at": "2026-02-18T03:14:22Z",
      "status": "completed",
      "triggered_by": "schedule",
      "files_discovered": 1024000,
      "files_hashed": 52000,
      "cache_hits": 46891,
      "cache_misses": 4109,
      "cache_hit_rate": 0.92,
      "duplicate_groups": 1204,
      "duplicate_files": 3891,
      "reclaimable_bytes": 15234567890,
      "errors": 3,
      "duration_seconds": 4582
    }
  ],
  "total": 15,
  "limit": 50,
  "offset": 0
}
```

---

### `GET /api/scans/:id`

Single scan record with full error list.

**Response `200`:**

```json
{
  "id": 41,
  "started_at": "2026-02-18T02:00:00Z",
  "finished_at": "2026-02-18T03:14:22Z",
  "status": "completed",
  "triggered_by": "schedule",
  "files_discovered": 1024000,
  "files_hashed": 52000,
  "cache_hits": 46891,
  "cache_misses": 4109,
  "cache_hit_rate": 0.92,
  "duplicate_groups": 1204,
  "duplicate_files": 3891,
  "reclaimable_bytes": 15234567890,
  "errors": 3,
  "duration_seconds": 4582,
  "error_list": [
    {
      "path": "/volume1/photos/corrupted.jpg",
      "stage": "partial_hash",
      "error": "permission denied",
      "occurred_at": "2026-02-18T02:43:11Z"
    }
  ]
}
```

**Response `404`** — scan not found.

---

### `GET /api/groups`

Filterable, paginated list of duplicate groups sorted by reclaimable space descending.

**Query params:**

| Param | Type | Default | Description |
|---|---|---|---|
| `status` | string | `unresolved` | `unresolved` \| `ignored` \| `resolved` \| `all` |
| `type` | string | — | `image` \| `video` \| `document` \| `other` |
| `min_reclaimable` | integer | — | Minimum reclaimable bytes |
| `limit` | integer | 50 | Max results |
| `offset` | integer | 0 | Pagination offset |

**Response `200`:**

```json
{
  "items": [
    {
      "id": 123,
      "content_hash": "a3f2c1d4e5b6...",
      "file_size": 4831838,
      "file_count": 3,
      "reclaimable_bytes": 9663676,
      "file_type": "image",
      "status": "unresolved",
      "thumbnail_url": "/api/groups/123/thumbnail",
      "created_at": "2026-01-10T08:00:00Z",
      "updated_at": "2026-02-18T03:14:00Z"
    }
  ],
  "total": 1204,
  "limit": 50,
  "offset": 0
}
```

---

### `GET /api/groups/:id`

Single group with all file copies.

**Response `200`:**

```json
{
  "id": 123,
  "content_hash": "a3f2c1d4e5b6...",
  "file_size": 4831838,
  "file_count": 3,
  "reclaimable_bytes": 9663676,
  "file_type": "image",
  "status": "unresolved",
  "files": [
    {
      "id": 456,
      "path": "/volume1/photos/2023/IMG_001.jpg",
      "size": 4831838,
      "mtime": "2023-06-15T14:22:00Z",
      "file_type": "image",
      "thumbnail_url": "/api/files/456/thumbnail",
      "preview_url": "/api/files/456/preview"
    },
    {
      "id": 457,
      "path": "/volume1/backup/photos/IMG_001.jpg",
      "size": 4831838,
      "mtime": "2023-06-15T14:22:00Z",
      "file_type": "image",
      "thumbnail_url": "/api/files/457/thumbnail",
      "preview_url": "/api/files/457/preview"
    }
  ],
  "created_at": "2026-01-10T08:00:00Z",
  "updated_at": "2026-02-18T03:14:00Z"
}
```

**Response `404`** — group not found.

---

### `POST /api/groups/:id/delete`

Delete selected files from a group. Triggers pre-deletion validation (§8.1 of requirements)
before moving anything to trash. All-or-nothing: if any file fails validation, nothing is deleted.

**Request:**

```json
{
  "delete_file_ids": [456, 457]
}
```

At least one file in the group must be kept. The server validates this; if all file IDs in
the group are submitted, the request is rejected.

**Response `200`** — files moved to trash:

```json
{
  "trashed": [
    {
      "file_id": 456,
      "trash_id": 789,
      "original_path": "/volume1/photos/2023/IMG_001.jpg",
      "expires_at": "2026-03-27T10:30:00Z"
    }
  ],
  "group": {
    "id": 123,
    "file_count": 1,
    "reclaimable_bytes": 0,
    "status": "resolved"
  }
}
```

**Response `400`** — all files submitted (no keeper):

```json
{
  "error": {
    "code": "NO_KEEPER",
    "message": "At least one file must be kept in the group"
  }
}
```

**Response `409`** — pre-deletion validation failed:

```json
{
  "error": {
    "code": "VALIDATION_FAILED",
    "message": "One or more files have changed since the last scan. Please re-scan.",
    "failures": [
      {
        "file_id": 457,
        "path": "/volume1/backup/photos/IMG_001.jpg",
        "reason": "FILE_MODIFIED"
      }
    ]
  }
}
```

---

### `POST /api/groups/:id/ignore`

Whitelist a group, its content hash, or a directory.

**Request:**

```json
{
  "type": "hash"
}
```

```json
{
  "type": "path_pair"
}
```

```json
{
  "type": "dir",
  "path": "/volume1/photos/originals"
}
```

- `type: "hash"` — suppresses all files with this content hash forever
- `type: "path_pair"` — suppresses this exact set of file paths
- `type: "dir"` — adds the given path to scan exclusions; `path` is required

**Response `200`:**

```json
{
  "whitelist_id": 12,
  "type": "hash",
  "value": "a3f2c1d4e5b6...",
  "group": {
    "id": 123,
    "status": "ignored"
  }
}
```

**Response `400`** — missing `path` when `type` is `dir`.

---

### `GET /api/groups/:id/thumbnail`

Returns a JPEG thumbnail for the group (derived from the first image/video file in the group).
Used for the groups list lazy-loading.

**Response `200`:** `Content-Type: image/jpeg`

**Response `404`** — group has no previewable file (e.g. document group).

---

### `GET /api/files/:id/thumbnail`

Returns a JPEG thumbnail for a specific file. For images: resized to 400×400 max.
For video: poster frame at 1s.

**Response `200`:** `Content-Type: image/jpeg`

**Response `404`** — file not found or not previewable.

---

### `GET /api/files/:id/preview`

Returns the full file content for lightbox display. Only supported for images and video.

**Response `200`:** `Content-Type: image/*` or `video/*` (derived from file extension)

**Response `404`** — file not found or not previewable.

---

### `GET /api/trash`

Active trash items (status = `trashed`), sorted by `trashed_at` descending.

**Query params:** `limit`, `offset`

**Response `200`:**

```json
{
  "items": [
    {
      "id": 789,
      "original_path": "/volume1/photos/2023/IMG_001.jpg",
      "file_size": 4831838,
      "trashed_at": "2026-02-25T10:30:00Z",
      "expires_at": "2026-03-27T10:30:00Z",
      "days_remaining": 30,
      "group_id": 123
    }
  ],
  "total": 42,
  "total_size": 183820384,
  "limit": 50,
  "offset": 0
}
```

---

### `POST /api/trash/:id/restore`

Restore a file from trash to its original path.

**Request:** no body required.

**Response `200`:**

```json
{
  "id": 789,
  "original_path": "/volume1/photos/2023/IMG_001.jpg",
  "status": "restored",
  "restored_at": "2026-02-25T11:00:00Z"
}
```

**Response `404`** — trash item not found or already purged/restored.

**Response `409`** — original path already occupied by another file:

```json
{
  "error": {
    "code": "RESTORE_PATH_CONFLICT",
    "message": "A file already exists at the original path",
    "path": "/volume1/photos/2023/IMG_001.jpg"
  }
}
```

---

### `DELETE /api/trash`

Purge all active trash items immediately. Requires explicit confirmation in the request body.

**Request:**

```json
{
  "confirm": true
}
```

**Response `200`:**

```json
{
  "purged_count": 42,
  "bytes_freed": 183820384
}
```

**Response `400`** — `confirm` not `true`:

```json
{
  "error": {
    "code": "CONFIRMATION_REQUIRED",
    "message": "Set confirm: true to proceed with purge"
  }
}
```

---

### `GET /api/stats`

Historical trend data for dashboard charts and all-time deletion totals.

**Response `200`:**

```json
{
  "snapshots": [
    {
      "scan_id": 40,
      "snapshot_at": "2026-02-11T03:00:00Z",
      "duplicate_groups": 1350,
      "duplicate_files": 4200,
      "reclaimable_bytes": 18000000000,
      "cumulative_deleted_files": 120,
      "cumulative_reclaimed_bytes": 5000000000
    },
    {
      "scan_id": 41,
      "snapshot_at": "2026-02-18T03:14:22Z",
      "duplicate_groups": 1204,
      "duplicate_files": 3891,
      "reclaimable_bytes": 15234567890,
      "cumulative_deleted_files": 145,
      "cumulative_reclaimed_bytes": 6200000000
    }
  ],
  "totals": {
    "deleted_files": 145,
    "reclaimed_bytes": 6200000000,
    "deleted_files_30d": 23,
    "reclaimed_bytes_30d": 980000000
  }
}
```

---

### `GET /api/config`

Current effective configuration (config.yaml defaults merged with settings table overrides).

**Response `200`:**

```json
{
  "scan_paths": ["/volume1/photos", "/volume1/documents"],
  "exclude_paths": ["/volume1/photos/originals"],
  "schedule": "0 2 * * 0",
  "scan_paused": false,
  "trash_retention_days": 30,
  "scan_workers": {
    "walkers": 4,
    "partial_hashers": 4,
    "full_hashers": 2
  }
}
```

---

### `PATCH /api/config`

Update one or more runtime settings. Only the fields listed below are writable via the API;
`scan_paths` and `exclude_paths` require editing `config.yaml` (v1 limitation).

**Writable fields:**

| Field | Type | Description |
|---|---|---|
| `schedule` | string | Cron expression |
| `scan_paused` | boolean | Pause/resume scheduled scans |
| `trash_retention_days` | integer | Days before auto-purge (min: 1, max: 365) |
| `scan_workers.walkers` | integer | Walker goroutine count (min: 1, max: 16) |
| `scan_workers.partial_hashers` | integer | Partial hash workers (min: 1, max: 16) |
| `scan_workers.full_hashers` | integer | Full hash workers (min: 1, max: 16) |

**Request** (send only fields to change):

```json
{
  "schedule": "0 3 * * 0",
  "scan_paused": true
}
```

**Response `200`:** full updated config object (same shape as `GET /api/config`).

**Response `400`** — invalid cron expression or out-of-range value:

```json
{
  "error": {
    "code": "INVALID_CONFIG",
    "message": "Invalid cron expression: '0 25 * * 0'"
  }
}
```

---

## 3. Error Code Reference

| Code | HTTP | Description |
|---|---|---|
| `SCAN_ALREADY_RUNNING` | 409 | Tried to start a scan while one is in progress |
| `NO_ACTIVE_SCAN` | 404 | Tried to cancel when no scan is running |
| `VALIDATION_FAILED` | 409 | Pre-deletion validation failed (files changed/missing) |
| `NO_KEEPER` | 400 | All files in group submitted for deletion |
| `RESTORE_PATH_CONFLICT` | 409 | Restore target path already occupied |
| `CONFIRMATION_REQUIRED` | 400 | Purge all called without `confirm: true` |
| `INVALID_CONFIG` | 400 | Invalid config value (bad cron, out-of-range integer) |
| `NOT_FOUND` | 404 | Generic resource not found |
| `INTERNAL_ERROR` | 500 | Unexpected server error |

---

## 4. Polling Contract

The UI polls `GET /api/status` every **2 seconds** while a scan is active. The response
`active_scan` field is `null` when idle. The UI transitions from "scanning" to "complete"
state when `active_scan` becomes `null` and `last_completed_scan.id` has changed.

No WebSockets or Server-Sent Events are needed in v1.

---

## 5. Media Endpoints Summary

| Endpoint | Returns | Notes |
|---|---|---|
| `GET /api/groups/:id/thumbnail` | JPEG | Thumbnail for groups list; 400×400 max |
| `GET /api/files/:id/thumbnail` | JPEG | Per-file thumbnail; 400×400 max |
| `GET /api/files/:id/preview` | image/* or video/* | Full file for lightbox; video served with range support |
