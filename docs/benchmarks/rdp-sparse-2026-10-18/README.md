# Sparse software AVC encoding

Measured 2026-10-18 against baseline `a7a5125176932cedf338be8f3f000b9c41749ebc`.
The candidate is the accompanying unchanged-macroblock patch. No benchmark fixture,
codec quality setting, default slice count, guest, or running service was changed.

## Results

Median **encoding** time, excluding RGB-to-I420 conversion:

| 1080p workload | Slices | Before | After | Comparison |
| --- | ---: | ---: | ---: | --- |
| 64×32 widget | 1 | 9.418 ms | 5.401 ms | −42.65%, p < 0.001 |
| 64×32 widget | 4 | 6.777 ms | 4.898 ms | −27.73%, p = 0.011 |
| Document scroll | 1 | 15.02 ms | 14.44 ms | Inconclusive, p = 0.529 |
| Document scroll | 4 | 8.718 ms | 10.158 ms | Inconclusive, p = 0.190 |

Ten samples per case, one second per sample, precompiled binaries, alternating
before/after order in each pair. Linux/amd64, Intel i7-7700HQ at nominal 2.80 GHz,
Go 1.26.8, `GOMAXPROCS=4`. This is a **shared host**, not an isolated capacity test:
background applications and existing gateway/VM processes stayed running; load
average was about 10 during one check. Test, lint, and build workers were paused.
The wide intervals, particularly for scrolling, do not establish equivalence or
exclude a scrolling regression. Repeat on the deployment host before sizing it.

Single-slice widget conversion remained about 7–8 µs. Widget output remained about
107 bytes/frame, with 12 reported allocations/op. The reusable mask costs about
8 KiB per 1080p encoder, allocated on its first partial update; this contributes
to the small increase in amortized B/op. No per-frame mask allocation is needed.

Raw [before](before.txt), [after](after.txt), and [benchstat](benchstat.txt) outputs
include all ten samples. Separate [before](before-cpu.txt) and [after](after-cpu.txt)
CPU profiles include initialization/calibration and are not latency measurements.

## What changed

Dirty-region conversion already retained a full I420 picture. The software codec
now also receives a reusable, conservative 16×16 macroblock mask covering luma
and chroma. With the audited fixed-QP, single-reference, deblocking-off policy,
an unchanged block can reuse the immediately preceding reconstructed block when
both the previous skip motion and the current predicted skip motion are zero.
This bypasses repeated motion-compensation scratch copies, residual transforms,
and mode decisions for eligible blocks. Reconstruction uses fixed-width copies.

The stream still contains full-size AVC pictures. Source preprocessing, picture
copies, macroblock bookkeeping, and changed-block encoding remain; cost is not
proportional only to dirty area. First interframes after IDR, uncertain references,
unsupported configurations, and ineligible blocks take the normal codec path.
Refreshes and retries preserve existing full-picture IDR behavior. NVENC is
unchanged. No new setting, RDP protocol feature, or guest change is required.

Hints are borrowed synchronously and never retained after the codec call. Nil or
incorrect-length masks disable the optimization. Callers must report every change;
an incorrect true bit can suppress real damage. Reused pixels preserve previous
decoded quality; output is not promised to be bit-identical to encoding without hints.

## Reproduce

Build `before.test` in a separate checkout of the baseline revision and `after.test`
from the patched tree, using the same toolchain. Do not run builds or tests while timing.

```sh
export GOTOOLCHAIN=go1.26.8 GOMAXPROCS=4
# Run once in each corresponding checkout:
go test -c -o /tmp/before.test ./internal/rdp/avc
go test -c -o /tmp/after.test ./internal/rdp/avc

# Use fresh output paths; alternate which binary runs first.
for sample in $(seq 1 10); do
  if [ $((sample % 2)) -eq 1 ]; then order='before after'; else order='after before'; fi
  for variant in $order; do
    /tmp/$variant.test -test.run='^$' \
      -test.bench='^BenchmarkSlicedDesktop/1080p/(Widget|Scroll)/(1|4)Slices$' \
      -test.benchmem -test.benchtime=1s -test.count=1 >> /tmp/$variant.txt
  done
done
benchstat /tmp/before.txt /tmp/after.txt
```

This synthetic benchmark measures encoder calls, not guest rendering, transport,
client decoding, input-to-display latency, or multi-VM capacity. Decoder regression
tests cover moving/erased edits, chroma-only changes, scaling, nonzero origins,
partial edge blocks, one/four slices, and forced refresh followed by an interframe.
They compare localized Y/U/V error using independent FFmpeg decoding; they do not
replace a visual quality and latency trial with real RDP clients.

## Verification

Passed with Go 1.26.8:

- Full `go test -count=1 -timeout 30m ./...` in the CI-style isolated network
  namespace, with loopback enabled.
- `go test -race ./pkg/h264/internal/encoder ./internal/rdp/avc -count=1`, including
  reference invalidation, borrowed-mask lifetime, inherited skip QP, and FFmpeg decoding.
- `CGO_ENABLED=1 go test -short -tags=nvenc ./internal/rdp/... ./internal/virtio ./internal/vmm ./internal/flag`.
- `CGO_ENABLED=0 go test -short ./internal/rdp/avc ./internal/virtio`.
- Full repository golangci-lint and integration-tagged VMM lint: zero issues.
- `go mod tidy -diff` and `git diff --check`: clean.

No live guest/service was modified, and no real-client latency or NVENC throughput
claim is made by these checks.
