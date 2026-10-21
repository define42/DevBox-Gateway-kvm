# Full-screen software H.264 review — 2026-10-21

The latest optimization pass reduces full-frame conversion plus encoding time
for the tested 1080p video clip by **11.9% with one slice**. It does **not**
establish smooth 30 FPS RDP video: that path still takes 62.7 ms per frame on
this host. Four slices reduce the same work to 23.4 ms, using more CPU in
parallel. Live RDP throughput and client display FPS were not measured.

This compares the preserved first-pass baseline with the completed second-pass
working tree, as in the [earlier report](../software-h264-round2-2026-10-21/README.md).
The earlier 55–61% reductions were for small desktop updates and must not be
applied to full-screen video. Production code and the existing index were not
changed during this review.

## Video measurements

Intel i7-7700HQ, Linux amd64, Go 1.27.3, GOMAXPROCS=4. Software encoder, QP20,
1920×1080 input/output, 30 FPS codec clock, no real-time pacing. Each row is
six serial samples per version, alternating before/after order. Each sample
encodes 60 frames after 16 untimed warmup frames. Values are medians of the
per-run means; percentages describe lower time, not higher throughput.

| Damage input | Slices | Before ms/frame | After ms/frame | Time change | benchstat p |
|---|---:|---:|---:|---:|---:|
| Whole screen | 1 | 71.191 | 62.695 | -11.93% | 0.002 |
| No hints | 1 | 70.976 | 62.646 | -11.74% | 0.002 |
| Whole screen | 4 | 26.579 | 23.362 | -12.11% | 0.041 |
| No hints | 4 | 26.902 | 24.883 | -7.50% | 0.015 |

Both damage modes require full conversion and certify no unchanged
macroblocks. Four-slice runs were more variable: the whole-screen candidate's
benchstat interval is ±16%. These results support a gain on this fixture, not
a guaranteed percentage for every video or machine.

For whole-screen damage:

| Metric per frame | 1 slice, before → after | 4 slices, before → after |
|---|---:|---:|
| Encoder wall time | 69.465 → 60.951 ms | 24.873 → 21.702 ms |
| Conversion wall time | 1.700 → 1.709 ms | 1.694 → 1.669 ms |
| Process CPU time | 71.397 → 62.875 ms | 80.097 → 69.915 ms |
| Allocated bytes | 126,792 → 105,496 | 153,688 → 105,768.5 |
| Allocated objects | 12 → 8 | 29 → 19 |
| Encoded bytes | 101,806 → 101,806 | 102,018 → 102,018 |

Conversion time is statistically unchanged. CPU time is process-wide user plus
system time across all threads, including runtime/GC and harness bookkeeping;
it is not encoder-only CPU time. Four slices consume more total CPU than one
slice while reducing elapsed time. CPU budgets in the live gateway still apply.

One-slice processing alone implies a ceiling of about **16 FPS** on this clip
and host, before capture and transport. A 30 FPS budget is 33.3 ms; 60 FPS is
16.7 ms. Four-slice average processing fits the 30 FPS budget, but cannot prove
that the full RDP session does. The median of the six runs' reported p95 values
is 67.8 ms for one slice and 30.3 ms for four. These are not pooled p95s; the
short sequence and sample count limit tail conclusions.

## Fixture and correctness

The clip contains only **five 1280×720 frames** from the local OpenH264 fixture
`res/Cisco_Absolute_Power_1280x720_30fps.yuv`. FFmpeg bilinearly scales them to
1920×1080 RGBA before measurement, representing a video player rendering a
full-screen image. The gateway's native-size RGBA-to-I420 conversion is timed;
this does not measure its resize path. Frames loop, introducing a reverse
transition every fifth frame. This is a repeatable A/B input, not a broad video
corpus or a long playback test. Fixture hashes are in [build-info.txt](build-info.txt);
video data is not copied into this report.

The comparison covers complete access-unit streams, including the initial IDR:
16 warmup plus 60 comparison frames for each of eight combinations of fixture,
slice count, and damage mode. All eight before/after SHA256 fingerprints match,
including packet lengths. Full-damage and no-hint versions also match within
each fixture/slice combination. See [parity.txt](parity.txt). Thus this speed
comparison preserves encoded output for the tested inputs; it does not trade
quality or bitrate for speed.

## Entropy stress control

