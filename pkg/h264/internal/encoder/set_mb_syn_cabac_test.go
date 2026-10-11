package encoder

// No C unit test exists for set_mb_syn_cabac.cpp; this is a round trip of the
// CABAC engine against a straightforward H.264 9.3.3.2 arithmetic decoder.

import (
	"math/rand"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

type refCabacDec struct {
	buf    []uint8
	bitPos int
	rng    uint32
	off    uint32
	state  [common.WELS_CONTEXT_COUNT]uint8
	mps    [common.WELS_CONTEXT_COUNT]uint8
}

func (d *refCabacDec) readBit() uint32 {
	b := uint32(0)
	if d.bitPos>>3 < len(d.buf) {
		b = uint32(d.buf[d.bitPos>>3]>>(7-uint(d.bitPos&7))) & 1
	}
	d.bitPos++
	return b
}

func (d *refCabacDec) init() {
	d.rng = 510
	d.off = 0
	for i := 0; i < 9; i++ {
		d.off = d.off<<1 | d.readBit()
	}
}

func (d *refCabacDec) renorm() {
	for d.rng < 256 {
		d.rng <<= 1
		d.off = d.off<<1 | d.readBit()
	}
}

func (d *refCabacDec) decision(ctx int32) uint32 {
	s := d.state[ctx]
	lps := uint32(common.G_kuiCabacRangeLps[s][(d.rng>>6)&3])
	d.rng -= lps
	var bin uint32
	if d.off >= d.rng {
		bin = uint32(1 - d.mps[ctx])
		d.off -= d.rng
		d.rng = lps
		if s == 0 {
			d.mps[ctx] = 1 - d.mps[ctx]
		}
		d.state[ctx] = common.G_kuiStateTransTable[s][0]
	} else {
		bin = uint32(d.mps[ctx])
		d.state[ctx] = common.G_kuiStateTransTable[s][1]
	}
	d.renorm()
	return bin
}

func (d *refCabacDec) bypass() uint32 {
	d.off = d.off<<1 | d.readBit()
	if d.off >= d.rng {
		d.off -= d.rng
		return 1
	}
	return 0
}

func (d *refCabacDec) terminate() uint32 {
	d.rng -= 2
	if d.off >= d.rng {
		return 1
	}
	d.renorm()
	return 0
}

func (d *refCabacDec) ueBypass(k int32) uint32 {
	var v uint32
	for d.bypass() == 1 {
		v += 1 << uint(k)
		k++
	}
	for k > 0 {
		k--
		v += d.bypass() << uint(k)
	}
	return v
}

type cabacOp struct {
	kind int // 0 decision, 1 bypass, 2 terminate(0), 3 ue bypass
	ctx  int32
	val  uint32
	k    int32
}

func TestCabacEngineRoundTrip(t *testing.T) {
	r := rand.New(rand.NewSource(51))
	for run := 0; run < 200; run++ {
		var ctx SCabacCtx
		var dec refCabacDec
		for i := 0; i < common.WELS_CONTEXT_COUNT; i++ {
			s := uint8(r.Intn(63))
			m := uint8(r.Intn(2))
			ctx.m_sStateCtx[i].Set(s, m)
			dec.state[i] = s
			dec.mps[i] = m
		}
		const lead = 3 // bytes already in the buffer before the CABAC data
		buf := make([]uint8, 1<<16)
		WelsCabacEncodeInit(&ctx, buf, lead, len(buf))

		nOps := 1 + r.Intn(3000)
		ops := make([]cabacOp, nOps)
		skew := r.Intn(4)
		for i := range ops {
			op := cabacOp{}
			switch x := r.Intn(20); {
			case x < 14:
				op.kind = 0
				op.ctx = int32(r.Intn(8))
				// skewed bins exercise long MPS runs and carry propagation
				if skew > 0 && r.Intn(skew+1) != 0 {
					op.val = uint32(ctx.m_sStateCtx[op.ctx].Mps())
				} else {
					op.val = uint32(r.Intn(2))
				}
			case x < 17:
				op.kind = 1
				op.val = uint32(r.Intn(2))
			case x < 18:
				op.kind = 2
			default:
				op.kind = 3
				op.k = int32(r.Intn(4))
				op.val = uint32(r.Intn(1 << uint(r.Intn(14))))
			}
			ops[i] = op
			switch op.kind {
			case 0:
				WelsCabacEncodeDecision(&ctx, op.ctx, op.val)
			case 1:
				WelsCabacEncodeBypassOne(&ctx, int32(op.val))
			case 2:
				WelsCabacEncodeTerminate(&ctx, 0)
			case 3:
				WelsCabacEncodeUeBypass(&ctx, op.k, op.val)
			}
		}
		WelsCabacEncodeFlush(&ctx)
		end := WelsCabacEncodeGetPtr(&ctx)
		if end <= lead {
			t.Fatalf("run %d: nothing written", run)
		}

		dec.buf = buf[lead:end]
		dec.init()
		for i, op := range ops {
			var got uint32
			switch op.kind {
			case 0:
				got = dec.decision(op.ctx)
			case 1:
				got = dec.bypass()
			case 2:
				got = dec.terminate()
			case 3:
				got = dec.ueBypass(op.k)
			}
			if got != op.val {
				t.Fatalf("run %d op %d (kind %d): got %d want %d", run, i, op.kind, got, op.val)
			}
		}
		if dec.terminate() != 1 {
			t.Fatalf("run %d: end of slice not decoded", run)
		}
		// the leading bytes must not be touched
		for i := 0; i < lead; i++ {
			if buf[i] != 0 {
				t.Fatalf("run %d: byte %d before start modified", run, i)
			}
		}
	}
}

func TestPropagateCarry(t *testing.T) {
	buf := []uint8{0x10, 0xFF, 0xFF, 0x00}
	PropagateCarry(buf, 3, 0)
	if buf[0] != 0x11 || buf[1] != 0 || buf[2] != 0 {
		t.Fatalf("got %x", buf)
	}
	// stops at the start
	buf = []uint8{0xFF, 0xFF}
	PropagateCarry(buf, 2, 1)
	if buf[0] != 0xFF || buf[1] != 0 {
		t.Fatalf("got %x", buf)
	}
}
