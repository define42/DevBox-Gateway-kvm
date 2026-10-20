//go:build linux

package virgl

import (
	"errors"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
)

var errTestInitialization = errors.New("initialization failed")

type threadBackend struct {
	backend
	threadID    int
	wrongThread atomic.Bool
	calls       atomic.Int32
	closed      atomic.Bool
}

func (b *threadBackend) check() {
	if syscall.Gettid() != b.threadID {
		b.wrongThread.Store(true)
	}
}

func (b *threadBackend) Capsets() []Capset {
	b.check()

	return []Capset{{ID: 1, MaxVersion: 1, MaxSize: 4}}
}

func (b *threadBackend) Reset() error {
	b.check()
	b.calls.Add(1)

	return nil
}

func (b *threadBackend) Close() error {
	b.check()
	b.closed.Store(true)

	return nil
}

//nolint:paralleltest // Exercises the process-wide native renderer singleton.
func TestRendererThreadAndClose(t *testing.T) {
	var state *threadBackend
	r, err := newRenderer(func() (backend, error) {
		state = &threadBackend{threadID: syscall.Gettid()}

		return state, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	secondFactory := func() (backend, error) {
		t.Fatal("second factory must not execute")

		return nil, errTestInitialization
	}
	if _, err := newRenderer(secondFactory); !errors.Is(err, ErrInUse) {
		t.Fatalf("second renderer: %v", err)
	}
	var wg sync.WaitGroup
	for range 24 {
		wg.Go(func() {
			for range 8 {
				if err := r.Reset(); err != nil {
					t.Error(err)
				}
			}
		})
	}
	wg.Wait()
	if got := state.calls.Load(); got != 24*8 {
		t.Fatalf("Reset calls = %d", got)
	}
	for range 16 {
		wg.Go(func() {
			if err := r.Close(); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if !state.closed.Load() || state.wrongThread.Load() {
		t.Fatalf("close=%v, wrong OS thread=%v", state.closed.Load(), state.wrongThread.Load())
	}
	if err := r.Reset(); !errors.Is(err, ErrClosed) {
		t.Fatalf("Reset after Close = %v", err)
	}
	if rendererInUse.Load() {
		t.Fatal("renderer singleton was not released")
	}
}

//nolint:paralleltest // Failed initialization must release the process-wide renderer singleton.
func TestRendererFailedInitReleasesSingleton(t *testing.T) {
	want := errTestInitialization
	for range 2 {
		_, err := newRenderer(func() (backend, error) { return nil, want })
		if !errors.Is(err, want) {
			t.Fatalf("New = %v", err)
		}
	}
}
