# H.264 macroblock batching and single validation, 2026-10-22

This round validates buffers once per macroblock or search instead of once per kernel call, and moves the remaining scalar routines to SIMD. The Go encoder now takes **9.5% less wall time and 9.6% less CPU time** (geometric means over ten cases) and executes **16% fewer instructions per frame**. All ten complete streams remain byte-identical to native OpenH264 2.6.0. Native is still faster: the Go/native ratio fell from **1.54× to 1.39×** in wall time and from **1.70× to 1.53×** in CPU time. Parity has not been reached.

## Changes

- **CAVLC residual writer.** Each macroblock is written in one call with a 64-bit accumulator passed through registers. Code tables are packed into power-of-two arrays, so masked indexes need no bounds checks, and the trailing-ones loop is unrolled. Words are stored at exactly the moments the 32-bit writer stored them.
- **Inter luma transform.** The 16×16 residual is transformed in one validated AVX2 call. The kernel body became an assembler macro shared with the four-block entry point.
- **Integer motion search.** The reference window, source block and MVD-cost range are validated once. These cover vectors in [start minimum − 1, start maximum] in each dimension, the reach of the initial-point candidates and of the diamond's four-candidate SAD. The initial point and diamond search then call the SAD kernels on pointers.
- **Fractional refinement.** One validation covers the six-tap reference halo, the source block, the ±3 quarter-pixel cost range and the scratch layout. After it, the AVX2 half-pixel filters, SATD and averaged-SATD kernels run directly.
- **Scalar routines.** Assembly row copies for 16-, 8- and 4-wide blocks. An SSSE3 intra-variance kernel for `AnalysisVaaInfoIntra`. Fixed-size views in `WelsHadamardQuant2x2`. Plane-prediction coefficients read through unchecked pointers after one span check.
- **Checked wrappers.** SAD, SATD, DCT and IDCT wrappers now do one inlined span check that returns a pointer. Before, each did a span check, a slice expression and an index check. The four-block IDCT kernel takes pointers instead of three slices.
- **Refinement disjointness.** Disjointness of the refinement buffers is proven from the fixed scratch layout instead of 21 pairwise range comparisons. The original pairwise check remains the fallback and the oracle in tests.

Each fast path takes effect only when the installed function pointers are the kernels it reproduces, and only when its spans validate. The function-table flags `bSadSIMD`, `bSatdAVX2`, `bDiamondSearch` and `BRefineAVX2` record which kernels are installed. In every other case the code falls back to the original checked functions. No search decision, quantizer or bitstream setting changed.

## Complete-codec timing

The setup is an Intel Xeon @ 2.10 GHz cloud VM with 4 vCPUs (AVX2 and AVX-512), Linux/amd64, Go 1.26.0, `CGO_ENABLED=0` and `GOMAXPROCS=4`. Each of the three encoders ran 8 serial repetitions per case, rotating through all six execution orders. Each run had 16 warmup frames and 80 measured frames. Values are medians of per-run averages, in ms/frame.

| Case | Go before | Go after | Change | p | Native 2.6.0 | After/native |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| ShortClip 1080p QP20, 1 slice | 35.46 | 31.42 | −11.4% | 0.021 | 21.45 | 1.46× |
| ShortClip 1080p QP20, 4 slices | 12.35 | 11.60 | ~ | 0.279 | 8.93 | 1.30× |
| Road 1080p QP20, 1 slice | 35.40 | 31.42 | −11.2% | 0.038 | 20.09 | 1.56× |
| Road 1080p QP20, 4 slices | 13.63 | 12.72 | ~ | 0.161 | 10.17 | 1.25× |
| Road 1080p QP32, 1 slice | 24.87 | 22.55 | −9.3% | 0.007 | 13.46 | 1.68× |
| Road 1080p QP32, 4 slices | 10.66 | 9.49 | −11.0% | 0.010 | 7.26 | 1.31× |
| PDF 1080p QP20, 1 slice | 11.05 | 10.40 | ~ | 0.083 | 7.08 | 1.47× |
| PDF 1080p QP20, 4 slices | 5.78 | 5.53 | ~ | 0.234 | 4.10 | 1.35× |
| Road 720p QP20, 1 slice | 14.81 | 13.10 | −11.5% | <0.001 | 9.14 | 1.43× |
| Road 720p QP20, 4 slices | 6.94 | 5.76 | −17.1% | 0.001 | 4.88 | 1.18× |
| **Geometric mean** | 14.34 | 12.98 | **−9.5%** | | 9.32 | **1.39×** |

Six of ten wall-time reductions are statistically significant at n=8. The four-slice ShortClip, Road QP20 and PDF cases, and the single-slice PDF case, are inconclusive. The VM is noisy: the same binary varied from 8.5 to 15 ms/frame on one case between runs. CPU time per frame shows the same pattern, −9.6% geometric mean, with after/native at 1.53×. See [benchstat](benchstat.txt) and the [before](go-before.txt), [after](go-after.txt) and [native](native.txt) samples.

