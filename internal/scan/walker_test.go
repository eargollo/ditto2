package scan

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

// TestDirQueueNeverLosesItems pushes 5 000 items, pops all, and verifies the
// exact set is returned (compaction must not drop entries).
func TestDirQueueNeverLosesItems(t *testing.T) {
	const n = 5000
	q := newDirQueue()

	for i := 0; i < n; i++ {
		q.pending.Add(1)
		q.Push(fmt.Sprintf("dir%04d", i))
	}

	var got []string
	for {
		item, ok := q.Pop()
		if !ok {
			break
		}
		got = append(got, item)
		q.Done()
	}

	if len(got) != n {
		t.Fatalf("got %d items, want %d", len(got), n)
	}
	sort.Strings(got)
	for i, v := range got {
		if want := fmt.Sprintf("dir%04d", i); v != want {
			t.Errorf("item %d: got %q, want %q", i, v, want)
		}
	}
}

// TestDirQueueCompactionBoundsMemory interleaves push/pop batches and verifies
// the backing slice doesn't grow to the total number of historical pushes.
func TestDirQueueCompactionBoundsMemory(t *testing.T) {
	const batchSize = 2000
	const batches = 5 // total pushes = 10 000
	q := newDirQueue()

	for b := 0; b < batches; b++ {
		for i := 0; i < batchSize; i++ {
			q.pending.Add(1)
			q.Push(fmt.Sprintf("d%d_%04d", b, i))
		}
		for i := 0; i < batchSize; i++ {
			if _, ok := q.Pop(); !ok {
				t.Fatal("queue closed unexpectedly during drain")
			}
			q.Done()
		}
	}

	q.mu.Lock()
	remaining := len(q.items) - q.head
	totalCap := cap(q.items)
	q.mu.Unlock()

	if remaining != 0 {
		t.Errorf("expected empty queue after full drain, got %d remaining items", remaining)
	}
	// The backing-array capacity must be smaller than the total items ever
	// pushed, proving that old entries were released for garbage collection.
	totalPushes := batchSize * batches
	if totalCap >= totalPushes {
		t.Errorf("backing array capacity %d >= total pushes %d — compaction not releasing memory",
			totalCap, totalPushes)
	}
}

// TestWalkFindsAllFiles creates a tree of 15 files across 3 subdirs and
// verifies Walk returns all of them.
func TestWalkFindsAllFiles(t *testing.T) {
	root := t.TempDir()
	want := map[string]struct{}{}
	for i := 0; i < 3; i++ {
		sub := filepath.Join(root, fmt.Sprintf("sub%d", i))
		if err := os.Mkdir(sub, 0755); err != nil {
			t.Fatal(err)
		}
		for j := 0; j < 5; j++ {
			p := filepath.Join(sub, fmt.Sprintf("file%d.txt", j))
			if err := os.WriteFile(p, []byte("hello"), 0644); err != nil {
				t.Fatal(err)
			}
			want[p] = struct{}{}
		}
	}

	out := make(chan FileInfo, 100)
	Walk(context.Background(), []string{root}, nil, 4, out, noErrors(t))

	got := map[string]struct{}{}
	for fi := range out {
		got[fi.Path] = struct{}{}
	}
	for p := range want {
		if _, ok := got[p]; !ok {
			t.Errorf("missing expected file %q", p)
		}
	}
	if len(got) != len(want) {
		t.Errorf("found %d files, want %d", len(got), len(want))
	}
}

// TestWalkExcludesPaths verifies that a file listed in excludePaths is not
// returned, while a sibling file is still found.
func TestWalkExcludesPaths(t *testing.T) {
	root := t.TempDir()
	keep := filepath.Join(root, "keep.txt")
	skip := filepath.Join(root, "skip.txt")
	_ = os.WriteFile(keep, []byte("a"), 0644)
	_ = os.WriteFile(skip, []byte("b"), 0644)

	excludes := map[string]struct{}{skip: {}}
	out := make(chan FileInfo, 10)
	Walk(context.Background(), []string{root}, excludes, 2, out, noErrors(t))

	var foundSkip, foundKeep bool
	for fi := range out {
		switch fi.Path {
		case skip:
			foundSkip = true
		case keep:
			foundKeep = true
		}
	}
	if foundSkip {
		t.Errorf("excluded file %q was returned by Walk", skip)
	}
	if !foundKeep {
		t.Errorf("expected file %q was not returned by Walk", keep)
	}
}

// TestWalkCancellation verifies Walk returns cleanly after ctx is cancelled.
func TestWalkCancellation(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 200; i++ {
		_ = os.WriteFile(filepath.Join(root, fmt.Sprintf("f%d.txt", i)), []byte("data"), 0644)
	}

	ctx, cancel := context.WithCancel(context.Background())
	out := make(chan FileInfo, 8)

	done := make(chan struct{})
	go func() {
		Walk(ctx, []string{root}, nil, 2, out, noErrors(t))
		close(done)
	}()

	cancel()
	for range out {
	} // drain so walkers aren't blocked on sends

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Walk did not return after context cancel")
	}
}
