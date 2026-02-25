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
}
