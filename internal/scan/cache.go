package scan

import (
	"context"
	"database/sql"
	"log/slog"
)

// RunCacheCheck looks up each candidate FileInfo in the file_cache table using
// the key (path, size, mtime). A match means the file is unchanged and its
// full hash is already known (cache hit) — sent to hits.
// Non-matching files are sent to misses for hashing.
// Both hits and misses are closed when in is exhausted or ctx is cancelled.
func RunCacheCheck(ctx context.Context, db *sql.DB, progress *Progress, in <-chan FileInfo, hits chan<- HashedFile, misses chan<- FileInfo) {
	go func() {
		defer close(hits)
		defer close(misses)

		stmt, err := db.PrepareContext(ctx,
			`SELECT full_hash FROM file_cache WHERE path = ? AND size = ? AND mtime = ?`)
		if err != nil {
			slog.Error("cache check: prepare statement", "error", err)
			for range in {
			} // drain to avoid blocking upstream
			return
		}
		defer stmt.Close()

		for {
			select {
			case <-ctx.Done():
				return
			case fi, ok := <-in:
				if !ok {
					return
				}

				var fullHash string
				err := stmt.QueryRowContext(ctx, fi.Path, fi.Size, fi.MTime.Unix()).Scan(&fullHash)
				if err == nil {
					progress.CacheHits.Add(1)
					select {
					case hits <- HashedFile{FileInfo: fi, Hash: fullHash}:
					case <-ctx.Done():
						return
					}
				} else {
					if err != sql.ErrNoRows {
						slog.Warn("cache check: query error", "path", fi.Path, "error", err)
					}
					progress.CacheMisses.Add(1)
					select {
					case misses <- fi:
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}()
}
