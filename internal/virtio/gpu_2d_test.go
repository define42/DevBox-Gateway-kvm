package virtio

import (
	"encoding/binary"
	"testing"
)

func TestGPUAdvertisesOnly2D(t *testing.T) {
	t.Parallel()
	gpu := NewGPU(11, nil, nil, nil)
	t.Cleanup(func() { _ = gpu.Close() })
	if got := gpu.DeviceFeatures(); got != 0 {
		t.Fatalf("optional GPU features = %#x, want none", got)
	}
	if gpu.features&1 != 0 {
		t.Fatal("transport advertised the removed 3D feature")
	}
	var config [gpuConfigLen]byte
	gpu.ReadDeviceConfig(0, config[:])
	if got := binary.LittleEndian.Uint32(config[12:]); got != 0 {
		t.Fatalf("capset count = %d, want zero", got)
	}
}

func TestGPURejects3DCommands(t *testing.T) {
	t.Parallel()
	// Wire command IDs remain guest-controlled even when their feature bit
	// is absent. Unsupported capset and context commands must fail safely.
	for _, test := range []struct {
		name    string
		command uint32
	}{
		{"capset info", 0x0108},
		{"capset", 0x0109},
		{"context create", 0x0200},
		{"context destroy", 0x0201},
		{"context attach", 0x0202},
		{"context detach", 0x0203},
		{"resource create 3D", 0x0204},
		{"transfer to host 3D", 0x0205},
		{"transfer from host 3D", 0x0206},
		{"submit 3D", 0x0207},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			gpu := NewGPU(11, nil, nil, nil)
			t.Cleanup(func() { _ = gpu.Close() })
			request := gpuResizeRequest(test.command)
			binary.LittleEndian.PutUint32(request[4:], gpuFlagFence)
			const fence = uint64(1)<<48 | 123
			binary.LittleEndian.PutUint64(request[8:], fence)
			response := gpu.handleControl(request)
			if len(response) != gpuCtrlHdrLen || binary.LittleEndian.Uint32(response) != gpuRespErrUnspec {
				t.Fatalf("unsupported command returned %x", response)
			}
			if binary.LittleEndian.Uint32(response[4:]) != gpuFlagFence ||
				binary.LittleEndian.Uint64(response[8:]) != fence {
				t.Fatalf("rejected command lost its fence: %x", response)
			}
			if len(gpu.resources) != 0 || gpu.resourceBytes != 0 {
				t.Fatal("unsupported command allocated GPU resources")
			}
		})
	}
}
