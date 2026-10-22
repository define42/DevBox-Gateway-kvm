FROZEN: I4 HD/HU/VR directional SSE2 candidate
Baseline: /tmp/h264-parity-target-20261022/round11-next
Candidate: /tmp/h264-parity-target-20261022/intra4-directional-next
Main repository untouched. No build, test, benchmark or profile run by author.
Go source formatted. Root owns all serialized validation and timing.

NATIVE/ACTIVE-PATH EVIDENCE
Native codec/encoder/core/src/get_intra_predictor.cpp:700-705 selects MMXEXT
HD/HU/VR plus DDR/DDL/VL. Native core/x86/intra_pred.asm:625-692 (HD), :715-775
(HU), :805-866 (VR) supplies the directional averaging design. Full upstream
BSD attribution/conditions/disclaimer are included in the new assembly.
Frozen Go initIntraPredSIMD installs no directional I4 SIMD; scalar HD/HU/VR
construct sixteen output bytes and call WelsFillingPred8x2to16_c. This candidate
changes only HD/HU/VR callbacks under the existing SSE2 flag. Other predictors,
including existing DC/H/V word stores, are unchanged.
WelsMdI4x4Fast's modeCost callback (svc_base_layer_md.go:573 onward) reaches
these predictors based on neighbors and V/H/DC costs. Sequential reconstruction,
mode costs/ties, callback order and scratch selection are unchanged.
The held intra4-sad-next candidate was DC/H/V prediction+SAD fusion, a different
proposal whose identity/preflight cost erased its kernel savings. No prior
HD/HU/VR directional predictor SIMD candidate was found in archived manifests.

CEILING LIMITATIONS
round10-pdf1-top.txt has HD scalar 0.02s flat / 0.04s cumulative out of 6.42s.
All WelsMdI4x4Fast is 0.54s cumulative (8.41%) PDF and 1.29/18.3s (7.05%) Road.
The all-I4 figures include many unchanged costs, NOT an expected gain or a
ceiling attributable to these three predictors. No speedup is claimed yet.
Checked component, complete caller and whole-codec evidence decide retention.

CONTRACT/ARITHMETIC
Wrappers gather individual bytes in the scalar read order with the same signed
int offset expressions. Native wider gathers/extra halo reads are not copied.
Every reference read completes before the legacy dst[off:off+16] slice check,
including its visible-length-to-capacity extension behavior. A tracked *byte
and copied uint64 then call a noescape SSE2 leaf ($0-16 ABI). No unsafe pointers,
custom callback bypass, or alias preflight is needed. Negative/zero/overlapping
strides and source/destination aliases retain scalar behavior. Invalid inputs
preserve exact panic precedence and have no destination writes.
SSE2 is mandatory on amd64; the existing mask controls dispatch. Initializer
reset and generic scalar dispatch remain intact.
Quarter averages use PAVGB(a,c) minus (a XOR c)&1, then PAVGB with b: exactly
(a+2*b+c+2)>>2 for every byte. Explicit low-lane dword interleave avoids MMX/SSE2
upper-half differences. HD/HU store one MOVOU; VR four MOVL, exactly sixteen
bytes after all sources have become copied values.

TEST SCOPE
Unchanged scalar predictors are the independent pixel oracle. Raw and checked
results cover all 0/255 corners, all 256 values per independent source position,
1000 deterministic random vectors per mode, and whole backing-store canaries.
Aliases and strides -1984,-32,-3,-1,0,1,2,3,4,13,32,1984 are exercised. Malformed
fixtures compare exact panic type/text and all bytes across short inputs,
invalid output, extreme int offsets/int32 strides, nil slices, aliasing, and
destination visible-length/capacity behavior. Dispatch tests cover reset and
0/SSE2/SSSE3-only/AVX2-only/combined masks.
Guard tests place top rows at either edge of read-only pages, separate rows by
inaccessible pages, and place exact16 output at either guard edge. HU has exact
one-byte row edges; HD/VR use4/5-byte top edges, so left-row end cases retain3/4
extra readable bytes. Checked byte gathers and value-only raw ABI still exclude
wider source accesses. Baseline intraDCGuardedBuffer supplies output guards.
Whole-caller parity compares results/panics, MB/MD/cache/source/reconstruction,
coefficients/transform/nonzero/modes/scratch over strides/QPs/all neighbor masks,
six contents, predicted modes and full/early limits. Custom predictor and DCT
callbacks replace predictors during execution and trace order. Caller aliases,
short buffers and unusual strides preserve partial failures. Visit tests log
actual HD/HU/VR usage over24 content/neighbor fixtures and require each mode to
be reached; they are not codec-frequency measurements. AllocsPerRun requires
zero complete-caller allocations.

