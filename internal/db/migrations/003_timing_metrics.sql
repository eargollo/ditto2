-- +goose Up
ALTER TABLE scan_history ADD COLUMN disk_read_ms INTEGER NOT NULL DEFAULT 0;
ALTER TABLE scan_history ADD COLUMN db_read_ms   INTEGER NOT NULL DEFAULT 0;
ALTER TABLE scan_history ADD COLUMN db_write_ms  INTEGER NOT NULL DEFAULT 0;

-- +goose Down
SELECT 1; -- SQLite does not support DROP COLUMN; leave columns in place.
