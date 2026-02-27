package scan

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/eargollo/ditto/internal/media"
)

// WriteStats holds the final counts returned by RunDBWriter.
type WriteStats struct {
	DuplicateGroups  int64
	DuplicateFiles   int64
	ReclaimableBytes int64
	FilesHashed      int64 // total files in duplicate groups (includes cache hits)
}

// groupBatchSize is the number of duplicate groups written per SQLite transaction.
// Batching reduces fsync calls ~500× on spinning-disk storage (e.g. NAS).
const groupBatchSize = 100

// groupEntry pairs a content hash with its matching files.
type groupEntry struct {
	hash  string
	files []HashedFile
}

// RunDBWriter collects all HashedFile results from in, then writes duplicate
// groups and files to the database in batched transactions.
// It updates file_cache progressively (every batchSize items) so that a
// cancelled scan still preserves partial hashing work for subsequent runs.
// Returns aggregate stats for updating scan_history.
func RunDBWriter(ctx context.Context, db *sql.DB, scanID int64, batchSize int, in <-chan HashedFile, progress *Progress) (WriteStats, error) {
	// Phase 1: accumulate all results into a map keyed by full hash.
	// Write file_cache entries progressively so cancelled scans preserve work.
	groups := make(map[string][]HashedFile)
	var cacheBuf []HashedFile

	flushCache := func() {
		if len(cacheBuf) == 0 {
			return
		}
		// Use Background so the flush survives context cancellation.
		if err := updateCache(context.Background(), db, scanID, cacheBuf, batchSize); err != nil {
			slog.Warn("progressive cache update failed", "error", err)
		}
		cacheBuf = cacheBuf[:0]
	}

	for hf := range in {
		groups[hf.Hash] = append(groups[hf.Hash], hf)
		cacheBuf = append(cacheBuf, hf)
		if len(cacheBuf) >= batchSize {
			flushCache()
		}
	}

	// Always flush remaining entries — even when the scan was cancelled.
	flushCache()

	if ctx.Err() != nil {
		return WriteStats{}, ctx.Err()
	}

	// Phase 2: write duplicate groups to the database.
	return persistGroups(ctx, db, scanID, groups, progress)
}

// persistGroups writes all duplicate groups and their files to the DB.
// Groups are batched groupBatchSize per transaction to minimise fsync overhead
// on spinning-disk storage (reduces ~307K individual statements to ~620 transactions).
func persistGroups(ctx context.Context, db *sql.DB, scanID int64, groups map[string][]HashedFile, progress *Progress) (WriteStats, error) {
	var stats WriteStats
	now := time.Now().Unix()

	// Separate duplicates (≥2 files) from singletons; count all FilesHashed.
	var dupGroups []groupEntry
	for hash, files := range groups {
		stats.FilesHashed += int64(len(files))
		if len(files) >= 2 {
			dupGroups = append(dupGroups, groupEntry{hash, files})
		}
	}

	// Signal Phase 2 start so the progress reporter and UX can reflect status.
	if progress != nil {
		progress.GroupsTotal.Store(int64(len(dupGroups)))
		progress.Phase2StartedAt.Store(time.Now().Unix())
	}

	// Write in batches of groupBatchSize, each a single SQLite transaction.
	for i := 0; i < len(dupGroups); i += groupBatchSize {
		if ctx.Err() != nil {
			return stats, ctx.Err()
		}
		end := i + groupBatchSize
		if end > len(dupGroups) {
			end = len(dupGroups)
		}
		batch := dupGroups[i:end]

		if err := writeGroupBatch(ctx, db, scanID, batch, now, &stats); err != nil {
			return stats, err
		}
		if progress != nil {
			progress.GroupsWritten.Add(int64(len(batch)))
		}
	}

	return stats, nil
}

