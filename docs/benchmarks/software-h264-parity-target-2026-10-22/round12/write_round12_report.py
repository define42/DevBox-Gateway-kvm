#!/usr/bin/env python3
"""Write round12 reports from completed archived evidence; no measurements."""
from pathlib import Path
import hashlib
import json
import math
import re

ROOT = Path('/home/define42/git/DevBox-Gateway-kvm/docs/benchmarks/software-h264-parity-target-2026-10-22')
OUT = ROOT / 'round12'
RUN = OUT / 'run'


def load(path):
    return json.loads(path.read_text())


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def tree(path):
    return {str(p.relative_to(path)): sha(p) for p in sorted(path.rglob('*')) if p.is_file()}


def wall_rows(path, names):
    wanted = {name.removeprefix('Benchmark') for name in names}
    rows = {}
    # sec/op is the first table; stop at its geomean before later metrics.
    for line in path.read_text().splitlines():
        if line.startswith('geomean') and rows:
            break
        name = line.split()[0] if line.split() else ''
        if name in wanted:
            assert name not in rows, f'Duplicate wall row: {name}'
            ps = [float(value) for value in re.findall(r'p=([0-9.]+)', line)]
            assert ps, line
            rows[name] = {'p': ps[0], 'line': line}
    assert set(rows) == wanted, (path, set(rows) ^ wanted)
    return {name: rows[name.removeprefix('Benchmark')] for name in names}


def counts(rows, probabilities, delta):
    wins = sum(probabilities[x['name']]['p'] <= .05 and x[delta] < 0 for x in rows)
    losses = sum(probabilities[x['name']]['p'] <= .05 and x[delta] > 0 for x in rows)
    return f'{wins} significant reductions, {losses} significant regressions and {len(rows)-wins-losses} inconclusive cases'


def geomean(rows, before, unit='ns/op'):
    return 100 * (math.exp(sum(math.log(x['medians']['current'][unit] / x['medians'][before][unit]) for x in rows) / len(rows)) - 1)


def label(name):
    return name.removeprefix('Benchmark').removeprefix('OpenH264/').removeprefix('FullscreenVideo/').removesuffix('-4').removesuffix('/FullDamage')


