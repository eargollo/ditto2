package scan

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// TestCacheCheckRoutesHitsAndMisses verifies that files matching file_cache
// entries go to hits, while unrecognised files go to misses.
func TestCacheCheckRoutesHitsAndMisses(t *testing.T) {
	db := mustOpenDB(t)
	scanID := mustInsertScan(t, db)
	const (
		numCached = 50
		numNew    = 50
	)
	seedFileCache(t, db, scanID, numCached)

	progress := &Progress{}
	in := make(chan FileInfo, numCached+numNew)
	hits := make(chan HashedFile, numCached+numNew)
	misses := make(chan FileInfo, numCached+numNew)

	RunCacheCheck(context.Background(), db, progress, 2, in, hits, misses)

	// Send files that are in cache.
	for i := 0; i < numCached; i++ {
		in <- FileInfo{
			Path:  fmt.Sprintf("/cached/file%04d.txt", i),
			Size:  int64(i*100 + 1),
			MTime: time.Unix(int64(1000+i), 0),
		}
	}
	// Send files that are NOT in cache.
	for i := 0; i < numNew; i++ {
		in <- FileInfo{Path: fmt.Sprintf("/new/file%04d.txt", i), Size: 1, MTime: time.Now()}
	}
	close(in)

	var gotHits, gotMisses int
	hitsDone := make(chan struct{})
	missesDone := make(chan struct{})

	go func() {
		for range hits {
			gotHits++
		}
		close(hitsDone)
	}()
	go func() {
		for range misses {
			gotMisses++
		}
		close(missesDone)
	}()
	<-hitsDone
	<-missesDone

	if gotHits != numCached {
		t.Errorf("hits: got %d, want %d", gotHits, numCached)
	}
	if gotMisses != numNew {
		t.Errorf("misses: got %d, want %d", gotMisses, numNew)
	}
	if progress.CacheHits.Load() != int64(numCached) {
		t.Errorf("CacheHits counter: got %d, want %d", progress.CacheHits.Load(), numCached)
	}
	if progress.CacheMisses.Load() != int64(numNew) {
		t.Errorf("CacheMisses counter: got %d, want %d", progress.CacheMisses.Load(), numNew)
	}
}

// TestCacheCheckAllHits sends only cached files and verifies zero misses.
func TestCacheCheckAllHits(t *testing.T) {
	db := mustOpenDB(t)
	scanID := mustInsertScan(t, db)
	const n = 30
	seedFileCache(t, db, scanID, n)

	progress := &Progress{}
	in := make(chan FileInfo, n)
	hits := make(chan HashedFile, n)
	misses := make(chan FileInfo, n)
	RunCacheCheck(context.Background(), db, progress, 1, in, hits, misses)

	for i := 0; i < n; i++ {
		in <- FileInfo{
			Path:  fmt.Sprintf("/cached/file%04d.txt", i),
			Size:  int64(i*100 + 1),
			MTime: time.Unix(int64(1000+i), 0),
		}
	}
	close(in)

	var gotHits, gotMisses int
	go func() {
		for range misses {
			gotMisses++
		}
	}()
	for range hits {
		gotHits++
	}

	if gotHits != n {
		t.Errorf("hits: got %d, want %d", gotHits, n)
	}
	if gotMisses != 0 {
		t.Errorf("unexpected misses: %d", gotMisses)
	}
}

// TestCacheCheckParallelConsistency runs the same workload with 1 and 4
// workers and verifies both produce the same hit/miss totals.
func TestCacheCheckParallelConsistency(t *testing.T) {
	const (
		numCached = 40
		numNew    = 40
	)

	runCheck := func(numWorkers int) (hits, misses int) {
		db := mustOpenDB(t)
		scanID := mustInsertScan(t, db)
		seedFileCache(t, db, scanID, numCached)

		progress := &Progress{}
		in := make(chan FileInfo, numCached+numNew)
		hitsCh := make(chan HashedFile, numCached+numNew)
		missesCh := make(chan FileInfo, numCached+numNew)
		RunCacheCheck(context.Background(), db, progress, numWorkers, in, hitsCh, missesCh)

		for i := 0; i < numCached; i++ {
			in <- FileInfo{
				Path:  fmt.Sprintf("/cached/file%04d.txt", i),
				Size:  int64(i*100 + 1),
				MTime: time.Unix(int64(1000+i), 0),
			}
		}
		for i := 0; i < numNew; i++ {
			in <- FileInfo{Path: fmt.Sprintf("/new/file%04d.txt", i), Size: 1, MTime: time.Now()}
		}
		close(in)

		hDone := make(chan struct{})
		mDone := make(chan struct{})
		go func() {
			for range hitsCh {
				hits++
			}
			close(hDone)
		}()
		go func() {
			for range missesCh {
				misses++
			}
			close(mDone)
		}()
		<-hDone
		<-mDone
		return hits, misses
	}

	h1, m1 := runCheck(1)
	h4, m4 := runCheck(4)

	if h1 != h4 {
		t.Errorf("hits: 1 worker=%d, 4 workers=%d — results differ", h1, h4)
	}
	if m1 != m4 {
		t.Errorf("misses: 1 worker=%d, 4 workers=%d — results differ", m1, m4)
	}
	if h1 != numCached {
		t.Errorf("hits: got %d, want %d", h1, numCached)
	}
}
