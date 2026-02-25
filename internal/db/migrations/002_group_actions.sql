-- +goose Up
-- +goose StatementBegin

-- Widen duplicate_groups.status to add 'watching' and 'watching_alert'.
-- SQLite does not support modifying CHECK constraints in-place.
CREATE TABLE duplicate_groups_new (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    content_hash        TEXT    NOT NULL UNIQUE,
    file_size           INTEGER NOT NULL,
    file_count          INTEGER NOT NULL DEFAULT 0,
    reclaimable_bytes   INTEGER NOT NULL DEFAULT 0,
    file_type           TEXT    NOT NULL DEFAULT 'other'
                            CHECK (file_type IN ('image','video','document','other')),
    status              TEXT    NOT NULL DEFAULT 'unresolved'
                            CHECK (status IN ('unresolved','ignored','resolved','watching','watching_alert')),
    ignored_at          INTEGER,
    resolved_at         INTEGER,
    first_seen_scan_id  INTEGER,
    last_seen_scan_id   INTEGER,
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL,

    FOREIGN KEY (first_seen_scan_id) REFERENCES scan_history(id) ON DELETE SET NULL,
    FOREIGN KEY (last_seen_scan_id)  REFERENCES scan_history(id) ON DELETE SET NULL
) STRICT;

INSERT INTO duplicate_groups_new SELECT * FROM duplicate_groups;

DROP TABLE duplicate_groups;
ALTER TABLE duplicate_groups_new RENAME TO duplicate_groups;

CREATE INDEX IF NOT EXISTS idx_groups_filter_sort
    ON duplicate_groups (status, file_type, reclaimable_bytes DESC);

CREATE INDEX IF NOT EXISTS idx_groups_content_hash
    ON duplicate_groups (content_hash);

-- Add expected_hash to whitelist for path_pair watch support.
ALTER TABLE whitelist ADD COLUMN expected_hash TEXT;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

CREATE TABLE duplicate_groups_old (
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

INSERT INTO duplicate_groups_old
    SELECT * FROM duplicate_groups
    WHERE status IN ('unresolved','ignored','resolved');

DROP TABLE duplicate_groups;
ALTER TABLE duplicate_groups_old RENAME TO duplicate_groups;

CREATE INDEX IF NOT EXISTS idx_groups_filter_sort
    ON duplicate_groups (status, file_type, reclaimable_bytes DESC);

CREATE INDEX IF NOT EXISTS idx_groups_content_hash
    ON duplicate_groups (content_hash);

-- +goose StatementEnd
