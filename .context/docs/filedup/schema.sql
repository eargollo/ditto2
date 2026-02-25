-- =============================================================================
-- Ditto — SQLite schema
-- WAL mode + foreign keys are enabled at connection time by the application:
--   PRAGMA journal_mode = WAL;
--   PRAGMA foreign_keys = ON;
--   PRAGMA busy_timeout = 5000;
--   PRAGMA synchronous = NORMAL;
--   PRAGMA cache_size = -64000;   -- 64 MB page cache
-- =============================================================================

-- =============================================================================
-- TABLE: scan_history
-- Must be created first — referenced by most other tables.
-- Doubles as the live progress record during an active scan via progress_* cols.
-- =============================================================================
CREATE TABLE IF NOT EXISTS scan_history (
    id                          INTEGER PRIMARY KEY AUTOINCREMENT,
    started_at                  INTEGER NOT NULL,
    finished_at                 INTEGER,
    status                      TEXT    NOT NULL DEFAULT 'running'
                                    CHECK (status IN ('running','completed','failed','cancelled')),
    triggered_by                TEXT    NOT NULL DEFAULT 'schedule'
                                    CHECK (triggered_by IN ('schedule','manual')),
    files_discovered            INTEGER NOT NULL DEFAULT 0,
    files_hashed                INTEGER NOT NULL DEFAULT 0,
    cache_hits                  INTEGER NOT NULL DEFAULT 0,  -- candidates reused from file_cache (no I/O)
    cache_misses                INTEGER NOT NULL DEFAULT 0,  -- candidates that required actual hashing
    duplicate_groups            INTEGER NOT NULL DEFAULT 0,
    duplicate_files             INTEGER NOT NULL DEFAULT 0,
    reclaimable_bytes           INTEGER NOT NULL DEFAULT 0,
    errors                      INTEGER NOT NULL DEFAULT 0,
    duration_seconds            INTEGER,
    -- Live progress counters updated ~1/sec by the reporter goroutine (§5.6)
    progress_candidates_found   INTEGER NOT NULL DEFAULT 0,
    progress_partial_hashed     INTEGER NOT NULL DEFAULT 0,
    progress_full_hashed        INTEGER NOT NULL DEFAULT 0,
    progress_bytes_read         INTEGER NOT NULL DEFAULT 0,
    created_at                  INTEGER NOT NULL
) STRICT;

CREATE INDEX IF NOT EXISTS idx_scan_history_started_at
    ON scan_history (started_at DESC);

CREATE INDEX IF NOT EXISTS idx_scan_history_running
    ON scan_history (status)
    WHERE status = 'running';


-- =============================================================================
-- TABLE: file_cache
-- Incremental scan cache: (path, size, mtime) → full_hash.
-- Unchanged files are not re-hashed on subsequent scans.
-- =============================================================================
CREATE TABLE IF NOT EXISTS file_cache (
    path        TEXT    NOT NULL,
    size        INTEGER NOT NULL,
    mtime       INTEGER NOT NULL,   -- Unix epoch seconds
    full_hash   TEXT    NOT NULL,   -- hex SHA-256 of full file content
    cached_at   INTEGER NOT NULL,   -- Unix epoch
    scan_id     INTEGER,            -- last scan that wrote this row

    PRIMARY KEY (path),
    FOREIGN KEY (scan_id) REFERENCES scan_history(id) ON DELETE SET NULL
) STRICT;

CREATE INDEX IF NOT EXISTS idx_file_cache_lookup
    ON file_cache (path, size, mtime);

CREATE INDEX IF NOT EXISTS idx_file_cache_scan_id
    ON file_cache (scan_id);