Eight generated pseudorandom RGB texture frames move coherently, with some
fractional-pixel motion. This is a high-entropy stress input, not natural video.
Each of six samples encodes 16 frames after 16 warmup frames, with whole-screen
damage:

| Slices | Before ms/frame | After ms/frame | Supported result |
|---|---:|---:|---|
| 1 | 148.409 | 139.216 | 6.19% lower time, p=0.002 |
| 4 | 48.943 | 47.257 | Inconclusive, p=0.485 |

At QP20 this stress input produces roughly 1.65 MB per frame and triggers codec
level-size diagnostic warnings. Trace logging is disabled equally in both timed
variants so diagnostic I/O is excluded; production logging is unchanged. The
benchmark's reported p95 selects the 15th sorted value for 16 frames, so the
stress tail metric should not be treated as a reliable p95 estimate.

## Applicability to RDP

The live writer constructs the encoder at the session output dimensions,
passes the captured RGBA image and damage to `EncodeDamage`, then sends the
access unit through `WriteAVC420Damage` in
[internal/virtio/rdp.go](../../../internal/virtio/rdp.go). This replay measures
the same conversion/encode call, excluding guest decode/rendering, framebuffer
capture/copy, RDP packetization, network writes, client decode/display, pacing,
and acknowledgment waits. Normal host services remained active, but no other
task builds or benchmarks ran alongside the timing measurements.

The repository defaults to one H.264 slice. The effective requested slice
count is capped by the host/guest CPU budget in
[rdp_workers.go](../../../internal/virtio/rdp_workers.go); the four-slice replay
requests four directly and does not exercise that cap. `H264_ENCODER=auto` may
select NVENC, which bypasses these software optimizations. The live startup log
reports the actual backend, slices, FPS limit, and QP.

For full-screen damage, unchanged-region conversion, copy elision, and early
unchanged-block decisions do not provide their sparse-update savings. Code
review confirms that SIMD transforms/chroma, zero-residual handling, and
metadata/scratch reuse still apply. The timing result measures their combined
net effect, including added bookkeeping; no per-feature attribution is claimed.

Existing RDP stats in [rdp_stats.go](../../../internal/virtio/rdp_stats.go)
report actual backend, source/output sizes, slice count, sent FPS,
copy/conversion/encoding/write time, pacing, and acknowledgment waits. A live
full-screen playback comparison using those stats is needed to establish
session throughput; sent FPS alone still does not measure client display FPS.

## Reproduction

The isolated before/after trees and binaries used here remain under `/tmp` in
this workspace. The before tree is `/tmp/h264-round2-20261021/baseline`; the
after tree is `/tmp/h264-fullscreen-20261021/candidate`. The baseline contains
the first-pass staged changes on commit
`9261ed14829c744bec4981dc54ada9e32ee15e93`. See the earlier report's build info
for the staged-patch fingerprint. The harnesses are saved as `.go.txt` so this
documentation directory does not become a Go package.

1. Copy both `encoder_video_review*.go.txt` files into each isolated
   tree's `internal/rdp/avc` directory, removing the final `.txt` suffix.
2. Prepare the five-frame fixture outside timing:

   ```sh
   ffmpeg -v error -f rawvideo -pixel_format yuv420p \
     -video_size 1280x720 -framerate 30 \
     -i /home/define42/git/openh264/res/Cisco_Absolute_Power_1280x720_30fps.yuv \
     -frames:v 5 -vf scale=1920:1080:flags=bilinear \
     -pix_fmt rgba -f rawvideo -y /tmp/h264-fullscreen-20261021/clip1080p.rgba
   ```

3. In each isolated tree, compile `go test -c ./internal/rdp/avc` to
   `/tmp/h264-fullscreen-20261021/before.test` or `after.test`, respectively,
   using Go 1.27.3. Run each binary with `GOMAXPROCS=4`, `H264_VIDEO_RGBA` set
   to the prepared file, and `-test.run=TestFullscreenVideoStreamDigest
   -test.v`; compare the `VIDEO_DIGEST` lines before interpreting timings.
4. Run `python3 measure.py /tmp/h264-fullscreen-20261021`. The runner performs
   serial alternating pairs with `-test.cpu=4`, fixed frame counts, and
   allocation reporting. Run benchstat on each before/after text pair.

Raw samples, benchstat summaries, binary hashes, the runner, and both harnesses
are included beside this report. No production implementation was changed as
part of this review.
