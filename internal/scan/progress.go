package scan

import "sync/atomic"

// Progress holds live counters updated by the pipeline stages.
// All fields are atomic so they can be written from worker goroutines and
// read from the HTTP handler without locks.
type Progress struct {
	FilesDiscovered atomic.Int64
	CandidatesFound atomic.Int64
	PartialHashed   atomic.Int64
	FullHashed      atomic.Int64
	BytesRead       atomic.Int64
	CacheHits       atomic.Int64
	CacheMisses     atomic.Int64
	Errors          atomic.Int64
}

// ErrorReporter records a per-file pipeline error: increments the error
// counter, emits a structured warning log, and persists the event to the
// scan_errors table so it is visible via GET /api/scans/:id.
type ErrorReporter func(path, stage, errMsg string)
