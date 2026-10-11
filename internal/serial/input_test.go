package serial

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"
	"time"
)

func TestStartContextCancelsBlockedInput(t *testing.T) {
	t.Parallel()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	s, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	received := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		done <- s.StartContext(ctx, r, func() error {
			received <- struct{}{}

			return nil
		})
	}()
	if _, err := w.Write([]byte{'a'}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-received:
	case <-time.After(time.Second):
		t.Fatal("reader did not consume input")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("reader did not stop")
	}
	if _, err := r.Stat(); err != nil {
		t.Fatalf("reader closed caller's file: %v", err)
	}
}

func TestStartContextCancelsFullInputQueue(t *testing.T) {
	t.Parallel()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	s, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	for range cap(s.inputChan) {
		s.inputChan <- 0
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.StartContext(ctx, r, func() error { return nil }) }()
	if _, err := w.Write([]byte{'a'}); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("full input queue prevented cancellation")
	}
}

func TestStartContextTerminalEscape(t *testing.T) {
	t.Parallel()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	s, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte{1, 'x'}); err != nil {
		t.Fatal(err)
	}
	if err := s.StartContext(t.Context(), r, func() error { return nil }); !errors.Is(err, io.EOF) {
		t.Fatalf("got %v", err)
	}
}