-- =============================================================================
-- TABLE: duplicate_groups
-- Persistent groups identified by content_hash.
-- Created once (INSERT OR IGNORE); user actions (ignore/resolve) survive re-scans.
-- file_count, reclaimable_bytes, file_type refreshed by each scan.
-- =============================================================================
CREATE TABLE IF NOT EXISTS duplicate_groups (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    content_hash        TEXT    NOT NULL UNIQUE,
    file_size           INTEGER NOT NULL,
    file_count          INTEGER NOT NULL DEFAULT 0,
    reclaimable_bytes   INTEGER NOT NULL DEFAULT 0,  -- (file_count - 1) * file_size
    file_type           TEXT    NOT NULL DEFAULT 'other'
                            CHECK (file_type IN ('image','video','document','other')),
    status              TEXT    NOT NULL DEFAULT 'unresolved'
                            CHECK (status IN ('unresolved','ignored','resolved')),
    ignored_at          INTEGER,
    resolved_at         INTEGER,
    first_seen_scan_id  INTEGER,
    last_seen_scan_id   INTEGER,
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL,

    FOREIGN KEY (first_seen_scan_id) REFERENCES scan_history(id) ON DELETE SET NULL,
    FOREIGN KEY (last_seen_scan_id)  REFERENCES scan_history(id) ON DELETE SET NULL
) STRICT;

-- Primary API query: filter by status+type, sort by reclaimable_bytes DESC
CREATE INDEX IF NOT EXISTS idx_groups_filter_sort
    ON duplicate_groups (status, file_type, reclaimable_bytes DESC);

CREATE INDEX IF NOT EXISTS idx_groups_content_hash
    ON duplicate_groups (content_hash);


-- =============================================================================
-- TABLE: duplicate_files
-- Current file paths per group. Replaced wholesale each scan:
--   DELETE WHERE group_id IN (groups touched this scan), then bulk INSERT.
-- Groups not touched by a scan retain their previous rows unchanged.
-- =============================================================================
CREATE TABLE IF NOT EXISTS duplicate_files (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    group_id    INTEGER NOT NULL,
    scan_id     INTEGER NOT NULL,
    path        TEXT    NOT NULL,
    size        INTEGER NOT NULL,
    mtime       INTEGER NOT NULL,   -- Unix epoch; used for pre-deletion validation
    file_type   TEXT    NOT NULL DEFAULT 'other'
                    CHECK (file_type IN ('image','video','document','other')),

    FOREIGN KEY (group_id) REFERENCES duplicate_groups(id) ON DELETE CASCADE,
    FOREIGN KEY (scan_id)  REFERENCES scan_history(id)     ON DELETE CASCADE,
    UNIQUE (path)
) STRICT;

CREATE INDEX IF NOT EXISTS idx_dup_files_group_id
    ON duplicate_files (group_id);

CREATE INDEX IF NOT EXISTS idx_dup_files_path
    ON duplicate_files (path);

CREATE INDEX IF NOT EXISTS idx_dup_files_scan_id
    ON duplicate_files (scan_id);


-- =============================================================================
-- TABLE: scan_errors
-- Non-fatal per-file errors during a scan. Scan continues on error.
-- =============================================================================
CREATE TABLE IF NOT EXISTS scan_errors (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    scan_id     INTEGER NOT NULL,
    path        TEXT    NOT NULL,
    stage       TEXT    NOT NULL
                    CHECK (stage IN ('walk','partial_hash','full_hash')),
    error       TEXT    NOT NULL,
    occurred_at INTEGER NOT NULL,

    FOREIGN KEY (scan_id) REFERENCES scan_history(id) ON DELETE CASCADE
) STRICT;

CREATE INDEX IF NOT EXISTS idx_scan_errors_scan_id
    ON scan_errors (scan_id);


-- =============================================================================
-- TABLE: scan_snapshots
-- One row per completed scan for trend charts.
-- Cumulative totals denormalised here to avoid aggregation at query time.
-- =============================================================================
CREATE TABLE IF NOT EXISTS scan_snapshots (
    id                          INTEGER PRIMARY KEY AUTOINCREMENT,
    scan_id                     INTEGER NOT NULL UNIQUE,
    snapshot_at                 INTEGER NOT NULL,
    duplicate_groups            INTEGER NOT NULL,
    duplicate_files             INTEGER NOT NULL,
    reclaimable_bytes           INTEGER NOT NULL,
    cumulative_deleted_files    INTEGER NOT NULL,
    cumulative_reclaimed_bytes  INTEGER NOT NULL,

    FOREIGN KEY (scan_id) REFERENCES scan_history(id) ON DELETE CASCADE
) STRICT;

