package virtio

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/internal/virgl"
)

var errGPU3DTestReadback = errors.New("readback failed")

type gpuRendererStub struct {
	created           []virgl.ResourceArgs
	attached          int
	submissions       int
	fenced, completed []uint32
	resets, closes    int
	readErr           error
	readColor         color.RGBA
}

func (r *gpuRendererStub) Capsets() []virgl.Capset {
	return []virgl.Capset{{ID: 1, MaxVersion: 1, MaxSize: 4}}
}
func (r *gpuRendererStub) Capset(uint32, uint32) ([]byte, error) { return []byte{1, 2, 3, 4}, nil }
func (r *gpuRendererStub) CreateContext(uint32, string) error    { return nil }
func (r *gpuRendererStub) DestroyContext(uint32) error           { return nil }
func (r *gpuRendererStub) CreateResource(a virgl.ResourceArgs) error {
	r.created = append(r.created, a)

	return nil
}
func (r *gpuRendererStub) UnrefResource(uint32) error { return nil }
func (r *gpuRendererStub) AttachBacking(uint32, [][]byte) error {
	r.attached++

	return nil
}
func (r *gpuRendererStub) DetachBacking(uint32) error                 { return nil }
func (r *gpuRendererStub) ContextAttachResource(uint32, uint32) error { return nil }
func (r *gpuRendererStub) ContextDetachResource(uint32, uint32) error { return nil }
func (r *gpuRendererStub) Transfer(uint32, uint32, uint32, uint32, uint32, virgl.Box, uint64, bool) error {
	return nil
}

func (r *gpuRendererStub) Submit(uint32, []byte) error {
	r.submissions++

	return nil
}

func (r *gpuRendererStub) ReadPixels(_, _, _, w, h uint32) (*image.RGBA, error) {
	if r.readErr != nil {
		return nil, r.readErr
	}
	img := image.NewRGBA(image.Rect(0, 0, int(w), int(h)))
	for y := 0; y < int(h); y++ {
		for x := 0; x < int(w); x++ {
			img.SetRGBA(x, y, r.readColor)
		}
	}

	return img, nil
}

func (r *gpuRendererStub) Fence(id uint32) error {
	r.fenced = append(r.fenced, id)

	return nil
}

func (r *gpuRendererStub) Poll() []uint32 {
	ids := r.completed
	r.completed = nil

	return ids
}

func (r *gpuRendererStub) Reset() error {
	r.resets++
	r.completed = nil

	return nil
}

func (r *gpuRendererStub) Close() error {
	r.closes++

	return nil
}

func gpu3DTestRequest(command, ctx uint32, words ...uint32) []byte {
	b := make([]byte, gpuCtrlHdrLen+4*len(words))
	binary.LittleEndian.PutUint32(b, command)
	binary.LittleEndian.PutUint32(b[16:], ctx)
	for i, w := range words {
		binary.LittleEndian.PutUint32(b[gpuCtrlHdrLen+4*i:], w)
	}

	return b
}

