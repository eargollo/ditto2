package scan

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
)

const partialHashBytes = 64 * 1024 // 64 KB

// hashPartial computes the SHA-256 of the first partialHashBytes of the file.
// Returns hex-encoded hash and bytes read.
func hashPartial(path string) (hash string, n int64, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, fmt.Errorf("open: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	n, err = io.CopyN(h, f, partialHashBytes)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return "", n, fmt.Errorf("read: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// hashFull computes the SHA-256 of the entire file.
// Returns hex-encoded hash and bytes read.
func hashFull(path string) (hash string, n int64, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, fmt.Errorf("open: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	n, err = io.Copy(h, f)
	if err != nil {
		return "", n, fmt.Errorf("read: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// RunPartialHashers spawns numWorkers goroutines. Each reads FileInfo from in,
// computes the partial SHA-256, and sends a HashedFile (with partial hash) to
// out. out is closed once all workers finish.
func RunPartialHashers(ctx context.Context, numWorkers int, progress *Progress, in <-chan FileInfo, out chan<- HashedFile) {
	var wg sync.WaitGroup
	for range numWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case fi, ok := <-in:
					if !ok {
						return
					}
					hash, n, err := hashPartial(fi.Path)
					if err != nil {
						slog.Warn("partial hash failed", "path", fi.Path, "error", err)
						continue
					}
					progress.BytesRead.Add(n)
					progress.PartialHashed.Add(1)
					select {
					case out <- HashedFile{FileInfo: fi, Hash: hash}:
					case <-ctx.Done():
						return
					}
				}
			}
		}()
	}
	go func() {
		wg.Wait()
		close(out)
	}()
}

// RunFullHashers spawns numWorkers goroutines. Each reads a HashedFile from in
// (whose Hash field currently holds a partial hash), computes the full
// SHA-256, and sends an updated HashedFile (with full hash) to out.
// out is closed once all workers finish.
func RunFullHashers(ctx context.Context, numWorkers int, progress *Progress, in <-chan HashedFile, out chan<- HashedFile) {
	var wg sync.WaitGroup
	for range numWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case hf, ok := <-in:
					if !ok {
						return
					}
					hash, n, err := hashFull(hf.Path)
					if err != nil {
						slog.Warn("full hash failed", "path", hf.Path, "error", err)
						continue
					}
					progress.BytesRead.Add(n)
					progress.FullHashed.Add(1)
					select {
					case out <- HashedFile{FileInfo: hf.FileInfo, Hash: hash}:
					case <-ctx.Done():
						return
					}
				}
			}
		}()
	}
	go func() {
		wg.Wait()
		close(out)
	}()
}