CREATE INDEX IF NOT EXISTS idx_scan_snapshots_at
    ON scan_snapshots (snapshot_at ASC);


-- =============================================================================
-- TABLE: trash
-- Files moved to /data/trash/ pending auto-purge.
-- Rows are never deleted; status tracks outcome (trashed → restored/purged).
-- =============================================================================
CREATE TABLE IF NOT EXISTS trash (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    group_id        INTEGER,                -- nullable: group may be cleaned up later
    original_path   TEXT    NOT NULL,
    trash_path      TEXT    NOT NULL UNIQUE,
    file_size       INTEGER NOT NULL,
    content_hash    TEXT    NOT NULL,
    trashed_at      INTEGER NOT NULL,
    expires_at      INTEGER NOT NULL,       -- trashed_at + retention_days * 86400
    status          TEXT    NOT NULL DEFAULT 'trashed'
                        CHECK (status IN ('trashed','restored','purged')),
    restored_at     INTEGER,
    purged_at       INTEGER,
    purge_trigger   TEXT    CHECK (purge_trigger IN ('user','auto',NULL)),
    scan_id         INTEGER,

    FOREIGN KEY (group_id) REFERENCES duplicate_groups(id) ON DELETE SET NULL,
    FOREIGN KEY (scan_id)  REFERENCES scan_history(id)     ON DELETE SET NULL
) STRICT;

-- Auto-purge job: active items past expiry
CREATE INDEX IF NOT EXISTS idx_trash_expires_at
    ON trash (expires_at)
    WHERE status = 'trashed';

-- Trash view: active items newest first
CREATE INDEX IF NOT EXISTS idx_trash_status_date
    ON trash (status, trashed_at DESC);

-- Restore lookup by original path
CREATE INDEX IF NOT EXISTS idx_trash_original_path
    ON trash (original_path)
    WHERE status = 'trashed';


-- =============================================================================
-- TABLE: deletion_log
-- Append-only permanent record of every file purged from trash.
-- Never pruned. Drives all-time stats and cumulative snapshot values.
-- =============================================================================
CREATE TABLE IF NOT EXISTS deletion_log (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    deleted_at      INTEGER NOT NULL,
    original_path   TEXT    NOT NULL,
    file_size       INTEGER NOT NULL,
    content_hash    TEXT    NOT NULL,
    trigger         TEXT    NOT NULL
                        CHECK (trigger IN ('user','auto')),
    trash_id        INTEGER             -- soft reference to trash.id (no FK)
) STRICT;

CREATE INDEX IF NOT EXISTS idx_deletion_log_deleted_at
    ON deletion_log (deleted_at DESC);


-- =============================================================================
-- TABLE: whitelist
-- Suppression rules. Three types:
--   'hash'      value = hex SHA-256
--   'path_pair' value = canonical JSON array of sorted absolute paths
--   'dir'       value = absolute directory path
-- =============================================================================
CREATE TABLE IF NOT EXISTS whitelist (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    type        TEXT    NOT NULL
                    CHECK (type IN ('hash','path_pair','dir')),
    value       TEXT    NOT NULL,
    added_by    TEXT    NOT NULL DEFAULT 'user'
                    CHECK (added_by IN ('user','config')),
    added_at    INTEGER NOT NULL,
    note        TEXT,

    UNIQUE (type, value)
) STRICT;

CREATE INDEX IF NOT EXISTS idx_whitelist_hash
    ON whitelist (value)
    WHERE type = 'hash';

CREATE INDEX IF NOT EXISTS idx_whitelist_dir
    ON whitelist (value)
    WHERE type = 'dir';


-- =============================================================================
-- TABLE: settings
-- Key-value store for UI runtime overrides (schedule, paused state, etc.).
-- Overlays config.yaml defaults at startup. Deleting a row resets to default.
-- =============================================================================
CREATE TABLE IF NOT EXISTS settings (
    key         TEXT    PRIMARY KEY,
    value       TEXT    NOT NULL,
    updated_at  INTEGER NOT NULL
) STRICT;