## Instruction counts

Wall time on this VM varies too much to resolve changes of a few percent. Each step was therefore also measured with cachegrind (`--cache-sim=no`), which counts instructions deterministically. All runs used the 1080p road clip, QP20, one slice and `GOMAXPROCS=1`. Per-frame values are the difference between 20- and 4-frame runs divided by 16, which removes setup.

| | Instructions per frame | Go/native |
| --- | ---: | ---: |
| Go before | 243.3 M | 2.01× |
| Go after | 204.3 M | 1.69× |
| Native OpenH264 2.6.0 | 121.0 M | |

Classifying functions by origin over the 36-frame run:

| | Assembly | Compiled code |
| --- | ---: | ---: |
| Go before | 2.71 G | 5.80 G |
| Go after | 2.81 G | 4.35 G |
| Native | 2.17 G | 2.03 G |

Native's assembly is its unsymbolized NASM code; native also spends 0.20 G in libc memory routines. Most of the gap is therefore in compiled code: Go's runs about 2.1× native's instructions, down from 2.9×. Go's assembly runs about 1.3× native's, partly because the new pointer entries moved work out of Go wrappers into kernels. The classification is approximate.

Disabling bounds checks (`-gcflags=-B`) removes about 6% of Go's instructions, so bounds checks are a minority of the difference.

[Per-function changes](instructions-by-function.txt) and the [remaining excess over native by function](instructions-vs-native.txt) are retained. Instruction counts measure work, not time; the timing table above is the speed evidence.

Profile-guided optimization reduced the previous baseline's instructions by 3.4%. It was not adopted here: an earlier round found that a narrowly trained profile slowed one workload.

## Output and correctness

All ten complete streams (96 frames each) are byte-identical across Go before, Go after, native 2.6.0 and native 2.7.0. They also match the SHA-256 values recorded by the previous rounds. This holds in the default configuration, with `GODEBUG=cpu.avx2=off`, and with `GODEBUG=cpu.all=off`: 30 complete-stream comparisons. FFmpeg decoded every stream without errors. See [parity](parity-native26-native27.txt).

New tests compare each fast path with the original code on randomized inputs:

- the macroblock CAVLC writer, including overflow exits and the state they leave;
- the bit-writer step for every pending-bit count and symbol length;
- the macroblock DCT, copies, intra variance, 2×2 Hadamard and plane coefficients;
- the integer search, on 20,000 configurations, including declined windows;
- the fractional refinement, on every block size, every real scratch offset and both SATD-in-MD settings;
- the layout proof, against the full pairwise disjointness check on 200,000 configurations.

Validation passed:

- the H.264 and RDP test suites;
- race checks on the common, encoder and AVC packages;
- AVX2-, SSSE3- and all-optional-features-disabled runs;
- Linux ARM64 test builds and `go vet`;
- a 386 build;
- gofmt;
- the repository's pinned golangci-lint v2.12.2, with 0 issues.

Logs are in [validation](validation/). Cross-builds do not establish ARM64 or 386 runtime behavior or speed. The full `make test` coverage target was not run in this environment.

## What remains

The largest remaining Go-only costs are:

- the CAVLC block writer, about 25% above native's writer including extraction;
- intra 4×4 and 16×16 mode decision with scalar 4×4 predictors;
- chroma motion compensation and reconstruction wrappers;
- neighbor-cache and macroblock-loop bookkeeping (`fillNeighborCacheInter`, `OutputPMbWithoutConstructCsRsNoCopy`, `UpdateNonZeroCountCache`).

Each is a 1–3% item. One attempt in this round, a struct-based intra 4×4 cost helper, increased instructions and was reverted.

## Reproduction

The scripts expect a workspace holding:

- `native_bench`, built from [native_bench.cpp.txt](native_bench.cpp.txt) against the OpenH264 v2.6.0 tag built with `make BUILDTYPE=Release USE_ASM=Yes libraries`;
- the `go-*.test` binaries, built with `CGO_ENABLED=0 go test -overlay=overlay.json -c ./internal/rdp/avc`, where the overlay maps a virtual `internal/rdp/avc/z_openh264_comparison_test.go` to [compare_test.go.txt](compare_test.go.txt);
- the fixtures listed in [cases.json](cases.json), whose hashes match those of the [previous round](../software-h264-native-parity-2026-10-22/fixtures.json).

The OpenH264 v2.6.0 tag has the same codec sources as the reference commit used by earlier rounds. Then:

1. [check-parity.sh](check-parity.sh) builds the candidate and compares all ten streams with native.
2. [measure.py](measure.py) runs the rotating timing.
3. [cachegrind.sh](cachegrind.sh) and [cachegrind_functions.py](cachegrind_functions.py) produce the instruction counts.
4. [validate.sh](validate.sh) runs the correctness steps.

Builds, validation and measurements ran serially.
