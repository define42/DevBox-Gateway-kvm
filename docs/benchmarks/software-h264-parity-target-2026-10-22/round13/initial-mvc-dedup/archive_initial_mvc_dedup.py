#!/usr/bin/env python3
"""Prepared only: archive one completed MVC dedup component comparison.

Explicit root decision required. No rerun, automatic selection, build/test,
measurement, source merge, or retained-milestone update occurs here.
"""
from pathlib import Path
import argparse
import collections
import difflib
import hashlib
import json
import math
import os
import re
import statistics
import traceback

ROOT = Path(__file__).resolve().parent
REPO = Path('/home/define42/git/DevBox-Gateway-kvm')
ROUND = REPO / 'docs/benchmarks/software-h264-parity-target-2026-10-22/round13'
RUN = ROOT / 'initial-mvc-dedup-review'
OUT = ROUND / 'initial-mvc-dedup'
STAGE = OUT.with_name(OUT.name + '.incomplete')
BASE, AFTER = ROOT / 'round12-next', ROOT / 'initial-mvc-dedup-next'
CONFIG = ROOT / 'initial-mvc-dedup-measurement.json'
CONFIG_SHA = '529726b8b3b416d30a80a93caaadfef1f5f4e6956554c5a926ab971b999a83f4'
DRIVER_SHA = '629a2a270b36ef04c1d86249408f207a10ae8f6ad5c52431bbecd110c8643304'
FILES = []


def need(ok, message):
    if not ok:
        raise RuntimeError(message)


def sha(path):
    with path.open('rb') as handle:
        return hashlib.file_digest(handle, 'sha256').hexdigest()


def load(path):
    return json.loads(path.read_text())


def tree(path):
    return {str(p.relative_to(path)): sha(p) for p in sorted(path.rglob('*')) if p.is_file()}


def sources(path):
    result = {prefix + '/' + name: digest for prefix in ['pkg', 'internal'] for name, digest in tree(path / prefix).items()}
    result.update({name: sha(path / name) for name in ['go.mod', 'go.sum']})
    return result


def read_hashes(path):
    result = {}
    for line in path.read_text().splitlines():
        match = re.fullmatch(r'([0-9a-f]{64})  (.+)', line)
        need(match is not None, 'Invalid inventory/source fingerprint')
        value, name = match.groups()
        need(name not in result and not Path(name).is_absolute() and '..' not in Path(name).parts, 'Invalid fingerprint path')
        result[name] = value
    return result


def put_bytes(relative, data, original=None):
    relative = Path(relative)
    need(not relative.is_absolute() and '..' not in relative.parts, 'Unsafe output path')
    target = STAGE / relative
    need(not target.exists(), f'Duplicate output: {relative}')
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_bytes(data)
    value = hashlib.sha256(data).hexdigest()
    need(sha(target) == value, 'Copy mismatch')
    FILES.append({'archive': str(relative), 'source': original, 'bytes': len(data), 'sha256': value})


def put_file(path, relative):
    need(path.is_file() and not path.is_symlink(), f'Expected regular input: {path}')
    put_bytes(relative, path.read_bytes(), str(path))


def put_json(relative, value):
    put_bytes(relative, (json.dumps(value, indent=2) + '\n').encode())


def atomic(path, data):
    temporary = path.with_name(path.name + '.mvc-tmp')
    need(not temporary.exists(), 'Unexpected temporary artifact')
    temporary.write_text(data)
    os.replace(temporary, path)


def raw_samples(text):
    values = []
    for line in text.splitlines():
        fields = line.split()
        if len(fields) < 8 or not fields[0].startswith('Benchmark') or fields[3] != 'ns/op':
            continue
        need(fields[5] == 'B/op' and fields[7] == 'allocs/op', 'Unexpected sample units')
        sample = {'case': re.sub(r'-\d+$', '', fields[0]), 'iterations': int(fields[1]),
                  'ns_per_op': float(fields[2]), 'bytes_per_op': float(fields[4]), 'allocations_per_op': float(fields[6])}
        need(sample['iterations'] > 0 and math.isfinite(sample['ns_per_op']) and sample['ns_per_op'] > 0, 'Invalid sample')
        need(sample['bytes_per_op'] == sample['allocations_per_op'] == 0, 'Allocation regression')
        values.append(sample)
    return values


