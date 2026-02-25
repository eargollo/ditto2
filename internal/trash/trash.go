package trash

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// ErrNotTrashed is returned when the item is not in 'trashed' state (not found,
// already purged, or already restored).
var ErrNotTrashed = errors.New("trash item not found or already purged/restored")

// ErrRestoreConflict is returned when the restore target path is already occupied.
type ErrRestoreConflict struct {
	Path string
}

func (e *ErrRestoreConflict) Error() string {
	return fmt.Sprintf("a file already exists at %q", e.Path)
}

// Manager handles moving files to/from/purging the trash directory.
type Manager struct {
	db       *sql.DB
	trashDir string
}

// New creates a trash Manager.
func New(db *sql.DB, trashDir string) *Manager {
	return &Manager{db: db, trashDir: trashDir}
}

// MoveToTrash moves the file at originalPath into the trash directory,
// records it in the trash table, and returns the new trash row ID.
// groupID == 0 is stored as NULL.
func (m *Manager) MoveToTrash(ctx context.Context, originalPath string, groupID int64, contentHash string, retentionDays int) (int64, error) {
	// Verify the file exists and capture its current size.
	info, err := os.Stat(originalPath)
	if err != nil {
		return 0, fmt.Errorf("stat %q: %w", originalPath, err)
	}
	fileSize := info.Size()

	// Build a unique trash path (trashDir/YYYY-MM-DD/<nanoseconds>_<basename>).
	trashPath := m.buildTrashPath(originalPath)

	// Ensure the date subdirectory exists.
	if err := os.MkdirAll(filepath.Dir(trashPath), 0o755); err != nil {
		return 0, fmt.Errorf("create trash subdir: %w", err)
	}

	// Move the file (with cross-device fallback).
	if err := moveFile(originalPath, trashPath); err != nil {
		return 0, fmt.Errorf("move to trash: %w", err)
	}

	// Persist to DB. On failure try to move the file back.
	now := time.Now()
	expiresAt := now.Add(time.Duration(retentionDays) * 24 * time.Hour)

	var gid interface{}
	if groupID != 0 {
		gid = groupID
	}

	res, err := m.db.ExecContext(ctx, `
		INSERT INTO trash
			(group_id, original_path, trash_path, file_size, content_hash,
			 trashed_at, expires_at, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'trashed')`,
		gid, originalPath, trashPath, fileSize, contentHash,
		now.Unix(), expiresAt.Unix())
	if err != nil {
		// Best-effort rollback.
		if rerr := moveFile(trashPath, originalPath); rerr != nil {
			slog.Error("rollback move-to-trash failed", "path", originalPath, "error", rerr)
		}
		return 0, fmt.Errorf("insert trash record: %w", err)
	}

	id, _ := res.LastInsertId()
	slog.Info("file trashed", "path", originalPath, "trash_id", id, "expires_at", expiresAt.Format(time.RFC3339))
	return id, nil
}

// Restore moves a trashed file back to its original path.
func (m *Manager) Restore(ctx context.Context, trashID int64) error {
	var originalPath, trashPath string
	err := m.db.QueryRowContext(ctx,
		`SELECT original_path, trash_path FROM trash WHERE id = ? AND status = 'trashed'`,
		trashID,
	).Scan(&originalPath, &trashPath)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotTrashed
	}
	if err != nil {
		return fmt.Errorf("lookup trash item %d: %w", trashID, err)
	}

	// Refuse if the original path is already occupied.
	if _, err := os.Stat(originalPath); err == nil {
		return &ErrRestoreConflict{Path: originalPath}
	}

	// Recreate any missing parent directories.
	if err := os.MkdirAll(filepath.Dir(originalPath), 0o755); err != nil {
		return fmt.Errorf("recreate restore dir: %w", err)
	}

	// Move back.
	if err := moveFile(trashPath, originalPath); err != nil {
		return fmt.Errorf("restore file: %w", err)
	}

	now := time.Now().Unix()
	if _, err := m.db.ExecContext(ctx,
		`UPDATE trash SET status='restored', restored_at=? WHERE id=?`,
		now, trashID,
	); err != nil {
		slog.Error("update trash status after restore", "trash_id", trashID, "error", err)
	}

	slog.Info("file restored", "path", originalPath, "trash_id", trashID)
	return nil
}

