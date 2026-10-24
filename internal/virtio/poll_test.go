package virtio

import (
	"encoding/binary"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func TestDevicePollIdleWatchdog(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		poll := newDevicePoll(time.Millisecond)
		defer poll.timer.Stop()
		// An idle device still checks for missed kicks, but does not wake at
		// millisecond frequency while no work or interrupt is pending.
		start := time.Now()
		for range 10 {
			<-poll.timer.C
			poll.reset(false)
		}
		if elapsed := time.Since(start); elapsed != time.Second {
			t.Fatalf("10 idle watchdog checks took %v, want 1s", elapsed)
		}
		poll.reset(true)
		start = time.Now()
		<-poll.timer.C
		if elapsed := time.Since(start); elapsed != time.Millisecond {
			t.Fatalf("active recovery took %v, want 1ms", elapsed)
		}
	})
}

type pollTestDevice struct {
	transport *ModernTransport
	run       func()
	close     func() error
	ready     func()
	post      func()
	queue     *SplitQueue
	interval  time.Duration
}

func newPollTestDevice(t *testing.T, kind string) pollTestDevice {
	t.Helper()
	mem := make([]byte, 1024)
	q := &SplitQueue{Desc: &[QueueSize]SplitDesc{}, Avail: &SplitAvail{}, Used: &SplitUsed{}}
	d := pollTestDevice{queue: q, interval: time.Millisecond}
	switch kind {
	case "block":
		v := newBlk(&batchImage{data: make([]byte, SectorSize)}, false, 10, benchIRQInjector{}, mem)
		addFlushTestRequest(q, mem, 0, 0)
		q.Avail.Idx = 0
		d.transport, d.run, d.close = v.ModernTransport, v.IOThreadEntry, v.Close
		d.ready = func() { v.QueueReady(blkQueue, q) }
		d.post = func() {
			v.queueMu.Lock()
			q.Avail.Idx++
			v.queueMu.Unlock()
		}
	case "network":
		v := NewNet(9, benchIRQInjector{}, benchNetBackend{}, mem)
		q.Desc[0] = SplitDesc{Len: netHdrLen + 1}
		d.transport, d.run, d.close = v.ModernTransport, v.TxThreadEntry, v.Close
		d.ready = func() { v.QueueReady(netTxQueue, q) }
		d.post = func() {
			v.queueMu[netTxQueue].Lock()
			q.Avail.Idx++
			v.queueMu[netTxQueue].Unlock()
		}
		d.interval = 10 * time.Millisecond
	case "tablet":
		v := NewInputTablet(12, func() error { return nil }, mem)
		d.transport, d.run, d.close = v.ModernTransport, v.IOThreadEntry, v.Close
		// Status queue setup must wake the worker too; event queue setup can
		// already flush a pending input synchronously.
		d.ready = func() { v.QueueReady(inputStatusQueue, q) }
		d.post = func() {
			v.mu.Lock()
			q.Avail.Idx++
			v.mu.Unlock()
		}
	case "gpu":
		v := NewGPU(11, benchIRQInjector{}, mem, nil)
		binary.LittleEndian.PutUint32(mem, gpuCmdGetDisplayInfo)
		q.Desc[0] = SplitDesc{Len: gpuCtrlHdrLen, Flags: descFNext, Next: 1}
		q.Desc[1] = SplitDesc{Addr: 256, Len: 512, Flags: descFWrite}
		d.transport, d.run, d.close = v.ModernTransport, v.IOThreadEntry, v.Close
		d.ready = func() { v.QueueReady(gpuControlQueue, q) }
		d.post = func() {
			v.mu.Lock()
			q.Avail.Idx++
			v.mu.Unlock()
		}
		d.interval = 16 * time.Millisecond
	default:
		t.Fatalf("unknown device %q", kind)
	}

	return d
}

func startPollTestDevice(t *testing.T, d pollTestDevice) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		d.run()
	}()
	t.Cleanup(func() {
		if err := d.close(); err != nil {
			t.Error(err)
		}
		<-done
	})
	synctest.Wait()
}