def stat_rows(text):
    rows = []
    for line in text.split('geomean', 1)[0].splitlines():
        if not line.startswith('InitialMVCDedup/'):
            continue
        p = re.search(r'\(p=([0-9.]+) n=(\d+)\)', line)
        change = re.search(r'([+-][0-9.]+)% \(p=', line)
        need(p and int(p[2]) == 6, 'Unexpected benchstat sample count')
        rows.append({'name': line.split()[0], 'significant_change_percent': float(change[1]) if change else None, 'p': float(p[1])})
    need(len(rows) == 34, 'Expected all 34 timing rows')
    return rows


def audit(config, decision):
    summary, provenance, commands = load(RUN / 'summary.json'), load(RUN / 'provenance.json'), load(RUN / 'commands.json')
    need(summary['status'] == 'complete' and summary['cases_per_run'] == 34 and summary['samples_per_binary'] == 204 and
         summary['total_samples'] == 408 and summary['zero_allocations'] and summary['source_binary_input_main_index_final_hashes_match'], 'Component run incomplete')
    expected = set(config['expected_cases'])
    need(len(expected) == 34 and config['repetitions'] == 6 and config['benchtime'] == '200ms', 'Declared scope changed')
    need(all(c['exit_code'] == 0 for c in commands), 'Command failure')
    by_name = {c['label']: c for c in commands}
    need(len(by_name) == len(commands), 'Duplicate command label')
    for c in commands:
        need(sha(RUN / c['log']) == c['log_sha256'], 'Command log changed')
    for label in ['after-full-encoder-tests', 'after-focused-race', 'after-cpu-all-off', 'after-arm64-build', 'base-build', 'after-build']:
        need(label in by_name, 'Missing validation gate')
    race = by_name['after-focused-race']
    need('-race' in race['args'] and '-json' in race['args'], 'Race gate changed')
    events = [json.loads(line) for line in (RUN / race['log']).read_text().splitlines() if line.startswith('{')]
    passed = {e['Test'] for e in events if e.get('Action') == 'pass' and 'Test' in e}
    need(set(config['required_race_tests']) <= passed and config['required_race_tests'], 'Focused tests did not execute/pass')
    need(by_name['after-cpu-all-off']['extra_environment'].get('GODEBUG') == 'cpu.all=off', 'Missing scalar fallback gate')
    need(by_name['after-arm64-build']['extra_environment'].get('GOARCH') == 'arm64', 'Missing ARM64 gate')
    parsed, iterations, order, aggregate = [], [], [], {'base': [], 'after': []}
    for pair in range(1, 7):
        for position, kind in enumerate(['base', 'after'] if pair % 2 else ['after', 'base'], 1):
            label = f'pair-{pair:02d}-{kind}'
            order.append(label)
            command = by_name[label]
            need(command['args'][:3] == ['taskset', '-c', '2'] and '-test.benchtime=200ms' in command['args'], 'Timing scope changed')
            batch = raw_samples((RUN / command['log']).read_text())
            need(len(batch) == 34 and {row['case'] for row in batch} == expected, 'Raw case matrix mismatch')
            aggregate[kind].extend(batch)
            for item in batch:
                record = {'pair': pair, 'position': position, 'kind': kind, **item}
                iterations.append(record)
                parsed.append({key: value for key, value in record.items() if key != 'iterations'})
    need([c['label'] for c in commands if c['label'].startswith('pair-')] == order, 'Pair rotation changed')
    need(parsed == load(RUN / 'samples.json'), 'Samples differ from raw logs')
    for kind in aggregate:
        need(aggregate[kind] == raw_samples((RUN / (kind + '.txt')).read_text()), 'Aggregate differs from raw logs')
        need(sha(RUN / 'bin' / (kind + '.test')) == provenance['binary_sha256'][kind], 'Measured binary changed')
    rows = stat_rows((RUN / 'benchstat.txt').read_text())
    need({'Benchmark' + re.sub(r'-\d+$', '', row['name']) for row in rows} == expected, 'Statistics matrix mismatch')
    counts = collections.Counter('inconclusive' if r['significant_change_percent'] is None else
                                 'significant_reductions' if r['significant_change_percent'] < 0 else 'significant_regressions' for r in rows)
    counts = {name: counts[name] for name in ['significant_reductions', 'significant_regressions', 'inconclusive']}
    declared_counts = {name: decision['counts'][old] for name, old in [
        ('significant_reductions', 'reduction'), ('significant_regressions', 'regression'), ('inconclusive', 'inconclusive')]}
    need(declared_counts == counts, 'Root decision changes measured counts')
    declared = {item['case']: item for item in decision['cases']}
    need(len(declared) == 34 and set(declared) == expected, 'Root decision omits measured cases')
    for row in rows:
        name = 'Benchmark' + re.sub(r'-\d+$', '', row['name'])
        item = declared[name]
        outcome = 'inconclusive' if row['significant_change_percent'] is None else 'reduction' if row['significant_change_percent'] < 0 else 'regression'
        need(item['outcome'] == outcome and item['p_value'] == row['p'] and item['significant_change_percent'] == row['significant_change_percent'], 'Decision/statistics mismatch')
        values = {kind: [sample['ns_per_op'] for sample in parsed if sample['case'] == name and sample['kind'] == kind] for kind in ['base', 'after']}
        before, after = statistics.median(values['base']), statistics.median(values['after'])
        need(item['base_median_ns'] == before and item['after_median_ns'] == after and
             math.isclose(item['median_change_percent'], (after / before - 1) * 100, rel_tol=1e-12, abs_tol=1e-12), 'Decision/raw medians mismatch')
    if decision['status'] == 'selected_for_full_validation':
        need(counts['significant_reductions'] > 0 and counts['significant_regressions'] == 0, 'Selection violates predeclared component rule')
    return summary, provenance, parsed, iterations, rows, counts


