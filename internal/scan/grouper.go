package scan

import "context"

// RunPartialHashGrouper reads HashedFiles whose Hash field contains a
// *partial* hash. The first file per partial hash is buffered. When a second
// file with the same partial hash arrives, both are emitted — indicating they
// are likely duplicates and should be fully hashed. Subsequent files with a
// seen partial hash are emitted immediately.
// out is closed when in is exhausted or ctx is cancelled.
func RunPartialHashGrouper(ctx context.Context, in <-chan HashedFile, out chan<- HashedFile) {
	go func() {
		defer close(out)

		first := make(map[string]HashedFile) // partialHash → first-seen file
		seen := make(map[string]bool)        // partial hashes with ≥2 files

		for {
			select {
			case <-ctx.Done():
				return
			case hf, ok := <-in:
				if !ok {
					return
				}

				if seen[hf.Hash] {
					select {
					case out <- hf:
					case <-ctx.Done():
						return
					}
					continue
				}

				if prev, ok := first[hf.Hash]; ok {
					seen[hf.Hash] = true
					delete(first, hf.Hash)
					for _, f := range [2]HashedFile{prev, hf} {
						select {
						case out <- f:
						case <-ctx.Done():
							return
						}
					}
				} else {
					first[hf.Hash] = hf
				}
			}
		}
	}()
}
