package virtio

import (
	"encoding/binary"
	"testing"
)

func TestGPUResourceCountLimitAndSlotReuse(t *testing.T) {
	t.Parallel()
	const resourceLimit = 1024
	gpu := NewGPU(11, nil, nil, nil)
	t.Cleanup(func() { _ = gpu.Close() })
	for id := uint32(1); id <= resourceLimit; id++ {
		gpuDamageOK(t, gpu, gpuResizeRequest(gpuCmdResourceCreate2D,
			id, gpuFormatB8G8R8X8, 1, 1))
	}
	request := gpuResizeRequest(gpuCmdResourceCreate2D,
		resourceLimit+1, gpuFormatB8G8R8X8, 1, 1)
	if got := binary.LittleEndian.Uint32(gpu.handleControl(request)); got != gpuRespErrOutOfMemory {
		t.Fatalf("2D count exhaustion returned %#x, want out of memory", got)
	}
	if len(gpu.resources) != resourceLimit || gpu.resourceBytes != resourceLimit*4 {
		t.Fatal("2D count rejection changed resources or accounting")
	}
	gpuDamageOK(t, gpu, gpuResizeRequest(gpuCmdResourceUnref, 1, 0))
	gpuDamageOK(t, gpu, request)
}
