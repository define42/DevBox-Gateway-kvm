# Encoder component measurements

These microbenchmarks show improvements in the retained kernels and wrappers. They do not establish whole-frame speedup, live RDP performance, or performance parity with native OpenH264. All timings below are medians from six samples on the Intel i7-7700HQ, Linux/amd64, Go 1.27.3, `GOMAXPROCS=4`. Independent agent builds/tests/timings were serialized; the desktop host was not isolated. Each measurement script alternates execution order. All reported Go cases allocate zero bytes per operation.

## Four-block forward transform

The final implementation processes four 4×4 blocks together using AVX2, with a typed-pointer assembly interface. The validated dispatch retains the existing SSE2 behavior for output/input aliases and negative strides. The assembly follows OpenH264 2.6.0 `codec/common/x86/dct.asm`; attribution is retained in [dct_avx2_amd64.s](../../../pkg/h264/internal/encoder/dct_avx2_amd64.s) and [LICENSE-OpenH264](../../../LICENSE-OpenH264).

| Pixel stride | Previous validated SSE2 | Final validated dispatch | Change |
| --- | ---: | ---: | ---: |
| 8 | 37.31 ns | 22.12 ns | −40.72% |
| 64 | 35.74 ns | 21.86 ns | −38.85% |
| 2048 | 37.40 ns | 21.95 ns | −41.30% |

All three differences have `p=.002, n=6`. These comparisons use the preserved old entry point and final entry point in the same Go test binary. [Statistics](dct-validated-benchstat.txt), [complete raw samples](dct-component-raw.txt), [reproduction script](dct-measure.py).

A separate direct-call comparison still favors native: native AVX2 takes **9.09–9.29 ns**, versus **14.24–14.89 ns** for the Go raw assembly entry. Native's C++ harness uses its native calling convention and a 32-byte-aligned output array; Go uses its assembly ABI and accepts unaligned outputs. Different call and benchmark-loop costs remain in these measurements, so they do not isolate instruction throughput or quantify each source of the gap. [Native comparison](dct-native-benchstat.txt), [native harness](dct_component.cpp).

Randomized/scalar parity, extreme pixel patterns, strides, aliases, actual slice lengths and guard pages pass. A native-generated 1024-block corpus at each of the three strides has matching coefficient fingerprints. Focused tests also pass with [AVX2 disabled](dct-tests-cpu.avx2-off.txt) and [all optional CPU features disabled](dct-tests-cpu.all-off.txt); the latter uses baseline SSE2 on amd64, not an SSE2-free CPU.

## Motion SATD and horizontal half-pixel filtering

The final SATD implementation computes packed byte-pair sums/differences and uses the exact `|a+b|+|a-b| = 2·max(|a|,|b|)` reduction. Typed-pointer entries retain validation before assembly. The horizontal six-tap filter uses packed multiply-adds with exact source-halo accesses. Other half-pixel kernels and motion-search ordering are unchanged. SATD source attribution is retained in [sample_simd_batch_amd64.s](../../../pkg/h264/internal/encoder/sample_simd_batch_amd64.s), following OpenH264 2.6.0 `codec/common/x86/satd_sad.asm`.

| Operation | Before | Final | Change |
| --- | ---: | ---: | ---: |
| SATD 8×8 | 37.42 ns | 26.06 ns | −30.35% |
| SATD 16×8 | 38.08 ns | 23.54 ns | −38.20% |
| SATD 8×16 | 61.73 ns | 40.34 ns | −34.65% |
| SATD 16×16 | 60.34 ns | 34.38 ns | −43.04% |
| Fused average + SATD 16×16 | 61.41 ns | 46.51 ns | −24.28% |
| Fused quarter-pixel refinement benchmark | 407.6 ns | 320.1 ns | −21.46% |
| Horizontal half-pixel filter 16×16 | 93.16 ns | 73.67 ns | −20.92% |

Every listed difference has `p=.002, n=6`. Scalar and SSE comparison cases have no statistically significant change. The sequential quarter-refinement comparison is also inconclusive (`p=.240`); it is not included as a demonstrated improvement above. [Full statistics](motion-benchstat.txt), [before samples](motion-before.txt), [final samples](motion-after.txt), [measurement script](measure_motion.py).

The native motion comparison uses matching pixel fixtures and strides, but compares **validated Go wrappers with unchecked native kernels**. Native remains faster in these component measurements: 16×16 SATD is **25.79 ns native versus 34.38 ns Go**, and the horizontal 16×16 filter is **28.19 ns native versus 73.67 ns Go**. The timing difference includes validation, ABI, loop and implementation differences; it must not be described as a pure assembly-kernel comparison or proof of encoder parity. [Native comparison](motion-native-benchstat.txt), [native raw samples](motion-native.txt), [native harness](motion_component.cpp).

The native and Go 1024-case motion corpus fingerprint is `928d2228b8537ee8`. Validation includes independent matrix-reference evaluation of every binary 4×4 pattern, impulses in every lane, randomized parity, average rounding, refinement decisions, alias/negative-stride fallbacks, actual slice lengths, and guard pages across vector-tail boundaries. [Encoder tests](motion-encoder-tests.txt), [filter tests](motion-common-tests.txt).

## Completed inter-luma quantize/scan batch

Completed inter-luma encoding always consumes all four groups. The final batch validates 256 raster coefficients, 256 scanned levels, 16 maxima and 16 masks once, then uses one assembly call and one FF/MF/table snapshot. It shares the existing SSSE3 block loop. Output aliases, or FF/MF overlapping any output across groups, retain the previous four-call ordering. Lazy skip attempts and chroma continue using their existing paths.

