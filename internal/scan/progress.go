package scan

import "sync/atomic"

// Progress holds live counters updated by the pipeline stages.
// All fields are atomic so they can be written from worker goroutines and
// read from the HTTP handler without locks.
type Progress struct {
	// Phase 1 — hashing pipeline
	FilesDiscovered atomic.Int64
	CandidatesFound atomic.Int64
	PartialHashed   atomic.Int64
	FullHashed      atomic.Int64
	BytesRead       atomic.Int64
	CacheHits       atomic.Int64
	CacheMisses     atomic.Int64
	Errors          atomic.Int64
	// Phase 2 — DB write
	// Phase2StartedAt is a Unix timestamp set when Phase 2 begins (0 = not started).
	Phase2StartedAt atomic.Int64
	GroupsTotal     atomic.Int64 // total duplicate groups to write
	GroupsWritten   atomic.Int64 // duplicate groups written so far
	// Timing counters (milliseconds, accumulated across all worker goroutines)
	DiskReadMs atomic.Int64 // time spent in hash operations (I/O + SHA256)
	DBReadMs   atomic.Int64 // time spent in cache-check SELECT queries
	DBWriteMs  atomic.Int64 // time spent in DB write operations (cache + groups)
}

// ErrorReporter records a per-file pipeline error: increments the error
// counter, emits a structured warning log, and persists the event to the
// scan_errors table so it is visible via GET /api/scans/:id.
type ErrorReporter func(path, stage, errMsg string)
