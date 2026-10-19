package vmm

import "testing"

func TestCloseBeforeInit(t *testing.T) {
	t.Parallel()

	v := New(Config{})
	for range 2 {
		if err := v.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
