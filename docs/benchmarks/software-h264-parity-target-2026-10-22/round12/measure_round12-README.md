# Round12 runner — prepared, not executed by author

`measure_round12.py` is frozen for root execution. It requires root's finished `round12-next` selection and never selects, merges, or edits production. It snapshots source under a new output directory and refuses to reuse an existing directory.

```sh
python3 /tmp/h264-parity-target-20261022/measure_round12.py
```

Defaults: source `round12-next`, output `round12-review`. A retry must use a different `--output` path; earlier evidence is never overwritten. Do not change selected source during a run.

Validation runs in the snapshot: full `pkg/h264/...` and AVC tests; race, AVX2-off and all-features-off tests for common/encoder/processing/AVC; Linux ARM64 cross-builds for those four packages; formatting checks and gopls only for actual changed Go files versus frozen round11. Build-context discovery checks amd64 files there and remaining generic/ARM64 files under ARM64. Git staging is irrelevant to change selection.

Parity captures ten new current streams and compares them with saved original/native streams, then decodes and independently counts 96 frames each (960 total). Capture results are not timing samples. Codec timing uses all ten standard cases, six rotating permutations of round11/current/native, 80 measured plus16 warmup frames. Complete FullDamage EncodeDamage timing uses ShortClip/Road/Dense at one/four slices, six rotating permutations of original/round11/current, 60 measured plus16 warmup frames. Codec clock is60 FPS; FullDamage uses the unchanged30 FPS harness. CPU affinity is2 for one slice and0-3 for four; GOMAXPROCS=4. No network/ACK/capture/presentation is measured.

Saved evidence includes raw per-invocation logs, sample rows, all benchstat comparisons, medians/CPU/native ratios, commands/environments, source/fixture/binary hashes, exact changed-file scope, and a completion marker only after all gates pass. Environment cleanup removes inherited COMPARE_*, CAVLC_*, H264_CAVLC_*, H264_VIDEO_*, GODEBUG and build flags. Source, fixtures, and binaries are rehashed at completion.

Native provenance follows the native_bench symlink to the actual ELF. OpenH264 is statically linked from `/tmp/openh264-comparison-20261021/native/libopenh264.a`, recorded by the existing build command. The runner hashes the actual ELF, archive, harness, and actual native source tree, saves the link command and readelf dependency list, and rejects an unexpected shell wrapper or shared OpenH264 linkage.

Prepared using source inspection and Python AST parsing only. No tests, builds, timing runs or gopls were executed by the author.

Frozen script SHA256: `0f4e126a3b8dbca79150db899c89cd4324f4062cc7e59c6112ab43b8f467f7db`.
