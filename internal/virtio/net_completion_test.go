package virtio_test

import (
	"bytes"
	"errors"
	"io"
	"syscall"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/internal/virtio"
)

type completionNetBackend struct {
	bytes.Buffer
	writes   int
	failAt   int
	writeErr error
	short    bool
	onWrite  func()
}

func (b *completionNetBackend) Write(p []byte) (int, error) {
	b.writes++
	if b.writes == b.failAt {
		if b.short {
			return len(p) - 1, nil
		}

		return 0, b.writeErr
	}
	if b.onWrite != nil {
		b.onWrite()
	}

	return b.Buffer.Write(p)
}

func TestNetTxBatchCompletions(t *testing.T) {
	t.Parallel()

	writeErr := io.ErrClosedPipe
	irqErr := syscall.EIO
	for _, tc := range []struct {
		name          string
		failAt        int
		short         bool
		invalid       bool
		closeOnWrite  bool
		irqErr        error
		wantErr       error
		wantCompleted uint16
		wantWrites    int
		retry         bool
	}{
		{name: "success", wantCompleted: 2, wantWrites: 2},
		{name: "close_during_batch", closeOnWrite: true, wantErr: virtio.ErrNoTxPacket, wantCompleted: 1, wantWrites: 1},
		{name: "first_write_error", failAt: 1, wantErr: writeErr, wantWrites: 1},
		{name: "second_write_error", failAt: 2, wantErr: writeErr, wantCompleted: 1, wantWrites: 2, retry: true},
		{name: "second_short_write", failAt: 2, short: true, wantErr: io.ErrShortWrite, wantCompleted: 1, wantWrites: 2},
		{name: "second_invalid_descriptor", invalid: true, wantErr: virtio.ErrNetDesc, wantCompleted: 1, wantWrites: 1},
		{name: "interrupt_error", irqErr: irqErr, wantErr: irqErr, wantCompleted: 2, wantWrites: 2},
		{
			name: "write_and_interrupt_error", failAt: 2, irqErr: irqErr,
			wantErr: writeErr, wantCompleted: 1, wantWrites: 2,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			backend := &completionNetBackend{failAt: tc.failAt, writeErr: writeErr, short: tc.short}
			mem := make([]byte, 4096)
			v := virtio.NewNet(9, &mockInjector{}, backend, mem)
			t.Cleanup(func() { _ = v.Close() })
			if tc.closeOnWrite {
				backend.onWrite = func() {
					if err := v.Close(); err != nil {
						t.Error(err)
					}
				}
			}
			q := virtio.NewSplitQueue(4)
			for i := range 2 {
				addr := 256 + i*128
				q.Desc[i] = virtio.SplitDesc{Addr: uint64(addr), Len: 12 + 60}
				copy(mem[addr+12:addr+72], bytes.Repeat([]byte{byte(i + 1)}, 60))
				q.Avail.Ring[i] = uint16(i)
				q.Used.Ring[i].Len = 999 // A reused ring entry must also report zero.
			}
			if tc.invalid {
				q.Desc[1].Len = 11
			}
			q.Avail.Idx = 2
			q.Avail.Flags = 1 // TX must still notify stopped napi_tx queues.
			v.VirtQueue[1] = q
			interrupts := 0
			v.SetIRQLine(func(high bool) error {
				if high {
					interrupts++
					if q.Used.Idx == 0 {
						t.Error("interrupt arrived before any completion")
					}
				}

				return tc.irqErr
			})

			err := v.Tx()
			if !errors.Is(err, tc.wantErr) || (tc.irqErr != nil && !errors.Is(err, tc.irqErr)) {
				t.Fatalf("Tx error = %v, want %v and %v", err, tc.wantErr, tc.irqErr)
			}
			if q.Used.Idx != tc.wantCompleted || v.LastAvailIdx[1] != tc.wantCompleted {
				t.Fatalf("completed=%d consumed=%d, want %d", q.Used.Idx, v.LastAvailIdx[1], tc.wantCompleted)
			}
			for i := uint16(0); i < tc.wantCompleted; i++ {
				if got := q.Used.Ring[i]; got.ID != uint32(i) || got.Len != 0 {
					t.Fatalf("completion %d = %+v, want ID=%d and zero written bytes", i, got, i)
				}
			}
			wantInterrupts := 0
			if tc.wantCompleted != 0 {
				wantInterrupts = 1
			}
			if interrupts != wantInterrupts || backend.writes != tc.wantWrites {
				t.Fatalf("interrupts=%d writes=%d, want %d/%d", interrupts, backend.writes, wantInterrupts, tc.wantWrites)
			}
			if tc.retry {
				backend.failAt = 0
				if err := v.Tx(); err != nil {
					t.Fatal(err)
				}
				want := append(bytes.Repeat([]byte{1}, 60), bytes.Repeat([]byte{2}, 60)...)
				if !bytes.Equal(backend.Bytes(), want) || q.Used.Idx != 2 || interrupts != 2 {
					t.Fatal("retry duplicated a packet or lost a completion")
				}
			}
		})
	}
}
