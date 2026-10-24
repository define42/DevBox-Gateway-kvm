# Reproducing the PR #3 port comparisons

These commands compare baseline commit `cc750a847437e50d54d360dd4f2d63dd43df4791` with the combined candidate source. They do not build or time native OpenH264. The OpenH264 repository supplies input fixtures and conformance data only.

The archived component measurements used separate candidates: baseline versus DCT-only, baseline versus corrected CAVLC-only, and baseline versus motion-only. `prepare.py` builds baseline and the combined current candidate. The combined component run below is a new comparison, not an exact recreation of those isolated measurements. Codec and desktop runs compare the combined candidate with the baseline. Component percentages must not be added together.

## Requirements and source selection

Use Linux amd64 with Python 3.9 or newer, Git, tar, FFmpeg/ffprobe, a Go toolchain compatible with `go.mod`, `taskset`, and benchstat. `measure.py` invokes benchstat specifically at `~/go/bin/benchstat`. The race check also needs a C compiler. Use the archived toolchain and FFmpeg versions when matching results; prepopulate the Go module cache if running offline.

The candidate checkout must contain the final implementation and its four benchmark files:

- `pkg/h264/internal/encoder/dct_mb_bench_test.go`
- `pkg/h264/internal/encoder/cavlc_mb_bench_test.go`
- `pkg/h264/internal/encoder/svc_motion_estimate_fast_bench_amd64_test.go`
- `pkg/h264/internal/encoder/md_refine_fast_bench_amd64_test.go`

`prepare.py` takes baseline from Git and snapshots the candidate's current tracked and nonignored untracked files. Uncommitted changes therefore affect the candidate. Keep unrelated changes out of that source tree. The archive directory containing the scripts must also contain `compare_test.go.txt`.

Supply an existing OpenH264 `res` directory containing these codec inputs:

- `VID_1920x1080_cabac_temporal_direct.264`
- `Adobe_PDF_sample_a_1024x768_50Frms.264`
- `Cisco_Absolute_Power_1280x720_30fps.yuv`

For the sixteen conformance cases, it must also contain `CiscoVT2people_320x192_12fps.yuv`, `CiscoVT2people_160x96_6fps.yuv`, and `Static_152_100.yuv`. Preparation links this directory as `baseline/res` and `after/res`; validation would otherwise skip missing conformance inputs.

Choose a new work directory outside the source checkout. It must not already exist. Set the following paths for your machine:

```bash
SOURCE_TREE=/absolute/path/to/DevBox-Gateway-kvm
HARNESS_DIR=/absolute/path/to/archived/harness
OPENH264_RES=/absolute/path/to/openh264/res
BENCH_ROOT=/tmp/pr3-reproduction-20261024

unset GOFLAGS GODEBUG GOOS GOARCH PYTHONOPTIMIZE
python3 "$HARNESS_DIR/prepare.py" \
  --source "$SOURCE_TREE" \
  --work "$BENCH_ROOT" \
  --fixtures "$OPENH264_RES" \
  --baseline cc750a847437e50d54d360dd4f2d63dd43df4791
```

Preparation writes frozen source trees, overlay mappings, four test binaries, decoded/scaled I420 fixtures, and `results/fixtures.json`. Compare the fixture hashes with the archived manifest before interpreting new results as the same corpus. The recorded fixture lengths were 54 frames for each Road size, 50 for PDF, and five for ShortClip. The harness cycles through these inputs when encoding more frames.

The scripts assume CPU 2 and CPUs 0–3 are available to the process. One-slice codec/parity jobs use `--cpu`; four-slice jobs always use CPUs 0–3. Component and desktop jobs use the affinity passed in `--cpu`. On a restricted CPU set, adjust the script's fixed four-slice affinity before a new experiment and record that change. `GOMAXPROCS=4` is fixed by the measurement runner.

## Timing

Run each command to completion, serially, while no builds, tests, validation, or other benchmarks run on the machine. Keep the frozen source, fixtures, and binaries unchanged. Every `--name` creates a fresh results directory and must be unique. The runner alternates baseline/candidate order across six repetitions, records commands and binary hashes, and writes raw logs plus benchstat output.

Combined component matrix, including scalar, custom-callback, narrow-block, sparse, and stationary controls:

```bash
python3 "$HARNESS_DIR/measure.py" --root "$BENCH_ROOT" \
  --variants baseline after --name combined-components --mode component \
  --regex '^Benchmark(WelsDctMb|CavlcMacroblock|WholeIntegerSearchCaller|MeRefineFracPixelCaller|WholeFractionalControls)$' \
  --repetitions 6 --benchtime 200ms --cpu 2
```