// PurgeAll immediately purges all active trash items (trigger = "user").
func (m *Manager) PurgeAll(ctx context.Context) (count int64, bytesFreed int64, err error) {
	rows, err := m.db.QueryContext(ctx,
		`SELECT id, original_path, trash_path, file_size, content_hash
		 FROM trash WHERE status = 'trashed'`)
	if err != nil {
		return 0, 0, fmt.Errorf("query trash: %w", err)
	}
	return m.purgeRows(ctx, rows, "user")
}

// AutoPurge purges all trash items whose expires_at is in the past (trigger = "auto").
// Intended to be called by the scheduler.
func (m *Manager) AutoPurge(ctx context.Context) error {
	rows, err := m.db.QueryContext(ctx,
		`SELECT id, original_path, trash_path, file_size, content_hash
		 FROM trash WHERE status = 'trashed' AND expires_at < ?`,
		time.Now().Unix())
	if err != nil {
		return fmt.Errorf("query expired trash: %w", err)
	}
	count, bytes, err := m.purgeRows(ctx, rows, "auto")
	if err != nil {
		return err
	}
	if count > 0 {
		slog.Info("auto-purge complete", "files_purged", count, "bytes_freed", bytes)
	}
	return nil
}

// ── private helpers ────────────────────────────────────────────────────────

// buildTrashPath returns a unique path inside trashDir for the given original file.
// Format: trashDir/YYYY-MM-DD/<unix_nano>_<basename>
func (m *Manager) buildTrashPath(originalPath string) string {
	now := time.Now()
	dateDir := now.Format("2006-01-02")
	basename := filepath.Base(originalPath)
	filename := fmt.Sprintf("%d_%s", now.UnixNano(), basename)
	return filepath.Join(m.trashDir, dateDir, filename)
}

type purgeItem struct {
	id           int64
	originalPath string
	trashPath    string
	fileSize     int64
	contentHash  string
}

func (m *Manager) purgeRows(ctx context.Context, rows *sql.Rows, trigger string) (count int64, bytesFreed int64, err error) {
	defer rows.Close()

	var items []purgeItem
	for rows.Next() {
		var it purgeItem
		if err := rows.Scan(&it.id, &it.originalPath, &it.trashPath, &it.fileSize, &it.contentHash); err != nil {
			return count, bytesFreed, fmt.Errorf("scan trash row: %w", err)
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return count, bytesFreed, err
	}

	now := time.Now().Unix()
	for _, it := range items {
		if ctx.Err() != nil {
			break
		}

		// Remove from disk; treat "already gone" as success.
		if rerr := os.Remove(it.trashPath); rerr != nil && !errors.Is(rerr, os.ErrNotExist) {
			slog.Warn("purge: remove file failed", "path", it.trashPath, "error", rerr)
			continue // leave DB row in 'trashed' to retry later
		}

		// Append-only deletion log.
		_, _ = m.db.ExecContext(ctx,
			`INSERT INTO deletion_log (deleted_at, original_path, file_size, content_hash, trigger, trash_id)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			now, it.originalPath, it.fileSize, it.contentHash, trigger, it.id)

		// Mark as purged.
		if _, dbErr := m.db.ExecContext(ctx,
			`UPDATE trash SET status='purged', purged_at=?, purge_trigger=? WHERE id=?`,
			now, trigger, it.id,
		); dbErr != nil {
			slog.Error("purge: update trash status", "trash_id", it.id, "error", dbErr)
		}

		count++
		bytesFreed += it.fileSize
	}

	return count, bytesFreed, nil
}

// moveFile tries os.Rename first; falls back to copy+delete on cross-device errors.
func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	} else if le, ok := err.(*os.LinkError); ok && errors.Is(le.Err, syscall.EXDEV) {
		return copyThenDelete(src, dst)
	} else {
		return err
	}
}

// copyThenDelete copies src to dst then removes src. dst is cleaned up on error.
func copyThenDelete(src, dst string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() {
		out.Close()
		if err != nil {
			os.Remove(dst)
		}
	}()

	if _, err = io.Copy(out, in); err != nil {
		return err
	}
	if err = out.Close(); err != nil {
		return err
	}
	in.Close()
	return os.Remove(src)
}
