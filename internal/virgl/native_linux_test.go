//go:build linux && cgo && virgl && integration

package virgl

import (
	"bytes"
	"fmt"
	"runtime"
	"testing"
	"time"
)

// Run explicitly with -tags 'virgl integration' on a host with EGL support.
//
//nolint:paralleltest // Cases sequentially create resources in the one process-wide native renderer.
func TestNativeTransferReadbackAndReset(t *testing.T) {
	r, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	})
	for _, format := range []uint32{1, 2, 67, 134} {
		for _, flags := range []uint32{0, 1} {
			t.Run(fmt.Sprintf("format%d_flags%d", format, flags), func(t *testing.T) {
				args := ResourceArgs{
					Handle: 1, Target: 2, Format: format, Bind: 1 << 1,
					Width: 2, Height: 2, Depth: 1, ArraySize: 1, Flags: flags,
				}
				if err := r.CreateResource(args); err != nil {
					t.Fatal(err)
				}
				// Every row/channel differs, including alpha, to catch byte-order
				// and Y-origin mistakes. Split iov midway through a pixel.
				original := []byte{11, 22, 33, 44, 55, 66, 77, 88, 99, 111, 122, 133, 144, 155, 166, 177}
				memory := bytes.Clone(original)
				if err := r.AttachBacking(1, [][]byte{memory[:7], memory[7:]}); err != nil {
					t.Fatal(err)
				}
				runtime.GC() // backing must remain pinned after the cgo call returns
				if err := r.Transfer(1, 0, 0, 8, 0, Box{W: 2, H: 2, D: 1}, 0, true); err != nil {
					t.Fatal(err)
				}
				got, err := r.ReadPixels(1, 0, 0, 2, 2)
				if err != nil {
					t.Fatal(err)
				}
				want := bytes.Clone(original)
				convertPixels(want, format)
				if !bytes.Equal(got.Pix, want) {
					t.Fatalf("RGBA=%v, want %v", got.Pix, want)
				}
				crop, err := r.ReadPixels(1, 1, 1, 1, 1)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(crop.Pix, want[12:16]) {
					t.Fatalf("crop=%v, want %v", crop.Pix, want[12:16])
				}
				if err := r.Reset(); err != nil {
					t.Fatal(err)
				}
				if ids := r.Poll(); len(ids) != 0 {
					t.Fatalf("stale fence after reset: %v", ids)
				}
			})
		}
	}
}

//nolint:paralleltest // Reset changes the process-wide native renderer state.
func TestNativeFenceReset(t *testing.T) {
	r, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	if err := r.Fence(7); err != nil {
		t.Fatal(err)
	}
	if err := r.Reset(); err != nil {
		t.Fatal(err)
	}
	if ids := r.Poll(); len(ids) != 0 {
		t.Fatalf("reset leaked fences: %v", ids)
	}
	if err := r.Fence(8); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ids := r.Poll(); len(ids) != 0 {
			if len(ids) != 1 || ids[0] != 8 {
				t.Fatalf("completion after reset = %v, want [8]", ids)
			}

			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("new fence did not complete after renderer reset")
}