The requested matrix has nine DCT cases, eight CAVLC macroblock cases, forty integer-search cases, twenty-eight fractional callers, and eight fractional controls. Inspect the logs for missing or skipped cases; a successful runner exit alone does not establish that the entire matrix ran.

Complete codec comparison, using five settings at one and four slices (ten cases):

```bash
python3 "$HARNESS_DIR/measure.py" --root "$BENCH_ROOT" \
  --variants baseline after --name codec --mode codec \
  --repetitions 6 --frames 64 --cpu 2
```

Each codec invocation encodes sixteen warmup frames followed by sixty-four measured frames. The measured operation includes encoding and an owning payload; I420 loading, RGB conversion, capture, network writes, and presentation are outside it. Logs report wall time, process CPU time, output size, and a per-run p95. Stream capture is disabled during timing.

Synthetic 1080p desktop scrolling/widget comparison through `EncodeDamage`:

```bash
python3 "$HARNESS_DIR/measure.py" --root "$BENCH_ROOT" \
  --variants baseline after --name desktop --mode desktop \
  --repetitions 6 --benchtime 600ms --cpu 0-3
```

This selects the four Scroll/Widget × one/four-slice cases. It exercises RGB preparation, damage handling, and encoding, not a live RDP session. Its `ns/op` values are Go benchmark operation timings; they are not network latency or the live RDP “Encoding average.”

## Stream parity and CPU fallbacks

Run parity outside timing. These jobs save every encoded frame, require matching baseline/candidate hashes per case, decode with FFmpeg, and count frames with ffprobe. Each stream contains eighty frames with `--frames 64`.

```bash
python3 "$HARNESS_DIR/measure.py" --root "$BENCH_ROOT" \
  --variants baseline after --name parity-default --mode parity --frames 64 --cpu 2
python3 "$HARNESS_DIR/measure.py" --root "$BENCH_ROOT" \
  --variants baseline after --name parity-avx2-off --mode parity --frames 64 --cpu 2 \
  --godebug cpu.avx2=off
python3 "$HARNESS_DIR/measure.py" --root "$BENCH_ROOT" \
  --variants baseline after --name parity-cpu-off --mode parity --frames 64 --cpu 2 \
  --godebug cpu.all=off
```

The runner compares variants within a run, not hashes between separate CPU configurations. Explicitly compare the candidate's ten hashes across all three manifests:

```bash
python3 - "$BENCH_ROOT" <<'PY'
import json
from pathlib import Path
import sys
root = Path(sys.argv[1]) / 'results'
reference = None
for name in ['parity-default', 'parity-avx2-off', 'parity-cpu-off']:
    manifest = json.loads((root / name / 'manifest.json').read_text())
    assert 'completed' in manifest, name
    streams = {r['case']: r['sha256'] for r in manifest['records'] if r['variant'] == 'after'}
    assert len(streams) == 10, (name, len(streams))
    if reference is None:
        reference = streams
    else:
        assert streams == reference, name
print('All ten candidate streams match across default, AVX2-off, and CPU-off runs.')
PY
```

CPU-disabled parity demonstrates fallback correctness on this host. It does not measure fallback speed or ARM64 runtime behavior.

## Validation and retained evidence

After all timing completes:

```bash
python3 "$HARNESS_DIR/validate.py" --root "$BENCH_ROOT"
```

Validation records package tests, verbose conformance cases, AVX2/optional-feature fallbacks, race checks, pointer checks, vet, a 386 build, and ARM64 cross-builds. Inspect `results/validation/conformance.log` for sixteen executed cases and no missing-fixture skips. Build compatibility is distinct from runtime validation on another architecture.

Keep `results/*/manifest.json`, raw logs, per-variant samples, `benchstat.txt`, `results/fixtures.json`, validation logs, frozen source trees, overlays, and binaries together. Results depend on CPU, compiler, operating conditions, and fixture hashes. A new combined component run should retain its own label and must not replace the archived isolated component evidence.

The final repository lint check used golangci-lint v2.12.2 with Go 1.26.9. When linting identical source snapshots, use a fresh `GOLANGCI_LINT_CACHE` and `GOCACHE` for each checkout so cached absolute source paths cannot defeat the existing path exclusions. The exact successful command and environment are recorded in [the lint receipt](results/validation/lint-fresh-cache.json).
