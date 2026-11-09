package search

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWorkerPoolBoundsConcurrentWorkAndCancelsWait(t *testing.T) {
	pool := NewWorkerPool(2)
	var active, peak atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if !pool.acquire(context.Background()) {
				t.Error("worker unexpectedly rejected")
				return
			}
			n := active.Add(1)
			for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
			}
			time.Sleep(time.Millisecond)
			active.Add(-1)
			pool.release()
		}()
	}
	wg.Wait()
	if peak.Load() != 2 || active.Load() != 0 {
		t.Fatalf("incorrect concurrency: peak %d, active %d", peak.Load(), active.Load())
	}
	pool.acquire(context.Background())
	pool.acquire(context.Background())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan bool, 1)
	go func() { done <- pool.acquire(ctx) }()
	cancel()
	select {
	case acquired := <-done:
		if acquired {
			t.Fatal("acquired occupied pool after cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled worker remained blocked")
	}
	pool.release()
	pool.release()
}

func TestConcurrentSearchReportsMatchSerialAndCancelWhenPoolFull(t *testing.T) {
	_, c, spectrum, ts, _ := benchmarkPoint()
	c.Bounds.South, c.Bounds.North, c.Bounds.West, c.Bounds.East = -.004, .004, -.004, .004
	c.ClockUncertainty = 2
	c.Refine = 100
	c.Workers = 4
	serial, err := Run(context.Background(), c, spectrum, ts)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := json.Marshal(serial)
	pool := NewWorkerPool(3)
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			report, err := RunWithPool(context.Background(), c, spectrum, ts, pool, nil)
			if err != nil {
				t.Error(err)
				return
			}
			got, _ := json.Marshal(report)
			if string(got) != string(want) {
				t.Error("concurrent evaluation changed scores, ranking or grid")
			}
		}()
	}
	wg.Wait()
	for i := 0; i < 3; i++ {
		pool.acquire(context.Background())
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := RunWithPool(ctx, c, spectrum, ts, pool, func(_ string, _, _ int) { cancel() })
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("wanted cancellation, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("search did not stop with occupied worker pool")
	}
	for i := 0; i < 3; i++ {
		pool.release()
	}
}