func TestGPU3DDisabledAndCapabilities(t *testing.T) {
	t.Parallel()
	plain := NewGPU(11, nil, nil, nil)
	defer plain.Close()
	if plain.DeviceFeatures() != 0 {
		t.Fatal("default GPU advertised native features")
	}
	cfg := make([]byte, 16)
	plain.ReadDeviceConfig(0, cfg)
	if binary.LittleEndian.Uint32(cfg[12:]) != 0 {
		t.Fatal("disabled GPU exposed capsets")
	}
	disabledCaps := plain.handleControl(gpu3DTestRequest(gpuCmdGetCapsetInfo, 0, 0, 0))
	if got := binary.LittleEndian.Uint32(disabledCaps); got != gpuRespErrUnspec {
		t.Fatalf("disabled capset request = %#x", got)
	}
	r := &gpuRendererStub{}
	g := newGPUWithRenderer(11, nil, nil, nil, r)
	defer g.Close()
	if g.DeviceFeatures() != gpuFeatureVirgl || g.features&gpuFeatureVirgl == 0 {
		t.Fatal("native feature was not cached in transport")
	}
	g.ReadDeviceConfig(0, cfg)
	if binary.LittleEndian.Uint32(cfg[12:]) != 1 {
		t.Fatal("incorrect native capset count")
	}
	info := g.handleControl(gpu3DTestRequest(gpuCmdGetCapsetInfo, 0, 0, 0))
	if len(info) != 40 || binary.LittleEndian.Uint32(info) != gpuRespOKCapsetInfo ||
		binary.LittleEndian.Uint32(info[32:]) != 4 {
		t.Fatalf("capset info: %x", info)
	}
	caps := g.handleControl(gpu3DTestRequest(gpuCmdGetCapset, 0, 1, 1))
	if !bytes.Equal(caps[24:], []byte{1, 2, 3, 4}) {
		t.Fatalf("caps: %x", caps)
	}
	for _, request := range [][]byte{
		gpu3DTestRequest(gpuCmdGetCapsetInfo, 0, 1, 0),
		gpu3DTestRequest(gpuCmdGetCapset, 0, 1, 2),
	} {
		if got := binary.LittleEndian.Uint32(g.handleControl(request)); got != gpuRespErrInvalidParameter {
			t.Fatalf("invalid capset response %#x", got)
		}
	}
}

//nolint:paralleltest,tparallel // Cases share a mutable renderer to verify rejected commands have no side effects.
func TestGPU3DValidatesBeforeCallingRenderer(t *testing.T) {
	t.Parallel()
	r := &gpuRendererStub{}
	g := newGPUWithRenderer(11, nil, make([]byte, 4096), nil, r)
	defer g.Close()
	gpuDamageOK(t, g, gpu3DTestRequest(gpuCmdContextCreate, 1, make([]uint32, 18)...))
	for _, tc := range []struct {
		name     string
		request  []byte
		response uint32
	}{
		{"unknown context", gpu3DTestRequest(gpuCmdSubmit3D, 2, 4, 0, 0), gpuRespErrInvalidContextID},
		{"duplicate context", gpu3DTestRequest(gpuCmdContextCreate, 1, make([]uint32, 18)...), gpuRespErrInvalidContextID},
		{"truncated context", gpu3DTestRequest(gpuCmdContextCreate, 3, 0, 0), gpuRespErrInvalidParameter},
		{"truncated submit", gpu3DTestRequest(gpuCmdSubmit3D, 1, 8, 0, 0), gpuRespErrInvalidParameter},
		{"unaligned submit", gpu3DTestRequest(gpuCmdSubmit3D, 1, 3, 0, 0), gpuRespErrInvalidParameter},
		{"unknown resource", gpu3DTestRequest(gpuCmdContextAttachResource, 1, 99, 0), gpuRespErrInvalidResourceID},
		{
			"resource allocation overflow",
			gpu3DTestRequest(gpuCmdResourceCreate3D, 0, 1, 2, 2, 2, 16384, 16384, 2048, 2048, 14, 16, 0, 0),
			gpuRespErrOutOfMemory,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := binary.LittleEndian.Uint32(g.handleControl(tc.request)); got != tc.response {
				t.Fatalf("response %#x, want %#x", got, tc.response)
			}
		})
	}
	if len(r.created) != 0 || r.submissions != 0 {
		t.Fatal("invalid commands reached native renderer")
	}
	// Mesa uses zero array_size for its legacy PIPE_BUFFER fence allocation;
	// buffer widths are bytes and may legitimately exceed screen dimensions.
	gpuDamageOK(t, g, gpu3DTestRequest(gpuCmdResourceCreate3D, 0, 2, 0, 64, 1<<17, 8<<20, 1, 1, 0, 0, 0, 0, 0))
	if g.resource3DBytes != 8<<20 {
		t.Fatalf("buffer charged %d bytes", g.resource3DBytes)
	}
	gpuDamageOK(t, g, gpu3DTestRequest(gpuCmdResourceUnref, 0, 2, 0))
	if g.resource3DBytes != 0 {
		t.Fatal("native allocation budget leaked")
	}
	gpuDamageOK(t, g, gpu3DTestRequest(gpuCmdResourceCreate2D, 0, 3, gpuFormatB8G8R8X8, 16, 16))
	if len(r.created) != 2 || r.created[1].Target != 2 || r.created[1].Flags != 1 {
		t.Fatal("2D resource did not reach native renderer")
	}
}

