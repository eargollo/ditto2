package scan

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
)

// dirQueue is an unbounded, concurrency-safe queue of directory paths.
// It tracks a pending counter so that Walk() knows when all work is done.
//
// Termination protocol:
//   - Push increments pending BEFORE enqueuing (caller must own the increment).
//   - Done decrements pending AFTER all children of a directory have been
//     pushed. When pending reaches 0, Done closes the queue and broadcasts.
type dirQueue struct {
	mu      sync.Mutex
	cond    *sync.Cond
	items   []string
	head    int // index of the next item to pop; avoids O(n) re-slicing
	pending atomic.Int64
	closed  bool
}

func newDirQueue() *dirQueue {
	q := &dirQueue{}
	q.cond = sync.NewCond(&q.mu)
	return q
}

// Push enqueues a directory. Must be called after incrementing pending.
func (q *dirQueue) Push(dir string) {
	q.mu.Lock()
	q.items = append(q.items, dir)
	q.mu.Unlock()
	q.cond.Signal()
}

// Pop blocks until an item is available or the queue is closed.
// Returns ("", false) when the queue is closed and empty.
func (q *dirQueue) Pop() (string, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for q.head >= len(q.items) && !q.closed {
		q.cond.Wait()
	}
	if q.head >= len(q.items) {
		return "", false
	}
	item := q.items[q.head]
	q.items[q.head] = "" // release string reference so GC can collect it
	q.head++
	// Compact when we've consumed at least 1 000 items and head has passed
	// the midpoint — keeps the backing array from growing without bound.
	if q.head >= 1000 && q.head >= len(q.items)/2 {
		q.items = append(q.items[:0], q.items[q.head:]...)
		q.head = 0
	}
	return item, true
}

// Done must be called once per directory after all its child-directories have
// been pushed. Decrements pending; if pending reaches 0, closes the queue.
func (q *dirQueue) Done() {
	if q.pending.Add(-1) == 0 {
		q.mu.Lock()
		q.closed = true
		q.mu.Unlock()
		q.cond.Broadcast()
	}
}

// Walk traverses roots concurrently using numWorkers goroutines and sends
// every regular file it finds to out. Walk closes out when done.
// Directories and files matching excludePaths are skipped.
// report is called for any filesystem errors encountered during traversal.
func Walk(ctx context.Context, roots []string, excludePaths map[string]struct{}, numWorkers int, out chan<- FileInfo, report ErrorReporter) {
	defer close(out)

	q := newDirQueue()

	// Seed the queue with root directories.
	for _, root := range roots {
		q.pending.Add(1)
		q.Push(root)
	}

	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			walkerWorker(ctx, q, excludePaths, out, report)
		}()
	}
	wg.Wait()
}

// walkerWorker pops directories from q, reads their entries, enqueues
// sub-directories (incrementing pending first), sends files to out, then
// calls q.Done() to decrement pending.
func walkerWorker(ctx context.Context, q *dirQueue, excludePaths map[string]struct{}, out chan<- FileInfo, report ErrorReporter) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		dir, ok := q.Pop()
		if !ok {
			return
		}

		entries, err := os.ReadDir(dir)
		if err != nil {
			report(dir, "walk", err.Error())
			q.Done()
			continue
		}

		for _, entry := range entries {
			path := filepath.Join(dir, entry.Name())

			if _, excluded := excludePaths[path]; excluded {
				continue
			}

			if entry.IsDir() {
				// Increment BEFORE pushing so pending is never zero prematurely.
				q.pending.Add(1)
				q.Push(path)
				continue
			}

			if entry.Type()&fs.ModeSymlink != 0 {
				continue
			}

			if !entry.Type().IsRegular() {
				continue
			}

			info, err := entry.Info()
			if err != nil {
				report(path, "walk", err.Error())
				continue
			}

			select {
			case <-ctx.Done():
				q.Done()
				return
			case out <- FileInfo{
				Path:  path,
				Size:  info.Size(),
				MTime: info.ModTime(),
			}:
			}
		}

		q.Done()
	}
}
