package vmm

import "testing"

func TestCloseBeforeInit(t *testing.T) {
	t.Parallel()

	v := New(Config{})
	cleaned := 0
	v.isoCleanup = func() { cleaned++ }
	for range 2 {
		if err := v.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if cleaned != 1 {
		t.Fatalf("temporary boot media cleaned %d times, want once", cleaned)
	}
}
