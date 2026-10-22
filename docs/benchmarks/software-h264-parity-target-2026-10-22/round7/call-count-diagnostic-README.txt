Diagnostic-only call counts — do not merge or use timings

Source tree: /tmp/h264-parity-target-20261022/call-count-diagnostic
Copied main pkg/internal (including plane, diamond selection and quarter batching) before the retained known-zero CAVLC branch and allocation-boundary repair. Algorithm/output semantics are identical; this snapshot is for call counts only. No builds, tests or runs performed by the author. Main is untouched.

Only instrumented files:
  pkg/h264/internal/encoder/z_call_count_diagnostic.go (new)
  pkg/h264/internal/encoder/set_mb_syn_cavlc.go (one classification call after extraction)
  pkg/h264/internal/encoder/svc_motion_estimate.go (local number of actual extra SAD calls, one aggregate call before directional callback)
  pkg/h264/internal/encoder/welsEncoderExt.go (begin-frame and final Uninitialize reporting)

Enable only H264_CALL_COUNT_DIAGNOSTIC=1. Actual encoder parameters must have one layer, one thread and one slice; the exact fixture must invoke one encoder serially; the non-atomic owner check is not a concurrency guard. No atomics are used. Counters reset to zero on the first frame of a new encoder, accumulate without periodic resets, report every 100000 CAVLC/initial-point calls, then emit final:true records and reset on Uninitialize. All counters are side data; no codec buffers, costs, callback decisions, filters or bitwriter state change. JSON goes only to stderr with prefix H264_CALL_COUNTS. Frames include the comparison fixture's 16 warmup frames. Never treat the fixture's timing line from this executable as a benchmark.

CAVLC: calls = known_zero(flag==0) + extracted_zero(flag!=0,totalCoeffs==0) + nonzero. Property order is LUMA_DC,LUMA_AC,LUMA_4x4,CHROMA_DC,CHROMA_AC,other.
Initial search: uiMvcNum columns 0,1,2,3,4,5,other. Block rows follow encoder BLOCK_* order:16x16,16x8,8x16,8x8,4x4,8x4,4x8,other. extra_sad_by_mvc_count excludes the one MVP SAD inherent in every call. skipped_equal_current_best counts dynamic duplicate elision. one_mvc_distinct_by_block counts uiMvcNum==1 AND an actual second SAD, giving the proposed two-candidate fast path's true opportunity.

Root-run build (from diagnostic source tree):
  CGO_ENABLED=0 GOMAXPROCS=1 /snap/go/current/bin/go test -c -overlay=/tmp/h264-parity-target-20261022/call-count-diagnostic-overlay.json -o /tmp/h264-parity-target-20261022/call-count-diagnostic-avc.test ./internal/rdp/avc

Exact Road run (including comparison fixture and stream for hash check):
  H264_CALL_COUNT_DIAGNOSTIC=1 GOMAXPROCS=1 COMPARE_RUN=1 COMPARE_FRAMES=600 COMPARE_SLICES=1 COMPARE_WIDTH=1920 COMPARE_HEIGHT=1080 COMPARE_QP=20 COMPARE_CASE=road1 COMPARE_I420=/tmp/h264-native-parity-20261021/road1080.i420 COMPARE_STREAM=/tmp/h264-parity-target-20261022/call-count-road1.264 taskset -c 2 /tmp/h264-parity-target-20261022/call-count-diagnostic-avc.test -test.run=^TestOpenH264Comparison$ > /tmp/h264-parity-target-20261022/call-count-road1.log 2>&1

Exact PDF run:
  H264_CALL_COUNT_DIAGNOSTIC=1 GOMAXPROCS=1 COMPARE_RUN=1 COMPARE_FRAMES=600 COMPARE_SLICES=1 COMPARE_WIDTH=1920 COMPARE_HEIGHT=1080 COMPARE_QP=20 COMPARE_CASE=pdf1 COMPARE_I420=/tmp/h264-native-parity-20261021/pdf1080.i420 COMPARE_STREAM=/tmp/h264-parity-target-20261022/call-count-pdf1.264 taskset -c 2 /tmp/h264-parity-target-20261022/call-count-diagnostic-avc.test -test.run=^TestOpenH264Comparison$ > /tmp/h264-parity-target-20261022/call-count-pdf1.log 2>&1

Run the same fixture/environment with H264_CALL_COUNT_DIAGNOSTIC unset and an uninstrumented matching binary, then compare stream hashes (all 616 encoded frames). This validates that diagnostic output has not changed the stream. Do not compare timings from these runs.

Read final:true H264_CALL_COUNTS records for exact complete totals. Periodic records are cumulative, so do not add them together.
