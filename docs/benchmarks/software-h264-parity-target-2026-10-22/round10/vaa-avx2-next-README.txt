VAA AVX2 candidate: frozen source manifest
==========================================
Tree: /tmp/h264-parity-target-20261022/vaa-avx2-next
Baseline: /tmp/h264-parity-target-20261022/round9
Author work was source-only: no builds/tests/benchmarks/profile jobs run by this agent.
Root owns execution and results. Final independent source review by motion agent
found no actionable issues, including disabled-CPU fallback and exact-span guards.
No production or test edits after source freeze; this manifest was completed later.

Bounded active-native gap
-------------------------
Matched camera/LOW_COMPLEXITY/RC_OFF fixtures disable AQ and BGD. All three VAA
flags are false on P frames and IDR: Go encoder/wels_preprocess.go:263,264,278,295
and native codec/encoder/core/src/wels_preprocess.cpp:272,273,283,300 establish this.
processing/vaacalculation.go:45-65 therefore selects the SAD-only path. Native
codec/processing/src/vaacalc/vaacalculation.cpp:53-75 upgrades SAD to AVX2 at lines
67-69. /tmp/h264-parity-target-20261022/native-road1-flat.txt:24 actually samples
VAACalcSad_avx2 (0.15 s, 1.30%). This proves active dispatch, not a matched component
ratio: the native profile method and sample period differ from Go's.

Native codec/processing/src/x86/vaa.asm:2344 AVX2_CalcSad_8Lines and :2396
VAACalcSad_avx2 process two 16-wide macroblocks per 32-byte load, unroll eight rows
and use four independent accumulators. The odd tail remains 16 bytes. Baseline Go
vaaSadRowSSE2 is one macroblock and one accumulated eight-row loop at a time.
Both read each pixel once. There is no extra Go plane copy or missing pixel reuse;
the bounded difference is vector width, loop overhead and instruction scheduling.
Go retains one checked raw call per macroblock row and original row commit order.

Round9 corrected profiles (no harness stream accumulation):
- round9-pdf1-top.txt:11: vaaSadRowSSE2 0.27/6.52 s = 4.14% CPU.
- round9-road1-top.txt:31: vaaSadRowSSE2 0.20/18.04 s = 1.11% CPU.
These are elimination ceilings, not predicted gains. A hypothetical 50% kernel
saving implies roughly 2.07%/0.56% CPU ceilings respectively before other costs;
whole-frame measurements decide retention. No native performance parity claim.

Implementation and contracts
----------------------------
Only SAD dispatch is upgraded; BGD/SSD/variance and generic builds are unchanged.
The checked wrapper falls back to SSE2 before state access if cpu.X86.HasAVX2 is
false, including GODEBUG disablement. Complete block validation, panic text and
initial frame reset match SSE2; partial dimensions retain scalar behavior.

The raw typed-pointer/noescape AVX2 row kernel packs TL/TR/BL/BR per macroblock,
uses dword modular frame accumulation and explicitly returns four bytes (MOVL).
Its paired path reads exactly 32 bytes per row; its odd tail exactly 16 bytes.
Zero macroblocks does not dereference any pointer. Negative/zero/overlapping row
strides retain existing wrapper validation/fallback behavior.

Paired input loads/stores may differ from the legacy per-MB ordering when output
aliases pixels. A checked output-versus-input enclosing-span overlap test routes
those cases to the old SSE2 implementation. The frame-SAD pointer may alias pixels
or output; the exact original per-row compound assignment expression is retained.
Masks use the unchanged existing vaaCalcSadMasked path, including borrowed-mask
release, clearing, changed-run iteration and partial failures.

Tests and benchmark dependencies
--------------------------------
New tests cover raw vs SSE2 and scalar; checked complete caller and all quadrants;
full frame and row int32 overflow; zero/overlapping/negative stride; input/output/
frame aliases across multiple MB rows; malformed offsets and visible lengths;
exact panic and entire backing partial state; masks and consumed hints; zero
allocations; read-only source pages and both-edge exact output/frame guards;
and AVX2-disabled subprocess complete/failure/alias parity.

The independent actual-native oracle is separately frozen at:
/tmp/h264-parity-target-20261022/vaa-native-oracle/README.md
It uses 146 deterministic fixtures, native C/AVX2 plus eligible SSE2, and additive
Go overlays (no candidate edits); --avx2 registers VAACalcSad_avx2 explicitly.
Consult that manifest for exact native build/overlay commands and scope limits.

