package scan

import (
	"context"
	"database/sql"
	"time"
)

// FileInfo is a filesystem entry emitted by the Walker.
type FileInfo struct {
	Path  string
	Size  int64
	MTime time.Time
}

// HashedFile is a FileInfo with a computed hash.
type HashedFile struct {
	FileInfo
	Hash string
}

// Config holds tuning parameters for the scan pipeline.
type Config struct {
	Walkers        int
	PartialHashers int
	FullHashers    int
	BatchSize      int
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		Walkers:        4,
		PartialHashers: 4,
		FullHashers:    2,
		BatchSize:      1000,
	}
}

// Scanner orchestrates the full duplicate-detection pipeline.
type Scanner struct {
	db   *sql.DB
	cfg  Config
	root []string
}

// New creates a Scanner.
func New(db *sql.DB, roots []string, cfg Config) *Scanner {
	return &Scanner{db: db, cfg: cfg, root: roots}
}

// Run executes a scan and blocks until done or ctx is cancelled.
// Returns the scan_history row ID for the completed scan.
// Stub — pipeline not yet implemented.
func (s *Scanner) Run(ctx context.Context, triggeredBy string) (int64, error) {
	return 0, errNotImplemented("scan pipeline")
}

// errNotImplemented returns a standard "not yet implemented" error.
func errNotImplemented(what string) error {
	return &notImplementedError{what: what}
}

type notImplementedError struct{ what string }

func (e *notImplementedError) Error() string {
	return e.what + ": not yet implemented"
}
