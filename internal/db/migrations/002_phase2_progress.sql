-- +goose Up
ALTER TABLE scan_history ADD COLUMN progress_groups_written INTEGER NOT NULL DEFAULT 0;
ALTER TABLE scan_history ADD COLUMN progress_groups_total   INTEGER NOT NULL DEFAULT 0;
ALTER TABLE scan_history ADD COLUMN phase2_started_at       INTEGER NOT NULL DEFAULT 0;

-- +goose Down
SELECT 1; -- SQLite does not support DROP COLUMN; leave columns in place.
