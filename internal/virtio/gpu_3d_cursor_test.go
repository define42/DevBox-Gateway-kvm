package virtio

import (
	"image/color"
	"testing"
)

func TestGPU3DCursorAlphaSurvivesSubmission(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		format uint32
		pixel  []byte
	}{
		{"bgrx", gpuFormatB8G8R8X8, []byte{0, 64, 128, 128}},
		{"xrgb", gpuFormatX8R8G8B8, []byte{128, 128, 64, 0}},
		{"xbgr", gpuFormatX8B8G8R8, []byte{128, 0, 64, 128}},
		{"rgbx", gpuFormatR8G8B8X8, []byte{128, 64, 0, 128}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			initial := color.RGBA{R: 128, G: 64, A: 255}
			g, renderer := newGPU3DCursorTest(t, test.format, test.pixel, initial)
			update := gpu3DCursorUpdateRequest()
			if !g.handleCursor(update) || g.cursor.image.RGBAAt(0, 0) != (color.RGBA{R: 128, G: 64, A: 128}) {
				t.Fatal("initial CPU cursor upload lost alpha")
			}

			// Even an unrelated draw submits the context containing the cursor.
			gpuDamageOK(t, g, gpu3DTestRequest(gpuCmdSubmit3D, 1, 4, 0, 0))
			if !g.handleCursor(update) || g.cursor.image.RGBAAt(0, 0) != (color.RGBA{R: 128, G: 64, A: 128}) {
				t.Fatal("unrelated submission discarded cursor transparency")
			}
			if got := g.cursor.image.RGBAAt(1, 0); got != (color.RGBA{}) {
				t.Fatalf("transparent cursor border became visible: %+v", got)
			}

			// Native color changes must not resurrect the old uploaded RGB.
			// Clamp channels to retained alpha to preserve premultiplication.
			renderer.readColor = color.RGBA{R: 200, G: 70, B: 40, A: 255}
			gpuDamageOK(t, g, gpu3DTestRequest(gpuCmdSubmit3D, 1, 4, 0, 0))
			if !g.handleCursor(update) {
				t.Fatal("native cursor update failed")
			}
			if got := g.cursor.image.RGBAAt(0, 0); got != (color.RGBA{R: 128, G: 70, B: 40, A: 128}) {
				t.Fatalf("native RGB and retained cursor alpha = %+v", got)
			}
			if got := g.cursor.image.RGBAAt(1, 0); got != (color.RGBA{}) {
				t.Fatalf("native RGB leaked through transparent cursor border: %+v", got)
			}

			gpuDamageOK(t, g, gpu3DTestRequest(gpuCmdResourceUnref, 0, 1, 0))
			if g.resourceBytes != 0 || g.resource3DBytes != 0 || g.backing3DBytes != 0 || g.backing3DEntries != 0 {
				t.Fatal("cursor unref leaked resource or backing accounting")
			}
		})
	}
}

func TestGPU3DCursorReadsNativeARGB(t *testing.T) {
	t.Parallel()
	initial := color.RGBA{R: 128, G: 64, A: 128}
	g, renderer := newGPU3DCursorTest(t, gpuFormatB8G8R8A8, []byte{0, 64, 128, 128}, initial)
	update := gpu3DCursorUpdateRequest()
	if !g.handleCursor(update) || g.cursor.image.RGBAAt(0, 0) != initial {
		t.Fatal("initial native ARGB cursor changed")
	}
	renderer.readColor = color.RGBA{R: 16, G: 32, B: 8, A: 64}
	gpuDamageOK(t, g, gpu3DTestRequest(gpuCmdSubmit3D, 1, 4, 0, 0))
	if !g.handleCursor(update) || g.cursor.image.RGBAAt(0, 0) != renderer.readColor {
		t.Fatal("native ARGB draw did not update cursor colors and alpha")
	}
}

func TestGPU3DCursorPartialUploadAfterDraw(t *testing.T) {
	t.Parallel()
	g, renderer := newGPU3DCursorTest(t, gpuFormatB8G8R8X8,
		[]byte{0, 64, 128, 128, 0, 64, 128, 255}, color.RGBA{R: 128, G: 64, A: 255})
	gpuDamageOK(t, g, gpu3DTestRequest(gpuCmdSubmit3D, 1, 4, 0, 0))
	renderer.readColor = color.RGBA{R: 50, G: 40, B: 20, A: 255}
	// Only the second pixel is uploaded. Untransferred backing changes must
	// not replace the first pixel's saved alpha or the latest native RGB.
	g.Mem[3] = 240
	copy(g.Mem[4:], []byte{16, 32, 48, 64})
	gpuDamageOK(t, g, gpu3DTestRequest(gpuCmdTransferToHost2D, 0, 1, 0, 1, 1, 4, 0, 1, 0))
	if !g.handleCursor(gpu3DCursorUpdateRequest()) {
		t.Fatal("cursor update after partial upload failed")
	}
	if got := g.cursor.image.RGBAAt(0, 0); got != (color.RGBA{R: 50, G: 40, B: 20, A: 128}) {
		t.Fatalf("partial upload replaced untouched cursor pixels: %+v", got)
	}
	if got := g.cursor.image.RGBAAt(1, 0); got != (color.RGBA{R: 50, G: 40, B: 20, A: 64}) {
		t.Fatalf("partial upload did not combine updated alpha with native RGB: %+v", got)
	}
}

func TestGPU3DCursorReadbackFailureKeepsShape(t *testing.T) {
	t.Parallel()
	g, renderer := newGPU3DCursorTest(t, gpuFormatB8G8R8X8,
		[]byte{0, 64, 128, 128}, color.RGBA{R: 128, G: 64, A: 255})
	update := gpu3DCursorUpdateRequest()
	if !g.handleCursor(update) {
		t.Fatal("initial cursor update failed")
	}
	renderer.readErr = errGPU3DTestReadback
	if g.handleCursor(update) {
		t.Fatal("failed native cursor readback was accepted")
	}
	if got := g.cursor.image.RGBAAt(0, 0); got != (color.RGBA{R: 128, G: 64, A: 128}) {
		t.Fatalf("failed native readback changed the previous cursor: %+v", got)
	}
}

func newGPU3DCursorTest(t *testing.T, format uint32, pixels []byte, native color.RGBA) (*GPU, *gpuRendererStub) {
	t.Helper()
	renderer := &gpuRendererStub{readColor: native}
	g := newGPUWithRenderer(11, nil, make([]byte, 65536), nil, renderer)
	t.Cleanup(func() {
		if err := g.Close(); err != nil {
			t.Errorf("close cursor GPU: %v", err)
		}
	})
	gpuDamageOK(t, g, gpu3DTestRequest(gpuCmdContextCreate, 1, make([]uint32, 18)...))
	gpuDamageOK(t, g, gpu3DTestRequest(gpuCmdResourceCreate2D, 0, 1, format, 64, 64))
	gpuDamageOK(t, g, gpu3DTestRequest(gpuCmdContextAttachResource, 1, 1, 0))
	gpuDamageOK(t, g, gpu3DTestRequest(gpuCmdResourceAttachBacking, 0, 1, 1, 0, 0, 64*64*4, 0))
	copy(g.Mem, pixels)
	gpuDamageOK(t, g, gpu3DTestRequest(gpuCmdTransferToHost2D, 0, 0, 0, 64, 64, 0, 0, 1, 0))

	return g, renderer
}

func gpu3DCursorUpdateRequest() []byte {
	return gpu3DTestRequest(gpuCmdUpdateCursor, 0, 0, 0, 0, 0, 1, 0, 0, 0)
}