// writeGroupBatch writes a slice of duplicate groups within a single transaction,
// reusing prepared statements across all groups in the batch.
func writeGroupBatch(ctx context.Context, db *sql.DB, scanID int64, batch []groupEntry, now int64, stats *WriteStats) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// Prepare once, reuse for every group in the batch.
	stmtInsertGroup, err := tx.PrepareContext(ctx, `
		INSERT OR IGNORE INTO duplicate_groups
			(content_hash, file_size, file_type,
			 first_seen_scan_id, last_seen_scan_id,
			 created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare insert_group: %w", err)
	}
	defer stmtInsertGroup.Close()

	stmtDeleteFiles, err := tx.PrepareContext(ctx,
		`DELETE FROM duplicate_files WHERE group_id = ?`)
	if err != nil {
		return fmt.Errorf("prepare delete_files: %w", err)
	}
	defer stmtDeleteFiles.Close()

	stmtInsertFile, err := tx.PrepareContext(ctx, `
		INSERT INTO duplicate_files (group_id, scan_id, path, size, mtime, file_type)
		VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare insert_file: %w", err)
	}
	defer stmtInsertFile.Close()

	stmtUpdateGroup, err := tx.PrepareContext(ctx, `
		UPDATE duplicate_groups
		SET file_count = ?, reclaimable_bytes = ?,
		    file_type = ?, last_seen_scan_id = ?, updated_at = ?
		WHERE id = ?`)
	if err != nil {
		return fmt.Errorf("prepare update_group: %w", err)
	}
	defer stmtUpdateGroup.Close()

	for _, g := range batch {
		if err := writeGroupInTx(ctx, tx, scanID, g.hash, g.files, now, stats,
			stmtInsertGroup, stmtDeleteFiles, stmtInsertFile, stmtUpdateGroup); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// writeGroupInTx writes a single duplicate group using pre-prepared statements
// within an existing transaction.
func writeGroupInTx(
	ctx context.Context,
	tx *sql.Tx,
	scanID int64,
	hash string,
	files []HashedFile,
	now int64,
	stats *WriteStats,
	stmtInsertGroup, stmtDeleteFiles, stmtInsertFile, stmtUpdateGroup *sql.Stmt,
) error {
	fileSize := files[0].Size
	fileType := string(media.Detect(files[0].Path))

	if _, err := stmtInsertGroup.ExecContext(ctx,
		hash, fileSize, fileType, scanID, scanID, now, now,
	); err != nil {
		return fmt.Errorf("insert group %s: %w", hash[:8], err)
	}

	var groupID int64
	if err := tx.QueryRowContext(ctx,
		`SELECT id FROM duplicate_groups WHERE content_hash = ?`, hash,
	).Scan(&groupID); err != nil {
		return fmt.Errorf("get group id %s: %w", hash[:8], err)
	}

	if _, err := stmtDeleteFiles.ExecContext(ctx, groupID); err != nil {
		return fmt.Errorf("delete old files group %d: %w", groupID, err)
	}

	for _, f := range files {
		ft := string(media.Detect(f.Path))
		if _, err := stmtInsertFile.ExecContext(ctx,
			groupID, scanID, f.Path, f.Size, f.MTime.Unix(), ft,
		); err != nil {
			return fmt.Errorf("insert file %s: %w", f.Path, err)
		}
	}

	reclaimable := fileSize * int64(len(files)-1)
	if _, err := stmtUpdateGroup.ExecContext(ctx,
		len(files), reclaimable, fileType, scanID, now, groupID,
	); err != nil {
		return fmt.Errorf("update group %d: %w", groupID, err)
	}

	stats.DuplicateGroups++
	stats.DuplicateFiles += int64(len(files))
	stats.ReclaimableBytes += reclaimable
	return nil
}

// updateCache upserts file_cache entries for all files that passed through
// the full hash stage.
func updateCache(ctx context.Context, db *sql.DB, scanID int64, files []HashedFile, batchSize int) error {
	now := time.Now().Unix()
	for i := 0; i < len(files); i += batchSize {
		end := i + batchSize
		if end > len(files) {
			end = len(files)
		}
		batch := files[i:end]

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		stmt, err := tx.PrepareContext(ctx, `
			INSERT OR REPLACE INTO file_cache (path, size, mtime, full_hash, cached_at, scan_id)
			VALUES (?, ?, ?, ?, ?, ?)`)
		if err != nil {
			tx.Rollback()
			return err
		}
		for _, f := range batch {
			if _, err := stmt.ExecContext(ctx, f.Path, f.Size, f.MTime.Unix(), f.Hash, now, scanID); err != nil {
				stmt.Close()
				tx.Rollback()
				return err
			}
		}
		stmt.Close()
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