func TestGPU3DWallpaperTransitionBudget(t *testing.T) {
	t.Parallel()
	r := &gpuRendererStub{}
	g := newGPUWithRenderer(11, nil, nil, nil, r)
	defer g.Close()
	// A transition keeps both 4096-square RGBA8 wallpapers, including their
	// complete mip chains, alongside three 4K compositor buffers.
	for id := uint32(1); id <= 2; id++ {
		request := gpu3DTestRequest(gpuCmdResourceCreate3D, 0,
			id, 2, 104, 1<<3, 4096, 4096, 1, 1, 12, 0, 0, 0)
		if got := binary.LittleEndian.Uint32(g.handleControl(request)); got != gpuRespOKNoData {
			t.Fatalf("wallpaper %d creation response = %#x, want success", id, got)
		}
	}
	for id := uint32(3); id <= 5; id++ {
		gpuDamageOK(t, g, gpu3DTestRequest(gpuCmdResourceCreate3D, 0,
			id, 2, gpuFormatB8G8R8X8, (1<<1)|(1<<18), 4096, 2160, 1, 1, 0, 0, 1, 0))
	}
	const transitionBytes = 285125288
	if g.resource3DBytes != transitionBytes {
		t.Fatalf("transition charged %d bytes, want %d", g.resource3DBytes, transitionBytes)
	}
	for id := uint32(1); id <= 5; id++ {
		gpuDamageOK(t, g, gpu3DTestRequest(gpuCmdResourceUnref, 0, id, 0))
	}
	if g.resource3DBytes != 0 {
		t.Fatal("native texture budget leaked after releasing the transition resources")
	}
}

func TestGPU3DWallpaperUploadStagingBudget(t *testing.T) {
	t.Parallel()
	g := newGPUWithRenderer(11, nil, nil, nil, &gpuRendererStub{})
	defer g.Close()
	// GNOME can overlap old and new full-resolution wallpapers, their mip
	// chains, the new base texture and an upload buffer during a transition.
	// The native allocation budget must accommodate that overlap, which
	// exceeds one GiB here.
	for id := uint32(1); id <= 2; id++ {
		request := gpu3DTestRequest(gpuCmdResourceCreate3D, 0,
			id, 2, 104, 1<<3, 10000, 5622, 1, 1, 13, 0, 0, 0)
		if got := binary.LittleEndian.Uint32(g.handleControl(request)); got != gpuRespOKNoData {
			t.Fatalf("wallpaper %d creation response = %#x, want success", id, got)
		}
	}
	gpuDamageOK(t, g, gpu3DTestRequest(gpuCmdResourceCreate3D, 0,
		3, 2, gpuFormatR8G8B8X8, 1<<3, 10000, 5622, 1, 1, 0, 0, 0, 0))
	gpuDamageOK(t, g, gpu3DTestRequest(gpuCmdResourceCreate3D, 0,
		4, 0, 64, 1<<17, 224882688, 1, 1, 0, 0, 0, 0, 0))
	for id := uint32(5); id <= 7; id++ {
		gpuDamageOK(t, g, gpu3DTestRequest(gpuCmdResourceCreate3D, 0,
			id, 2, gpuFormatB8G8R8X8, (1<<1)|(1<<18), 1920, 1080, 1, 1, 0, 0, 1, 0))
	}
	const transitionBytes = 1074301616
	if g.resource3DBytes != transitionBytes {
		t.Fatalf("transition charged %d bytes, want %d", g.resource3DBytes, transitionBytes)
	}
	for id := uint32(1); id <= 7; id++ {
		gpuDamageOK(t, g, gpu3DTestRequest(gpuCmdResourceUnref, 0, id, 0))
	}
	if g.resource3DBytes != 0 {
		t.Fatal("native texture or staging buffer budget leaked")
	}
}

