#!/usr/bin/env python3
"""Prepared only: archive a completed quarter-plan tune after root's decision.

No builds, tests, measurements, automatic selection, merge, or parent-report
updates. Run only after the serialized CPU queue is free. Main stays round12.
"""
from pathlib import Path
import argparse
import collections
import difflib
import gzip
import hashlib
import json
import math
import re
import statistics
import traceback

ROOT = Path(__file__).resolve().parent
REPO = Path('/home/define42/git/DevBox-Gateway-kvm')
RUN = ROOT / 'quarter-plan-tuned-review'
DOCS = REPO / 'docs/benchmarks/software-h264-parity-target-2026-10-22'
INITIAL = DOCS / 'round13/quarter-plan-review'
OUT = DOCS / 'round13/quarter-plan-tuned-review'
STAGE = OUT.with_name(OUT.name + '.incomplete')
BASE, AFTER = ROOT / 'round11', ROOT / 'quarter-plan-tuned-next'
HELD = ROOT / 'quarter-plan-next'
CODEGEN = ROOT / 'quarter-plan-tuned-codegen-review.json'
DRIVER_HASH = '4743e992caad8063cec30e3b61cfd6437c0cff6fc990a878d84bf7b2342b5bed'
MANIFEST_HASH = 'e3ea191260ed937b98395dae818531f81e68f39467a782e9de962cde5e5f829c'
EVIDENCE = ['summary.json', 'benchstat.txt', 'samples.json', 'commands.json',
            'provenance.json', 'changes-vs-round11.json',
            'harness/quarter-plan-tuned-next-README.txt']
RECORDS = []


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
    result = {prefix + '/' + name: digest for prefix in ['pkg', 'internal']
              for name, digest in tree(path / prefix).items()}
    result.update({name: sha(path / name) for name in ['go.mod', 'go.sum']})
    return result


def read_hashes(path):
    result = {}
    for line in path.read_text().splitlines():
        match = re.fullmatch(r'([0-9a-f]{64})  (.+)', line)
        need(match is not None, f'Invalid fingerprint: {path}')
        digest, name = match.groups()
        need(name not in result and not Path(name).is_absolute() and '..' not in Path(name).parts,
             f'Invalid/duplicate fingerprint path: {name}')
        result[name] = digest
    return result


def put_bytes(name, data, original=None, compress=False):
    relative = Path(name)
    need(not relative.is_absolute() and '..' not in relative.parts, 'Unsafe output path')
    if compress:
        relative = relative.with_name(relative.name + '.gz')
    target = STAGE / relative
    need(not target.exists(), f'Duplicate archive target: {relative}')
    target.parent.mkdir(parents=True, exist_ok=True)
    payload = gzip.compress(data, compresslevel=9, mtime=0) if compress else data
    target.write_bytes(payload)
    decoded = gzip.decompress(target.read_bytes()) if compress else target.read_bytes()
    need(decoded == data, f'Archive copy differs: {relative}')
    RECORDS.append({'archive': str(relative), 'source': original, 'source_bytes': len(data),
                    'source_sha256': hashlib.sha256(data).hexdigest(),
                    'stored_bytes': len(payload), 'stored_sha256': sha(target),
                    'encoding': 'gzip' if compress else 'identity'})


def put_file(path, relative, compress=False):
    need(path.is_file() and not path.is_symlink(), f'Expected regular input: {path}')
    put_bytes(relative, path.read_bytes(), str(path), compress)


def put_json(name, value):
    put_bytes(name, (json.dumps(value, indent=2) + '\n').encode())


def parse_raw(text):
    result = []
    for line in text.splitlines():
        fields = line.split()
        if not fields or not fields[0].startswith('Benchmark') or len(fields) < 8:
            continue
        need(fields[3:8:2] == ['ns/op', 'B/op', 'allocs/op'], 'Unexpected benchmark units')
        item = {'case': re.sub(r'-\d+$', '', fields[0]), 'iterations': int(fields[1]),
                'ns_per_op': float(fields[2]), 'bytes_per_op': float(fields[4]),
                'allocations_per_op': float(fields[6])}
        need(item['iterations'] > 0 and math.isfinite(item['ns_per_op']) and item['ns_per_op'] > 0,
             'Invalid timing sample')
        need(item['bytes_per_op'] == item['allocations_per_op'] == 0, 'Allocation regression')
        result.append(item)
    return result


