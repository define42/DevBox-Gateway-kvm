#!/usr/bin/env python3
"""Prepared only: preserve completed diagnostic counts and byte-parity evidence.

Root may run after approval/results and the serialized CPU queue is free.
No builds, tests, encoding, profiling, measurements, source merge or selection.
"""
from pathlib import Path
import hashlib
import json
import os
import re
import traceback

ROOT = Path(__file__).resolve().parent
REPO = Path('/home/define42/git/DevBox-Gateway-kvm')
DOCS = REPO / 'docs/benchmarks/software-h264-parity-target-2026-10-22'
ROUND = DOCS / 'round13'
RUN = ROOT / 'initial-mvc-diagnostic-review'
OUT = ROUND / 'initial-mvc-diagnostic'
STAGE = OUT.with_name(OUT.name + '.incomplete')
OVERLAY = ROOT / 'initial-mvc-diagnostic-overlay'
BASE = ROOT / 'round11'
DRIVER = ROOT / 'measure_initial_mvc_diagnostic.py'
REVIEW = ROOT / 'remaining-motion-after-quarter-plan-review.txt'
README = ROOT / 'initial-mvc-diagnostic-README.txt'
DRIVER_SHA = '87e7c1a04ad131ec90c607b1d527042d2b059bce0792f080317992c14bd8b103'
MANIFEST_SHA = '894e90faf56ef0821da8c9749a8642d469ae65589f53e6b25ae7504fcc2443d7'
BASE_DIGEST = 'c60f1bb69991e68584e2b9512b42acf4a3454c51ec68389da0844825cf95529f'
EXPECTED = {
    'road1': ('fec3891d0f30674fd132b1cffade2b79b2ff6e9855574c5c233dcd2703e11d31', 10025547, 3770737, 14507502),
    'pdf1': ('ee3b0368c76e429a2be6a280c61038e0c50c76170c2c845f0fb2e80298eef9a3', 1073336, 490448, 1462625),
}
COMPONENTS = ['quarter-plan-review', 'quarter-plan-tuned-review', 'cavlc-emitter-tuned-review']
FILES = []


def need(ok, message):
    if not ok:
        raise RuntimeError(message)


def sha(path):
    with path.open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def load(path):
    return json.loads(path.read_text())


def tree(path):
    return {str(p.relative_to(path)): sha(p) for p in sorted(path.rglob('*')) if p.is_file()}


def sources(path):
    result = {prefix + '/' + relative: digest for prefix in ['pkg', 'internal'] for relative, digest in tree(path / prefix).items()}
    result.update({name: sha(path / name) for name in ['go.mod', 'go.sum']})
    return result


def map_digest(mapping):
    return hashlib.sha256(json.dumps(mapping, sort_keys=True, separators=(',', ':')).encode()).hexdigest()


def read_hashes(path):
    result = {}
    for line in path.read_text().splitlines():
        match = re.fullmatch(r'([0-9a-f]{64})  (.+)', line)
        need(match is not None, f'Invalid fingerprint: {path}')
        digest, name = match.groups()
        need(name not in result and not Path(name).is_absolute() and '..' not in Path(name).parts, 'Invalid fingerprint path')
        result[name] = digest
    return result


def put_bytes(relative, data, original=None):
    relative = Path(relative)
    need(not relative.is_absolute() and '..' not in relative.parts, 'Unsafe destination')
    target = STAGE / relative
    need(not target.exists(), f'Duplicate archive artifact: {relative}')
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_bytes(data)
    digest = hashlib.sha256(data).hexdigest()
    need(sha(target) == digest, 'Archive copy mismatch')
    FILES.append({'archive': str(relative), 'source': original, 'bytes': len(data), 'sha256': digest})


def put_file(path, relative):
    need(path.is_file() and not path.is_symlink(), f'Expected regular input: {path}')
    put_bytes(relative, path.read_bytes(), str(path))


def put_json(relative, value):
    put_bytes(relative, (json.dumps(value, indent=2) + '\n').encode())


def atomic(path, text):
    temporary = path.with_name(path.name + '.diagnostic-tmp')
    need(not temporary.exists(), f'Unexpected temporary artifact: {temporary}')
    temporary.write_text(text)
    os.replace(temporary, path)


def nonnegative(value):
    return isinstance(value, int) and not isinstance(value, bool) and value >= 0


