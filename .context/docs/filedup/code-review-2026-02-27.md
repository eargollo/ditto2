# Code Review — 2026-02-27

**Branch**: `main` (unstaged changes)
**Files reviewed**: `cmd/ditto/main.go`, `internal/db/db.go`, `internal/scan/cache.go`,
`internal/scan/hasher.go`, `internal/scan/scan.go`

---

## Summary of Changes

| Change | Purpose |
|--------|---------|
| `db.go` — `Open`: cache size 64 MB → 128 MB | Larger SQLite page cache for write connection |
| `db.go` — `OpenReadPool` (new) | Dedicated read-only connection pool for parallel cache lookups |
| `cache.go` — batched `SELECT … IN (…)` | Replace 1-row-per-query with 500-row batches |
| `hasher.go` — `RunSizeRouter` (new) | Files ≤ 64 KB skip full hasher (partial hash == full hash) |
| `scan.go` — `Config.ReadDB` + pipeline rewiring | Wire read pool + size router into pipeline; make `mergeHashedFiles` variadic |
| `main.go` — open read pool, inject into scan config | Bootstrap the read pool |

Overall the changes are well-motivated performance improvements. The code is clean and well-commented. Issues below are ordered by priority.

---

## Issues

### [HIGH] `PRAGMA query_only = ON` is not reliably applied to all pool connections

**File**: `internal/db/db.go:57`, `OpenReadPool`

PRAGMAs in SQLite are **connection-level** settings. Go's `database/sql` pool opens connections lazily, on demand. The loop that calls `rdb.Exec(pragma)` sequentially will typically reuse the same single connection (since each `Exec` completes before the next begins and the connection is returned to the idle pool). The remaining `maxConns - 1` connections, created later when concurrent goroutines make simultaneous requests, will **not** have `PRAGMA query_only = ON`.

In practice only 1 of the 4 pool connections is guaranteed to be query-only. A write-capable connection in the read pool could theoretically conflict with the WAL writer.

The standard fix for `modernc.org/sqlite` is to embed PRAGMAs in the DSN as URI parameters (e.g., `file:path?_pragma=query_only(1)`), which the driver applies to every new connection at open time.

---

### [MEDIUM] Pool size hardcoded to 4, disconnected from `CacheCheckers` setting

**File**: `cmd/ditto/main.go:70`

```go
readDB, err := db.OpenReadPool(cfg.DBPath, 4)
```

`4` is hardcoded. If a user increases `cfg.ScanWorkers.CacheCheckers` to, say, 8, the pool will be a bottleneck — 8 workers competing for 4 connections. The pool size should track the number of cache-checker goroutines.

---

### [MEDIUM] `drainBatch` has no unit tests despite being pure, testable logic

**File**: `internal/scan/cache.go:71-84`

`drainBatch` is a self-contained, deterministic function with no external dependencies. It handles the important edge case of channel closure mid-drain, which is exactly the kind of logic that benefits from table-driven tests. The black-box regression strategy doesn't exercise this function in isolation, making regressions harder to detect.

---

### [MEDIUM] `RunSizeRouter` has no unit tests

**File**: `internal/scan/hasher.go:56-84`

Like `drainBatch`, `RunSizeRouter` is stateless and requires only channel fixtures to test. It deserves tests for: correct routing on the boundary (`Size == partialHashBytes`), channel closure propagation to both outputs, and context cancellation draining both outputs.

---

### [LOW] `[]interface{}` should be `[]any` (Go 1.18+ idiom)

**File**: `internal/scan/cache.go:94`

```go
args := make([]interface{}, len(batch))
```

`any` is the idiomatic alias since Go 1.18. Minor style consistency issue.

---

### [LOW] Irrelevant PRAGMAs in the read-only pool

**File**: `internal/db/db.go:55, 59`

- `PRAGMA journal_mode = WAL` — journal mode is a database-level property, not per-connection. Setting it on the read pool is redundant (the main `Open` already sets it) and slightly misleading.
- `PRAGMA synchronous = NORMAL` — durability flushing applies only to writers. It is silently ignored on a query-only connection but adds confusion to the function.

