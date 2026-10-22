# Remaining native OpenH264 gap: follow-up review

This review inspects the final Go source, the existing final Go/native binaries, and the saved final profile. It does not introduce encoder changes or new timing runs. Measurements below come from the [matched suite](README.md) and [component benchmarks](components.md), on the i7-7700HQ. Native is the port's OpenH264 2.6.0 reference commit, not a comparison against latest upstream.

Native remains 1.60–1.84× faster in the codec-only cases. For 1080p road video at QP20, Go/native medians are 46.05/25.88 ms with one slice and 19.41/10.56 ms with four slices. All ten complete tested streams were byte-identical. RGB conversion, socket writes and ACK waits are outside this comparison. The substantial one-slice gap also rules out worker coordination as the primary explanation.

## 1. Native specializes more of the actual motion workload

The current Go encoder already has AVX2 SATD and half-pixel filtering. However, having AVX2 dispatch does not imply the same implementation efficiency:

- Fractional refinement requests horizontal widths 5, 9 and 17 at [md.go:544](../../../pkg/h264/internal/encoder/md.go). Our [horizontal dispatch](../../../pkg/h264/internal/common/mc_avx2_amd64.go) uses SSE2 below width 16; width 17 uses a scalar last-pixel tail on every row. Native selects `McHorVer20Width5Or9Or17_avx2` (`codec/common/src/mc.cpp:4555`), with dedicated multi-row paths in `codec/common/x86/mc_luma.asm:3560`. Existing filter timings cover 16×16, so the actual refinement dimensions need their own benchmarks.
- Our [8-wide SATD loader](../../../pkg/h264/internal/encoder/sample_simd_batch_amd64.s) processes four rows at a time with unused lane capacity. Native packs eight rows into the two AVX2 lanes (`codec/common/x86/satd_sad.asm:1538`). Measured Go/native times are 26.06/9.22 ns for 8×8 and 40.34/15.42 ns for 8×16, versus 34.38/25.79 ns for 16×16.
- Our vertical filters reload and widen six rows per output row. Native carries overlapping row pairs forward in registers and uses packed multiply-adds across multiple output rows. Native-versus-Go vertical and center-filter timings have not yet been measured. Both implementations use two passes and scratch for center filtering; two-pass filtering alone is not a Go-specific disadvantage.

The component comparisons include Go validation and call overhead versus unchecked native entry points. They establish a difference in the measured operations, not an isolated cost for SIMD lane packing. Fractional refinement is 17.87% cumulative CPU in the final Go profile; SATD is 5.20% flat. These figures overlap and do not quantify shares of the native gap.

## 2. The complete CAVLC writer still has extra inner-loop work

SIMD coefficient extraction is already implemented. Go's fully dense extraction is actually faster in the component comparison: 6.99 ns versus native's 9.61 ns. Ordinary partial blocks still favor native.

The remaining larger target is [the complete writer](../../../pkg/h264/internal/encoder/set_mb_syn_cavlc.go): 11.94% cumulative / 7.31% flat CPU, compared with 1.38% flat in SIMD extraction. Inspection of the existing final binaries confirms:

- Go's flush expands [WRITE_BE_32](../../../pkg/h264/internal/common/golomb_common_h.go) into four checked byte stores. Native emits a byte swap and one 32-bit store.
- Go initializes a local writer structure and repeatedly loads/stores accumulator state on the stack. Native's macro-expanded writer keeps that state in registers. The previous split-flush change made the Go common method inline, but did not make its generated code equivalent to native's.
- Bounds checks remain around token, suffix, total-zero and run-before tables. The maximum-three trailing-ones loop also remains a loop in Go; the native compiler unrolls it.

These are verified instruction differences, not measured percentages of the overall gap. The next entropy experiment should compare the complete native and Go writers, then test word stores, state handling and several residual blocks sharing one validated writer state. Retain the Go level lookup table unless measurement supports replacing it; it already avoids arithmetic present in native.

## 3. Small operations still pay repeated validation and call costs

Typed-pointer assembly interfaces have reduced argument traffic. Nevertheless, the compiled [SATD wrapper](../../../pkg/h264/internal/encoder/sample_simd_amd64.go) still computes two buffer spans, performs final index checks, writes six stack arguments, calls an ABI0 entry, and restores Go ABI registers.

Quarter refinement checks overlap for the group and then validates three spans again for each averaged-SATD candidate. A private batch can validate the complete region once and amortize call overhead. Native also calls individual candidates; it should not be described as already batching the whole refinement operation. Our fused average-plus-SATD already avoids intermediate writes that native performs.

The prior luma experiment establishes that batching can matter here: sixteen-block quantize/scan takes about 98–130 ns, versus 162–198 ns through four checked calls. This is evidence for that specific batch, not a prediction for other operations. Preserve visible-length, alias and stride contracts when introducing internal batches. Go's [assembler documentation](https://go.dev/doc/asm) describes stack argument conventions and inspecting final generated code.

## 4. Smaller native specializations remain unported

Our [block-copy functions](../../../pkg/h264/internal/common/copy_mb.go) still use generic row loops, whereas native selects unrolled block kernels (`codec/encoder/core/src/encode_mb_aux.cpp:499`). The Go compiler already lowers a 16-byte row copy to vector loads/stores, so it is incorrect to claim a `memmove` call for each row. The final binary still has repeated slice-bound and same-address branches per row. `welsCopyNxM` accounts for 1.95% flat CPU; this is a bounded, smaller candidate after motion and entropy work.

Larger Go slice-bearing structures are another source-level difference, but no current cache measurements establish their contribution. A broad data-layout rewrite is lower-confidence than the concrete candidates above.

## Runtime and scope

Filtering the existing final profile for `runtime.` yields 0.23 s / 12.31 s, or 1.87% sampled CPU: 0.17 s in `memmove`, 0.05 s in clearing memory and 0.01 s in scheduler bookkeeping. No GC routines appear in these samples. This is not evidence for GC as a major bottleneck. Compiler-inserted checks and ABI setup are charged to encoder functions, so the runtime percentage does not measure all Go-related overhead.

The profile cannot assign an exact portion of the Go/native gap to each source difference. Fractional refinement and the residual writer are separate regions totaling about 29.8% of sampled CPU. Even halving both would save only about 14.9% overall under that workload; the matched single-slice road result requires about a 44% latency reduction to equal native. Several paths must improve to reach parity.

Priority: benchmark and specialize the real refinement dimensions and 8-wide SATD; optimize the complete bit writer; amortize validated boundaries across useful batches; then address smaller block-copy and scalar-control costs. Keep whole-frame measurements and byte-for-byte/decoder/fallback validation as the acceptance gate. Current data supports these priorities, not a promise that they will achieve parity.