EXACT BASELINE BENCHMARK DEPENDENCIES (copy both unchanged)
pkg/h264/internal/encoder/intra4_directional_bench_test.go
pkg/h264/internal/encoder/intra4_sad_bench_test.go
The first is self-contained: scalar/dispatched checked HD/HU/VR at strides32
and1984 (12cases). The second is byte-identical to the held intra4-sad-next
caller fixture; its older comment names round9, but it compiles against round11
and is copied identically to both measured trees. It times complete
WelsMdI4x4Fast at32/32 and1920/1984, QP20/32, content/neighbor variants, full and
first-block exits, wrapped SAD and SATD controls (22cases). Common timed reset
restores reconstructed pixels and mode cache. Neither benchmark needs another
new test helper. New caller tests depend on the copied caller benchmark fixture;
new guard tests use existing intra_dc_guard_linux_amd64_test.go.

ROOT COMMANDS (not run by author; serialize)
cd /tmp/h264-parity-target-20261022/intra4-directional-next
/snap/go/current/bin/go test ./pkg/h264/internal/encoder -run '^TestIntra4Directional' -count=1
GODEBUG=cpu.all=off /snap/go/current/bin/go test ./pkg/h264/internal/encoder -run '^TestIntra4Directional' -count=1
/snap/go/current/bin/go test -race ./pkg/h264/internal/encoder -run '^TestIntra4Directional' -count=1
/snap/go/current/bin/go test ./pkg/h264/internal/encoder -count=1
/snap/go/current/bin/go test ./pkg/h264/internal/encoder -run '^$' -bench '^(BenchmarkIntra4Directional|BenchmarkIntra4SadCaller)$' -benchmem -count=6
Use alternating baseline/candidate pairs and benchstat. If retained, include
generic-architecture compile, full H264, native/hash/stream/decoder gates.
Packing/Go ABI overhead may consume the SIMD savings; complete callers matter.
The inherited common/copy_simd_amd64.s trailingblank was restored byte-for-byte
to frozen round11 at root request; it is not part of this candidate.

SOURCE SHA256 MANIFEST
modified 04086280b9fbc80d0d933bab70bbba1faf7c83a5c0dfc26ded7c407561875a6b  pkg/h264/internal/encoder/get_intra_predictor_simd_amd64.go
new a90b4853bf3b7527eee33ac3640c3435f1fbafc0cf019f1020c1b9b89002712c  pkg/h264/internal/encoder/intra4_directional_amd64.go
new 2b02e9c3ca79895d895a18a4ad0ecba1e2e7b330b82a738e85ee4a07603b620d  pkg/h264/internal/encoder/intra4_directional_amd64.s
new d66cfa9928cde8544f3173643b95958ff813c6d967e19d2ef926e3c69a98fd0b  pkg/h264/internal/encoder/intra4_directional_amd64_test.go
new f6a2f32dd8a084137aca92f04bd7b9ada94510a6f09ef9e9a78041e24af00ce1  pkg/h264/internal/encoder/intra4_directional_bench_test.go
new fd7aa49d5393ef6acfe428713d9b2cf271321ec345eece4e03b278498e2425a5  pkg/h264/internal/encoder/intra4_directional_caller_amd64_test.go
new 625eff6ab3e5c5e95bd1d79edee6f6fcd80412df987cd0303328248ff3769f4c  pkg/h264/internal/encoder/intra4_directional_guard_linux_amd64_test.go
new ea3df981d74bad52d1ee206394500a6ae807da9482dfafa28d9921e8b9e7e0df  pkg/h264/internal/encoder/intra4_sad_bench_test.go
