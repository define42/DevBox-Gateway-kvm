Frozen source-only I4 DC/H/V SAD fusion candidate
Tree: /tmp/h264-parity-target-20261022/intra4-sad-next
Baseline: /tmp/h264-parity-target-20261022/round9

No Go build, test, benchmark, profile, gopls or assembly tool was run by the author.
Only source reads, source edits and gofmt were performed.

Production scope:
- svc_base_layer_md.go: WelsMdI4x4Fast only; captured SAD capability plus DC/H/V seed for availability 7/9.
- intra4_sad_amd64.go: SSE2/captured SAD identity; fresh DC/H/V identity per block; exact visible-span and scratch/source/reference overlap opt-outs.
- intra4_sad_amd64.s: exact four-byte row/top loads and byte left gathers, three SIMD SADs, int32 wrapped penalties and signed H<DC scratch selection.
- intra4_sad_generic.go: untouched scalar fallback on non-amd64.

Both scratch buffers are written DC, H, V in original order. Input/scratch overlap and unusual/overflowing strides decline without mutation.
No angular-mode selection, early-cost test, per-block reconstruction, lambda map, public callback or native algorithm changes.
Native low-complexity I4 also evaluates the three candidates separately; this is an optional Go fusion, not an already-used native combined-SATD kernel.

Baseline-compatible benchmark dependency:
Copy ONLY pkg/h264/internal/encoder/intra4_sad_bench_test.go to frozen round9 or a separate baseline clone.
The file contains every new fixture/helper it needs and invokes only the existing WelsMdI4x4Fast entry.
BenchmarkIntra4SadCaller: strides 32/32 and 1920/1984; QP20/32; flat/smooth/noise; all/left-top/top-only/isolated neighbors; full/first-block exit; wrapped SAD and SATD controls.
Timed reset restores 16x16 reconstructed pixels and the prediction-mode cache identically on both sides. Source/edge pixels stay fixed; transforms/scans/reconstruction/identity/preflight are all timed.

Tests:
- Exact frozen round9 whole-caller reference (Python text equality checked, function name normalized).
- All 52 QPs, all16 neighbor values, six content classes, predicted modes -1..8; realistic strides; full/partial/first-block exits.
- Signed int32 lambda extremes, DC/H/V equal costs and both scratch orientations via independent raw oracle.
- Custom predictor/SAD/SATD callbacks, callback replacement from DC/H/angular/DCT, captured cost/context/data behavior.
- Scratch/source/reference/coefficient/count/mode aliases; negative/zero/overlapping/extreme strides, short/nil visible buffers and callbacks; compare exact recovered panic and partial output state.
- Exact-width guard pages at both ends, per-row 4-byte and per-left-byte read-only pages; protected top/penalty inputs, bounded32-byte scratch/12-byte costs.
- Complete-caller allocation parity; raw outputs surrounded by byte/int32 sentinels.

Suggested serialized commands (root owns CPU):
go test ./pkg/h264/internal/encoder -run ^TestIntra4Sad -count=1
go test ./pkg/h264/... -count=1
go test -race ./pkg/h264/internal/encoder -run ^TestIntra4Sad -count=1
GODEBUG=cpu.avx2=off go test ./pkg/h264/internal/encoder -run ^TestIntra4Sad -count=1
GODEBUG=cpu.all=off go test ./pkg/h264/internal/encoder -run ^TestIntra4Sad -count=1
GOOS=linux GOARCH=arm64 go test ./pkg/h264/internal/encoder -run ^$ -exec=true
go test ./pkg/h264/internal/encoder -run ^$ -bench ^BenchmarkIntra4SadCaller$ -benchmem -count=6

Acceptance: do not retain based on raw-kernel speed; per-block reflect and span checks may consume the DC/H/V ceiling. Require whole-caller wins without material ineligible/early-exit regressions, then stream/decoder/full-frame timing.

Manifest (SHA-256):
e4aa813219eedf1a631cc9567ea1a11d28e77302968648a386fe340b05af920a  pkg/h264/internal/encoder/svc_base_layer_md.go
13609d054208a523051fa9accd17d6c442bccab9d1b7caeb11ff3110bbee4dd3  pkg/h264/internal/encoder/intra4_sad_amd64.go
ab68263dd1f5a094fb0307efc824e22a07dacac87cf4ac4fc8dd91fb9d6b827e  pkg/h264/internal/encoder/intra4_sad_amd64.s
460cd39552d08314fa5c42f976136fc509b1ec9864e23c19dc2a909e9674e75d  pkg/h264/internal/encoder/intra4_sad_amd64_test.go
ea3df981d74bad52d1ee206394500a6ae807da9482dfafa28d9921e8b9e7e0df  pkg/h264/internal/encoder/intra4_sad_bench_test.go
6ae5d2a331ff098ba14f57dd190b030c00ecdd6b6c6ee2c0aa5aa6d93337da7c  pkg/h264/internal/encoder/intra4_sad_generic.go
71e19de80884d82685182d7b438137c8f1eda793bfdc06d241db0357a0c48ead  pkg/h264/internal/encoder/intra4_sad_guard_linux_amd64_test.go
5c93abb156fc746dd918f8c31212166b2aacea9cf94031b396304fa0fa5105ab  pkg/h264/internal/encoder/intra4_sad_reference_test.go
aaeb6d7ed3572f5cdf3d5b4cf06b77120fe8b57a83b7321d8b969d9cbb1c9223  pkg/h264/internal/encoder/intra4_sad_test.go