def audit_record(record, case, result):
    expected_hash, calls, eligible, extra = EXPECTED[case]
    need(record['kind'] == 'initial_unique_mvc' and record['case'] == case and record['final'] is True and
         record['frames_including_warmup'] == 616, f'Wrong lifecycle/scope: {case}')
    c = record['counts']
    exclusions = c['short_circuit_exclusions']
    need(all(nonnegative(v) for v in exclusions.values()), 'Invalid exclusion count')
    need(c['initial_calls'] == calls and c['eligible_16x16_five_mvc'] == eligible, 'Expected activity changed')
    histogram, preflight = c['unique_clipped_mv_histogram'], c['preflight_unique_histogram']
    need(len(histogram) == len(preflight) == 7 and histogram[0] == preflight[0] == 0 and
         all(nonnegative(v) for v in histogram + preflight), 'Invalid histograms')
    need(sum(histogram) == eligible - exclusions['short_list'], 'Unique histogram total mismatch')
    need(c['unique_clipped_mv_total'] == sum(i * count for i, count in enumerate(histogram)), 'Unique weighted total mismatch')
    need(c['duplicate_clipped_mv_total'] == 6 * sum(histogram) - c['unique_clipped_mv_total'], 'Duplicate total mismatch')
    need(c['initial_calls'] == eligible + exclusions['other_block'] + exclusions['other_mvc_count'], 'Initial exclusions mismatch')
    need(c['preflight_eligible'] + sum(value for key, value in exclusions.items() if key not in ['other_block', 'other_mvc_count']) == eligible, 'Preflight exclusions mismatch')
    need(sum(preflight) == c['preflight_eligible'] and all(a <= b for a, b in zip(preflight, histogram)), 'Preflight histogram mismatch')
    need(c['preflight_duplicate_total'] == 6 * sum(preflight) - sum(i * count for i, count in enumerate(preflight)), 'Preflight duplicate total mismatch')
    need(nonnegative(c['mvc_slots_equal_initial']) and c['mvc_slots_equal_initial'] <= 5 * sum(histogram), 'Initial-MV duplicate count invalid')
    old_sads = eligible + extra
    need(result['frames'] == 616 and result['byte_parity'] is True and result['baseline_sha256'] == result['diagnostic_sha256'] == expected_hash,
         'Saved parity claim mismatch')
    need(result['prior_five_mvc_sad_evaluations'] == old_sads and
         result['distinct_mv_evaluations'] == c['unique_clipped_mv_total'] and
         result['potential_repeated_mv_evaluations'] == old_sads - c['unique_clipped_mv_total'] >= 0, 'Prior/unique evaluation accounting mismatch')
    return c