| Coefficient density | Four current group calls | Final luma batch | Change |
| --- | ---: | ---: | ---: |
| Zero | 162.05 ns | 97.94 ns | −39.56% |
| Sparse | 197.7 ns | 130.0 ns | −34.24% |
| Dense | 196.1 ns | 130.0 ns | −33.66% |

All differences have `p=.002, n=6`. Both cases include restoring the same 256-coefficient input and use the same current four-block arithmetic; this isolates batching after the earlier typed-pointer improvement. This experiment does not compare the complete operation against native OpenH264. [Statistics](quant-luma-benchstat.txt), [complete raw samples](quant-luma-component-raw.txt), [reproduction script](quant-luma-measure.py).

Validation covers all 52 QPs, full signed coefficient extremes, nonnegative FF/MF values, stale-output clearing, canaries, cross-group output/table aliases, visible lengths, guard pages, and complete coefficient-encoding state. Focused tests pass [normally](quant-luma-tests-normal.txt), with [SSSE3 disabled](quant-luma-tests-cpu.ssse3-off.txt), and with [all optional CPU features disabled](quant-luma-tests-cpu.all-off.txt). The checked-in [benchmark and parity tests](../../../pkg/h264/internal/encoder/quant_scan_luma_test.go) preserve an independent scalar reference and the previous four-call sequence.

## CAVLC coefficient extraction and complete writer

The retained implementation independently checks SSSE3 and POPCNT. Fully populated blocks use reverse shuffles; a 15-coefficient block with one missing coefficient uses a specialized compaction path. Other partial blocks enumerate a SIMD-generated mask inside assembly. The writer's disjoint local scratch permits exact-width stores while preserving every unused output entry. Small, short and unsupported-CPU cases retain scalar extraction.

The final complete-writer benchmark uses the same current bit writer on both sides, changing only coefficient extraction. Six alternating scalar/SIMD samples, 100 ms each:

| Input, end=15 | Scalar extraction writer | Final writer | Change |
| --- | ---: | ---: | ---: |
| Zero | 33.15 ns | 25.00 ns | −24.59% |
| DC only | 41.91 ns | 35.87 ns | −14.42% |
| Sparse, low frequency | 56.52 ns | 51.70 ns | −8.54% |
| Sparse, scattered | 71.65 ns | 65.79 ns | −8.19% |
| Medium | 107.8 ns | 101.2 ns | −6.03% |
| One missing coefficient | 154.6 ns | 137.7 ns | −10.99% |
| Dense | 121.4 ns | 104.4 ns | −13.97% |

All listed differences have p≤0.015, n=6. Nearly dense cases and the tiny end=3 cases are inconclusive. Some early scalar samples are large outliers, producing very wide intervals; they are retained. The range check adds a small dispatch cost to tiny blocks, around 2 ns in some earlier runs, rather than making that path faster. [Final matrix](cavlc-confirm-benchstat.txt), [scalar samples](cavlc-confirm-scalar.txt), [SIMD samples](cavlc-confirm-simd.txt), [runner](measure_cavlc.py). These final-source results supersede the development variants, including the rejected nibble-only and enumerate-all-partial paths.

A separate matched-fixture extraction comparison shows mixed results. Go's specialized fully dense end=15 path takes 6.99 ns versus native 9.61 ns; a one-zero block is approximately equal (10.06 versus 9.71 ns, p=.310). Native remains faster on ordinary partial blocks: for example, medium end=15 is 10.12 ns native versus 18.77 ns Go. The Go measurements include its checked dispatch and ABI; native directly calls its padded-input kernel, whose unused-tail/run semantics differ. These are not interchangeable general APIs or a pure instruction-throughput comparison. [Matched comparison](cavlc-native-final-benchstat.txt), [Go raw output](cavlc-native-final-go.txt), [native raw output](cavlc-native-final-native.txt), [native helper](cavlc_native_bench.cpp), [runner](measure_cavlc_native.py). Only metadata headers were removed from the `*-matched.txt` files; no samples were discarded.

The native helper links with `g++ -O3 -std=c++17 cavlc_native_bench.cpp /tmp/openh264-comparison-20261021/native/libopenh264.a -pthread -o cavlc_native_final`; each native sample uses 3,000,000 calls. Correctness is checked independently through exhaustive masks, the complete signed coefficient domain, sentinel tails, short buffers, guard pages, writer-reference comparisons and the full Go/native stream comparisons.

## Reproduction scope

The scripts and C++ harnesses preserve their original `/tmp/h264-native-parity-20261021` and native OpenH264 2.6.0 build paths. Recreate or adjust those paths before running them from the repository root. DCT and luma scripts include their build commands; the motion script consumes the frozen before/current test binaries and native harness. Component runs used 200 ms Go samples for DCT/luma and 100 ms for motion. Native loops use 14 million calls for DCT and 3 million calls per motion case, after warmup. Native corpus generation is outside the timed intervals.

The final motion native harness was built with:

```sh
g++ -O3 -std=c++17 \
  /tmp/h264-native-parity-20261021/motion_component.cpp \
  /tmp/openh264-comparison-20261021/native/libopenh264.a \
  -pthread -o /tmp/h264-native-parity-20261021/motion_component
```

`motion-before.txt` uses the frozen pre-change binaries; `motion-after.txt` includes the final packed arithmetic, SATD pointer ABI and horizontal packed-tap filter. Initial arithmetic-only smoke measurements are excluded from this report.
