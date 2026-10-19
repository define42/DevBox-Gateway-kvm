package virtio

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"testing"
	"testing/synctest"
	"time"

	"github.com/define42/DevBox-Gateway-kvm/internal/telemetry"
)

func gpuTelemetryTransferCommand(id uint32, rect image.Rectangle, offset uint64) []byte {
	req := make([]byte, gpuCtrlHdrLen+gpuRectLen+16)
	le := binary.LittleEndian
	le.PutUint32(req, gpuCmdTransferToHost2D)
	le.PutUint32(req[gpuCtrlHdrLen:], uint32(rect.Min.X))
	le.PutUint32(req[gpuCtrlHdrLen+4:], uint32(rect.Min.Y))
	le.PutUint32(req[gpuCtrlHdrLen+8:], uint32(rect.Dx()))
	le.PutUint32(req[gpuCtrlHdrLen+12:], uint32(rect.Dy()))
	le.PutUint64(req[gpuCtrlHdrLen+gpuRectLen:], offset)
	le.PutUint32(req[gpuCtrlHdrLen+gpuRectLen+8:], id)

	return req
}

func TestGPUTelemetryTransferCountsActualBytes(t *testing.T) {
	t.Parallel()
	type copiedRange struct{ dst, src, length int }
	for _, tc := range []struct {
		name    string
		rect    image.Rectangle
		offset  uint64
		backing []gpuMemEntry
		copied  []copiedRange
		want    uint64
	}{
		{
			name: "full", rect: image.Rect(0, 0, 4, 2),
			backing: []gpuMemEntry{{addr: 0, length: 32}},
			copied:  []copiedRange{{dst: 0, src: 0, length: 32}}, want: 32,
		},
		{
			name: "partial rows", rect: image.Rect(1, 0, 3, 2), offset: 4,
			backing: []gpuMemEntry{{addr: 0, length: 32}},
			copied:  []copiedRange{{dst: 4, src: 4, length: 8}, {dst: 20, src: 20, length: 8}}, want: 16,
		},
		{
			name: "fragmented", rect: image.Rect(0, 0, 4, 2),
			backing: []gpuMemEntry{{addr: 0, length: 0}, {addr: 4, length: 5}, {addr: 20, length: 27}},
			copied:  []copiedRange{{dst: 0, src: 4, length: 5}, {dst: 5, src: 20, length: 27}}, want: 32,
		},
		{
			name: "invalid and missing backing", rect: image.Rect(0, 0, 4, 2),
			backing: []gpuMemEntry{{addr: 0, length: 4}, {addr: 100, length: 4}, {addr: 4, length: 4}},
			copied:  []copiedRange{{dst: 0, src: 0, length: 4}, {dst: 8, src: 4, length: 4}}, want: 8,
		},
		{
			name: "short partial backing", rect: image.Rect(1, 0, 3, 2), offset: 4,
			backing: []gpuMemEntry{{addr: 0, length: 23}},
			copied:  []copiedRange{{dst: 4, src: 4, length: 8}, {dst: 20, src: 20, length: 3}}, want: 11,
		},
		{name: "no backing", rect: image.Rect(0, 0, 4, 2)},
		{
			name: "empty rectangle", rect: image.Rect(1, 0, 1, 2),
			backing: []gpuMemEntry{{addr: 0, length: 32}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mem := make([]byte, 64)
			for i := range mem {
				mem[i] = byte(i + 1)
			}
			res := &gpuResource{
				width: 4, height: 2, data: bytes.Repeat([]byte{0xa5}, 32), backing: tc.backing,
			}
			want := bytes.Clone(res.data)
			for _, part := range tc.copied {
				copy(want[part.dst:part.dst+part.length], mem[part.src:part.src+part.length])
			}
			collector := telemetry.New()
			g := NewGPU(11, nil, mem, nil)
			g.SetTelemetry(collector)
			g.resources[1] = res
			gpuDamageOK(t, g, gpuTelemetryTransferCommand(1, tc.rect, tc.offset))
			got := collector.Snapshot().Pipeline
			if got.TransferCount != 1 || got.TransferBytes != tc.want {
				t.Fatalf("transfer counters: %+v; want one transfer, %d bytes", got, tc.want)
			}
			if !bytes.Equal(res.data, want) {
				t.Fatalf("transfer changed backing semantics: got %x, want %x", res.data, want)
			}
		})
	}
}