def readme(summary):
    rows, hist = [], []
    for result in summary['results']:
        case, c = result['case'], result['diagnostic']['counts']
        eligible, unique = c['eligible_16x16_five_mvc'], c['unique_clipped_mv_total']
        rows.append(f"| {case} | {eligible:,} | {unique / eligible:.3f} | {result['prior_five_mvc_sad_evaluations']:,} | {unique:,} | {result['potential_repeated_mv_evaluations']:,} ({100 * result['potential_repeated_mv_evaluations'] / result['prior_five_mvc_sad_evaluations']:.2f}%) | {c['preflight_eligible']:,} |")
        hist.append(f"| {case} | " + ' | '.join(f'{value:,}' for value in c['unique_clipped_mv_histogram'][1:]) + ' |')
    return '''# Initial motion-candidate diagnostic

This is an isolated **counting diagnostic**, not a retained optimization or a performance measurement. Main remains validated round12 (34 groups, 573 H264 files). No candidate is selected by this archive.

A three-file overlay on frozen round11 observes the initial MVP plus five clipped motion-vector candidates for 16×16 blocks. It preserves the existing SAD calls and decision order. It records distinct vectors and conservative eligibility for a possible future built-in batch; it does not implement that batch. Native also evaluates these candidates separately, so this is not evidence of a missing native six-candidate kernel. [Source review](remaining-motion-review.txt), [overlay source](overlay/), [diagnostic instructions](diagnostic-README.txt).

## Parity and scope

Road and PDF each encoded 600 requested frames plus 16 warmup frames, at 1920×1080, QP20, one layer/thread/slice, pinned to CPU2. Each complete output stream's SHA256 matches the frozen uninstrumented round11 stream: **616 frames per case, 1,232 recorded frames total**. The archive verifies the complete stream bytes by hash and the recorded frame lifecycle; it does not run a new decoder or compare against native streams. The giant streams and test binary are omitted with exact size/hash records. [Parity/payload records](omitted-payloads.json), [completed summary](summary.json), [provenance](provenance.json).

Every timing line printed by this instrumented binary is unsuitable for performance comparisons. Logging, counting and stream capture change execution cost. No benchmark statistics, encoder speedup or native speed ratio is derived here. The full round13 codec/EncodeDamage/native timing gate was not invoked for this diagnostic.

## Counted work

| Fixture | Eligible searches | Mean unique vectors | Prior SAD evaluations | Unique vector evaluations | Potential repeated evaluations (share of prior calls) | Conservative preflight-eligible searches |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
''' + '\n'.join(rows) + '''

The prior SAD total includes the initial evaluation and only the extra calls surviving the original current-best-vector skip. Unique-vector totals deduplicate all six clipped positions in first-occurrence order. Their difference counts potentially repeated evaluation work for immutable built-in inputs; it is not time saved. The entry-snapshot histogram includes excluded/custom paths; only the separate preflight histogram describes conservative eligibility for a possible pure built-in route.

| Fixture | 1 unique | 2 unique | 3 unique | 4 unique | 5 unique | 6 unique |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
''' + '\n'.join(hist) + '''

All histogram, duplicate and exclusion identities are checked, including the preflight subset. Prior initial-search and five-MVC totals must match exactly. [Road counters](road1-counts.json), [PDF counters](pdf1-counts.json), [prior counter evidence](reference-counts/), [archive audit](archive-audit.json).

These counts may inform a separately authorized, correctness-tested and benchmarked implementation. The entire InitialPoint path was only 6.28% of Road and 1.28% of PDF CPU in the cited earlier profile; this subset's possible gain is smaller, and dispatch/deduplication/preflight costs may erase it. Those historical cumulative shares are ceilings, not measured savings from this diagnostic.

The archive preserves all three earlier held component archives exactly and leaves main and the Git index unchanged. [File hashes](archive-files.sha256) cover every final artifact except the inventory itself; [archive manifest](archive-manifest.json) identifies copied/generated evidence. Source is stored as `.go.txt` references and must not be merged.
'''


