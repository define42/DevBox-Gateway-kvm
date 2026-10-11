package common

import (
	"strings"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
)

func TestCFormatToGo(t *testing.T) {
	for in, want := range map[string]string{
		"plain":                 "plain",
		"%d %u %i":              "%d %d %d",
		"%lld %llu %ld %lu %zu": "%d %d %d %d %d",
		"%I64d %hhd %hd":        "%d %d %d",
		"%5.2f %-3d %03.3u %%":  "%5.2f %-3d %03.3d %%",
		"%s %p %x %v %t %q %lf": "%s %p %x %v %t %q %f",
		"trailing %":            "trailing %",
	} {
		if got := cFormatToGo(in); got != want {
			t.Errorf("cFormatToGo(%q) = %q want %q", in, got, want)
		}
	}
}

func TestWelsLogTrace(t *testing.T) {
	tr := NewWelsCodecTrace()
	var got []string
	var gotLevels []int32
	tr.SetTraceCallback(func(ctx any, level int32, msg string) {
		if ctx != "ctx" {
			t.Errorf("ctx = %v", ctx)
		}
		got = append(got, msg)
		gotLevels = append(gotLevels, level)
	})
	tr.SetTraceCallbackContext("ctx")
	tr.SetTraceLevel(api.WELS_LOG_INFO)

	WelsLog(&tr.M_sLogCtx, api.WELS_LOG_ERROR, "value %d of %u, %s", int32(3), uint32(7), "x")
	WelsLog(&tr.M_sLogCtx, api.WELS_LOG_DEBUG, "filtered out")
	WelsLog(&tr.M_sLogCtx, api.WELS_LOG_INFO, "%lld", int64(-5))
	if len(got) != 2 {
		t.Fatalf("got %d messages: %q", len(got), got)
	}
	if got[0] != "[OpenH264] this = 0x(nil), Error:value 3 of 7, x" || gotLevels[0] != api.WELS_LOG_ERROR {
		t.Errorf("message 0 = %q", got[0])
	}
	if !strings.HasSuffix(got[1], "Info:-5") {
		t.Errorf("message 1 = %q", got[1])
	}
	tr.SetCodecInstance(tr)
	WelsLog(&tr.M_sLogCtx, api.WELS_LOG_WARNING, "%s", strings.Repeat("a", 2000))
	if len(got) != 3 || !strings.HasPrefix(got[2], "[OpenH264] this = 0x0x") || len(got[2]) != MAX_LOG_SIZE-1 {
		t.Errorf("message 2 = %q (len %d)", got[2][:40], len(got[2]))
	}
}

func TestWelsCalcPsnr(t *testing.T) {
	a := make([]uint8, 64)
	b := make([]uint8, 64)
	if WelsCalcPsnr(a, 0, 8, b, 0, 8, 8, 8) != 99.99 {
		t.Error("identical psnr")
	}
	if WelsCalcPsnr(nil, 0, 8, b, 0, 8, 8, 8) != -1 {
		t.Error("nil psnr")
	}
	b[0] = 10
	got := WelsCalcPsnr(a, 0, 8, b, 0, 8, 8, 8)
	if got < 46.19 || got > 46.20 { // 10*log10(65025*64/100)
		t.Errorf("psnr = %v", got)
	}
}

func TestCrtUtil(t *testing.T) {
	var s string
	if n := WelsSnprintf(&s, 6, "%s-%u", "abcd", uint32(12)); n != 7 || s != "abcd-" {
		t.Errorf("WelsSnprintf = %d %q", n, s)
	}
	d := "abc"
	WelsStrcat(&d, 6, "defgh")
	if d != "abcde" {
		t.Errorf("WelsStrcat = %q", d)
	}
	var tm SWelsTime
	WelsGetTimeOfDay(&tm)
	var ts string
	if n := WelsStrftime(&ts, 64, "%y%m%d%H%M%S", &tm); n != 12 || len(ts) != 12 {
		t.Errorf("WelsStrftime = %d %q", n, ts)
	}
	if WelsTime() <= 0 {
		t.Error("WelsTime")
	}
}

func TestCMemoryAlign(t *testing.T) {
	ma := NewCMemoryAlign(0)
	if ma.WelsGetCacheLineSize() != 16 {
		t.Error("cache line")
	}
	p := ma.WelsMallocz(100, "p")
	if len(p) != 100 || ma.WelsGetMemoryUsage() != 100+15+12 {
		t.Errorf("usage %d", ma.WelsGetMemoryUsage())
	}
	ma.WelsFree(p, "p")
	if ma.WelsGetMemoryUsage() != 0 {
		t.Error("usage after free")
	}
}