def main():
    complete = load(RUN / 'complete.json')
    archive = load(OUT / 'archive-manifest.json')
    assert complete['complete'] and complete['decoded_frames'] == 960 and complete['source_and_inputs_stable']
    assert archive['retained_groups'] == 34 and archive['h264_files'] == 573 and archive['cumulative_patch_replayed']
    scratch = OUT / 'scratch-isolation-review'
    assert tree(scratch) == archive['existing_scratch_archive']
    assert not (OUT / 'README.md').exists(), 'Report already exists; review before replacing it'
    codec, full = load(RUN / 'codec/summary.json'), load(RUN / 'full/summary.json')
    assert len(codec) == 10 and len(full) == 6
    assert all(set(x['samples']) == {'round11', 'current', 'native'} and set(x['samples'].values()) == {6} for x in codec)
    assert all(set(x['samples']) == {'original', 'round11', 'current'} and set(x['samples'].values()) == {6} for x in full)
    cp = wall_rows(RUN / 'codec/benchstat.txt', [x['name'] for x in codec])
    fi = wall_rows(RUN / 'full/incremental-benchstat.txt', [x['name'] for x in full])
    fc = wall_rows(RUN / 'full/cumulative-benchstat.txt', [x['name'] for x in full])
    ratios = {s: [x['current_vs_native_wall_ratio'] for x in codec if '1920x1080' in x['name'] and f'{s}Slices' in x['name']] for s in [1, 4]}
    cpu_ratios = [x['current_vs_native_cpu_ratio'] for x in codec if '1920x1080' in x['name']]
    assert len(ratios[1]) == len(ratios[4]) == 4
    # A faster median is not equivalence. Stop for manual reassessment if native
    # CPU/wall gaps unexpectedly disappear, instead of writing a stale claim.
    assert max(ratios[1]) > 1.05 or max(cpu_ratios) > 1.05, 'Reassess native-parity conclusion from new evidence'
    noisy_four = any(any(float(value) > 20 for value in re.findall(r'±\s*([0-9.]+)%', cp[x['name']]['line'])) for x in codec if '4Slices' in x['name'])
    noise_note = ('At least one four-slice wall comparison has a confidence range above 20%; those medians are especially uncertain. '
                  if noisy_four else '')
    ct = '\n'.join('| ' + label(x['name']) + ' | ' + ' | '.join(f"{x['medians'][kind]['ns/op']/1e6:.3f}" for kind in ['round11', 'current', 'native']) +
                   f" | {x['current_vs_round11_wall_percent']:+.2f}% | {cp[x['name']]['p']:.3f} | {x['current_vs_native_wall_ratio']:.3f}× | {x['current_vs_native_cpu_ratio']:.3f}× |" for x in codec)
    ft = '\n'.join('| ' + label(x['name']) + ' | ' + ' | '.join(f"{x['medians'][kind]['ns/op']/1e6:.3f}" for kind in ['original', 'round11', 'current']) +
                   f" | {x['current_vs_round11_wall_percent']:+.2f}% | {fi[x['name']]['p']:.3f} | {x['current_vs_original_wall_percent']:+.2f}% | {fc[x['name']]['p']:.3f} |" for x in full)
    commands = load(RUN / 'validation.json')
    assert all(x['exit_code'] == 0 for x in commands)
    scope = load(RUN / 'gopls-scope.json')
    assert not scope['uncovered']
    validated = {x['name'] for x in commands}
    assert {'tests', 'race', 'fallback-avx2', 'fallback-all', 'arm64-common', 'arm64-encoder', 'arm64-processing', 'arm64-avc', 'build-comparison'} <= validated
    parent = (ROOT / 'README.md').read_text()
    assert 'thirty-three optimization groups' in parent and '(round11/README.md) contains current' in parent
    before_sentence = 'The full-path before/after comparison uses original commit `641af9e6216055b6a9d918bdcba4ea7d40020750`. Its codec before column uses round ten to isolate the two newest changes; do not combine those baselines.'
    assert before_sentence in parent
    report = f'''# Software H.264 performance parity: round twelve

**Native speed parity remains unmet.** Matched 1080p one-slice Go median times are **{min(ratios[1]):.2f}–{max(ratios[1]):.2f}× native**; four-slice wall ratios are {min(ratios[4]):.2f}–{max(ratios[4]):.2f}×. CPU ratios across those 1080p cases are {min(cpu_ratios):.2f}–{max(cpu_ratios):.2f}×. {noise_note}All ten checked streams remain byte-identical to original Go and native. Similar wall time alone would not establish equal CPU efficiency.

This round adds SSE2 horizontal-down, horizontal-up and vertical-right I4 predictors to the thirty-three groups retained in [round eleven](../round11/README.md), for **thirty-four groups**. Native installs directional MMXEXT predictors; Go had scalar directional construction/copying. Checked Go wrappers now gather the exact source bytes in the original order and pass copied values to an SSE2 leaf. Source aliases, unusual strides, validation/panic order and exact sixteen-byte output stores are preserved. Native's wider source reads are not copied. Other predictors and encoder settings remain unchanged. [Source and native references](intra4-directional-next-README.txt).

The six dispatched predictor cases improve 36.04–49.73%; all six unchanged scalar controls are inconclusive. Of 22 complete `WelsMdI4x4Fast` caller cases, ten improve 3.41–7.01%, twelve are inconclusive and none significantly regress. The caller-only descriptive geomean is −3.06%; all 408 samples report zero allocations. The two first-block/DC128 controls are inconclusive. Six alternating pairs use CPU 2 and 200 ms per case against frozen round eleven. These component results are not additive frame gains. [Component audit](intra4-directional-review/README.md), [statistics](intra4-directional-review/benchstat.txt).

## Complete codec comparison

**The baseline is frozen round eleven.** Six rotating triples compare round eleven/current/native with 16 warmup and 80 measured frames per run. One-slice cases use CPU 2; four-slice cases use CPUs 0–3. Linux amd64, Intel i7-7700HQ, Go 1.27.3, CGO disabled and GOMAXPROCS=4. Other workstation activity was not disabled. Stream capture is off during timing.

| Case | Round eleven ms | Current ms | Native ms | Change vs eleven | p-value | Wall/native | CPU/native |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
{ct}

This incremental comparison has {counts(codec, cp, 'current_vs_round11_wall_percent')}. The descriptive median wall-time geomean changes by {geomean(codec, 'round11'):+.2f}%; CPU geomean changes by {geomean(codec, 'round11', 'cpu_ns/frame'):+.2f}%. Inconclusive results do not establish equality or improvement. [Three-way statistics](run/codec/benchstat.txt), [current/native statistics](run/codec/native-benchstat.txt), [raw medians and CPU metrics](run/codec/summary.json).

Native is OpenH264 2.6.0 commit `4c8c5cdc0299407af14751a4c37449ff3d367745`, built with GCC 13.3 `-O3` and assembly/AVX2. Its actual executable and statically linked OpenH264 archive are fingerprinted; the system C/C++ runtime remains dynamically linked. Both implementations use camera real-time, low complexity, QP20 unless QP32 is labeled, one reference/spatial/temporal layer, Baseline/CAVLC, fixed slices, full-range BT.709 and a 60 FPS codec clock. Rate control, deblocking, periodic IDR, frame dropping, scene/background detection, AQ, denoising, LTR and load balancing are disabled. [Matching settings](../../software-h264-native-parity-2026-10-22/README.md#inputs-and-matching), [exact binaries and inputs](run/provenance.json), [binary hashes](run/binary-hashes.json).

Codec timing includes encoding and an owning payload. RGB conversion, initialization, loading, capture, framing, network/ACK waits and presentation are excluded. Absolute times across separate rounds are not paired comparisons.

## Full EncodeDamage comparison

Six rotating triples compare the original commit `641af9e6216055b6a9d918bdcba4ea7d40020750`, frozen round eleven and current source. Each uses 16 warmup and 60 measured frames, full-screen damage, RGB conversion, damage preparation, encoding and owning payload construction. This harness uses FrameRate=30; codec-only timing uses 60 FPS. Capture, socket writes, ACK waits and presentation are excluded.

| 1080p case | Original ms | Round eleven ms | Current ms | Change vs eleven | p-value | Change vs original | p-value |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
{ft}

Against round eleven there are {counts(full, fi, 'current_vs_round11_wall_percent')} (descriptive geomean {geomean(full, 'round11'):+.2f}%). Against the original there are {counts(full, fc, 'current_vs_original_wall_percent')} (descriptive geomean {geomean(full, 'original'):+.2f}%). [Incremental statistics](run/full/incremental-benchstat.txt), [cumulative statistics](run/full/cumulative-benchstat.txt), [three-way metrics](run/full/summary.json). These timings do not measure the live RDP “Encoding average”.

## Correctness, held candidates and provenance

All ten current streams match saved original-Go and native streams byte for byte. FFmpeg decoded all 960 frames without errors; ffprobe independently counted 96 frames per case. Capture and decoding ran outside timing. [Stream hashes and counts](run/parity/parity.json), [decoder evidence](run/parity/parity.txt).

Full H264/AVC package tests, four-package race checks, AVX2-disabled and optional-features-disabled tests, four Linux ARM64 test-binary cross-builds, formatting and gopls on this round's changed Go files passed on the isolated selected snapshot. Cross-builds establish build compatibility, not ARM64 runtime performance. Nonverbose package logs do not prove that every optional external-fixture test ran. [Validation commands](run/validation.json), [gopls file scope](run/gopls-scope.json). Directional tests cover scalar/random/extreme parity, exact span guards, aliases, signed/overlapping strides, malformed-input panic order, custom callback behavior, complete I4 state and zero allocations.

The initial register-state **CAVLC emitter remains held**. Six paired complete-caller/public measurements include dense and mixed gains, but significant sparse macroblock, short chroma and extracted-zero regressions reject it for this milestone. Native 447-suite correctness, race and CPU-off checks passed after correcting an unused test import; these are correctness comparisons, not native timing runs. The initial failed build, correction, frozen/working source fingerprints and all six timing pairs remain archived. The two verbose test logs are gzip-compressed with verified decompressed hashes. [Decision](cavlc-emitter-review/decision.txt), [complete statistics](cavlc-emitter-review/benchstat.txt), [source provenance](cavlc-emitter-review/provenance.json), [compression metadata](compression.json). A separately tuned experiment is not part of this selected source or these timings.

The earlier allocation-isolation experiment also remains held, and its existing archive is preserved byte for byte. [Scratch evidence](scratch-isolation-review/README.md). No fresh round-twelve CPU profiles or call-count diagnostics were collected by this runner; earlier profile findings remain scoped to their respective binaries.

The selected source has 573 H264 files and exactly eight changed/added files against round eleven. [Delta manifest](run/changes-vs-round11.json), [current fingerprint](run/current-sources.sha256), [round-eleven fingerprint](run/round11-sources.sha256), [original measurement fingerprint](run/original-sources.sha256). The original measurement tree contains five benchmark-only additions while its 427 original tracked H264 files remain unchanged; [prior baseline audit](run/round11-baseline-audit.json) records that distinction. The round-eleven binary's prior formatting-only rebuild equivalence remains in [its audit](run/round11-whitespace-cleanup.json).

The [cumulative patch](encoder-change.patch) replays on the pristine original Git tree to exactly reproduce all 573 measured H264 files. Post-validation merge verifies the main H264 tree equals the measured snapshot and leaves the Git index untouched. [Merge record](round12-main-merged.json), [archive manifest](archive-manifest.json), [artifact hashes](archive-files.sha256). Binaries, encoded streams and duplicate source snapshots are omitted from the repository archive, with [payload hashes](omitted-payloads.json). The [runner](measure_round12.py), [archiver](save_round12.py), [report helper](write_round12_report.py) and raw logs preserve the procedure. Restore archived harness `.go.txt`/`.cpp.txt` files to their original names before rebuilding overlays; local fixture/native paths require adaptation on another machine.
'''
    (OUT / 'README.md').write_text(report)
    parent = parent.replace('thirty-three optimization groups', 'thirty-four optimization groups')
    parent = parent.replace('(round11/README.md) contains current', '(round12/README.md) contains current')
    parent = parent.replace(before_sentence, 'The full-path comparison includes original commit `641af9e6216055b6a9d918bdcba4ea7d40020750`, frozen round eleven and current source. The codec comparison uses round eleven/current/native to isolate the directional-predictor change; distinguish incremental and cumulative gains.')
    needle = '| [Round eleven](round11/README.md) | Smaller four-candidate SAD arguments; SSE2 macroblock motion-vector fill. |'
    assert needle in parent
    parent = parent.replace(needle, needle + '\n| [Round twelve](round12/README.md) | SSE2 horizontal-down, horizontal-up and vertical-right I4 predictors. |')
    (ROOT / 'README.md').write_text(parent)
    audit = {'codec_wall': {x['name']: cp[x['name']]['p'] for x in codec},
             'full_incremental_wall': {x['name']: fi[x['name']]['p'] for x in full},
             'full_cumulative_wall': {x['name']: fc[x['name']]['p'] for x in full},
             'geomean_percent': {'codec_wall_vs_round11': geomean(codec, 'round11'),
                 'codec_cpu_vs_round11': geomean(codec, 'round11', 'cpu_ns/frame'),
                 'full_wall_vs_round11': geomean(full, 'round11'), 'full_wall_vs_original': geomean(full, 'original')},
             'existing_scratch_archive_unchanged': tree(scratch) == archive['existing_scratch_archive']}
    assert audit['existing_scratch_archive_unchanged']
    (OUT / 'report-audit.json').write_text(json.dumps(audit, indent=2) + '\n')
    inventory = tree(OUT)
    inventory.pop('archive-files.sha256', None)
    (OUT / 'archive-files.sha256').write_text(''.join(f'{digest}  {name}\n' for name, digest in sorted(inventory.items())))
    print(OUT / 'README.md')


if __name__ == '__main__':
    main()
