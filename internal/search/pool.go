package search

import "context"

// WorkerPool bounds simultaneous point evaluations across independent searches.
// Each search owns its terrain, grids and results; only the worker budget is
// shared. Waiting for a worker is cancellable.
type WorkerPool struct{ slots chan struct{} }

func NewWorkerPool(workers int) *WorkerPool {
	if workers < 1 || workers > 256 {
		panic("search worker count must be between 1 and 256")
	}
	return &WorkerPool{slots: make(chan struct{}, workers)}
}

func (p *WorkerPool) acquire(ctx context.Context) bool {
	if ctx.Err() != nil {
		return false
	}
	if p == nil {
		return true
	}
	select {
	case p.slots <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}

func (p *WorkerPool) release() {
	if p != nil {
		<-p.slots
	}
}
