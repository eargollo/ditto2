package scan

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	internaldb "github.com/eargollo/ditto/internal/db"
)

// mustOpenDB opens a temp file SQLite database with the full schema applied.
func mustOpenDB(tb testing.TB) *sql.DB {
	tb.Helper()
	dbPath := filepath.Join(tb.TempDir(), "test.db")
	db, err := internaldb.Open(dbPath)
	if err != nil {
		tb.Fatalf("open test DB: %v", err)
	}
	if err := internaldb.RunMigrations(db); err != nil {
		db.Close()
		tb.Fatalf("run migrations: %v", err)
	}
	tb.Cleanup(func() { db.Close() })
	return db
}

// mustInsertScan inserts a scan_history row and returns its ID.
func mustInsertScan(tb testing.TB, db *sql.DB) int64 {
	tb.Helper()
	now := time.Now().Unix()
	res, err := db.Exec(
		`INSERT INTO scan_history (started_at, status, triggered_by, created_at) VALUES (?, 'running', 'manual', ?)`,
		now, now)
	if err != nil {
		tb.Fatalf("insert scan: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

// seedFileCache inserts n entries into file_cache keyed by index i:
// path=/cached/fileNNNN.txt, size=i*100+1, mtime=1000+i, hash=hashNNNN.
func seedFileCache(tb testing.TB, db *sql.DB, scanID int64, n int) {
	tb.Helper()
	for i := 0; i < n; i++ {
		_, err := db.Exec(
			`INSERT INTO file_cache (path, size, mtime, full_hash, cached_at, scan_id) VALUES (?, ?, ?, ?, ?, ?)`,
			fmt.Sprintf("/cached/file%04d.txt", i),
			int64(i*100+1),
			int64(1000+i),
			fmt.Sprintf("hash%04d", i),
			time.Now().Unix(),
			scanID,
		)
		if err != nil {
			tb.Fatalf("seed cache entry %d: %v", i, err)
		}
	}
}

// noErrors is an ErrorReporter that fails the test if invoked.
func noErrors(tb testing.TB) ErrorReporter {
	return func(path, stage, errMsg string) {
		tb.Errorf("unexpected scan error: path=%q stage=%q err=%q", path, stage, errMsg)
	}
}

// createSyntheticTree builds a flat-ish directory tree with numFiles files.
// Every 10th file shares identical content (1 KB), creating a ~10% duplicate
// rate. Returns numFiles.
func createSyntheticTree(tb testing.TB, root string, numFiles int) int {
	tb.Helper()
	for i := 0; i < numFiles; i++ {
		subdir := filepath.Join(root, fmt.Sprintf("dir%03d", i/50))
		if err := os.MkdirAll(subdir, 0755); err != nil {
			tb.Fatalf("mkdir %q: %v", subdir, err)
		}
		p := filepath.Join(subdir, fmt.Sprintf("file%04d.bin", i))
		// 1 KB content; every 10 files share the same content → duplicates.
		content := fmt.Sprintf("%-1024d", i%10)
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			tb.Fatalf("write %q: %v", p, err)
		}
	}
	return numFiles
}