def parse_stats(text):
    rows = []
    for line in text.split('geomean', 1)[0].splitlines():
        if not line.startswith(('MeRefineFracPixelCaller/', 'QuarterRefineLocalParams/', 'QuarterPlanFallbackCaller/')):
            continue
        probability = re.search(r'\(p=([0-9.]+) n=(\d+)\)', line)
        change = re.search(r'([+-][0-9.]+)% \(p=', line)
        need(probability and int(probability[2]) == 6, 'Unexpected statistical sample count')
        rows.append({'name': line.split()[0], 'significant_change_percent': float(change[1]) if change else None,
                     'p': float(probability[1])})
    need(len(rows) == 47, 'Expected all 47 timing rows')
    return rows


def audit_run(decision):
    summary, provenance = load(RUN / 'summary.json'), load(RUN / 'provenance.json')
    need(summary['status'] == 'complete' and summary['repetitions'] == 6 and
         summary['cases_per_run'] == 47 and summary['total_samples'] == 564 and
         summary['samples_per_binary'] == 282 and summary['all_samples_zero_allocations'], 'Incomplete component experiment')
    need(sha(RUN / 'harness/measure_quarter_plan_tuned.py') == DRIVER_HASH, 'Driver source changed')
    need(sha(RUN / 'harness/quarter-plan-tuned-next-README.txt') == MANIFEST_HASH, 'Candidate manifest changed')
    expected_groups = load(RUN / 'expected-cases.json')['groups']
    expected = set().union(*(set(names) for names in expected_groups.values()))
    need(len(expected) == 47 and sorted(map(len, expected_groups.values())) == [7, 12, 28], 'Case matrix changed')
    commands = load(RUN / 'commands.json')
    need(all(command['exit_code'] == 0 for command in commands), 'Failed command')
    by_name = {command['name']: command for command in commands}
    need(len(by_name) == len(commands), 'Duplicate commands')
    gates = ['after-full-encoder-tests', 'after-focused-race', 'after-cpu-all-off',
             'after-arm64-build', 'base-build', 'after-build', 'base-allocation-tests', 'after-allocation-tests']
    need(all(name in by_name for name in gates), 'Missing validation gate')
    need('-race' in by_name['after-focused-race']['command'], 'Missing race flag')
    need(by_name['after-cpu-all-off']['environment_override'].get('GODEBUG') == 'cpu.all=off', 'Missing fallback flag')
    need(by_name['after-arm64-build']['environment_override'].get('GOARCH') == 'arm64', 'Missing ARM64 build')
    samples, aggregate = [], {'base': [], 'after': []}
    names = []
    for pair in range(1, 7):
        order = ['base', 'after'] if pair % 2 else ['after', 'base']
        for position, kind in enumerate(order, 1):
            name = f'pair-{pair:02d}-{kind}'
            names.append(name)
            command = by_name[name]
            need(command['command'][:3] == ['taskset', '-c', '2'] and
                 '-test.benchtime=200ms' in command['command'], 'Timing settings changed')
            batch = parse_raw((RUN / command['log']).read_text())
            need(len(batch) == 47 and {row['case'] for row in batch} == expected, 'Raw sample matrix changed')
            aggregate[kind].extend(batch)
            samples.extend({'pair': pair, 'order_in_pair': position, 'kind': kind, **row} for row in batch)
    need([c['name'] for c in commands if c['name'].startswith('pair-')] == names, 'Alternating order changed')
    need(samples == load(RUN / 'samples.json'), 'Saved sample extraction differs from raw logs')
    for kind in aggregate:
        need(aggregate[kind] == parse_raw((RUN / (kind + '.txt')).read_text()), 'Aggregate differs from raw logs')
        need(sha(RUN / 'bin' / (kind + '.test')) == provenance['binary_sha256'][kind], 'Measured binary changed')
    rows = parse_stats((RUN / 'benchstat.txt').read_text())
    normalized = {'Benchmark' + re.sub(r'-\d+$', '', row['name']) for row in rows}
    need(normalized == expected, 'Statistics cases differ from timing matrix')
    counts = collections.Counter('inconclusive' if row['significant_change_percent'] is None else
                                 'significant_reductions' if row['significant_change_percent'] < 0 else
                                 'significant_regressions' for row in rows)
    counts = {key: counts[key] for key in ['significant_reductions', 'significant_regressions', 'inconclusive']}
    need(decision['counts'] == counts and decision['rows'] == rows, 'Root decision must include the complete recorded statistics')
    return summary, provenance, samples, rows, counts