---

### [LOW] `smallOut` buffer may back-pressure `RunSizeRouter` under many small files

**File**: `internal/scan/scan.go:160`

```go
smallOut = make(chan HashedFile, finalBufSize)  // finalBufSize = 10_000
```

`RunSizeRouter` is a **single goroutine** dispatching to both `smallOut` and `largeOut`. If the scan contains many files ≤ 64 KB (e.g., a source-code tree), `smallOut` fills quickly and blocks the router. This stalls delivery to `largeOut` as well, idling the full-hash workers. Raising `smallOut` to `pipelineBufSize` (100 K) would match the throughput of the upstream stages.

---

### [LOW] "~500× reduction" comment overstates batching gain with N > 1 workers

**File**: `internal/scan/cache.go:18`

The claim of "~500× fewer round-trips" assumes a single cache worker. With `numWorkers = 4`, all workers read from the same channel concurrently; items are spread across workers, so each worker's average batch is `total_items / 4`. Actual round-trip reduction is closer to `~125×` per worker. The comment should reflect the per-worker reality.

---

### [LOW] `go mod tidy` required — two direct dependencies marked indirect

**File**: `go.mod:20, 24`

```
github.com/rwcarlsen/goexif  // indirect
golang.org/x/image            // indirect
```

Both are direct dependencies (used in the codebase) but are incorrectly classified as indirect. Running `go mod tidy` will promote them and silence the diagnostics.

---

### [LOW · PRE-EXISTING] `progressReporter` final flush may not persist after context cancellation

**File**: `internal/scan/scan.go:244-282`

`progressReporter.flush()` calls `db.ExecContext(ctx, ...)`. When the scan is **cancelled** (not completed), `ctx` is already done when the final `flush()` fires via `close(reporterStop)`. The cancelled context causes `ExecContext` to fail silently, so the last progress snapshot is lost. Only affects UI display of cancelled scans. Fix: use `context.Background()` for the final flush, or use a separate non-cancellable context for DB writes.

---

## Correctness of `RunSizeRouter` Optimization

The optimization is **correct under normal conditions**. For files ≤ 64 KB:
- `hashPartial` uses `io.CopyN(..., 64 KB)` and silently handles `io.ErrUnexpectedEOF`, so it hashes the entire file.
- The partial hash equals the full hash.
- The router check `hf.Size <= partialHashBytes` reliably identifies these files.

There is a pre-existing TOCTOU class of issue: if a file grows from ≤ 64 KB to > 64 KB between the walk and the hash, the router will send it to `small` based on the original size, but the partial hash will cover the first 64 KB of the now-larger file. This matches the same TOCTOU risk in the rest of the pipeline and is not a regression introduced by this change.

---

## Summary Table

| # | Priority | Category | Location | Issue |
|---|----------|----------|----------|-------|
| 1 | **High** | Reliability | `db.go:57` | `PRAGMA query_only` not applied to all pool connections |
| 2 | Medium | Design | `main.go:70` | Pool size hardcoded to 4, not tied to `CacheCheckers` |
| 3 | Medium | Testability | `cache.go:71` | `drainBatch` has no unit tests |
| 4 | Medium | Testability | `hasher.go:56` | `RunSizeRouter` has no unit tests |
| 5 | Low | Code style | `cache.go:94` | `[]interface{}` → `[]any` |
| 6 | Low | Clarity | `db.go:55,59` | Irrelevant PRAGMAs in read pool |
| 7 | Low | Performance | `scan.go:160` | `smallOut` buffer too small for small-file-heavy scans |
| 8 | Low | Documentation | `cache.go:18` | "~500×" comment overstates gain with N workers |
| 9 | Low | Maintenance | `go.mod:20,24` | Run `go mod tidy` |
| 10 | Low | Pre-existing | `scan.go:278` | Final progress flush fails after cancellation |
