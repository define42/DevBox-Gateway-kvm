//go:build !linux || !cgo || !virgl

package virgl

import (
	"errors"
	"testing"
)

func TestUnavailable(t *testing.T) {
	t.Parallel()
	if Available() {
		t.Fatal("stub must not advertise native support")
	}
	if r, err := New(); r != nil || !errors.Is(err, ErrUnavailable) {
		t.Fatalf("New = %v, %v", r, err)
	}
}
