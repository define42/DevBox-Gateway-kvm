package virtio

import (
	"encoding/binary"
	"testing"
)

func TestGPU3DManySmallTexturesAndNativeCursor(t *testing.T) {
	t.Parallel()
	renderer := &gpuRendererStub{}
	gpu := newGPUWithRenderer(11, nil, nil, nil, renderer)
	t.Cleanup(func() { _ = gpu.Close() })

	// Games can retain many small textures without approaching a byte budget.
	// Cross the existing 2D count limit before creating a native 2D cursor:
	// that command must use the same native count policy as CREATE_3D.
	const textureCount = 1152
	for id := uint32(1); id <= textureCount; id++ {
		request := gpu3DTestRequest(gpuCmdResourceCreate3D, 0,
			id, 2, gpuFormatR8G8B8A8, 1<<3, 2, 2, 1, 1, 0, 0, 0, 0)
		if got := binary.LittleEndian.Uint32(gpu.handleControl(request)); got != gpuRespOKNoData {
			t.Fatalf("small texture %d rejected: response %#x", id, got)
		}
	}
	gpuDamageOK(t, gpu, gpu3DTestRequest(gpuCmdResourceCreate2D, 0,
		textureCount+1, gpuFormatB8G8R8X8, gpuCursorSize, gpuCursorSize))
	if len(gpu.resources) != textureCount+1 || len(renderer.created) != textureCount+1 {
		t.Fatal("small textures and native cursor did not all reach the renderer")
	}
	if got, want := gpu.resource3DBytes, uint64(textureCount*2*2*4+gpuCursorSize*gpuCursorSize*4); got != want {
		t.Fatalf("native allocation charge = %d, want %d", got, want)
	}
}

func TestGPU3DResourceCountLimitAndSlotReuse(t *testing.T) {
	t.Parallel()
	renderer := &gpuRendererStub{}
	gpu := newGPUWithRenderer(11, nil, nil, nil, renderer)
	t.Cleanup(func() { _ = gpu.Close() })

	if gpuMax3DResources <= gpuMaxResources {
		t.Fatal("native resource count must accommodate more than the 2D limit")
	}
	for id := uint32(1); id <= gpuMax3DResources; id++ {
		gpuDamageOK(t, gpu, gpu3DTestRequest(gpuCmdResourceCreate3D, 0,
			id, 2, gpuFormatR8G8B8A8, 1<<3, 1, 1, 1, 1, 0, 0, 0, 0))
	}
	beforeBytes := gpu.resource3DBytes
	const extraID = uint32(gpuMax3DResources + 1)
	requests := [][]byte{
		gpu3DTestRequest(gpuCmdResourceCreate3D, 0,
			extraID, 2, gpuFormatR8G8B8A8, 1<<3, 1, 1, 1, 1, 0, 0, 0, 0),
		gpu3DTestRequest(gpuCmdResourceCreate2D, 0,
			extraID, gpuFormatB8G8R8X8, gpuCursorSize, gpuCursorSize),
	}
	for _, request := range requests {
		if got := binary.LittleEndian.Uint32(gpu.handleControl(request)); got != gpuRespErrOutOfMemory {
			t.Fatalf("native count exhaustion returned %#x, want out of memory", got)
		}
	}
	if len(renderer.created) != gpuMax3DResources || len(gpu.resources) != gpuMax3DResources ||
		gpu.resource3DBytes != beforeBytes || gpu.resources[extraID] != nil {
		t.Fatal("rejected native allocations changed renderer resources or accounting")
	}

	gpuDamageOK(t, gpu, gpu3DTestRequest(gpuCmdResourceUnref, 0, 1, 0))
	// A freed slot must be usable by a native 2D resource while the total
	// remains far above the ordinary 2D resource-count limit.
	gpuDamageOK(t, gpu, requests[1])
	if len(gpu.resources) != gpuMax3DResources || len(renderer.created) != gpuMax3DResources+1 {
		t.Fatal("unref did not restore one native resource slot")
	}
	if got, want := gpu.resource3DBytes, beforeBytes-4+gpuCursorSize*gpuCursorSize*4; got != want {
		t.Fatalf("native slot replacement charge = %d, want %d", got, want)
	}
}

func TestGPU2DResourceCountLimitUnchanged(t *testing.T) {
	t.Parallel()
	const legacyLimit = 1024
	gpu := NewGPU(11, nil, nil, nil)
	t.Cleanup(func() { _ = gpu.Close() })
	for id := uint32(1); id <= legacyLimit; id++ {
		gpuDamageOK(t, gpu, gpu3DTestRequest(gpuCmdResourceCreate2D, 0,
			id, gpuFormatB8G8R8X8, 1, 1))
	}
	request := gpu3DTestRequest(gpuCmdResourceCreate2D, 0,
		legacyLimit+1, gpuFormatB8G8R8X8, 1, 1)
	if got := binary.LittleEndian.Uint32(gpu.handleControl(request)); got != gpuRespErrOutOfMemory {
		t.Fatalf("2D count exhaustion returned %#x, want out of memory", got)
	}
	if len(gpu.resources) != legacyLimit || gpu.resourceBytes != legacyLimit*4 {
		t.Fatal("2D count rejection changed resources or accounting")
	}
	gpuDamageOK(t, gpu, gpu3DTestRequest(gpuCmdResourceUnref, 0, 1, 0))
	gpuDamageOK(t, gpu, request)
}