func TestGPUTelemetryRejectsInvalidTransfer(t *testing.T) {
	t.Parallel()
	collector := telemetry.New()
	g := NewGPU(11, nil, make([]byte, 32), nil)
	g.SetTelemetry(collector)
	g.resources[1] = &gpuResource{width: 4, height: 2, data: make([]byte, 32)}
	for _, req := range [][]byte{
		gpuTelemetryTransferCommand(1, image.Rect(0, 0, 5, 2), 0),
		gpuTelemetryTransferCommand(1, image.Rect(0, 0, 4, 2), 33),
		gpuTelemetryTransferCommand(2, image.Rect(0, 0, 4, 2), 0),
	} {
		if got := binary.LittleEndian.Uint32(g.handleControl(req)); got == gpuRespOKNoData {
			t.Fatal("invalid transfer accepted")
		}
	}
	got := collector.Snapshot().Pipeline
	if got.TransferCount != 0 || got.TransferBytes != 0 || got.TransferNanos != 0 {
		t.Fatalf("invalid commands recorded transfers: %+v", got)
	}
}

func TestGPUTelemetryFlushDamageAndScanout(t *testing.T) {
	t.Parallel()
	collector := telemetry.New()
	connection := collector.Connect()
	t.Cleanup(connection.Close)
	display := &gpuDamageSink{}
	g := NewGPU(11, nil, nil, display)
	g.SetTelemetry(collector)
	res := &gpuResource{width: 6, height: 5, format: gpuFormatB8G8R8X8, data: bytes.Repeat([]byte{7, 13, 29, 0}, 30)}
	g.resources[1] = res
	viewport := image.Rect(2, 1, 6, 4)
	partial := image.Rect(3, 2, 4, 3)
	gpuDamageOK(t, g, gpuDamageCommand(gpuCmdSetScanout, 1, viewport))
	gpuDamageOK(t, g, gpuDamageCommand(gpuCmdSetScanout, 1, viewport))
	gpuDamageOK(t, g, gpuDamageCommand(gpuCmdResourceFlush, 1, partial))
	got := collector.Snapshot().Pipeline
	if got.FlushCount != 1 || got.FlushPixels != 12 || got.FullRefreshCount != 1 || got.ScanoutSwitches != 1 {
		t.Fatalf("initial refresh counters: %+v", got)
	}
	if got.SourceWidth != 4 || got.SourceHeight != 3 || display.damage[0] != image.Rect(0, 0, 4, 3) {
		t.Fatalf("source geometry or initial damage: %+v, %v", got, display.damage)
	}
	copy(res.data, bytes.Repeat([]byte{31, 37, 41, 0}, 30))
	gpuDamageOK(t, g, gpuDamageCommand(gpuCmdResourceFlush, 1, partial))
	if display.damage[0] != image.Rect(1, 1, 2, 2) ||
		display.frame.RGBAAt(0, 0) != (color.RGBA{R: 29, G: 13, B: 7, A: 255}) ||
		display.frame.RGBAAt(1, 1) != (color.RGBA{R: 41, G: 37, B: 31, A: 255}) {
		t.Fatal("instrumented flush changed damage or exposed unflushed pixels")
	}
	// Ordinary full damage counts its pixels, but is not forced initialization.
	gpuDamageOK(t, g, gpuDamageCommand(gpuCmdResourceFlush, 1, viewport))
	gpuDamageOK(t, g, gpuDamageCommand(gpuCmdResourceFlush, 1, image.Rect(0, 0, 1, 1)))
	gpuDamageOK(t, g, gpuDamageCommand(gpuCmdResourceFlush, 1, image.Rect(2, 1, 2, 1)))
	got = collector.Snapshot().Pipeline
	if got.FlushCount != 3 || got.FlushPixels != 25 || got.FullRefreshCount != 1 || display.frames != 3 {
		t.Fatalf("damage counters: %+v, frames=%d", got, display.frames)
	}
	viewport = image.Rect(1, 2, 5, 5)
	gpuDamageOK(t, g, gpuDamageCommand(gpuCmdSetScanout, 1, viewport))
	gpuDamageOK(t, g, gpuDamageCommand(gpuCmdResourceFlush, 1, partial))
	g.resources[2] = &gpuResource{width: 6, height: 5, format: gpuFormatB8G8R8X8, data: bytes.Clone(res.data)}
	gpuDamageOK(t, g, gpuDamageCommand(gpuCmdSetScanout, 2, viewport))
	gpuDamageOK(t, g, gpuDamageCommand(gpuCmdResourceFlush, 1, viewport)) // No longer visible.
	gpuDamageOK(t, g, gpuDamageCommand(gpuCmdResourceFlush, 2, partial))
	got = collector.Snapshot().Pipeline
	if got.FlushCount != 5 || got.FlushPixels != 49 || got.FullRefreshCount != 3 || got.ScanoutSwitches != 3 {
		t.Fatalf("scanout switch counters: %+v", got)
	}
	gpuDamageOK(t, g, gpuDamageCommand(gpuCmdSetScanout, 0, image.Rectangle{}))
	gpuDamageOK(t, g, gpuDamageCommand(gpuCmdSetScanout, 0, image.Rectangle{}))
	got = collector.Snapshot().Pipeline
	if got.SourceWidth != 0 || got.SourceHeight != 0 || got.ScanoutSwitches != 4 {
		t.Fatalf("disabled scanout counters: %+v", got)
	}
}