def main():
    need(not OUT.exists() and not STAGE.exists(), 'Fresh diagnostic destination/staging required')
    summary = load(RUN / 'summary.json')
    need(summary['status'] == 'complete' and summary['timings_are_performance_evidence'] is False and
         [result['case'] for result in summary['results']] == ['road1', 'pdf1'], 'Completed two-fixture diagnostic required')
    provenance, commands = load(RUN / 'provenance.json'), load(RUN / 'commands.json')
    need([command['name'] for command in commands] == ['go-version', 'overlay-format', 'build', 'road1', 'pdf1'] and
         all(command['exit_code'] == 0 for command in commands), 'Diagnostic commands incomplete/failed')
    need(provenance['baseline_source_map_sha256'] == BASE_DIGEST and provenance['overlay_manifest_sha256'] == MANIFEST_SHA, 'Wrong baseline/overlay')
    need(provenance['fixture'] == {'width': 1920, 'height': 1080, 'qp': 20, 'slices': 1, 'requested_frames': 600,
         'warmup_frames': 16, 'encoded_frames': 616, 'affinity': '2'}, 'Diagnostic fixture scope changed')
    need(provenance['environment']['GOMAXPROCS'] == '1', 'Serial diagnostic configuration required')
    for command in commands[-2:]:
        case = command['name']
        need(command['command'][:3] == ['taskset', '-c', '2'], 'Diagnostic affinity changed')
        actual = command['environment_override']
        need(all(actual.get(key) == value for key, value in {
            'H264_MVC_UNIQUE_DIAGNOSTIC': '1', 'COMPARE_RUN': '1', 'COMPARE_FRAMES': '600',
            'COMPARE_SLICES': '1', 'COMPARE_WIDTH': '1920', 'COMPARE_HEIGHT': '1080',
            'COMPARE_QP': '20', 'COMPARE_CASE': case,
            'COMPARE_STREAM': str(RUN / 'streams' / (case + '.264'))}.items()), 'Actual encode settings differ from recorded scope')
    need(sha(DRIVER) == sha(RUN / DRIVER.name) == DRIVER_SHA, 'Driver changed')
    need(sha(OVERLAY / 'manifest.json') == sha(RUN / 'overlay/manifest.json') == MANIFEST_SHA, 'Manifest changed')
    manifest = load(OVERLAY / 'manifest.json')
    input_hashes = dict(provenance['input_sha256'])
    input_hashes.update({str(README): sha(README), str(REVIEW): sha(REVIEW), str(DRIVER): DRIVER_SHA,
                        str(OVERLAY / 'manifest.json'): MANIFEST_SHA})
    for name, digest in manifest['files'].items():
        need(sha(OVERLAY / name) == sha(RUN / 'overlay' / name) == digest, 'Overlay bytes changed')
        input_hashes[str(OVERLAY / name)] = digest
    for path, digest in input_hashes.items():
        need(sha(Path(path)) == digest, f'Recorded input changed: {path}')
    baseline = sources(BASE)
    need(map_digest(baseline) == BASE_DIGEST and baseline == read_hashes(RUN / 'baseline-sources.sha256') == sources(RUN / 'source'), 'Baseline/private source changed')
    binary = RUN / 'diagnostic-avc.test'
    need(sha(binary) == provenance['diagnostic_binary_sha256'], 'Diagnostic binary changed')
    omitted = [{'source': str(binary), 'bytes': binary.stat().st_size, 'sha256': sha(binary), 'reason': 'instrumented test binary'}]
    for result in summary['results']:
        case = result['case']
        record = load(RUN / (case + '-counts.json'))
        need(record == result['diagnostic'], 'Summary differs from final counter record')
        text = (RUN / 'logs' / (case + '.txt')).read_text()
        raw = [json.loads(line.removeprefix('H264_MVC_UNIQUE ')) for line in text.splitlines() if line.startswith('H264_MVC_UNIQUE ')]
        need(raw == [record], 'Raw log does not contain exactly one matching final record')
        audit_record(record, case, result)
        prior = load(ROOT / f'call-count-{case}-totals.json')
        initial = [row for row in prior if row['kind'] == 'initial' and row['final']]
        need(len(initial) == 1 and initial[0]['frames_including_warmup'] == 616, 'Prior counter scope changed')
        count = initial[0]['counts']
        need((count['calls'], count['calls_by_mvc_count'][5], count['extra_sad_by_mvc_count'][5]) == EXPECTED[case][1:], 'Prior initial activity changed')
        for role, stream in [('diagnostic', RUN / 'streams' / (case + '.264')), ('uninstrumented-round11', ROOT / f'round11-{case}.264')]:
            need(sha(stream) == EXPECTED[case][0], 'Complete stream hash mismatch')
            omitted.append({'case': case, 'role': role, 'source': str(stream), 'bytes': stream.stat().st_size,
                            'sha256': EXPECTED[case][0], 'recorded_frames': 616, 'reason': 'full stream omitted; exact hash verified'})
    component_before = {name: tree(ROUND / name) for name in COMPONENTS}
    for name, mapping in component_before.items():
        need(read_hashes(ROUND / name / 'archive-files.sha256') == {k: v for k, v in mapping.items() if k != 'archive-files.sha256'}, 'Component inventory mismatch')
    round_before = tree(ROUND)
    need(read_hashes(ROUND / 'archive-files.sha256') == {k: v for k, v in round_before.items() if k != 'archive-files.sha256'}, 'Round13 inventory mismatch')
    index_before = (ROUND / 'README.md').read_text()
    anchor = 'The [index audit](held-index-audit.json)'
    need(index_before.count(anchor) == 1 and '(initial-mvc-diagnostic/README.md)' not in index_before, 'Unexpected diagnostic index context')
    parent_before = sha(DOCS / 'README.md')
    main_before, git_before = tree(REPO / 'pkg/h264'), sha(REPO / '.git/index')
    round12 = {name.removeprefix('pkg/h264/'): digest for name, digest in read_hashes(ROOT / 'round12-review/current-sources.sha256').items() if name.startswith('pkg/h264/')}
    need(main_before == round12 and len(main_before) == 573, 'Main must remain validated round12')
    run_metadata = {str(p.relative_to(RUN)): sha(p) for p in RUN.rglob('*') if p.is_file() and
                    p.relative_to(RUN).parts[0] not in ['source', 'streams'] and p.name != 'diagnostic-avc.test'}
    STAGE.mkdir(parents=True)
    for relative in sorted(run_metadata):
        source = RUN / relative
        target = relative + '.txt' if source.suffix == '.go' else relative
        put_file(source, target)
    put_file(README, 'diagnostic-README.txt')
    put_file(REVIEW, 'remaining-motion-review.txt')
    put_file(Path(__file__).resolve(), Path(__file__).name)
    for case in EXPECTED:
        put_file(ROOT / f'call-count-{case}-totals.json', f'reference-counts/{case}-totals.json')
    put_file(ROUND / 'README.md', 'prior-round13-index.md')
    put_file(ROUND / 'archive-files.sha256', 'prior-round13-inventory.sha256')
    put_json('omitted-payloads.json', omitted)
    put_json('input-verification.json', input_hashes)
    put_bytes('README.md', readme(summary).encode())
    section = '''## Diagnostic only: initial motion candidates

A separate [initial-MVC counting diagnostic](initial-mvc-diagnostic/README.md) records distinct clipped candidates on Road and PDF using an isolated round11 overlay. Its complete streams match the uninstrumented round11 streams across 616 recorded frames each. It changes no production source and is not a new optimization, native comparison or performance measurement. All instrumented timing lines are excluded from performance evidence. The verified counts may inform a separate proposal; the three component decisions above remain held.

'''
    updated_index = index_before.replace('is established by this index.', 'is established by these component experiments.').replace(anchor, section + anchor)
    # Recheck all measurement/source/stream inputs after copying, before publishing.
    for path, digest in input_hashes.items():
        need(sha(Path(path)) == digest, 'Recorded input changed during archival')
    need(sources(BASE) == sources(RUN / 'source') == baseline, 'Source changed during archival')
    need(sha(binary) == provenance['diagnostic_binary_sha256'], 'Binary changed during archival')
    for item in omitted:
        need(sha(Path(item['source'])) == item['sha256'], 'Omitted payload changed during archival')
    need(all(sha(RUN / relative) == digest for relative, digest in run_metadata.items()), 'Diagnostic metadata changed during archival')
    need(tree(ROUND) == round_before | {str(p.relative_to(ROUND)): sha(p) for p in STAGE.rglob('*') if p.is_file()}, 'Prior round13 evidence changed during archival')
    need(tree(REPO / 'pkg/h264') == main_before and sha(REPO / '.git/index') == git_before and sha(DOCS / 'README.md') == parent_before, 'Main/index/parent changed')
    put_json('archive-audit.json', {'status': 'diagnostic_only', 'complete': True, 'cases': 2, 'recorded_frames_per_case': 616,
             'complete_stream_hashes_equal': True, 'new_decoder_run': False, 'native_stream_comparison': False,
             'all_histogram_and_exclusion_identities_checked': True, 'all_recorded_inputs_rehashed': True,
             'main_h264_unchanged': True, 'git_index_unchanged': True, 'parent_report_unchanged': True,
             'latest_retained_milestone': 'round12', 'retained_groups': 34, 'timing_is_performance_evidence': False,
             'preserved_component_archives': component_before, 'prior_round13_files': round_before,
             'source_merge': False, 'candidate_selection': False})
    for item in FILES:
        need(sha(STAGE / item['archive']) == item['sha256'], 'Archived evidence hash mismatch')
    (STAGE / 'archive-manifest.json').write_text(json.dumps({'files': FILES,
        'note': 'Manifest and final local inventory excluded from records to avoid self-reference.'}, indent=2) + '\n')
    inventory = tree(STAGE)
    (STAGE / 'archive-files.sha256').write_text(''.join(f'{digest}  {name}\n' for name, digest in sorted(inventory.items())))
    STAGE.rename(OUT)
    atomic(ROUND / 'README.md', updated_index)
    for name, mapping in component_before.items():
        need(tree(ROUND / name) == mapping, 'Existing component archive changed')
    inventory = {name: digest for name, digest in tree(ROUND).items() if name != 'archive-files.sha256'}
    atomic(ROUND / 'archive-files.sha256', ''.join(f'{digest}  {name}\n' for name, digest in sorted(inventory.items())))
    print(OUT)


if __name__ == '__main__':
    try:
        main()
    except BaseException:
        if STAGE.exists():
            (STAGE / 'archive-failure.txt').write_text(traceback.format_exc())
        raise