BASELINE BENCHMARK: copy ONLY pkg/h264/internal/processing/vaa_avx2_bench_test.go
to a separate round9 baseline. It is self-contained and baseline-compatible, with
no test-helper dependencies outside this file. Candidate parity tests use its
fixture helper, so retain that benchmark file when copying candidate test files.
BenchmarkVAAActiveCaller times actual dispatched Set+Process, including validation,
mask borrowing/release and row commits. Construction, buffers and input filling are
outside timing. Sizes 1920x1088/1280x720/1904x1088/48x32/16x16 cover even and odd MB
widths, six mask/content modes each. Reported bytes/op counts visible pixels once,
not total memory traffic. All inputs are offset by 13 bytes.

Source classification of reported masked controls
--------------------------------------------------
AllUnchangedMask executes no SAD callback at all: timed Set/Process/clear/mask scan
and final zero assignment are unchanged. Any measured difference is outside AVX2
kernel/wrapper execution; binary layout/cache/noise is plausible but unproved.
SparseOddRun at 1920/1280/1904 has exactly one changed run of three MB in the middle
MB row: one AVX2 pair plus one exact tail. The new wrapper performs one CPU feature
check and one disjointness preflight. The rest is full-frame clearing and mask
traversal. These are controls for small changed regions; retain reported regressions
honestly and do not extrapolate full-frame gains to sparse damage.

Suggested serialized root commands (not executed by source author)
-------------------------------------------------------------------
/snap/go/current/bin/go test ./pkg/h264/internal/processing -run 'TestVAA|TestVpFrameWorkUsesDetectedVAAFeatures' -count=1
GODEBUG=cpu.avx2=off /snap/go/current/bin/go test ./pkg/h264/internal/processing -run 'TestVAA|TestVpFrameWorkUsesDetectedVAAFeatures' -count=1
/snap/go/current/bin/go test ./pkg/h264/... -count=1
GOOS=linux GOARCH=arm64 /snap/go/current/bin/go test -c -o /tmp/h264-parity-target-20261022/vaa-processing-arm64.test ./pkg/h264/internal/processing
GOMAXPROCS=1 /snap/go/current/bin/go test ./pkg/h264/internal/processing -run '^$' -bench '^BenchmarkVAAActiveCaller$' -benchmem -benchtime=250ms -count=1
Run the final command in alternating isolated baseline/candidate pairs, then benchstat;
root's recorded actual command/settings and raw results take precedence.
Require existing native stream/decoder/hash, full codec/native and fullscreen gates
before making whole-encoder performance claims.

Frozen candidate file SHA256
-----------------------------
9dd98630377efe256024ce1a1d05ef3d34fcf9c4060519ee913f9e0dabf7849e  pkg/h264/internal/processing/vaacalcfuncs_simd_amd64.go
bb7851997efb631e414559c0a2b0282a88cecaf2450720e0f12da423a7ec364e  pkg/h264/internal/processing/vaacalcfuncs_amd64_test.go
93ba50b95218a7a58aeb913f1fffa0809d5dd8165b6e12e781242734c37b9d76  pkg/h264/internal/processing/vaa_avx2_amd64.go
9a2703fab0261449a4ea70823c6db8bc8c4c7fa5c2aefaeb8af2cc6632989e43  pkg/h264/internal/processing/vaa_avx2_amd64.s
a9fc7c161fb9248d5d27a92afd5b813a9389cd2021a4d9cc4aeef062f04be5e9  pkg/h264/internal/processing/vaa_avx2_amd64_test.go
262acd58343d3dfdfb2cbb16acdbade81da261ebb7f0b1e1ca31ba4e133df328  pkg/h264/internal/processing/vaa_avx2_cpu_amd64_test.go
07043da208f7eb38f527ce0fc79038212dacd69be0f5907611ade4b37925d9e0  pkg/h264/internal/processing/vaa_avx2_guard_linux_amd64_test.go
aaa1848cbb7e6fe01f5163491bdf546e0c94436a67c278ff25ecf1517ce1e7c6  pkg/h264/internal/processing/vaa_avx2_bench_test.go

Baseline hashes of modified existing files
------------------------------------------
154c46e71be491942ab652bac2c7796105b127e5c4cd21e6217e3a5ff4b5a3b9  pkg/h264/internal/processing/vaacalcfuncs_simd_amd64.go
77727708bc2c1ee030870dbf4b9154c124be0317503440d6ae8e157fa273d9f9  pkg/h264/internal/processing/vaacalcfuncs_amd64_test.go