type gpuTelemetryTimedSink struct{ gpuDamageSink }

func (d *gpuTelemetryTimedSink) FlushDamage(width, height int, img *image.RGBA, damage []image.Rectangle) error {
	time.Sleep(3 * time.Millisecond)

	return d.gpuDamageSink.FlushDamage(width, height, img, damage)
}

func TestGPUTelemetryFlushIncludesPublication(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		collector := telemetry.New()
		g := NewGPU(11, nil, nil, &gpuTelemetryTimedSink{})
		g.SetTelemetry(collector)
		g.resources[1] = &gpuResource{width: 2, height: 2, format: gpuFormatB8G8R8X8, data: make([]byte, 16)}
		rect := image.Rect(0, 0, 2, 2)
		gpuDamageOK(t, g, gpuDamageCommand(gpuCmdSetScanout, 1, rect))
		gpuDamageOK(t, g, gpuDamageCommand(gpuCmdResourceFlush, 1, rect))
		if got := collector.Snapshot().Pipeline.FlushNanos; got != uint64(3*time.Millisecond) {
			t.Fatalf("flush service time = %d, want publication duration", got)
		}
	})
}

func TestGPUTelemetrySurvivesReset(t *testing.T) {
	t.Parallel()
	collector := telemetry.New()
	connection := collector.Connect()
	t.Cleanup(connection.Close)
	g := NewGPU(11, nil, []byte{7, 13, 29, 0}, &gpuDamageSink{})
	g.SetTelemetry(collector)
	rect := image.Rect(0, 0, 1, 1)
	for pass := uint64(1); pass <= 2; pass++ {
		g.resources[1] = &gpuResource{
			width: 1, height: 1, format: gpuFormatB8G8R8X8, data: make([]byte, 4),
			backing: []gpuMemEntry{{addr: 0, length: 4}},
		}
		gpuDamageOK(t, g, gpuDamageCommand(gpuCmdSetScanout, 1, rect))
		gpuDamageOK(t, g, gpuTelemetryTransferCommand(1, rect, 0))
		gpuDamageOK(t, g, gpuDamageCommand(gpuCmdResourceFlush, 1, rect))
		g.Reset()
		got := collector.Snapshot().Pipeline
		if got.TransferCount != pass || got.TransferBytes != pass*4 || got.FlushCount != pass ||
			got.FlushPixels != pass || got.FullRefreshCount != pass || got.ScanoutSwitches != pass {
			t.Fatalf("reset discarded lifetime counters: %+v", got)
		}
		if got.SourceWidth != 0 || got.SourceHeight != 0 {
			t.Fatalf("reset retained source geometry: %+v", got)
		}
	}
}

func TestGPUTelemetryDisabled(t *testing.T) {
	t.Parallel()
	display := &gpuDamageSink{}
	g := NewGPU(11, nil, []byte{7, 13, 29, 0}, display)
	g.SetTelemetry(nil)
	g.resources[1] = &gpuResource{
		width: 1, height: 1, format: gpuFormatB8G8R8X8, data: make([]byte, 4),
		backing: []gpuMemEntry{{addr: 0, length: 4}},
	}
	rect := image.Rect(0, 0, 1, 1)
	gpuDamageOK(t, g, gpuDamageCommand(gpuCmdSetScanout, 1, rect))
	gpuDamageOK(t, g, gpuTelemetryTransferCommand(1, rect, 0))
	gpuDamageOK(t, g, gpuDamageCommand(gpuCmdResourceFlush, 1, rect))
	if display.frames != 1 || display.frame.RGBAAt(0, 0) != (color.RGBA{R: 29, G: 13, B: 7, A: 255}) {
		t.Fatal("disabled telemetry changed rendering")
	}
	g.Reset()
}
