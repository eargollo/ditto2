package scan

import (
	"container/heap"
	"context"
)

// sizeHeap is a min-heap of HashedFiles ordered by file size (ascending).
type sizeHeap []HashedFile

func (h sizeHeap) Len() int            { return len(h) }
func (h sizeHeap) Less(i, j int) bool  { return h[i].Size < h[j].Size }
func (h sizeHeap) Swap(i, j int)       { h[i], h[j] = h[j], h[i] }
func (h *sizeHeap) Push(x any)         { *h = append(*h, x.(HashedFile)) }
func (h *sizeHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}

// RunSizePriorityQueue sits between the partial-hash grouper and the full
// hashers. It buffers incoming HashedFiles in a min-heap keyed by file size
// and dispatches the smallest file first. Hashing small files first maximises
// cache coverage early in the scan and keeps the full-hasher slots busy with
// short-running jobs, improving overall pipeline throughput.
//
// out is closed when in is exhausted or ctx is cancelled.
func RunSizePriorityQueue(ctx context.Context, in <-chan HashedFile, out chan<- HashedFile) {
	go func() {
		defer close(out)

		h := &sizeHeap{}
		heap.Init(h)

		for {
			if h.Len() > 0 {
				// Race: either accept a new item from in, or dispatch the
				// smallest item to out — whichever is ready first.
				select {
				case item, ok := <-in:
					if !ok {
						// in is closed; drain the heap smallest-first.
						for h.Len() > 0 {
							item := heap.Pop(h).(HashedFile)
							select {
							case out <- item:
							case <-ctx.Done():
								return
							}
						}
						return
					}
					heap.Push(h, item)
				case out <- (*h)[0]:
					heap.Pop(h)
				case <-ctx.Done():
					return
				}
			} else {
				// Heap is empty — block until a new item arrives.
				select {
				case item, ok := <-in:
					if !ok {
						return
					}
					heap.Push(h, item)
				case <-ctx.Done():
					return
				}
			}
		}
	}()
}