def report(decision, samples, rows, counts):
    selected = decision['status'] == 'selected_for_full_validation'
    heading = 'selected for full validation' if selected else 'held'
    outcome = ('Root selected this component candidate for a separate full gate. It is not merged and no frame gain is established.' if selected else
               'Root held this candidate. No full gate or production merge follows this component result.')
    groups = collections.defaultdict(collections.Counter)
    details, ratios = [], []
    for row in rows:
        plain = re.sub(r'-\d+$', '', row['name'])
        group = 'Fallback controls' if '/control/' in plain else 'All-unique controls' if '/unique6/' in plain else 'Duplicate target cases'
        change = row['significant_change_percent']
        groups[group]['inconclusive' if change is None else 'reductions' if change < 0 else 'regressions'] += 1
        values = {kind: [r['ns_per_op'] for r in samples if r['case'] == 'Benchmark' + plain and r['kind'] == kind] for kind in ['base', 'after']}
        ratio = statistics.median(values['after']) / statistics.median(values['base'])
        ratios.append(ratio)
        details.append(f"| `{row['name']}` | {(ratio-1)*100:+.2f}% | {'inconclusive' if change is None else f'{change:+.2f}%'} | {row['p']:.3f} |")
    put_bytes('all-34-cases.md', ('# All 34 component cases\n\n| Case | Raw median change | Significant change | p |\n| --- | ---: | ---: | ---: |\n' + '\n'.join(details) + '\n').encode())
    group_lines = '\n'.join(f'| {name} | {groups[name]["reductions"]} | {groups[name]["regressions"]} | {groups[name]["inconclusive"]} |' for name in ['Duplicate target cases', 'All-unique controls', 'Fallback controls'])
    return f'''# Five-MVC deduplication: {heading}

{outcome} Main remains validated round12: 34 retained groups and 573 H264 files. This archive adds no retained milestone and makes no codec/EncodeDamage/native-performance, new stream-parity or live-RDP claim.

Root's rationale: {decision['rationale']}

The pure Go candidate deduplicates previously evaluated clipped vectors only on the exact built-in SAD16×16 / five-MVC path. First occurrences retain checked SAD then MVD reads; custom callbacks and other cases retain their existing routes. There is no new assembly or changed search heuristic. Native also evaluates these initial candidates separately. The [prior counting diagnostic](../initial-mvc-diagnostic/README.md) motivated the experiment but its instrumented timings are not performance evidence.

## Complete component evidence

The baseline is frozen round12, not the earlier round11 diagnostic. The unchanged benchmark overlay runs the complete initial-search caller at strides32/2048: 18 duplicate target cases, four all-unique controls and twelve capability/scalar/custom/count/narrow controls. Six alternating CPU2 pairs, GOMAXPROCS=4, 200ms per leaf yield 204 samples per binary / 408 total. All report zero bytes and allocations per operation.

| Group | Significant reductions | Significant regressions | Inconclusive |
| --- | ---: | ---: | ---: |
{group_lines}
| Total | {counts['significant_reductions']} | {counts['significant_regressions']} | {counts['inconclusive']} |

The unweighted raw-median geomean is {100*(math.exp(sum(math.log(r) for r in ratios)/len(ratios))-1):+.2f}%; it is not workload-weighted or a frame speedup. Inconclusive does not prove neutrality. All recorded p-values are unadjusted for multiplicity. [Statistics](benchstat.txt), [all 34 cases](all-34-cases.md), [samples](samples.json), [raw iteration counts](raw-samples-with-iterations.json), [root decision](root-decision.json).

The predeclared rule rejects any significant target/control increase or an entirely inconclusive result. It permits no sample exclusions, pooling, retiming or further source tuning. Selection, if explicitly recorded above, requires a separate root-authored full-gate selection; the component result cannot authorize a merge.

Full encoder tests, actual required focused race-test pass events, CPU-all-off tests and ARM64 cross-compilation passed. The independent reference is the frozen complete round12 caller, renamed only. Tests cover duplicate losers/replaced winners, clipping/ties, malformed SAD/MVD ordering, mutable custom callbacks, directional behavior, aliases and allocation parity. [Commands](commands.json), [frozen configuration](harness/initial-mvc-dedup-measurement.json), [provenance](provenance.json), [source delta](source-delta/changes.json), [frozen input evidence](input-evidence.json).

The [saved codegen note](codegen-review.json) records a parent frame of 288 bytes before and 296 bytes after, plus a 288-byte helper frame. These are code-shape facts, not isolated runtime costs or an explanation of each timing result.

All prior round13 artifacts, including both quarter attempts, held CAVLC evidence and the diagnostic, remain unchanged. This helper copies source/evidence and updates only the round13 index/inventory. [Archive audit](archive-audit.json), [file hashes](archive-files.sha256). Binaries and duplicate full source trees are omitted with hashes; changed source and benchmark bodies are `.go.txt` references. This report is frozen at the component decision and does not describe later outcomes.
'''


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--decision', type=Path, default=RUN / 'decision.json')
    args = parser.parse_args()
    need(not OUT.exists() and not STAGE.exists(), 'Fresh archive/staging required')
    config = load(CONFIG)
    need(sha(CONFIG) == CONFIG_SHA and sha(RUN / 'harness' / CONFIG.name) == CONFIG_SHA, 'Frozen config changed')
    need(sha(ROOT / 'measure_initial_mvc_dedup.py') == sha(RUN / 'harness/measure_initial_mvc_dedup.py') == DRIVER_SHA, 'Frozen driver changed')
    decision_path = args.decision.resolve()
    decision = load(decision_path)
    need(decision['status'] in ['held', 'selected_for_full_validation'] and isinstance(decision['rationale'], str) and decision['rationale'].strip(), 'Explicit root decision required')
    need(decision['schema_version'] == 1 and decision['latest_main_milestone'] == 12 and
         decision['source_merged'] is False and decision['full_frame_gain_demonstrated'] is False and
         decision['native_speed_parity_achieved'] is False and
         decision['full_driver_selected'] == (decision['status'] == 'selected_for_full_validation'),
         'Component decision must preserve main and separate any future full gate')
    summary, provenance, samples, iterations, rows, counts = audit(config, decision)
    inputs = dict(provenance['input_sha256'])
    inputs[str(decision_path)] = sha(decision_path)
    for relative, value in decision['evidence_sha256'].items():
        need(not Path(relative).is_absolute() and '..' not in Path(relative).parts, 'Unsafe decision evidence path')
        inputs[str(ROOT / relative)] = value
    for path, value in inputs.items():
        need(sha(Path(path)) == value, 'Recorded input/evidence changed')
    snapshots = {}
    for kind, frozen in [('base', BASE), ('after', AFTER)]:
        original, measured = sources(frozen), sources(RUN / 'work' / kind)
        need(original == read_hashes(RUN / f'{kind}-frozen-sources.sha256') and
             measured == read_hashes(RUN / f'{kind}-measured-sources.sha256'), 'Source snapshot drift')
        snapshots[kind] = {'frozen': original, 'measured': measured}
    need({name: snapshots['after']['frozen'].get(name) for name in sorted(snapshots['base']['frozen'].keys() | snapshots['after']['frozen'].keys())
          if snapshots['base']['frozen'].get(name) != snapshots['after']['frozen'].get(name)} == config['changes'], 'Candidate delta changed')
    prior_round = tree(ROUND)
    need(read_hashes(ROUND / 'archive-files.sha256') == {k: v for k, v in prior_round.items() if k != 'archive-files.sha256'}, 'Existing round13 inventory mismatch')
    prior_index = (ROUND / 'README.md').read_text()
    marker = '<!-- initial-mvc-dedup-component -->'
    need(marker not in prior_index, 'Component index already exists')
    anchor = '## Diagnostic only: initial motion candidates'
    need(prior_index.count(anchor) == 1, 'Expected diagnostic index anchor')
    index_replacements = [
        ('All three component experiments below', 'All measured component experiments below'),
        ('the three component decisions above', 'all measured component decisions above'),
        ('All candidates passed their recorded component correctness gates.',
         'The three candidates in the table passed their recorded component correctness gates.'),
    ]
    updated_index = prior_index
    for old, new in index_replacements:
        need(updated_index.count(old) == 1, 'Expected existing index wording')
        updated_index = updated_index.replace(old, new)
    main_before, index_before, parent_before = tree(REPO / 'pkg/h264'), sha(REPO / '.git/index'), sha(ROUND.parent / 'README.md')
    expected_main = {name.removeprefix('pkg/h264/'): value for name, value in
                     read_hashes(ROOT / 'round12-review/current-sources.sha256').items() if name.startswith('pkg/h264/')}
    need(main_before == expected_main and len(main_before) == 573, 'Main must remain validated round12')
    codegen = load(RUN / 'codegen-review.json')
    for kind, value in codegen['assembly_sha256'].items():
        need(kind in ['base', 'after'] and sha(RUN / 'validation' / (kind + '-initial-search-assembly.txt')) == value, 'Saved codegen provenance mismatch')
    metadata = {str(p.relative_to(RUN)): sha(p) for p in RUN.rglob('*') if p.is_file() and p.relative_to(RUN).parts[0] not in ['work', 'bin']}
    STAGE.mkdir(parents=True)
    for relative in sorted(metadata):
        source = RUN / relative
        put_file(source, relative + '.txt' if source.suffix == '.go' else relative)
    put_file(decision_path, 'root-decision.json')
    evidence_records = []
    for number, (path, value) in enumerate(sorted(inputs.items())):
        source = Path(path)
        relative = Path('input-evidence') / (f'{number:02d}-' + source.name + ('.txt' if source.suffix == '.go' else ''))
        put_file(source, relative)
        evidence_records.append({'source': path, 'sha256': value, 'archive': str(relative)})
    put_json('input-evidence.json', evidence_records)
    put_file(Path(__file__).resolve(), Path(__file__).name)
    put_file(ROUND / 'README.md', 'prior-round13-index.md')
    put_file(ROUND / 'archive-files.sha256', 'prior-round13-inventory.sha256')
    omitted = [{'source': str(p), 'bytes': p.stat().st_size, 'sha256': sha(p)} for p in sorted((RUN / 'bin').iterdir()) if p.is_file()]
    put_json('omitted-binaries.json', omitted)
    put_json('raw-samples-with-iterations.json', iterations)
    changes, patch = [], []
    for relative, value in config['changes'].items():
        need(relative.startswith('pkg/h264/'), 'Unexpected non-H264 candidate change')
        old, new = BASE / relative, AFTER / relative
        old_lines = old.read_text().splitlines(keepends=True) if old.exists() else []
        new_lines = new.read_text().splitlines(keepends=True) if value is not None else []
        need(all(not lines or lines[-1].endswith('\n') for lines in [old_lines, new_lines]), 'Missing final newline')
        for name, path in [('before', old), ('after', new)]:
            if path.exists():
                put_file(path, Path('source-delta') / name / (relative + '.txt'))
        changes.append({'path': relative, 'before_sha256': snapshots['base']['frozen'].get(relative), 'after_sha256': value})
        patch.extend(difflib.unified_diff(old_lines, new_lines, fromfile='a/' + relative if old.exists() else '/dev/null',
                                         tofile='b/' + relative if value is not None else '/dev/null'))
    put_json('source-delta/changes.json', changes)
    put_bytes('source-delta/candidate.patch', ''.join(patch).encode())
    put_bytes('README.md', report(decision, samples, rows, counts).encode())
    status = 'selected for a separate full validation gate' if decision['status'] == 'selected_for_full_validation' else 'held'
    section = f'''{marker}
## Five-MVC deduplication component: {status}

The separate [pure Go deduplication experiment](initial-mvc-dedup/README.md) is **{status}** after its one predeclared 34-case component comparison. It has {counts['significant_reductions']} significant reductions, {counts['significant_regressions']} regressions and {counts['inconclusive']} inconclusive cases. Main remains round12; no source merge or frame/native-speed claim follows this component archive. The report preserves every target/control and root's explicit decision.

'''
    for path, value in inputs.items():
        need(sha(Path(path)) == value, 'Input/evidence changed during archival')
    for kind, frozen in [('base', BASE), ('after', AFTER)]:
        need(sources(frozen) == snapshots[kind]['frozen'] and sources(RUN / 'work' / kind) == snapshots[kind]['measured'], 'Source changed during archival')
    need(all(sha(RUN / name) == value for name, value in metadata.items()), 'Run metadata changed')
    need(all(sha(Path(item['source'])) == item['sha256'] for item in omitted), 'Binary changed during archival')
    need(all(sha(ROUND / name) == value for name, value in prior_round.items()), 'Existing round13 evidence changed')
    need(tree(REPO / 'pkg/h264') == main_before and sha(REPO / '.git/index') == index_before and sha(ROUND.parent / 'README.md') == parent_before, 'Main/index/parent changed')
    put_json('archive-audit.json', {'status': decision['status'], 'counts': counts, 'cases': 34, 'samples': 408,
             'raw_samples_and_statistics_verified': True, 'required_race_pass_events_verified': config['required_race_tests'],
             'source_input_binary_hashes_verified': True, 'main_h264_unchanged': True, 'git_index_unchanged': True,
             'parent_report_unchanged': True, 'latest_retained_milestone': 'round12', 'retained_groups': 34,
             'source_merge': False, 'full_frame_or_native_timing_performed': False, 'preserved_prior_round13_files': prior_round,
             'patch_generated_from_verified_source': True, 'patch_replayed': False})
    for item in FILES:
        need(sha(STAGE / item['archive']) == item['sha256'], 'Archive hash mismatch')
    (STAGE / 'archive-manifest.json').write_text(json.dumps({'files': FILES, 'note': 'Manifest/inventory excluded from records to avoid self-reference.'}, indent=2) + '\n')
    inventory = tree(STAGE)
    (STAGE / 'archive-files.sha256').write_text(''.join(f'{value}  {name}\n' for name, value in sorted(inventory.items())))
    STAGE.rename(OUT)
    atomic(ROUND / 'README.md', updated_index.replace(anchor, section + anchor))
    inventory = {name: value for name, value in tree(ROUND).items() if name != 'archive-files.sha256'}
    atomic(ROUND / 'archive-files.sha256', ''.join(f'{value}  {name}\n' for name, value in sorted(inventory.items())))
    print(OUT)


if __name__ == '__main__':
    try:
        main()
    except BaseException:
        if STAGE.exists():
            (STAGE / 'archive-failure.txt').write_text(traceback.format_exc())
        raise