func TestGPU3DNativeBudgetLimitAndRelease(t *testing.T) {
	t.Parallel()
	r := &gpuRendererStub{}
	g := newGPUWithRenderer(11, nil, nil, nil, r)
	defer g.Close()
	// The renderer stub records resources without allocating their pixels.
	// Two one-GiB RGBA8 textures fill the native budget exactly.
	for id := uint32(1); id <= 2; id++ {
		gpuDamageOK(t, g, gpu3DTestRequest(gpuCmdResourceCreate3D, 0,
			id, 2, gpuFormatR8G8B8A8, 1<<3, 16384, 16384, 1, 1, 0, 0, 0, 0))
	}
	extra := gpu3DTestRequest(gpuCmdResourceCreate3D, 0,
		3, 2, gpuFormatR8G8B8A8, 1<<3, 1, 1, 1, 1, 0, 0, 0, 0)
	if got := binary.LittleEndian.Uint32(g.handleControl(extra)); got != gpuRespErrOutOfMemory {
		t.Fatalf("over-budget creation response = %#x, want out of memory", got)
	}
	if len(r.created) != 2 || g.resource3DBytes != 2<<30 {
		t.Fatal("rejected allocation changed native resources or their budget")
	}
	gpuDamageOK(t, g, gpu3DTestRequest(gpuCmdResourceUnref, 0, 1, 0))
	if g.resource3DBytes != 1<<30 {
		t.Fatal("released resource did not restore its quota")
	}
	gpuDamageOK(t, g, gpu3DTestRequest(gpuCmdResourceCreate3D, 0,
		3, 2, gpuFormatR8G8B8A8, 1<<3, 16384, 16384, 1, 1, 0, 0, 0, 0))
	for id := uint32(2); id <= 3; id++ {
		gpuDamageOK(t, g, gpu3DTestRequest(gpuCmdResourceUnref, 0, id, 0))
	}
	if g.resource3DBytes != 0 {
		t.Fatal("native allocation budget leaked")
	}
}

func TestGPU3DUbuntuWallpaperBudget(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		lastLevel uint32
	}{
		{name: "base image", lastLevel: 0},
		{name: "complete mip chain", lastLevel: 13},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := newGPUWithRenderer(11, nil, nil, nil, &gpuRendererStub{})
			defer g.Close()
			gpuDamageOK(t, g, gpu3DTestRequest(gpuCmdResourceCreate3D, 0,
				1, 2, 104, 1<<3, 4096, 4096, 1, 1, 12, 0, 0, 0))
			for id := uint32(2); id <= 4; id++ {
				gpuDamageOK(t, g, gpu3DTestRequest(gpuCmdResourceCreate3D, 0,
					id, 2, gpuFormatB8G8R8X8, (1<<1)|(1<<18), 1920, 1080, 1, 1, 0, 0, 1, 0))
			}
			// Ubuntu's Province_of_the_south_of_france_by_orbitelambda.jpg is
			// 10000x5622. It fits with the old wallpaper and compositor buffers.
			request := gpu3DTestRequest(gpuCmdResourceCreate3D, 0,
				5, 2, 104, 1<<3, 10000, 5622, 1, 1, tc.lastLevel, 0, 0, 0)
			if got := binary.LittleEndian.Uint32(g.handleControl(request)); got != gpuRespOKNoData {
				t.Fatalf("Ubuntu wallpaper creation response = %#x, want success", got)
			}
		})
	}
}

