package trash

import (
	"context"
	"database/sql"
)

// Manager handles moving files to/from/purging the trash directory.
type Manager struct {
	db       *sql.DB
	trashDir string
}

// New creates a trash Manager.
func New(db *sql.DB, trashDir string) *Manager {
	return &Manager{db: db, trashDir: trashDir}
}

// MoveToTrash moves the file at originalPath into the trash directory and
// records it in the trash table with the given expiry.
// Stub — not yet implemented.
func (m *Manager) MoveToTrash(ctx context.Context, originalPath string, groupID int64, retentionDays int) (int64, error) {
	return 0, errNotImplemented("MoveToTrash")
}

// Restore moves a trashed file back to its original path.
// Stub — not yet implemented.
func (m *Manager) Restore(ctx context.Context, trashID int64) error {
	return errNotImplemented("Restore")
}

// PurgeAll immediately purges all active trash items.
// Stub — not yet implemented.
func (m *Manager) PurgeAll(ctx context.Context) (count int64, bytesFreed int64, err error) {
	return 0, 0, errNotImplemented("PurgeAll")
}

// AutoPurge purges all trash items whose expires_at is in the past.
// Intended to be called by the scheduler.
// Stub — not yet implemented.
func (m *Manager) AutoPurge(ctx context.Context) error {
	return nil // no-op stub
}

func errNotImplemented(what string) error {
	return &notImplementedError{what: what}
}

type notImplementedError struct{ what string }

func (e *notImplementedError) Error() string {
	return e.what + ": not yet implemented"
}
