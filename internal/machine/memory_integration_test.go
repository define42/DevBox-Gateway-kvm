//go:build integration

package machine

import "testing"

func TestNewGuestMemoryStartsZeroed(t *testing.T) {
	t.Parallel()
	m := newLifecycleMachine(t, 1)
	// Cover the full RAM allocation, including the high-memory area that was
	// previously overwritten with diagnostic instructions during construction.
	for addr, value := range m.mem {
		if value != 0 {
			t.Fatalf("new guest RAM at %#x = %#x, want zero", addr, value)
		}
	}
}
