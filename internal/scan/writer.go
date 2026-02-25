package scan

import (
	"context"
	"database/sql"
)

// RunDBWriter reads HashedFiles from in and writes them to the database
// in batched transactions (batchSize rows per tx). It closes done when
// the input channel is exhausted.
// Stub — not yet implemented.
func RunDBWriter(ctx context.Context, db *sql.DB, scanID int64, batchSize int, in <-chan HashedFile) error {
	for range in {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
	return nil
}
