package scan

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// BenchmarkPipelineCold measures end-to-end scan throughput with an empty
// cache (worst case). Each iteration clears file_cache and re-scans 300 files.
// Run with: go test -bench=BenchmarkPipelineCold -benchtime=3x ./internal/scan/
func BenchmarkPipelineCold(b *testing.B) {
	root := b.TempDir()
	numFiles := createSyntheticTree(b, root, 300)
	db := mustOpenDB(b)

	cfg := DefaultConfig()
	s := New(db, []string{root}, nil, cfg)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Clear cache to simulate a cold (first-ever) scan.
		db.Exec("DELETE FROM file_cache")

		p := &Progress{}
		start := time.Now()
		if _, err := s.Run(context.Background(), "manual", p); err != nil {
			b.Fatalf("scan failed: %v", err)
		}
		elapsed := time.Since(start)

		b.ReportMetric(float64(numFiles), "files/op")
		b.ReportMetric(float64(numFiles)/elapsed.Seconds(), "files/s")
		b.ReportMetric(float64(p.CacheHits.Load()), "cache_hits/op")
	}
}

// BenchmarkPipelineWarm measures scan throughput when all duplicate candidates
// are already in file_cache (subsequent-scan case). The walk still happens;
// only hashing is skipped via cache hits.
// Run with: go test -bench=BenchmarkPipelineWarm -benchtime=3x ./internal/scan/
func BenchmarkPipelineWarm(b *testing.B) {
	root := b.TempDir()
	numFiles := createSyntheticTree(b, root, 300)
	db := mustOpenDB(b)

	cfg := DefaultConfig()
	s := New(db, []string{root}, nil, cfg)

	// Warmup: one cold scan to populate file_cache.
	if _, err := s.Run(context.Background(), "manual", &Progress{}); err != nil {
		b.Fatalf("warmup scan failed: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := &Progress{}
		start := time.Now()
		if _, err := s.Run(context.Background(), "manual", p); err != nil {
			b.Fatalf("scan failed: %v", err)
		}
		elapsed := time.Since(start)

		b.ReportMetric(float64(numFiles), "files/op")
		b.ReportMetric(float64(numFiles)/elapsed.Seconds(), "files/s")
		b.ReportMetric(float64(p.CacheHits.Load()), "cache_hits/op")
	}
}

// BenchmarkCacheCheck measures cache-check throughput at different worker
// counts. Since db.Open sets MaxOpenConns(1), queries serialize at the pool
// level regardless of worker count — this establishes a baseline and will
// show real gains if MaxOpenConns is increased in future.
// Run with: go test -bench=BenchmarkCacheCheck -benchtime=5x ./internal/scan/
func BenchmarkCacheCheck(b *testing.B) {
	const numCandidates = 500

	for _, numWorkers := range []int{1, 2, 4} {
		b.Run(fmt.Sprintf("workers=%d", numWorkers), func(b *testing.B) {
			db := mustOpenDB(b)
			scanID := mustInsertScan(b, db)
			seedFileCache(b, db, scanID, numCandidates)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				in := make(chan FileInfo, numCandidates)
				hits := make(chan HashedFile, numCandidates)
				misses := make(chan FileInfo, numCandidates)

				progress := &Progress{}
				RunCacheCheck(context.Background(), db, progress, numWorkers, in, hits, misses)

				for j := 0; j < numCandidates; j++ {
					in <- FileInfo{
						Path:  fmt.Sprintf("/cached/file%04d.txt", j),
						Size:  int64(j*100 + 1),
						MTime: time.Unix(int64(1000+j), 0),
					}
				}
				close(in)

				hDone := make(chan struct{})
				mDone := make(chan struct{})
				go func() {
					for range hits {
					}
					close(hDone)
				}()
				go func() {
					for range misses {
					}
					close(mDone)
				}()
				<-hDone
				<-mDone

				b.SetBytes(int64(numCandidates))
			}
		})
	}
}
