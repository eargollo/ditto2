package scan

import (
	"context"
	"database/sql"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// cacheBatchSize is the number of candidates sent in a single SELECT … IN (…)
// query. Larger batches mean fewer round-trips; 500 is a good balance between
// query size and latency.
const cacheBatchSize = 500

// RunCacheCheck spawns numWorkers goroutines. Each worker accumulates incoming
// FileInfos into batches of up to cacheBatchSize and looks them all up in a
// single SELECT … WHERE path IN (…) query, reducing database round-trips by
// ~500×.
//
// A result row whose (size, mtime) still matches → cache hit → sent to hits.
// Everything else (no row, or stale row) → cache miss → sent to misses.
//
// Both hits and misses are closed when all workers finish or ctx is cancelled.
func RunCacheCheck(ctx context.Context, db *sql.DB, progress *Progress, numWorkers int, in <-chan FileInfo, hits chan<- HashedFile, misses chan<- FileInfo) {
	var wg sync.WaitGroup
	for range numWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cacheWorker(ctx, db, in, hits, misses, progress)
		}()
	}
	go func() {
		wg.Wait()
		close(hits)
		close(misses)
	}()
}

// cacheWorker is the per-goroutine body of RunCacheCheck.
func cacheWorker(ctx context.Context, db *sql.DB, in <-chan FileInfo, hits chan<- HashedFile, misses chan<- FileInfo, progress *Progress) {
	batch := make([]FileInfo, 0, cacheBatchSize)

	for {
		// Block until we get the first item of a new batch.
		select {
		case <-ctx.Done():
			return
		case fi, ok := <-in:
			if !ok {
				return
			}
			batch = append(batch, fi)
		}

		// Greedily drain more items without blocking (fills the batch).
		var open bool
		batch, open = drainBatch(in, batch, cacheBatchSize)
		lookupBatch(ctx, db, batch, hits, misses, progress)
		batch = batch[:0]
		if !open {
			return
		}
	}
}

// drainBatch appends items from in to batch (up to maxSize) using non-blocking
// receives. Returns (batch, true) when the channel is still open, or
// (batch, false) when it was closed during draining.
func drainBatch(in <-chan FileInfo, batch []FileInfo, maxSize int) ([]FileInfo, bool) {
	for len(batch) < maxSize {
		select {
		case fi, ok := <-in:
			if !ok {
				return batch, false
			}
			batch = append(batch, fi)
		default:
			return batch, true
		}
	}
	return batch, true
}

// lookupBatch issues a single batched SELECT for all paths in batch and routes
// each item to hits (cache hit with matching size+mtime) or misses.
func lookupBatch(ctx context.Context, db *sql.DB, batch []FileInfo, hits chan<- HashedFile, misses chan<- FileInfo, progress *Progress) {
	if len(batch) == 0 {
		return
	}

	// Build: SELECT path, size, mtime, full_hash FROM file_cache WHERE path IN (?,?,...).
	args := make([]interface{}, len(batch))
	for i, fi := range batch {
		args[i] = fi.Path
	}
	placeholders := strings.Repeat("?,", len(batch))
	placeholders = placeholders[:len(placeholders)-1]

	t0 := time.Now()
	rows, err := db.QueryContext(ctx,
		"SELECT path, size, mtime, full_hash FROM file_cache WHERE path IN ("+placeholders+")",
		args...)
	progress.DBReadMs.Add(time.Since(t0).Milliseconds())

	// Build a map of path → cached entry from the result set.
	type cacheEntry struct {
		size, mtime int64
		hash        string
	}
	cached := make(map[string]cacheEntry, len(batch))
	if err != nil {
		if ctx.Err() == nil {
			slog.Warn("cache check batch query", "error", err)
		}
	} else {
		defer rows.Close()
		for rows.Next() {
			var path, hash string
			var size, mtime int64
			if rows.Scan(&path, &size, &mtime, &hash) == nil {
				cached[path] = cacheEntry{size, mtime, hash}
			}
		}
		if rerr := rows.Err(); rerr != nil && ctx.Err() == nil {
			slog.Warn("cache check batch rows error", "error", rerr)
		}
	}

	// Route each item in batch order.
	for _, fi := range batch {
		if e, ok := cached[fi.Path]; ok && e.size == fi.Size && e.mtime == fi.MTime.Unix() {
			progress.CacheHits.Add(1)
			select {
			case hits <- HashedFile{FileInfo: fi, Hash: e.hash}:
			case <-ctx.Done():
				return
			}
		} else {
			progress.CacheMisses.Add(1)
			select {
			case misses <- fi:
			case <-ctx.Done():
				return
			}
		}
	}
}