def save_delta(label, before, after):
    old, new = sources(before), sources(after)
    changes, patch = [], []
    for relative in sorted(old.keys() | new.keys()):
        if old.get(relative) == new.get(relative):
            continue
        need(relative.startswith('pkg/h264/'), 'Unexpected non-H264 candidate delta')
        changes.append({'path': relative, 'before_sha256': old.get(relative), 'after_sha256': new.get(relative)})
        versions = []
        for kind, root, mapping in [('before', before, old), ('after', after, new)]:
            path = root / relative
            if relative in mapping:
                put_file(path, Path('source-delta') / label / kind / (relative + '.txt'))
                versions.append(path.read_text().splitlines(keepends=True))
            else:
                versions.append([])
        need(all(not lines or lines[-1].endswith('\n') for lines in versions), 'Patch requires final newlines')
        patch.extend(difflib.unified_diff(*versions, fromfile='a/' + relative if relative in old else '/dev/null',
                                         tofile='b/' + relative if relative in new else '/dev/null'))
    put_json(Path('source-delta') / label / 'changes.json', changes)
    put_bytes(Path('source-delta') / label / 'candidate.patch', ''.join(patch).encode())
    return changes


def report(decision, samples, rows, counts):
    selected = decision['status'] == 'selected_for_full_validation'
    status = 'Selected for full validation' if selected else 'Held'
    outcome = ('Root selected this component candidate for a separate full validation run. This is not a merge decision.' if selected else
               'Root held this candidate. Main remains the validated round12 implementation.')
    groups = collections.defaultdict(collections.Counter)
    ratios = []
    table = []
    for row in rows:
        plain = re.sub(r'-\d+$', '', row['name'])
        parts = plain.split('/')
        group = ('Wide complete callers' if parts[1] not in ['4x4', '4x8'] else 'Narrow complete caller controls') if parts[0] == 'MeRefineFracPixelCaller' else (
                'Unchanged public scorer controls' if parts[0] == 'QuarterRefineLocalParams' else 'Wide fallback caller controls')
        value = row['significant_change_percent']
        groups[group]['inconclusive' if value is None else 'reductions' if value < 0 else 'regressions'] += 1
        values = {kind: [x['ns_per_op'] for x in samples if x['case'] == 'Benchmark' + plain and x['kind'] == kind] for kind in ['base', 'after']}
        ratio = statistics.median(values['after']) / statistics.median(values['base'])
        ratios.append(ratio)
        table.append(f"| `{row['name']}` | {(ratio-1)*100:+.2f}% | {'inconclusive' if value is None else f'{value:+.2f}%'} | {row['p']:.3f} |")
    put_bytes('all-47-cases.md', ('# All component timing cases\n\n| Case | Raw median change | Significant change | p |\n| --- | ---: | ---: | ---: |\n' + '\n'.join(table) + '\n').encode())
    group_rows = '\n'.join(f'| {name} | {groups[name]["reductions"]} | {groups[name]["regressions"]} | {groups[name]["inconclusive"]} |' for name in
                          ['Wide complete callers', 'Narrow complete caller controls', 'Unchanged public scorer controls', 'Wide fallback caller controls'])
    geo = 100 * (math.exp(sum(math.log(x) for x in ratios) / len(ratios)) - 1)
    return f'''# Quarter-refinement fallback tune: {status.lower()}

{outcome} This component experiment makes **no codec/full-frame, native-performance, stream-parity or live-RDP claim**. Main is not modified by this archive; the latest retained milestone at archival time is round12 (34 groups).

Root's explicit rationale: {decision['rationale']}

## Complete component result

The baseline is frozen **round11**, matching the initial held experiment. It is not round12 and the held initial candidate is not the timing baseline. The unchanged 47-case matrix covers 28 complete refinement callers (20 wide and eight narrow), seven public scorer controls and twelve wide scalar/nonbatch/validated-off caller controls. Six alternating pairs on CPU2, GOMAXPROCS=4 and 200ms per leaf produced 282 samples per binary / 564 total. Every sample reports zero bytes and allocations per operation.

| Case group | Significant reductions | Significant regressions | Inconclusive |
| --- | ---: | ---: | ---: |
{group_rows}
| Total | {counts['significant_reductions']} | {counts['significant_regressions']} | {counts['inconclusive']} |

The unweighted raw-median geomean is {geo:+.2f}%; it mixes paths and is not a frame-level gain. Inconclusive results do not prove neutrality. P-values are the recorded benchstat results without multiplicity correction. All wins and controls remain visible in [statistics](benchstat.txt), [all 47 cases](all-47-cases.md), [raw samples](samples.json), [base samples](base.txt), [candidate samples](after.txt), and [root decision](root-decision.json). The archive does not select candidates from p-values automatically.

## Change and validation scope

This one bounded tune checks width/current capabilities before entering the compact helper, and restores inline legacy descriptor construction for fallback. The original center callback and ordered MVD reads remain shared. The former legacy helper moved byte-for-byte into a test-only file. The existing four-candidate SATD assembly is unchanged. See [source review](harness/quarter-plan-tuned-next-README.txt), [full candidate delta](source-delta/vs-round11/changes.json), and [tune-only delta](source-delta/vs-held/changes.json).

Full encoder tests, focused race tests, full CPU-feature-disabled encoder tests, ARM64 cross-compilation and both allocation-test commands passed. Tests are component-scoped; there is no new full H264/AVC, native stream or decoder gate in this result. [Commands](commands.json), [provenance](provenance.json), [archive audit](archive-audit.json). The [saved codegen review](codegen-review.json) records a 768-byte parent frame versus 736 bytes in round11 and 416 bytes in the held initial version. The successful compact branch bypasses descriptor zeroing, and fallback calls the public scorer directly. This is evidence of generated call/frame structure, not a measurement of individual overhead costs.

The [initial held experiment](../quarter-plan-review/README.md) remains unchanged, including its narrow/validated-off regressions and unchanged public-control regression. Its source and selected evidence are guarded by this experiment, and its complete archived inventory is checked again before/after archival. No initial samples are combined with this follow-up. The [initial archive record](initial-archive-preservation.json) preserves those hashes.

## Archive and decision boundary

{'A separate root-authored round13 selection and the full validation/comparison driver are still required before any retention or merge decision.' if selected else 'No full validation or merge is authorized by this held result. The prepared round13 full driver remains independent and unused for this candidate unless root makes a new explicit decision.'}

This helper writes only this fresh component archive. It never changes main, the Git index, parent milestone reports, the initial quarter archive, or existing round12 evidence. Binaries and duplicate full source trees are omitted; their hashes remain recorded. Exact changed source bodies have `.txt` suffixes. Compressed logs, if any, retain stored and decoded hashes in [archive-manifest.json](archive-manifest.json); [archive-files.sha256](archive-files.sha256) covers every final file except itself. This report is frozen at the component decision and does not describe later outcomes.
'''


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--decision', type=Path, default=RUN / 'decision.json')
    args = parser.parse_args()
    need(not OUT.exists() and not STAGE.exists(), 'Fresh destination and staging required')
    decision_path = args.decision.resolve()
    decision = load(decision_path)
    need(decision['schema_version'] == 1 and decision['status'] in ['held', 'selected_for_full_validation'], 'Explicit root component decision required')
    need(isinstance(decision['rationale'], str) and decision['rationale'].strip(), 'Root rationale required')
    need(decision['candidate_merged'] is False and decision['full_frame_or_native_timing_performed'] is False,
         'This helper archives a pre-merge component decision only')
    need(decision['latest_main_milestone'] == 'round12', 'Expected unchanged main milestone round12')
    expected_evidence = {relative: sha(RUN / relative) for relative in EVIDENCE}
    need(decision['evidence_sha256'] == expected_evidence, 'Root decision must identify exact completed evidence')
    decision_hash = sha(decision_path)
    codegen_hash = sha(CODEGEN)
    codegen = load(CODEGEN)
    need(codegen['performance_claim'] is False, 'Codegen note must not claim timing')
    for name, digest in codegen['assembly_sha256'].items():
        need(sha(Path(name)) == digest, 'Saved assembly note provenance mismatch')
    summary, provenance, samples, rows, counts = audit_run(decision)
    run_before = tree(RUN)
    initial_before = tree(INITIAL)
    initial_inventory = read_hashes(INITIAL / 'archive-files.sha256')
    need(initial_inventory == {name: digest for name, digest in initial_before.items() if name != 'archive-files.sha256'}, 'Initial archive inventory mismatch')
    main_before, index_before = tree(REPO / 'pkg/h264'), sha(REPO / '.git/index')
    round12 = {name.removeprefix('pkg/h264/'): digest for name, digest in
               read_hashes(ROOT / 'round12-review/current-sources.sha256').items() if name.startswith('pkg/h264/')}
    need(main_before == round12 and len(main_before) == 573, 'Main is not the validated round12 source')
    source_records = {}
    for kind, source in [('base', BASE), ('after', AFTER)]:
        frozen, measured = sources(source), sources(RUN / 'work' / kind)
        need(frozen == read_hashes(RUN / f'{kind}-frozen-sources.sha256'), 'Frozen source drift')
        need(measured == read_hashes(RUN / f'{kind}-measured-sources.sha256'), 'Measured source drift')
        source_records[kind] = {'frozen': frozen, 'measured': measured}
    STAGE.mkdir(parents=True)
    omitted = []
    for path in sorted(RUN.rglob('*')):
        if not path.is_file():
            continue
        relative = path.relative_to(RUN)
        if relative.parts[0] == 'work':
            continue
        if relative.parts[0] == 'bin':
            omitted.append({'source': str(path), 'bytes': path.stat().st_size, 'sha256': sha(path)})
            continue
        target = relative.with_name(relative.name + '.txt') if relative.suffix in ['.go', '.s', '.h', '.cpp', '.c'] else relative
        compress = relative.parts[0] == 'validation' and path.stat().st_size > 1_000_000
        put_file(path, target, compress)
    put_file(decision_path, 'root-decision.json')
    put_file(CODEGEN, 'codegen-review.json')
    changes = save_delta('vs-round11', BASE, AFTER)
    need(changes == load(RUN / 'changes-vs-round11.json') and len(changes) == 8, 'Candidate delta differs from measured source')
    tune_changes = save_delta('vs-held', HELD, AFTER)
    need({x['path'].rsplit('/', 1)[-1] for x in tune_changes} == {'md.go', 'quarter_refine_plan.go', 'quarter_refine_plan_test.go'}, 'Unexpected tune delta')
    put_file(Path(__file__).resolve(), Path(__file__).name)
    put_json('initial-archive-preservation.json', {'path': str(INITIAL), 'files': initial_before})
    put_json('omitted-binaries.json', omitted)
    put_bytes('README.md', report(decision, samples, rows, counts).encode())
    need(tree(RUN) == run_before and sha(decision_path) == decision_hash and sha(CODEGEN) == codegen_hash, 'Completed input changed during archival')
    need(tree(INITIAL) == initial_before, 'Initial quarter archive changed during archival')
    need(tree(REPO / 'pkg/h264') == main_before and sha(REPO / '.git/index') == index_before, 'Main or Git index changed during archival')
    for kind, source in [('base', BASE), ('after', AFTER)]:
        need(sources(source) == source_records[kind]['frozen'] and sources(RUN / 'work' / kind) == source_records[kind]['measured'], 'Source changed during archival')
    put_json('archive-audit.json', {'status': decision['status'], 'root_decision_sha256': decision_hash,
             'counts': counts, 'cases': 47, 'samples': 564, 'all_samples_zero_allocations': True,
             'raw_samples_and_statistics_checked': True, 'initial_archive_unchanged': True,
             'main_h264_unchanged': True, 'git_index_unchanged': True, 'latest_main_milestone': 'round12',
             'native_codec_frame_stream_validation_performed': False, 'production_merge': False,
             'source_delta_verified': True, 'patch_replayed': False, 'omitted_binaries': omitted})
    for record in RECORDS:
        need(sha(STAGE / record['archive']) == record['stored_sha256'], 'Stored archive hash changed')
    (STAGE / 'archive-manifest.json').write_text(json.dumps({'files': RECORDS,
        'note': 'Manifest and final inventory excluded from file records to avoid self-reference; source_sha256 is also decoded SHA256.'}, indent=2) + '\n')
    inventory = tree(STAGE)
    (STAGE / 'archive-files.sha256').write_text(''.join(f'{digest}  {name}\n' for name, digest in sorted(inventory.items())))
    STAGE.rename(OUT)
    print(OUT)


if __name__ == '__main__':
    try:
        main()
    except BaseException:
        if STAGE.exists():
            (STAGE / 'archive-failure.txt').write_text(traceback.format_exc())
        raise
