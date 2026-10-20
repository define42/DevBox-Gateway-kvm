package virgl

import (
	"bytes"
	"testing"
)

func TestConvertPixels(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		format uint32
		input  []byte
	}{
		{"BGRA", 1, []byte{30, 20, 10, 255}},
		{"BGRX", 2, []byte{30, 20, 10, 0}},
		{"ARGB", 3, []byte{255, 10, 20, 30}},
		{"XRGB", 4, []byte{0, 10, 20, 30}},
		{"RGBA", 67, []byte{10, 20, 30, 255}},
		{"XBGR", 68, []byte{0, 30, 20, 10}},
		{"ABGR", 121, []byte{255, 30, 20, 10}},
		{"RGBX", 134, []byte{10, 20, 30, 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			convertPixels(tc.input, tc.format)
			if want := []byte{10, 20, 30, 255}; !bytes.Equal(tc.input, want) {
				t.Fatalf("RGBA = %v, want %v", tc.input, want)
			}
		})
	}
}