func TestDeviceWorkersRecoverPendingInterrupt(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"block", "network", "tablet", "gpu"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				d := newPollTestDevice(t, kind)
				var asserted atomic.Int64
				d.transport.SetIRQLine(func(high bool) error {
					if high {
						asserted.Add(1)
					}

					return nil
				})
				// A pre-worker interrupt (early boot) must leave a buffered wake.
				if err := d.transport.Interrupt(0); err != nil {
					t.Fatal(err)
				}
				// Leave that wake queued across reset and another interrupt.
				// Recovery must inspect the current ISR rather than lose the new
				// interrupt when its wake coalesces with the old buffered one.
				writeCfg(d.transport, 20, 0, 1)
				if err := d.transport.ConfigChanged(); err != nil {
					t.Fatal(err)
				}
				startPollTestDevice(t, d)
				if asserted.Load() < 3 {
					t.Fatal("early interrupt across reset did not wake recovery")
				}
				before := asserted.Load()
				time.Sleep(3 * d.interval)
				synctest.Wait()
				if asserted.Load()-before < 3 {
					t.Fatal("outstanding interrupt lost its original recovery frequency")
				}
				_ = d.transport.clearInterrupt()
				before = asserted.Load()
				time.Sleep(time.Second)
				synctest.Wait()
				if asserted.Load() != before {
					t.Fatal("acknowledged interrupt was reasserted")
				}
				// Configuration changes and RX IRQs may originate outside the
				// polling worker, after it has already backed off.
				if err := d.transport.ConfigChanged(); err != nil {
					t.Fatal(err)
				}
				synctest.Wait()
				if asserted.Load()-before < 2 {
					t.Fatal("new interrupt after idle did not wake recovery immediately")
				}
				// Reset can acknowledge one IRQ just before another is raised;
				// a coalesced or stale wake must still recover the new ISR state.
				writeCfg(d.transport, 20, 0, 1)
				before = asserted.Load()
				if err := d.transport.Interrupt(0); err != nil {
					t.Fatal(err)
				}
				time.Sleep(d.interval)
				synctest.Wait()
				if asserted.Load()-before < 2 {
					t.Fatal("reset lost the next pending interrupt recovery")
				}
			})
		})
	}
}

func TestDeviceWorkersProcessQueueReadyWithoutGuestKick(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"block", "network", "tablet", "gpu"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				d := newPollTestDevice(t, kind)
				startPollTestDevice(t, d)
				time.Sleep(time.Second)
				synctest.Wait()
				for range 2 {
					d.post()
					d.ready()
					synctest.Wait()
					if d.queue.Used.Idx != 1 {
						t.Fatal("queue setup did not immediately complete its posted request")
					}
					writeCfg(d.transport, 20, 0, 1)
					d.queue.Avail.Idx, d.queue.Used.Idx = 0, 0
				}
			})
		})
	}
}

func TestDeviceWatchdogRecoversMissedKick(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"block", "network", "tablet", "gpu"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				d := newPollTestDevice(t, kind)
				d.ready()
				startPollTestDevice(t, d)
				time.Sleep(time.Second)
				synctest.Wait()
				d.post() // Deliberately omit Notify after posting the descriptor.
				time.Sleep(deviceIdlePollInterval)
				synctest.Wait()
				if d.queue.Used.Idx != 1 {
					t.Fatal("idle watchdog did not recover a missing guest kick")
				}
			})
		})
	}
}

func TestDeviceWorkersCoalesceInterruptBurst(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"block", "network", "tablet", "gpu"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				d := newPollTestDevice(t, kind)
				var asserted atomic.Int64
				d.transport.SetIRQLine(func(high bool) error {
					if high {
						asserted.Add(1)
					}

					return nil
				})
				startPollTestDevice(t, d)
				if err := d.transport.Interrupt(0); err != nil {
					t.Fatal(err)
				}
				synctest.Wait()
				// Recovery is now armed at the original fast interval. A burst
				// of acknowledged IRQs must not schedule an extra worker drain
				// for each transition, especially for RX-generated net IRQs.
				before := asserted.Load()
				const burst = 64
				for range burst {
					_ = d.transport.clearInterrupt()
					if err := d.transport.Interrupt(0); err != nil {
						t.Fatal(err)
					}
				}
				synctest.Wait()
				if got := asserted.Load() - before; got != burst {
					t.Fatalf("burst added %d interrupt assertions, want only %d original IRQs", got, burst)
				}
				before = asserted.Load()
				time.Sleep(3 * d.interval)
				synctest.Wait()
				if asserted.Load()-before < 3 {
					t.Fatal("coalescing the burst lost pending IRQ recovery")
				}
				_ = d.transport.clearInterrupt()
				time.Sleep(time.Second)
				synctest.Wait()
				before = asserted.Load()
				if err := d.transport.ConfigChanged(); err != nil {
					t.Fatal(err)
				}
				synctest.Wait()
				if asserted.Load()-before < 2 {
					t.Fatal("coalesced wake prevented immediate recovery after idle")
				}
			})
		})
	}
}
