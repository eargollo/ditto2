-- +goose Up
-- +goose StatementBegin

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
    cache_hits                  INTEGER NOT NULL DEFAULT 0,
    cache_misses                INTEGER NOT NULL DEFAULT 0,
    duplicate_groups            INTEGER NOT NULL DEFAULT 0,
    duplicate_files             INTEGER NOT NULL DEFAULT 0,
    reclaimable_bytes           INTEGER NOT NULL DEFAULT 0,
    errors                      INTEGER NOT NULL DEFAULT 0,
    duration_seconds            INTEGER,
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


CREATE TABLE IF NOT EXISTS file_cache (
    path        TEXT    NOT NULL,
    size        INTEGER NOT NULL,
    mtime       INTEGER NOT NULL,
    full_hash   TEXT    NOT NULL,
    cached_at   INTEGER NOT NULL,
    scan_id     INTEGER,

    PRIMARY KEY (path),
    FOREIGN KEY (scan_id) REFERENCES scan_history(id) ON DELETE SET NULL
) STRICT;

CREATE INDEX IF NOT EXISTS idx_file_cache_lookup
    ON file_cache (path, size, mtime);

CREATE INDEX IF NOT EXISTS idx_file_cache_scan_id
    ON file_cache (scan_id);


CREATE TABLE IF NOT EXISTS duplicate_groups (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    content_hash        TEXT    NOT NULL UNIQUE,
    file_size           INTEGER NOT NULL,
    file_count          INTEGER NOT NULL DEFAULT 0,
    reclaimable_bytes   INTEGER NOT NULL DEFAULT 0,
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

CREATE INDEX IF NOT EXISTS idx_groups_filter_sort
    ON duplicate_groups (status, file_type, reclaimable_bytes DESC);

CREATE INDEX IF NOT EXISTS idx_groups_content_hash
    ON duplicate_groups (content_hash);


CREATE TABLE IF NOT EXISTS duplicate_files (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    group_id    INTEGER NOT NULL,
    scan_id     INTEGER NOT NULL,
    path        TEXT    NOT NULL,
    size        INTEGER NOT NULL,
    mtime       INTEGER NOT NULL,
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


CREATE TABLE IF NOT EXISTS trash (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    group_id        INTEGER,
    original_path   TEXT    NOT NULL,
    trash_path      TEXT    NOT NULL UNIQUE,
    file_size       INTEGER NOT NULL,
    content_hash    TEXT    NOT NULL,
    trashed_at      INTEGER NOT NULL,
    expires_at      INTEGER NOT NULL,
    status          TEXT    NOT NULL DEFAULT 'trashed'
                        CHECK (status IN ('trashed','restored','purged')),
    restored_at     INTEGER,
    purged_at       INTEGER,
    purge_trigger   TEXT    CHECK (purge_trigger IN ('user','auto',NULL)),
    scan_id         INTEGER,

    FOREIGN KEY (group_id) REFERENCES duplicate_groups(id) ON DELETE SET NULL,
    FOREIGN KEY (scan_id)  REFERENCES scan_history(id)     ON DELETE SET NULL
) STRICT;

CREATE INDEX IF NOT EXISTS idx_trash_expires_at
    ON trash (expires_at)
    WHERE status = 'trashed';

CREATE INDEX IF NOT EXISTS idx_trash_status_date
    ON trash (status, trashed_at DESC);

CREATE INDEX IF NOT EXISTS idx_trash_original_path
    ON trash (original_path)
    WHERE status = 'trashed';


CREATE TABLE IF NOT EXISTS deletion_log (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    deleted_at      INTEGER NOT NULL,
    original_path   TEXT    NOT NULL,
    file_size       INTEGER NOT NULL,
    content_hash    TEXT    NOT NULL,
    trigger         TEXT    NOT NULL
                        CHECK (trigger IN ('user','auto')),
    trash_id        INTEGER
) STRICT;

CREATE INDEX IF NOT EXISTS idx_deletion_log_deleted_at
    ON deletion_log (deleted_at DESC);


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


CREATE TABLE IF NOT EXISTS settings (
    key         TEXT    PRIMARY KEY,
    value       TEXT    NOT NULL,
    updated_at  INTEGER NOT NULL
) STRICT;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS settings;
DROP TABLE IF EXISTS whitelist;
DROP TABLE IF EXISTS deletion_log;
DROP TABLE IF EXISTS trash;
DROP TABLE IF EXISTS scan_snapshots;
DROP TABLE IF EXISTS scan_errors;
DROP TABLE IF EXISTS duplicate_files;
DROP TABLE IF EXISTS duplicate_groups;
DROP TABLE IF EXISTS file_cache;
DROP TABLE IF EXISTS scan_history;
-- +goose StatementEnd
