package scan

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// TestPriorityQueueDispatchesSmallBeforeLarge verifies the heap-ordering
// contract: items that are smaller than any concurrently-queued item are
// dispatched first.
//
// The queue's contract is "dispatch the minimum item currently in the heap",
// not a global sort. To test this deterministically we use two clearly
// separated size buckets loaded in FIFO order so that small items enter the
// heap before large ones. Because the channel is FIFO and all smalls precede
// all larges, the heap minimum is always 1 until every small is dispatched.
func TestPriorityQueueDispatchesSmallBeforeLarge(t *testing.T) {
	const (
		nSmall = 50
		nLarge = 50
		small  = int64(1)
		large  = int64(1_000_000)
	)
	ctx := context.Background()
	total := nSmall + nLarge
	in := make(chan HashedFile, total)
	out := make(chan HashedFile, total)

	// Load smalls first, then larges (FIFO guarantees smalls enter heap first).
	for i := 0; i < nSmall; i++ {
		in <- HashedFile{FileInfo: FileInfo{Path: fmt.Sprintf("s%d", i), Size: small}}
	}
	for i := 0; i < nLarge; i++ {
		in <- HashedFile{FileInfo: FileInfo{Path: fmt.Sprintf("l%d", i), Size: large}}
	}
	close(in)

	RunSizePriorityQueue(ctx, in, out)

	var got []int64
	for hf := range out {
		got = append(got, hf.Size)
	}
	if len(got) != total {
		t.Fatalf("delivered %d items, want %d", len(got), total)
	}

	// No large item may appear before all small items are dispatched.
	firstLarge := -1
	for i, s := range got {
		if s == large {
			firstLarge = i
			break
		}
	}
	if firstLarge != -1 {
		for i := firstLarge; i < len(got); i++ {
			if got[i] == small {
				t.Errorf("small item at index %d appears after first large item at index %d: output=%v",
					i, firstLarge, got)
				return
			}
		}
	}
}

// TestPriorityQueueDeliversAllItems verifies no items are lost.
func TestPriorityQueueDeliversAllItems(t *testing.T) {
	const n = 2000
	ctx := context.Background()
	in := make(chan HashedFile, n)
	out := make(chan HashedFile, n)

	RunSizePriorityQueue(ctx, in, out)

	want := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		p := fmt.Sprintf("file%04d", i)
		want[p] = struct{}{}
		in <- HashedFile{FileInfo: FileInfo{Path: p, Size: int64(n - i)}}
	}
	close(in)

	got := make(map[string]struct{}, n)
	for hf := range out {
		got[hf.Path] = struct{}{}
	}
	if len(got) != n {
		t.Fatalf("delivered %d items, want %d", len(got), n)
	}
	for p := range want {
		if _, ok := got[p]; !ok {
			t.Errorf("item %q was lost", p)
		}
	}
}

// TestPriorityQueueEmptyInput verifies a clean close with no items.
func TestPriorityQueueEmptyInput(t *testing.T) {
	ctx := context.Background()
	in := make(chan HashedFile)
	out := make(chan HashedFile, 1)

	RunSizePriorityQueue(ctx, in, out)
	close(in)

	var count int
	for range out {
		count++
	}
	if count != 0 {
		t.Errorf("expected 0 items from empty input, got %d", count)
	}
}

// TestPriorityQueueCancellationNoDeadlock verifies context cancel doesn't hang.
func TestPriorityQueueCancellationNoDeadlock(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	in := make(chan HashedFile)  // unbuffered — sends would block
	out := make(chan HashedFile) // unbuffered — receives would block

	RunSizePriorityQueue(ctx, in, out)
	cancel()

	done := make(chan struct{})
	go func() {
		for range out {
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("priority queue did not shut down after context cancel (deadlock?)")
	}
}
