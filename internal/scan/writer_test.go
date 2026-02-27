package scan

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// TestRunDBWriterFindsDuplicates verifies that RunDBWriter groups files by
// hash and writes duplicate_groups rows for hashes that appear ≥2 times.
func TestRunDBWriterFindsDuplicates(t *testing.T) {
	db := mustOpenDB(t)
	scanID := mustInsertScan(t, db)

	// 50 files with 5 distinct hashes → 5 groups of 10 files each.
	const (
		numFiles  = 50
		numHashes = 5
	)
	in := make(chan HashedFile, numFiles)
	for i := 0; i < numFiles; i++ {
		in <- HashedFile{
			FileInfo: FileInfo{
				Path:  fmt.Sprintf("/vol1/file%04d.txt", i),
				Size:  1024,
				MTime: time.Unix(1000, 0),
			},
			Hash: fmt.Sprintf("deadbeef%04d", i%numHashes),
		}
	}
	close(in)

	stats, err := RunDBWriter(context.Background(), db, scanID, 100, in, nil)
	if err != nil {
		t.Fatalf("RunDBWriter: %v", err)
	}

	if stats.DuplicateGroups != numHashes {
		t.Errorf("DuplicateGroups: got %d, want %d", stats.DuplicateGroups, numHashes)
	}
	if stats.DuplicateFiles != numFiles {
		t.Errorf("DuplicateFiles: got %d, want %d", stats.DuplicateFiles, numFiles)
	}

	// All files must be in file_cache.
	var cacheCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM file_cache WHERE scan_id = ?`, scanID).Scan(&cacheCount); err != nil {
		t.Fatalf("count file_cache: %v", err)
	}
	if cacheCount != numFiles {
		t.Errorf("file_cache entries: got %d, want %d", cacheCount, numFiles)
	}
}

// TestProgressiveCacheUpdateSurvivesCancellation verifies that file_cache is
// populated even when the context is cancelled before the scan completes.
// This ensures partial hashing work is preserved for subsequent scans.
func TestProgressiveCacheUpdateSurvivesCancellation(t *testing.T) {
	db := mustOpenDB(t)
	scanID := mustInsertScan(t, db)

	// Use batchSize=100; send 150 items to trigger one mid-scan flush (100)
	// plus a final flush (50) on cancel.
	const (
		numItems  = 150
		batchSize = 100
	)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel: RunDBWriter should still persist the cache

	in := make(chan HashedFile, numItems)
	for i := 0; i < numItems; i++ {
		in <- HashedFile{
			FileInfo: FileInfo{
				Path:  fmt.Sprintf("/vol1/file%04d.txt", i),
				Size:  int64(i + 1),
				MTime: time.Unix(1000, 0),
			},
			Hash: fmt.Sprintf("hash%02d", i%10), // 10 distinct hashes
		}
	}
	close(in)

	_, err := RunDBWriter(ctx, db, scanID, batchSize, in, nil)
	if err == nil {
		t.Fatal("expected a non-nil error from cancelled context, got nil")
	}

	// Despite cancellation, ALL hashed files must be in file_cache.
	var cacheCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM file_cache WHERE scan_id = ?`, scanID).Scan(&cacheCount); err != nil {
		t.Fatalf("count file_cache: %v", err)
	}
	if cacheCount != numItems {
		t.Errorf("file_cache entries after cancel: got %d, want %d", cacheCount, numItems)
	}

	// duplicate_groups must be empty — persistGroups is skipped on cancel.
	var groupCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM duplicate_groups`).Scan(&groupCount); err != nil {
		t.Fatalf("count duplicate_groups: %v", err)
	}
	if groupCount != 0 {
		t.Errorf("duplicate_groups: got %d after cancel, want 0", groupCount)
	}
}
