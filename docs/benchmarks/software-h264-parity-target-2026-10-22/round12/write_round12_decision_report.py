#!/usr/bin/env python3
"""Decision-aware round12 report; preserves the frozen first-pass report helper."""
from pathlib import Path
import json
import math
import write_round12_report as original

ROOT = original.ROOT
OUT = original.OUT
RUN = original.RUN


def load(path):
    return json.loads(path.read_text())


def main():
    decision = load(OUT / 'decision.json')
    assert decision['status'] in {'retained', 'held'} and decision['rationale'].strip()
    archive = load(OUT / 'archive-manifest.json')
    audit = load(OUT / 'decision-archive-audit.json')
    assert audit['first_pass_evidence_preserved'] and audit['initial_component_evidence_preserved'] and audit['scratch_archive_preserved']
    confirmation = load(OUT / 'confirmation/summary.json')
    complete = load(OUT / 'confirmation/complete.json')
    verified = load(OUT / 'confirmation/post-run-input-verification.json')
    assert complete['complete'] and verified['all_match'] and len(confirmation) == 5
    cp = original.wall_rows(OUT / 'confirmation/benchstat.txt', [x['name'] for x in confirmation])
    assert all(x['p'] == cp[x['name']]['p'] and '/1Slices-' in x['name'] for x in confirmation)
    codec = load(RUN / 'codec/summary.json')
    full = load(RUN / 'full/summary.json')
    assert {x['name'] for x in confirmation} == {x['name'] for x in codec if '/1Slices-' in x['name']}
    first_p = original.wall_rows(RUN / 'codec/benchstat.txt', [x['name'] for x in codec])
    fi = original.wall_rows(RUN / 'full/incremental-benchstat.txt', [x['name'] for x in full])
    fc = original.wall_rows(RUN / 'full/cumulative-benchstat.txt', [x['name'] for x in full])
    wins = sum(x['p'] <= .05 and x['wall_percent'] < 0 for x in confirmation)
    losses = sum(x['p'] <= .05 and x['wall_percent'] > 0 for x in confirmation)
    neutral = len(confirmation) - wins - losses
    geo = 100 * (math.exp(sum(math.log(x['current']['ns/op'] / x['round11']['ns/op']) for x in confirmation) / len(confirmation)) - 1)
    table = '\n'.join(f"| {original.label(x['name'])} | {x['round11']['ns/op']/1e6:.3f} | {x['current']['ns/op']/1e6:.3f} | {x['wall_percent']:+.2f}% | {x['cpu_percent']:+.2f}% | {x['p']:.3f} |" for x in confirmation)
    section = f'''## Selection and the predeclared longer confirmation

**Decision: {decision['status']}.** {decision['rationale']}

The original ten-case codec pass had a ShortClip 1080p/one-slice regression of +1.44% (p=0.026), a Road QP20 1080p/one-slice improvement of −1.52% (p=0.041), and eight inconclusive cases. All six incremental full `EncodeDamage` comparisons were inconclusive. Four-slice timing was noisy. These observations remain archived and are not replaced by the confirmation.

One bounded confirmation was declared before its results: **all five one-slice codec settings**, six alternating round-eleven/candidate pairs, 240 measured plus 16 warmup frames, CPU 2 and the same frozen binaries. There were no native reruns. It uses a separate longer run; its samples are not pooled with the first 80-frame pass.

| One-slice case | Round eleven ms | Candidate ms | Wall change | CPU change | p-value |
| --- | ---: | ---: | ---: | ---: | ---: |
{table}

The confirmation has {wins} significant wall-time reductions, {losses} significant regressions and {neutral} inconclusive cases. Its descriptive median wall-time geomean is {geo:+.2f}%; that aggregate has no separate significance test. [Predeclared scope](confirmation/provenance.json), [all raw statistics](confirmation/benchstat.txt), [metrics](confirmation/summary.json), [explicit decision](decision.json). Inconclusive results do not prove equal performance, and this confirmation does not establish a full-frame or live RDP gain.

The confirmation driver verified the binary hashes and preserved the first-pass artifacts before and after its run. A **separate post-run** check compared all five fixture cases ({verified['distinct_input_files']} distinct input files) against the validated first-pass hashes; all matched. No initial or mid-confirmation fixture hash check is claimed. [Post-run input verification](confirmation/post-run-input-verification.json), [archive preservation audit](decision-archive-audit.json).

'''
    report_path = OUT / 'README.md'
    if decision['status'] == 'retained':
        assert archive['retained_groups'] == 34 and archive['main_equals_measured_snapshot']
        if not report_path.exists():
            original.main()
        report = report_path.read_text()
        assert '## Selection and the predeclared longer confirmation' not in report
        needle = '## Complete codec comparison\n'
        assert needle in report
        report = report.replace(needle, section + needle, 1)
        report += '\nThe final decision-aware [archive helper](save_round12_decision.py) and [report helper](write_round12_decision_report.py) preserve the frozen first-pass helpers and add the confirmation without overwriting prior evidence.\n'
        report_path.write_text(report)
    else:
        assert archive['retained_groups'] == 33 and archive['main_equals_round11'] and not archive['candidate_merged']
        assert not report_path.exists(), 'Held report already exists; review before replacement'
        ct = '\n'.join('| ' + original.label(x['name']) + ' | ' + ' | '.join(f"{x['medians'][kind]['ns/op']/1e6:.3f}" for kind in ['round11', 'current', 'native']) +
                       f" | {x['current_vs_round11_wall_percent']:+.2f}% | {first_p[x['name']]['p']:.3f} | {x['current_vs_native_wall_ratio']:.3f}× | {x['current_vs_native_cpu_ratio']:.3f}× |" for x in codec)
        ft = '\n'.join('| ' + original.label(x['name']) + ' | ' + ' | '.join(f"{x['medians'][kind]['ns/op']/1e6:.3f}" for kind in ['original', 'round11', 'current']) +
                       f" | {x['current_vs_round11_wall_percent']:+.2f}% | {fi[x['name']]['p']:.3f} | {x['current_vs_original_wall_percent']:+.2f}% | {fc[x['name']]['p']:.3f} |" for x in full)
        report = f'''# Software H.264 review: round twelve candidate held

**The directional-predictor candidate is held. Main remains [round eleven](../round11/README.md), with thirty-three retained optimization groups and 566 H264 files.** No round-twelve production change was merged. Native speed parity remains unmet. All candidate results below refer to an isolated 573-file snapshot, not the current main encoder.

The proposed SSE2 horizontal-down, horizontal-up and vertical-right I4 predictors gather exact source bytes in scalar read order and preserve aliases, unusual strides, panic precedence and exact sixteen-byte stores. Native installs directional MMXEXT predictors, while Go uses scalar construction/copying for these modes. [Source/native evidence](intra4-directional-next-README.txt). Six dispatched predictor cases improved 36.04–49.73%; six unchanged scalar controls were inconclusive. Of 22 complete I4 caller cases, ten improved 3.41–7.01% and twelve were inconclusive, with no significant regressions and a descriptive caller geomean of −3.06%. These component gains did not by themselves justify a production merge. [Component audit](intra4-directional-review/README.md), [statistics](intra4-directional-review/benchstat.txt).

{section}## First-pass codec and native comparison

The baseline is frozen round eleven. Six rotating round-eleven/candidate/native triples use 16 warmup and 80 measured frames. One slice uses CPU 2; four slices use CPUs 0–3. The first pass is preserved in full. Linux amd64, Intel i7-7700HQ, Go 1.27.3, CGO disabled, GOMAXPROCS=4; other workstation activity was not disabled.

| Case | Round eleven ms | Candidate ms | Native ms | Change vs eleven | p-value | Candidate/native wall | Candidate/native CPU |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
{ct}

There are {original.counts(codec, first_p, 'current_vs_round11_wall_percent')}. The descriptive wall-time geomean is {original.geomean(codec, 'round11'):+.2f}%, strongly influenced by noisy four-slice medians; it does not establish a frame gain. [Three-way statistics](run/codec/benchstat.txt), [candidate/native comparison](run/codec/native-benchstat.txt), [wall/CPU metrics](run/codec/summary.json).

Native is the local OpenH264 2.6.0 executable built with GCC 13.3 `-O3` and assembly/AVX2. The actual executable, statically linked OpenH264 archive and source are fingerprinted. Matched settings use camera real-time, low complexity, fixed slices, Baseline/CAVLC, QP20 unless QP32 is labeled, one reference/spatial/temporal layer and a 60 FPS codec clock; optional tools remain disabled. [Exact provenance](run/provenance.json), [matching settings](../../software-h264-native-parity-2026-10-22/README.md#inputs-and-matching). Codec timing includes encoding and an owning payload; RGB conversion, loading, initialization, capture, framing, socket/ACK waits and presentation are excluded.

## First-pass full EncodeDamage comparison

Six rotating original/round-eleven/candidate triples use 16 warmup and 60 measured full-damage frames. Original means commit `641af9e6216055b6a9d918bdcba4ea7d40020750`. Timing includes RGB conversion, damage preparation, encoding and owning payload construction at FrameRate=30. Capture, socket writes, ACK waits and presentation are excluded.

| 1080p case | Original ms | Round eleven ms | Candidate ms | Change vs eleven | p-value | Change vs original | p-value |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
{ft}

Against round eleven there are {original.counts(full, fi, 'current_vs_round11_wall_percent')} (descriptive geomean {original.geomean(full, 'round11'):+.2f}%). Against the original there are {original.counts(full, fc, 'current_vs_original_wall_percent')} (descriptive geomean {original.geomean(full, 'original'):+.2f}%). The cumulative comparison includes thirty-three previously retained groups plus this isolated candidate; it cannot isolate the directional change. [Incremental statistics](run/full/incremental-benchstat.txt), [cumulative statistics](run/full/cumulative-benchstat.txt), [metrics](run/full/summary.json). These results do not measure live RDP “Encoding average”.

## Correctness and preserved evidence

The isolated candidate passed H264/AVC package tests, four-package race checks, AVX2-disabled and optional-features-disabled tests, four Linux ARM64 cross-builds, formatting and gopls on this round's changed Go files. All ten streams matched original Go and native byte for byte; FFmpeg decoded all 960 frames without errors and ffprobe counted 96 per case. [Validation commands](run/validation.json), [gopls scope](run/gopls-scope.json), [stream/decoder evidence](run/parity/parity.json). Cross-builds do not establish ARM64 runtime performance. Nonverbose package logs do not prove every optional external-fixture test ran. No new profile or call-count diagnostic was collected by this runner.

The initial CAVLC emitter also remains held after sparse complete-macroblock, short-chroma and extracted-zero regressions. Its native 447-suite correctness, race and CPU-off gates passed after an unused-import repair, but no native timing run was performed for that component. The initial failed build, correction, original/working source fingerprints and all six paired samples remain archived. [Held decision](cavlc-emitter-review/decision.txt), [statistics](cavlc-emitter-review/benchstat.txt), [source provenance](cavlc-emitter-review/provenance.json). Two verbose test logs are gzip-compressed with verified decompressed hashes in [compression metadata](compression.json). A separately tuned emitter is outside this selected experiment. The earlier [scratch-isolation archive](scratch-isolation-review/README.md) remains byte-identical.

The [eight-file candidate patch](candidate-vs-round11.patch) was replayed on frozen round eleven and exactly reproduces the 573-file measured H264 candidate. [Delta](run/changes-vs-round11.json), [candidate fingerprint](run/current-sources.sha256), [round-eleven fingerprint](run/round11-sources.sha256), [original measurement baseline audit](run/round11-baseline-audit.json). Main remains byte-identical to round eleven and the Git index is untouched. The cumulative patch for retained main remains in [round eleven](../round11/encoder-change.patch).

[Archive manifest](archive-manifest.json), [artifact hashes](archive-files.sha256), [omitted binary/stream hashes](omitted-payloads.json), [runner](measure_round12.py), [decision-aware archiver](save_round12_decision.py), [held-candidate archiver](save_round12_held.py), [report helper](write_round12_decision_report.py). Archived harness source has `.txt` suffixes; restore its original extension and adapt local fixture/native paths before rerunning. The provisional [selection manifest](round12-selection.json) predates the final [hold decision](decision.json).
'''
        parent = (ROOT / 'README.md').read_text()
        assert 'thirty-three optimization groups' in parent and '(round11/README.md) contains current' in parent
        notice = 'The [round twelve directional candidate](round12/README.md) remains held after mixed first-pass results and one predeclared longer confirmation. Main and its thirty-three retained groups remain at round eleven; candidate measurements are archived separately.\n\n'
        needle = 'Rejected or held experiments are recorded'
        assert needle in parent and notice not in parent
        report_path.write_text(report)
        (ROOT / 'README.md').write_text(parent.replace(needle, notice + needle, 1))
    report_audit = {'status': decision['status'], 'retained_groups': archive['retained_groups'],
                    'confirmation_wins': wins, 'confirmation_regressions': losses, 'confirmation_inconclusive': neutral,
                    'confirmation_wall_geomean_percent': geo, 'confirmation_cases': confirmation,
                    'first_pass_samples_preserved': True, 'confirmation_samples_not_pooled': True,
                    'input_verification_scope': verified['scope'],
                    'scratch_archive_unchanged': original.tree(OUT / 'scratch-isolation-review') == archive['existing_scratch_archive']}
    assert report_audit['scratch_archive_unchanged']
    (OUT / 'decision-report-audit.json').write_text(json.dumps(report_audit, indent=2) + '\n')
    inventory = original.tree(OUT)
    inventory.pop('archive-files.sha256', None)
    (OUT / 'archive-files.sha256').write_text(''.join(f'{digest}  {name}\n' for name, digest in sorted(inventory.items())))
    print(report_path)


if __name__ == '__main__':
    main()
