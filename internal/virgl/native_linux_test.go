//go:build linux && cgo && virgl && integration

package virgl

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"runtime"
	"testing"
	"time"
)

// Native creation must preserve the context and texture bindings established
// by guest commands. Virgl caches both, so setting the same sampler view again
// cannot repair an overwritten binding or a stale current-context cache.
//
//nolint:paralleltest // Uses the one process-wide native renderer.
func TestNativeCreationPreservesGuestTextureBinding(t *testing.T) {
	args := ResourceArgs{
		Handle: 1, Target: 2, Format: 67, Bind: 1 << 3,
		Width: 2, Height: 2, Depth: 1, ArraySize: 1,
	}
	testCases := []struct {
		name   string
		create func(*Renderer) error
	}{
		{
			name: "resource",
			create: func(r *Renderer) error {
				other := args
				other.Handle = 2

				return r.CreateResource(other)
			},
		},
		{
			name: "context",
			create: func(r *Renderer) error {
				return r.CreateContext(2, "other")
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			r, err := New()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := r.Close(); err != nil {
					t.Error(err)
				}
			})
			if err := r.CreateContext(1, "texture-binding"); err != nil {
				t.Fatal(err)
			}
			if err := r.CreateResource(args); err != nil {
				t.Fatal(err)
			}
			if err := r.ContextAttachResource(1, 1); err != nil {
				t.Fatal(err)
			}
			submit := func(words ...uint32) {
				t.Helper()
				data := make([]byte, len(words)*4)
				for i, word := range words {
					binary.LittleEndian.PutUint32(data[i*4:], word)
				}
				if err := r.Submit(1, data); err != nil {
					t.Fatal(err)
				}
			}
			binding := func() int {
				t.Helper()
				var value int
				if err := r.call(func(backend) error {
					var err error
					value, err = nativeTextureBinding()

					return err
				}); err != nil {
					t.Fatal(err)
				}

				return value
			}
			// CREATE_OBJECT(SAMPLER_VIEW): RGBA8, mip/layer zero, identity swizzle.
			// SET_SAMPLER_VIEWS: fragment shader, slot zero, the created view.
			submit(0x00060601, 1, 1, 67, 0, 0, 0x688, 0x0003000a, 1, 0, 1)
			want := binding()
			if want == 0 {
				t.Fatal("guest sampler view did not bind its texture")
			}
			if err := tc.create(r); err != nil {
				t.Fatal(err)
			}
			// Resume the guest context with its unchanged sampler view. This is a
			// valid command that deliberately preserves virgl's cached binding.
			submit(0x0003000a, 1, 0, 1)
			if got := binding(); got != want {
				t.Fatalf("native creation changed guest texture binding: got %d, want %d", got, want)
			}
		})
	}
}

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