func TestGPU3DTextureFormatCharges(t *testing.T) {
	t.Parallel()
	// Format IDs are the stable virgl_hw.h wire values, including the sRGB
	// variants used for desktop image textures.
	for _, tc := range []struct {
		name   string
		format uint32
		bytes  uint64
	}{
		{name: "BGRA8", format: 1, bytes: 64},
		{name: "BGRX8", format: 2, bytes: 64},
		{name: "ARGB8", format: 3, bytes: 64},
		{name: "XRGB8", format: 4, bytes: 64},
		{name: "RGBA8", format: 67, bytes: 64},
		{name: "XBGR8", format: 68, bytes: 64},
		{name: "ABGR8", format: 121, bytes: 64},
		{name: "RGBX8", format: 134, bytes: 64},
		{name: "ABGR8 sRGB", format: 98, bytes: 64},
		{name: "XBGR8 sRGB", format: 99, bytes: 64},
		{name: "BGRA8 sRGB", format: 100, bytes: 64},
		{name: "BGRX8 sRGB", format: 101, bytes: 64},
		{name: "ARGB8 sRGB", format: 102, bytes: 64},
		{name: "XRGB8 sRGB", format: 103, bytes: 64},
		{name: "RGBA8 sRGB", format: 104, bytes: 64},
		{name: "RGBX8 sRGB", format: 230, bytes: 64},
		{name: "RGBA32 float", format: 31, bytes: 256},
		{name: "RGBA16 float conservative", format: 94, bytes: 256},
		{name: "unknown conservative", format: ^uint32(0), bytes: 256},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			charge, valid := nativeResourceCharge(virgl.ResourceArgs{
				Target: 2, Format: tc.format, Width: 4, Height: 4, Depth: 1, ArraySize: 1,
			})
			if !valid || charge != tc.bytes {
				t.Fatalf("texture charge = %d, valid = %t; want %d", charge, valid, tc.bytes)
			}
		})
	}
}

func TestGPU3DFencesCompleteOnlyAfterNativeSignal(t *testing.T) {
	t.Parallel()
	r := &gpuRendererStub{}
	mem := make([]byte, 1<<16)
	g := newGPUWithRenderer(11, nil, mem, nil, r)
	defer g.Close()
	g.nextFence = ^uint32(0) - 1
	q := &SplitQueue{Desc: new([QueueSize]SplitDesc), Avail: new(SplitAvail), Used: new(SplitUsed), Size: 8}
	g.VirtQueue[0] = q
	guestIDs := []uint64{1<<60 | 99, 7} // Arbitrary and deliberately non-monotonic.
	for i, id := range guestIDs {
		req := gpu3DTestRequest(gpuCmdGetDisplayInfo, 0)
		binary.LittleEndian.PutUint32(req[4:], gpuFlagFence)
		binary.LittleEndian.PutUint64(req[8:], id)
		addr := uint64(0x1000 + i*0x1000)
		copy(mem[addr:], req)
		q.Desc[i*2] = SplitDesc{Addr: addr, Len: uint32(len(req)), Flags: descFNext, Next: uint16(i*2 + 1)}
		q.Desc[i*2+1] = SplitDesc{Addr: addr + 0x800, Len: 512, Flags: descFWrite}
		q.Avail.Ring[i] = uint16(i * 2)
	}
	q.Avail.Idx = 2
	if err := g.ProcessControlQueue(); err != nil && !errors.Is(err, ErrNoGPUReq) {
		t.Fatal(err)
	}
	if q.Used.Idx != 0 || len(r.fenced) != 2 || r.fenced[0] != ^uint32(0) || r.fenced[1] != 0 {
		t.Fatalf("premature completion or incorrect fence tokens: used=%d tokens=%v", q.Used.Idx, r.fenced)
	}
	g.pollFences()
	if q.Used.Idx != 0 {
		t.Fatal("poll without completion published responses")
	}
	r.completed = []uint32{0} // Coalesces both fences across token wrap.
	g.pollFences()
	if q.Used.Idx != 2 || len(g.fences) != 0 {
		t.Fatal("coalesced native fence did not finish all requests")
	}
	for i, id := range guestIDs {
		response := mem[0x1800+i*0x1000:]
		if binary.LittleEndian.Uint64(response[8:]) != id {
			t.Fatal("guest 64-bit fence ID was changed")
		}
		if q.Used.Ring[i].ID != uint32(i*2) || q.Used.Ring[i].Len != gpuCtrlHdrLen+gpuMaxScanouts*gpuDisplayOneLen {
			t.Fatal("incorrect used descriptor")
		}
	}
	// Reset must not finish a request against queues the guest has discarded.
	g.fences = []gpuPendingFence{{id: 1, queue: q, queueSize: 8, head: 4}}
	g.Reset()
	r.completed = []uint32{1}
	g.pollFences()
	if q.Used.Idx != 2 || r.resets != 1 || len(g.fences) != 0 {
		t.Fatal("reset retained native queues or fences")
	}
	if err := g.Close(); err != nil {
		t.Fatal(err)
	}
	if r.closes != 1 {
		t.Fatal("native cleanup not called exactly once")
	}
}

