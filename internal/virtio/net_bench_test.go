package virtio

import (
	"fmt"
	"io"
	"testing"
)

type benchNetBackend struct{}

func (benchNetBackend) Read([]byte) (int, error)    { return 0, io.EOF }
func (benchNetBackend) Write(p []byte) (int, error) { return len(p), nil }
func (benchNetBackend) Ready() <-chan struct{}      { return nil }

type benchIRQInjector struct{}

func (benchIRQInjector) InjectVirtioNetIRQ() error { return nil }
func (benchIRQInjector) InjectVirtioBlkIRQ() error { return nil }
func (benchIRQInjector) InjectVirtioGPUIRQ() error { return nil }

func BenchmarkNetTx(b *testing.B) {
	for _, tc := range []struct{ packets, segments int }{{1, 2}, {8, 2}, {1, 16}} {
		b.Run(fmt.Sprintf("packets=%d/segments=%d", tc.packets, tc.segments), func(b *testing.B) {
			const packetSize = 1514 + netHdrLen
			mem := make([]byte, tc.packets*packetSize)
			v := NewNet(9, benchIRQInjector{}, benchNetBackend{}, mem)
			q := &SplitQueue{Desc: &[QueueSize]SplitDesc{}, Avail: &SplitAvail{}, Used: &SplitUsed{}}
			for p := 0; p < tc.packets; p++ {
				q.Avail.Ring[p] = uint16(p * tc.segments)
				for s := 0; s < tc.segments; s++ {
					idx := p*tc.segments + s
					start, end := s*packetSize/tc.segments, (s+1)*packetSize/tc.segments
					q.Desc[idx] = SplitDesc{Addr: uint64(p*packetSize + start), Len: uint32(end - start)}
					if s+1 < tc.segments {
						q.Desc[idx].Flags = descFNext
						q.Desc[idx].Next = uint16(idx + 1)
					}
				}
			}
			v.VirtQueue[netTxQueue] = q
			b.ReportAllocs()
			b.SetBytes(int64(tc.packets * (packetSize - netHdrLen)))
			for b.Loop() {
				v.LastAvailIdx[netTxQueue], q.Used.Idx = 0, 0
				q.Avail.Idx = uint16(tc.packets)
				if err := v.Tx(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
