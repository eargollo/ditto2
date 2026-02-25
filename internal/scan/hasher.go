package scan

import (
	"context"
)

const partialHashBytes = 64 * 1024 // 64 KB

// RunPartialHashers reads from in, computes the SHA-256 of the first
// partialHashBytes of each file, and sends results to out.
// numWorkers goroutines are spawned; out is closed when all are done.
// Stub — not yet implemented.
func RunPartialHashers(ctx context.Context, numWorkers int, in <-chan FileInfo, out chan<- HashedFile) {
	go func() {
		defer close(out)
		for range in {
			// stub: drain input
			select {
			case <-ctx.Done():
				return
			default:
			}
		}
	}()
}

// RunFullHashers reads from in, computes the full SHA-256 of each file,
// and sends results to out. numWorkers goroutines are spawned; out is
// closed when all are done.
// Stub — not yet implemented.
func RunFullHashers(ctx context.Context, numWorkers int, in <-chan HashedFile, out chan<- HashedFile) {
	go func() {
		defer close(out)
		for range in {
			select {
			case <-ctx.Done():
				return
			default:
			}
		}
	}()
}