func TestGPU3DBackingBudgetAndLifecycle(t *testing.T) {
	t.Parallel()
	r := &gpuRendererStub{}
	g := newGPUWithRenderer(11, nil, make([]byte, 4096), nil, r)
	defer g.Close()
	gpuDamageOK(t, g, gpu3DTestRequest(gpuCmdResourceCreate3D, 0, 1, 0, 64, 1<<17, 4096, 1, 1, 0, 0, 0, 0, 0))
	attach := gpu3DTestRequest(gpuCmdResourceAttachBacking, 0, 1, 1, 0, 0, 4096, 0)
	// Budget is aggregate, including duplicate guest pages across resources.
	g.backing3DBytes = (512 << 20) - 4095
	if got := binary.LittleEndian.Uint32(g.handleControl(attach)); got != gpuRespErrInvalidParameter {
		t.Fatalf("backing budget response %#x", got)
	}
	g.backing3DBytes = 0
	g.backing3DEntries = gpuMax3DBacking
	if got := binary.LittleEndian.Uint32(g.handleControl(attach)); got != gpuRespErrInvalidParameter {
		t.Fatalf("vector budget response %#x", got)
	}
	g.backing3DEntries = 0
	gpuDamageOK(t, g, attach)
	if r.attached != 1 || g.backing3DBytes != 4096 || g.backing3DEntries != 1 {
		t.Fatal("backing not charged")
	}
	if got := binary.LittleEndian.Uint32(g.handleControl(attach)); got != gpuRespErrInvalidParameter {
		t.Fatal("duplicate backing accepted")
	}
	gpuDamageOK(t, g, gpu3DTestRequest(gpuCmdResourceDetachBacking, 0, 1, 0))
	if g.backing3DBytes != 0 || g.backing3DEntries != 0 {
		t.Fatal("detach leaked backing charge")
	}
	gpuDamageOK(t, g, attach)
	gpuDamageOK(t, g, gpu3DTestRequest(gpuCmdResourceUnref, 0, 1, 0))
	if g.backing3DBytes != 0 || g.backing3DEntries != 0 || g.resource3DBytes != 0 {
		t.Fatal("unref leaked native accounting")
	}
}

func TestGPU3DReadbackFailureAndDamage(t *testing.T) {
	t.Parallel()
	r := &gpuRendererStub{readErr: errGPU3DTestReadback, readColor: color.RGBA{G: 255, A: 255}}
	d := &gpuDamageSink{}
	g := newGPUWithRenderer(11, nil, nil, d, r)
	defer g.Close()
	gpuDamageOK(t, g, gpu3DTestRequest(gpuCmdResourceCreate2D, 0, 1, gpuFormatB8G8R8X8, 4, 4))
	gpuDamageOK(t, g, gpuDamageCommand(gpuCmdSetScanout, 1, image.Rect(0, 0, 4, 4)))
	partial := gpuDamageCommand(gpuCmdResourceFlush, 1, image.Rect(1, 1, 2, 2))
	if got := binary.LittleEndian.Uint32(g.handleControl(partial)); got != gpuRespErrUnspec {
		t.Fatal("readback failure not reported")
	}
	if g.frame != nil || d.frames != 0 {
		t.Fatal("failed readback committed a partial frame")
	}
	r.readErr = nil
	gpuDamageOK(t, g, partial)
	if d.frame.RGBAAt(3, 3) != r.readColor {
		t.Fatal("first successful flush did not restore full frame")
	}
	r.readColor = color.RGBA{R: 255, A: 255}
	gpuDamageOK(t, g, partial)
	if d.frame.RGBAAt(1, 1) != r.readColor || d.frame.RGBAAt(3, 3) != (color.RGBA{G: 255, A: 255}) {
		t.Fatal("native partial flush changed pixels outside damage")
	}
}

