# Intra4 scratch isolation experiment

**Held; not selected for production.** All sixteen frame comparisons are inconclusive. [Retention decision](decision.txt).

This is an isolated experiment against [frozen round11](../../round11/README.md).
Archival does not select or merge the candidate. This helper makes no retention
decision. Component measurements do not establish a full-frame speedup or native
parity, and none of these harnesses measures the live RDP Encoding average.

The candidate places the 32-byte prediction buffer and two 16-byte mode buffers
inside one allocation with leading and trailing `cpu.CacheLinePad` guards. The
five-file [patch](scratch-isolation.patch) changes only allocation ownership and
adds tests, a complete I4 caller benchmark and real InitializeExt address diagnostics.
Exact slice lengths/capacities remain unchanged. The geometry assumption is the
architecture's x/sys padding size; this is not a universal hardware guarantee.
[Scope, source evidence and contracts](intra4-scratch-isolation-next-README.txt).

## Component and allocation evidence

[Component statistics](component/benchstat.txt), [baseline addresses](component/base-addresses.txt),
[candidate addresses](component/after-addresses.txt) and all raw samples are retained.
The diagnostic uses real one/four-slice InitializeExt allocations, not artificially
packed workers. Address sharing identifies placement; it alone does not measure
contention. One/four-worker complete I4 benchmarks have six alternating pairs,
200 ms per case, CPUs 2/0-3 and GOMAXPROCS=4. They measure caller throughput.

## Full EncodeDamage gate

All six results remain inconclusive. These data are retained in full and are not
replaced by the separate codec scope below. Before is frozen round11, not the
original project baseline. Six alternating pairs use 60 measured + 16 warmup frames,
30 FPS, CPUs 2/0-3, including conversion, damage preparation, encoding and owning
payload construction. Capture, socket/ACK waits and presentation are excluded.

| Case | Round11 ms | Scratch ms | Change | p-value |
| --- | ---: | ---: | ---: | ---: |
| FullscreenVideo/ShortClip/1080p/1Slices/FullDamage-4 | 32.585 | 33.128 | +1.66% | 0.589 |
| FullscreenVideo/ShortClip/1080p/4Slices/FullDamage-4 | 10.971 | 11.155 | +1.68% | 0.589 |
| FullscreenVideo/Road/1080p/1Slices/FullDamage-4 | 33.530 | 33.965 | +1.30% | 0.937 |
| FullscreenVideo/Road/1080p/4Slices/FullDamage-4 | 12.548 | 12.951 | +3.21% | 0.818 |
| FullscreenVideo/Dense/1080p/1Slices/FullDamage-4 | 81.161 | 80.047 | -1.37% | 0.699 |
| FullscreenVideo/Dense/1080p/4Slices/FullDamage-4 | 24.545 | 24.351 | -0.79% | 0.937 |

[Raw/statistical results](full-path/full-benchstat.txt),
[exact summary](full-path/full-summary.json), [provenance](full-path/provenance.json).

## Distinct codec-only gate

The codec scope adds PDF, 720p and QP32 absent from the full-path case set. It uses
six adjacent alternating pairs for all ten standard cases, 80 measured + 16 warmup
frames, 60 FPS and the same affinity. It times encoding and owning payload only.
Native is not rerun. Classifications at p<0.05: 0 reductions, 0
regressions and 10 inconclusive cases. These classifications do not select
the experiment or supersede the six neutral full-path results.

| Case | Round11 ms | Scratch ms | Change | p-value |
| --- | ---: | ---: | ---: | ---: |
| OpenH264/ShortClip/1920x1080/QP20/1Slices-4 | 34.040 | 33.707 | -0.98% | 0.485 |
| OpenH264/ShortClip/1920x1080/QP20/4Slices-4 | 10.882 | 11.101 | +2.01% | 0.485 |
| OpenH264/Road/1920x1080/QP20/1Slices-4 | 33.388 | 33.585 | +0.59% | 0.818 |
| OpenH264/Road/1920x1080/QP20/4Slices-4 | 14.899 | 14.231 | -4.48% | 0.818 |
| OpenH264/Road/1920x1080/QP32/1Slices-4 | 24.797 | 24.753 | -0.18% | 1.000 |
| OpenH264/Road/1920x1080/QP32/4Slices-4 | 12.088 | 11.219 | -7.19% | 0.818 |
| OpenH264/PDF/1920x1080/QP20/1Slices-4 | 12.794 | 12.754 | -0.31% | 0.699 |
| OpenH264/PDF/1920x1080/QP20/4Slices-4 | 5.731 | 7.691 | +34.21% | 0.394 |
| OpenH264/Road/1280x720/QP20/1Slices-4 | 14.556 | 14.793 | +1.63% | 0.394 |
| OpenH264/Road/1280x720/QP20/4Slices-4 | 9.842 | 7.518 | -23.61% | 0.937 |

[Raw/statistical results](codec/benchstat.txt), [exact summary](codec/summary.json),
[scope and binary provenance](codec/provenance.json).

## Correctness and reproduction

The preflight checks ten candidate streams against saved original/native hashes.
FFmpeg decoded 960 frames; ffprobe independently counted 96 frames in each case.
[Parity/hash summary](full-path/parity/parity.json), [decoder records](full-path/parity/parity.txt).
Component package/race results are preserved with the component artifacts.
Binaries and stream payloads are excluded; [omitted-payloads.json](omitted-payloads.json)
retains their paths, sizes and hashes. Overlay files retain exact content with
`.go.txt` names; restore `.go` before adapting the measurement scripts' local paths.

The patch is against the complete [round11 source fingerprint](round11-sources.sha256),
not the original commit. [Changed-file hashes](changed-files.json) and the archived
[candidate fingerprint](full-path/candidate-sources.sha256) identify all source.
The three measurement scripts and this archival helper are saved here. Their
absolute workspace/fixture paths require adaptation elsewhere. The helper refuses
to overwrite its target, requires both gates to finish, and verifies the five-file
delta and fingerprints before copying evidence. [Archive manifest](archive-manifest.json).
