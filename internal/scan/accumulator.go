package scan

import "context"

// RunSizeAccumulator reads all FileInfo from in, counting every file as
// "discovered". The first file seen per size is buffered. When a second file
// with the same size arrives, both are emitted to out as candidates for
// hashing. Subsequent files with a seen size are emitted immediately.
// Empty (zero-byte) files are skipped — they cannot be meaningful duplicates.
// out is closed when in is exhausted or ctx is cancelled.
func RunSizeAccumulator(ctx context.Context, progress *Progress, in <-chan FileInfo, out chan<- FileInfo) {
	go func() {
		defer close(out)

		first := make(map[int64]FileInfo) // size → first-seen file
		seen := make(map[int64]bool)      // sizes with ≥2 files

		for {
			select {
			case <-ctx.Done():
				return
			case fi, ok := <-in:
				if !ok {
					return
				}
				progress.FilesDiscovered.Add(1)

				if fi.Size == 0 {
					continue
				}

				if seen[fi.Size] {
					progress.CandidatesFound.Add(1)
					select {
					case out <- fi:
					case <-ctx.Done():
						return
					}
					continue
				}

				if prev, ok := first[fi.Size]; ok {
					// Second file with this size — emit both.
					seen[fi.Size] = true
					delete(first, fi.Size)
					progress.CandidatesFound.Add(2)
					for _, f := range [2]FileInfo{prev, fi} {
						select {
						case out <- f:
						case <-ctx.Done():
							return
						}
					}
				} else {
					first[fi.Size] = fi
				}
			}
		}
	}()
}