func TestGPU3DCursorShadowInvalidatedByNativeUpload(t *testing.T) {
	t.Parallel()
	r := &gpuRendererStub{readColor: color.RGBA{R: 128, G: 64, A: 255}}
	g := newGPUWithRenderer(11, nil, make([]byte, 65536), nil, r)
	defer g.Close()
	gpuDamageOK(t, g, gpu3DTestRequest(gpuCmdResourceCreate2D, 0, 1, gpuFormatB8G8R8X8, 64, 64))
	gpuDamageOK(t, g, gpu3DTestRequest(gpuCmdResourceAttachBacking, 0, 1, 1, 0, 0, 64*64*4, 0))
	copy(g.Mem, []byte{0, 64, 128, 128})
	gpuDamageOK(t, g, gpu3DTestRequest(gpuCmdTransferToHost2D, 0, 0, 0, 64, 64, 0, 0, 1, 0))
	cursor := gpu3DTestRequest(gpuCmdUpdateCursor, 0, 0, 0, 0, 0, 1, 0, 0, 0)
	if !g.handleCursor(cursor) || g.cursor.image.RGBAAt(0, 0) != (color.RGBA{R: 128, G: 64, A: 128}) {
		t.Fatal("native XRGB upload lost cursor transparency")
	}
	gpuDamageOK(t, g, gpu3DTestRequest(gpuCmdTransferToHost3D, 0, 0, 0, 0, 64, 64, 1, 0, 0, 1, 0, 256, 0))
	if g.resources[1].data != nil || g.resourceBytes != 0 {
		t.Fatal("native upload kept stale cursor shadow")
	}
}

func TestGPU3DLargeCommandBufferThroughQueue(t *testing.T) {
	t.Parallel()
	r := &gpuRendererStub{}
	mem := make([]byte, 1<<20)
	g := newGPUWithRenderer(11, nil, mem, nil, r)
	defer g.Close()
	gpuDamageOK(t, g, gpu3DTestRequest(gpuCmdContextCreate, 1, make([]uint32, 18)...))
	q := &SplitQueue{Desc: new([QueueSize]SplitDesc), Avail: new(SplitAvail), Used: new(SplitUsed)}
	g.VirtQueue[0] = q
	// Mesa command buffers exceed the old limit sized only for 2D backing
	// descriptors. Exercise the descriptor collector, not just the dispatcher.
	const commandBytes = 256 << 10
	req := gpu3DTestRequest(gpuCmdSubmit3D, 1, commandBytes, 0)
	req = append(req, make([]byte, commandBytes)...)
	copy(mem[4096:], req)
	q.Desc[0] = SplitDesc{Addr: 4096, Len: uint32(len(req)), Flags: descFNext, Next: 1}
	q.Desc[1] = SplitDesc{Addr: 800000, Len: gpuCtrlHdrLen, Flags: descFWrite}
	q.Avail.Idx = 1
	if err := g.ProcessControlQueue(); err != nil {
		t.Fatal(err)
	}
	if r.submissions != 1 || q.Used.Idx != 1 || binary.LittleEndian.Uint32(mem[800000:]) != gpuRespOKNoData {
		t.Fatal("large virgl command buffer was rejected by the control queue")
	}
}
