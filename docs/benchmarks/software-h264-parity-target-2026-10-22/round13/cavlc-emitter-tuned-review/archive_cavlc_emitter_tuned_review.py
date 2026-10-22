#!/usr/bin/env python3
"""Prepared source only. Root may run after the serialized timing queue is free.

Archives the completed held experiment; never builds, tests, reruns benchstat,
or changes any source/round12 evidence. Fresh round13 destination only.
"""
from pathlib import Path
from datetime import datetime, timezone
import collections
import difflib
import gzip
import hashlib
import json
import math
import re
import shutil
import statistics
import traceback

R = Path('/tmp/h264-parity-target-20261022')
REPO = Path('/home/define42/git/DevBox-Gateway-kvm')
SRC = R / 'cavlc-emitter-tuned-review'
PRIOR = R / 'cavlc-emitter-review'
DOCS = REPO / 'docs/benchmarks/software-h264-parity-target-2026-10-22'
DEST = DOCS / 'round13/cavlc-emitter-tuned-review'
STAGE = DEST.with_name(DEST.name + '.incomplete')
INITIAL_ARCHIVE = DOCS / 'round12/cavlc-emitter-review'
FROZEN_BASE = R / 'round11'
FROZEN_AFTER = R / 'cavlc-emitter-tuned-next'
BASE = R / 'cavlc-emitter-tuned-round11-base'
AFTER = R / 'cavlc-emitter-tuned-measure-next'
INITIAL_AFTER = R / 'cavlc-emitter-measure-next'
AUDIT = R / 'cavlc-emitter-tuned-independent-audit.json'
DESIGN = R / 'cavlc-mb-certificate-review.md'
NATIVE = R / 'cavlc-native-review/native_cavlc'
PACKAGE = Path('pkg/h264/internal/encoder')
REQUIRED_NATIVE = {'TestCavlcNativeFixtures', 'TestCavlcNativeFullParity',
                   'TestCavlcNativeFullParity/auto', 'TestCavlcNativeFullParity/scalar'}
EXPECTED = {
    'measure_cavlc_emitter_tuned.py': 'e1fdff4fbd2e072299a3f92b29c7bcd918170582f6346c5859c1e1c7ca4d8e79',
    'measure_cavlc_emitter.py': '3365c7e5fb97a108aa0c1bd699e650d0708ccb54b4f5e9cedcffc64fcdbd6c35',
    'cavlc-emitter-tuned-next-README.txt': '735c04bb2697d2280ef923850a972e777374d6ce430b0817c7d06ee54123f6bc',
}
EXTERNAL_EXPECTED = {
    AUDIT: '6c8099e03f880c7aa66c6b33f11a26e1080b71b75b9cf454e2482a77912604dd',
    DESIGN: 'cc8847fa0322524c138ed09e0bec32bc14d47fbe2cedba10295a33eaf3694cd9',
}
FILES = []
OMITTED = []


def need(condition, message):
    if not condition:
        raise RuntimeError(message)


def sha_bytes(data):
    return hashlib.sha256(data).hexdigest()


def sha(path):
    return sha_bytes(path.read_bytes())


def read_json(path):
    return json.loads(path.read_text())


def source_hashes(tree):
    suffixes = {'.go', '.s', '.h', '.c', '.cc', '.cpp', '.mod', '.sum'}
    return {str(p.relative_to(tree)): sha(p) for p in sorted(tree.rglob('*'))
            if p.is_file() and (p.suffix in suffixes or 'testdata' in p.parts)}


def put_bytes(rel, data, source=None, compress=False):
    rel = Path(rel)
    need(not rel.is_absolute() and '..' not in rel.parts, f'unsafe archive path {rel}')
    original_hash = sha_bytes(data)
    target = STAGE / rel
    if compress:
        target = target.with_name(target.name + '.gz')
    target.parent.mkdir(parents=True, exist_ok=True)
    need(not target.exists(), f'duplicate archive target {target}')
    if compress:
        with target.open('wb') as raw:
            with gzip.GzipFile(filename='', mode='wb', fileobj=raw, mtime=0, compresslevel=9) as out:
                out.write(data)
        need(gzip.decompress(target.read_bytes()) == data, f'gzip verification failed {target}')
    else:
        target.write_bytes(data)
    FILES.append({'archive_path': str(target.relative_to(STAGE)), 'source': source,
                  'source_bytes': len(data), 'source_sha256': original_hash,
                  'stored_bytes': target.stat().st_size, 'stored_sha256': sha(target),
                  'encoding': 'gzip' if compress else 'identity',
                  'decoded_sha256': original_hash})


def put_file(path, rel, compress=False):
    need(path.is_file() and not path.is_symlink(), f'not a regular source file: {path}')
    put_bytes(rel, path.read_bytes(), str(path), compress)


def put_json(rel, data):
    put_bytes(rel, (json.dumps(data, indent=2, sort_keys=True) + '\n').encode())


def check_passes(path, required):
    passed, skipped = set(), set()
    for line in path.open():
        if not ('"Action":"pass"' in line or '"Action":"skip"' in line):
            continue
        event = json.loads(line)
        if event.get('Action') == 'pass':
            passed.add(event.get('Test'))
        elif event.get('Action') == 'skip':
            skipped.add(event.get('Test'))
    need(required <= passed and not required & skipped, f'missing/skipped actual test pass: {path}')
    return sorted(required)


def parse_samples(text):
    result = []
    for line in text.splitlines():
        match = re.match(r'^(Benchmark\S+)\s+(\d+)\s+([0-9.]+)\s+ns/op\b', line)
        if match:
            result.append({'benchmark': match[1], 'iterations': int(match[2]), 'ns_per_op': float(match[3])})
    return result


def audit_measurement():
    completion = read_json(SRC / 'completion.json')
    need(completion.get('complete') and completion.get('pairs_completed') == 6
         and completion.get('matched_cases_per_pair') == 87, 'incomplete 87-case experiment')
    need(completion.get('native_timing_runs') == 0, 'unexpected native timing claim')
    decision = read_json(SRC / 'decision.json')
    need(decision['status'] == 'held' and not decision['candidate_merged']
         and not decision['full_frame_or_native_timing_performed'], 'decision changed')
    for name, digest in EXPECTED.items():
        need(sha(SRC / name) == digest, f'frozen input changed: {name}')
    for path, digest in EXTERNAL_EXPECTED.items():
        need(sha(path) == digest, f'external audit/design changed: {path}')
    provenance = read_json(SRC / 'provenance.json')
    need(sha(NATIVE) == provenance['native_binary_sha256'], 'native binary changed')
    for name, key, count in [('correctness-fixtures447.txt', 'correctness_sha256', 447),
                             ('timing-fixtures333.txt', 'timing_sha256', 333)]:
        path = SRC / name
        need(path.read_text().splitlines()[0] == f'cavlc-native-v1 {count}', f'fixture count {name}')
        need(sha(path) == provenance[key], f'fixture hash {name}')

    commands = [json.loads(line) for line in (SRC / 'commands.jsonl').read_text().splitlines()]
    need(len(commands) == 50 and all(item['returncode'] == 0 for item in commands), 'command failures/count changed')
    for item in commands:
        need(sha(SRC / Path(item['log']).name) == item['log_sha256'], f'command log changed {item["name"]}')
    by_command = {item['name']: item for item in commands}
    for name in ('after-full-encoder-tests', 'after-emitter-race', 'after-emitter-cpu-all-off',
                 'base-build', 'after-build', 'base-density-fixture-sanity', 'after-density-fixture-sanity'):
        need(name in by_command, f'missing validation gate {name}')
    need('-race' in by_command['after-emitter-race']['argv'], 'race gate missing')
    need(by_command['after-emitter-cpu-all-off']['environment'].get('GODEBUG') == 'cpu.all=off', 'fallback gate missing')
    for kind in ('base', 'after'):
        need(len(parse_samples((SRC / (kind + '-density-fixture-sanity.txt')).read_text())) == 20, 'density sanity coverage')
    native_passes = {
        'candidate': check_passes(SRC / 'after-full-encoder-tests.txt', REQUIRED_NATIVE | {
            'TestCavlcEmitterDensityThreshold', 'TestCavlcEmitterSignedDomain'}),
        'reused_baseline': check_passes(SRC / 'reused-baseline-evidence/base-full-encoder-tests.txt', REQUIRED_NATIVE),
    }
    reused_commands = [json.loads(line) for line in (SRC / 'reused-baseline-evidence/commands.jsonl').read_text().splitlines()]
    reused = [item for item in reused_commands if item['name'] == 'base-full-encoder-tests']
    need(reused and reused[-1]['returncode'] == 0, 'reused baseline command failed')
    need(sha(SRC / 'reused-baseline-evidence/base-full-encoder-tests.txt') == reused[-1]['log_sha256'], 'reused log hash changed')
    need(read_json(SRC / 'reused-baseline-evidence/completion.json')['complete'], 'initial experiment incomplete')

    rows = []
    for line in (SRC / 'benchstat.txt').read_text().split('geomean', 1)[0].splitlines():
        if not line.startswith('Cavlc'):
            continue
        pm = re.search(r'\(p=([0-9.]+) n=(\d+)\)', line)
        dm = re.search(r'([+-][0-9.]+)% \(p=', line)
        need(pm and int(pm[2]) == 6, 'unexpected benchstat row')
        rows.append({'name': line.split()[0], 'significant_change_percent': float(dm[1]) if dm else None,
                     'p': float(pm[1])})
    need(len(rows) == 87 and rows == decision['rows'], 'benchstat/decision mismatch')
    samples, groups = {}, collections.defaultdict(collections.Counter)
    expected_order = []
    for rep in range(1, 7):
        for kind in (('base', 'after') if rep % 2 else ('after', 'base')):
            for group in ('callers', 'public', 'density'):
                expected_order.append(f'pair{rep:02d}-{kind}-{group}')
    need([x['name'] for x in commands if x['name'].startswith('pair')] == expected_order, 'pair rotation changed')
    for kind in ('base', 'after'):
        merged = []
        for rep in range(1, 7):
            for group, count in [('callers', 52), ('public', 15), ('density', 20)]:
                name = f'pair{rep:02d}-{kind}-{group}'
                item = by_command[name]
                need(item['argv'][:3] == ['taskset', '-c', '2'], 'benchmark CPU changed')
                need('-test.benchtime=200ms' in item['argv'] and item['environment'].get('GOMAXPROCS') == '1', 'timing settings changed')
                need('CAVLC_NATIVE_BINARY' not in item['environment'], 'native timing unexpectedly enabled')
                need(item['environment'].get('CAVLC_NATIVE_FIXTURES') == str(SRC / 'timing-fixtures333.txt'), 'wrong timing fixture')
                batch = parse_samples((SRC / (name + '.txt')).read_text())
                need(len(batch) == count and len({x['benchmark'] for x in batch}) == count, 'raw sample coverage')
                merged.extend(batch)
        need(merged == parse_samples((SRC / (kind + '.txt')).read_text()), 'aggregate differs from raw logs')
        by_name = collections.defaultdict(list)
        for item in merged:
            name = re.sub(r'-\d+$', '', item['benchmark'].removeprefix('Benchmark'))
            by_name[name].append({'iterations': item['iterations'], 'ns_per_op': item['ns_per_op']})
        need(set(by_name) == {x['name'] for x in rows} and all(len(v) == 6 for v in by_name.values()), 'matched names/counts differ')
        samples[kind] = dict(by_name)
    for row in rows:
        change = row['significant_change_percent']
        label = 'inconclusive' if change is None else 'significant_reductions' if change < 0 else 'significant_regressions'
        groups[row['name'].split('/')[0]][label] += 1
    counts = {key: sum(g[key] for g in groups.values()) for key in ('significant_reductions', 'significant_regressions', 'inconclusive')}
    need(counts == decision['counts'] == read_json(AUDIT)['counts'], 'summary count mismatch')
    return rows, samples, dict(groups), native_passes


def verify_sources():
    specs = [
        ('frozen-base', FROZEN_BASE, SRC / 'frozen-round11-source-sha256.json'),
        ('frozen-candidate', FROZEN_AFTER, SRC / 'frozen-tuned-source-sha256.json'),
        ('measured-base', BASE, SRC / 'base-postformat-source-sha256.json'),
        ('measured-candidate', AFTER, SRC / 'after-postformat-source-sha256.json'),
        ('initial-measured-candidate', INITIAL_AFTER, PRIOR / 'after-postformat-source-sha256.json'),
        ('reused-initial-base', R / 'cavlc-emitter-round11-base', SRC / 'reused-baseline-evidence/base-postformat-source-sha256.json'),
    ]
    result = {}
    for name, tree, record in specs:
        expected = read_json(record)
        need(source_hashes(tree) == expected, f'source drift: {name}')
        result[name] = {'tree': str(tree), 'record': str(record), 'record_sha256': sha(record), 'files': len(expected)}
    for entry in read_json(SRC / 'manifest-verification.json'):
        need(sha(Path(entry['path'])) == entry['expected'] == entry['actual'], f'manifest drift {entry["path"]}')
    return result


def save_source_delta(name, left, right):
    before, after = source_hashes(left), source_hashes(right)
    changes = []
    for rel in sorted(before.keys() | after.keys()):
        if before.get(rel) == after.get(rel):
            continue
        item = {'path': rel, 'base_sha256': before.get(rel), 'candidate_sha256': after.get(rel)}
        for kind, tree, mapping in [('base', left, before), ('candidate', right, after)]:
            if rel in mapping:
                put_file(tree / rel, Path('source-delta') / name / kind / (rel + '.txt'))
        a = (left / rel).read_text().splitlines(keepends=True) if rel in before else []
        b = (right / rel).read_text().splitlines(keepends=True) if rel in after else []
        patch = ''.join(difflib.unified_diff(a, b, fromfile='base/' + rel, tofile='candidate/' + rel))
        if patch:
            put_bytes(Path('source-delta') / name / 'patches' / (rel + '.patch'), patch.encode())
        changes.append(item)
    put_json(Path('source-delta') / name / 'changes.json', {
        'base': str(left), 'candidate': str(right), 'changes': changes,
        'archive_note': 'Source copies have .txt suffixes; bytes/hashes are unchanged. Paths absent on a side are additions/removals.',
    })


def save_design():
    put_file(DESIGN, 'design/cavlc-mb-certificate-review.original.md')
    text = DESIGN.read_text()
    links = {}
    for target in re.findall(r'\]\((/[^)]+)\)', text):
        plain = re.sub(r':\d+$', '', target)
        path = Path(plain)
        if path in links:
            continue
        if str(path).startswith(str(FROZEN_AFTER) + '/'):
            rel = Path('source-delta/frozen/candidate') / (str(path.relative_to(FROZEN_AFTER)) + '.txt')
            need((STAGE / rel).is_file(), f'design source absent from frozen delta: {path}')
        else:
            rel = Path('design/evidence') / (path.name + ('.txt' if path.suffix in ('.go', '.cpp', '.s') else ''))
            need(not (STAGE / rel).exists(), f'design filename collision: {path}')
            put_file(path, rel)
        links[path] = '../' + str(rel)
    def replace(match):
        target = match[1]
        plain = re.sub(r':\d+$', '', target)
        # Portable Markdown cannot resolve local line-number URI suffixes;
        # exact line references remain explicitly stated in the prose.
        return '](' + links[Path(plain)] + ')'
    text = re.sub(r'\]\((/[^)]+)\)', replace, text)
    put_bytes('design/cavlc-mb-certificate-review.md', text.encode(), 'portable-link derivative of ' + str(DESIGN))


def make_readme(rows, samples, groups):
    group_lines = []
    for name in ('CavlcLevelWriter', 'CavlcMacroblock', 'CavlcNativeFull', 'CavlcEmitterDensity'):
        g = groups[name]
        group_lines.append(f'| `{name}` | {g.get("significant_reductions", 0)} | {g.get("significant_regressions", 0)} | {g.get("inconclusive", 0)} |')
    regression_lines = [f'| `{x["name"]}` | +{x["significant_change_percent"]:.2f}% | {x["p"]:.3f} |'
                        for x in rows if (x['significant_change_percent'] or 0) > 0]
    lines = ['name\tbase_median_ns\tcandidate_median_ns\traw_median_change_percent\tbenchstat_significant_change_percent\tp']
    ratios = []
    for row in rows:
        name = row['name']
        a = statistics.median(x['ns_per_op'] for x in samples['base'][name])
        b = statistics.median(x['ns_per_op'] for x in samples['after'][name])
        ratios.append(b / a)
        sig = '' if row['significant_change_percent'] is None else str(row['significant_change_percent'])
        lines.append(f'{name}\t{a:.9g}\t{b:.9g}\t{(b/a-1)*100:.6f}\t{sig}\t{row["p"]}')
    put_bytes('all-87-cases.tsv', ('\n'.join(lines) + '\n').encode())
    geo = (math.exp(sum(math.log(x) for x in ratios) / len(ratios)) - 1) * 100
    return f'''# Tuned CAVLC emitter — held

The single density-gated follow-up still fails the predeclared caller controls. It is **not merged**. All correctness gates passed, but 9 of 87 timing cases regressed, including complete-macroblock zero/density controls. No full-frame or native timing run followed this candidate, and this archive makes no frame-rate or parity-speed claim.

The tune attempts the checked assembly emitter only inside the existing `totalCoeffs > 3` branch and for end14/15. Assembly, ABI, table semantics, fallback and commit behavior are unchanged from the first held emitter. End3 blocks are excluded by this source gate yet several public end3 controls still regress; this does not establish a specific compiler/layout cause. The exact frozen and formatted sources are preserved under [source-delta](source-delta/).

## Complete timing outcome

Six alternating base/candidate pairs, pinned CPU2, GOMAXPROCS=1, 200ms per case. The baseline is frozen round11 with matched test fixtures, not round12 or the original user version. There are 52 existing caller/level cases, 15 public native-shaped cases and 20 complete-caller density controls: 87 cases, 522 samples per binary.

| Group | Significant reductions | Significant regressions | Inconclusive |
| --- | ---: | ---: | ---: |
{chr(10).join(group_lines)}
| Total | 22 | 9 | 56 |

The unweighted raw-median geomean is {geo:.2f}%. It is not a workload-weighted encoder speedup and does not override the failed controls. `Inconclusive` does not prove neutrality. P-values below are the recorded benchstat values, without a multiplicity correction; raw variance and all samples remain visible. There was no significance-seeking rerun or another cutoff search.

| Failed case | Change | p |
| --- | ---: | ---: |
{chr(10).join(regression_lines)}

Dense/mixed reductions are retained in the complete tables, including end14/end15 mixed public writers and dense MB callers. The hold decision considers all cases. See [benchstat](benchstat.txt), [all 87 rows](all-87-cases.tsv), [root decision](decision.json), [independent audit](independent-audit.json), [base samples](base.txt) and [candidate samples](after.txt). Every original pair log is also present; [raw-samples.json](raw-samples.json) is a lossless extraction of the iteration counts and ns/op values.

## Correctness and provenance

The baseline full-encoder and native 447-suite evidence was reused only after exact source/log/native-binary verification. The baseline was rebuilt with the new density benchmark; all 20 fixtures executed once for setup sanity. The candidate received fresh full encoder/native 447-suite tests, exhaustive signed-domain and density-threshold tests, focused race tests and CPU-all-off tests. Actual native auto/scalar pass events were required, not merely successful process exit. All 50 recorded commands exited zero and their log hashes match. See [gate audit](archive-audit.json), [baseline reuse](baseline-validation-reuse.json), [candidate native validation](after-native447-validation.json) and [commands](commands.jsonl).

Correctness used the exact archived 447-suite coefficient fixture; timing used the exact prior 333-suite fixture and invoked no native binary. `CavlcNativeFull` names describe matched native-shaped fixtures, not native timing in this experiment. [provenance.json](provenance.json) and [timing-environment.json](timing-environment.json) record the settings and fixture hashes.

The frozen candidate inherited the first experiment's corrected unused-import test fix. [source-delta/tune-only](source-delta/tune-only/) records the five-file source-only tune against that validated first candidate. [source-delta/frozen](source-delta/frozen/) records the full candidate delta from round11; [source-delta/measured](source-delta/measured/) records the exact formatted baseline/candidate comparison. All source snapshots are `.txt` copies, so documentation does not introduce active Go packages. Shared correctness/benchmark fixtures and the diagnostic generator are included. Full build-input fingerprints are preserved separately.

The first held experiment remains unchanged in [round12](../../round12/cavlc-emitter-review/). [initial-evidence-references.json](initial-evidence-references.json) records its links and hashes. This experiment neither replaces its failed first-build log nor combines its timing samples with the follow-up.

## Archive contract and next-step boundary

Verbose full-test logs are deterministic gzip files. [archive-manifest.json](archive-manifest.json) records the stored and decoded SHA256 for every evidence file; [archive-files.sha256](archive-files.sha256) covers the final archive, excluding itself. Native/test binaries and any stream/profile payloads are omitted with size/hash records in [omitted-payloads.json](omitted-payloads.json). Coefficient fixtures are reproducible textual test inputs and are retained. The [native oracle source](native-oracle-source/) and its historical instructions are included as source references; the actual executable used is identified by the provenance hash and is not rebuilt here.

The [macroblock certificate design](design/cavlc-mb-certificate-review.md) is source review only. It includes native active-callsite evidence, safety constraints and an explicitly hypothetical CPU ceiling. The original note is preserved beside its portable-link copy. It is not implemented, measured or authorized as a third iteration by this archive.

This helper performs no builds, tests, benchmarks or benchstat reruns. It only verifies and archives completed evidence after the root's CPU queue is free. Round12 and all source/measurement trees are read-only inputs.
'''


def main():
    need(not DEST.exists() and not STAGE.exists(), 'fresh destination/staging paths required')
    rows, samples, groups, native_passes = audit_measurement()
    sources = verify_sources()
    initial_names = ['benchstat.txt', 'decision.txt', 'completion.json', 'provenance.json',
                     'source-delta/frozen/changes.json', 'source-delta/measured/changes.json']
    initial_refs = []
    for name in initial_names:
        path = INITIAL_ARCHIVE / name
        need(path.is_file(), f'initial archived evidence missing: {path}')
        initial_refs.append({'path': str(path), 'relative_archive_link': '../../round12/cavlc-emitter-review/' + name,
                             'bytes': path.stat().st_size, 'sha256': sha(path)})
    STAGE.mkdir(parents=True)
    # Copy all original evidence. Preserve coefficient text fixtures; omit only
    # executable/binary/stream/profile payloads, with their actual hashes.
    for path in sorted(SRC.rglob('*')):
        if not path.is_file():
            continue
        rel = path.relative_to(SRC)
        if path.suffix in {'.test', '.264', '.h264', '.yuv', '.rgb', '.prof', '.pprof', '.a', '.o'}:
            OMITTED.append({'source': str(path), 'relative_source': str(rel), 'bytes': path.stat().st_size,
                            'sha256': sha(path), 'reason': 'binary/stream/profile payload; evidence hashes retained'})
            continue
        # Compress verbose logs, not benchmark samples, fixtures or JSON maps.
        compress = path.name.endswith('-tests.txt') and path.stat().st_size > 1_000_000
        put_file(path, rel, compress)
    OMITTED.append({'source': str(NATIVE), 'bytes': NATIVE.stat().st_size, 'sha256': sha(NATIVE),
                    'reason': 'native correctness executable; zero native timing runs'})
    for kind in ('base', 'after'):
        need(sha(SRC / (kind + '.test')) == read_json(SRC / (kind + '-binary-sha256.json'))['sha256'], 'binary provenance mismatch')
    save_source_delta('frozen', FROZEN_BASE, FROZEN_AFTER)
    save_source_delta('measured', BASE, AFTER)
    save_source_delta('tune-only', INITIAL_AFTER, FROZEN_AFTER)
    for path in sorted((R / 'cavlc-emitter-tuned-diagnostic').iterdir()):
        if path.is_file():
            put_file(path, Path('diagnostic-generator') / path.name)
    # Preserve oracle source alongside its binary hash; this archive does not rebuild it.
    for name in ('native_cavlc.cpp', 'generate_fixtures.py', 'README.md'):
        source = R / 'cavlc-native-review' / name
        put_file(source, Path('native-oracle-source') / (name + '.txt'))
    put_file(AUDIT, 'independent-audit.json')
    put_file(Path(__file__).resolve(), 'archive_cavlc_emitter_tuned_review.py')
    put_json('raw-samples.json', samples)
    put_json('initial-evidence-references.json', initial_refs)
    put_json('source-verification.json', sources)
    put_json('omitted-payloads.json', OMITTED)
    save_design()
    put_bytes('README.md', make_readme(rows, samples, groups).encode())
    put_json('archive-audit.json', {
        'decision': 'held', 'correctness_gates_passed': True, 'source_maps_verified': sources,
        'native_required_pass_events': native_passes, 'cases': 87, 'pairs': 6,
        'counts': read_json(SRC / 'decision.json')['counts'], 'command_count': 50,
        'command_hashes_and_exits_verified': True, 'raw_logs_equal_aggregates': True,
        'native_timing_runs': 0, 'full_frame_timing_runs': 0, 'production_merge': False,
        'archive_only_no_cpu_measurement': True, 'source_review_audit_sha256': sha(AUDIT),
    })
    # Verify source/initial inputs again after copying; preserve separate timing
    # completion.json and do not replace it with archive status.
    need(verify_sources() == sources, 'source drift during archival')
    for item in initial_refs:
        need(sha(Path(item['path'])) == item['sha256'], 'round12 input changed during archival')
    for item in FILES:
        path = STAGE / item['archive_path']
        data = path.read_bytes()
        need(sha_bytes(data) == item['stored_sha256'], 'stored archive hash mismatch')
        decoded = gzip.decompress(data) if item['encoding'] == 'gzip' else data
        need(sha_bytes(decoded) == item['decoded_sha256'], 'decoded archive hash mismatch')
    manifest = {'archive_complete': True, 'created_utc': datetime.now(timezone.utc).isoformat(),
                'source': str(SRC), 'destination': str(DEST), 'decision': 'held',
                'files': FILES, 'omitted_payloads': OMITTED,
                'manifest_note': 'This file and archive-files.sha256 are excluded from files to avoid self-reference.'}
    (STAGE / 'archive-manifest.json').write_text(json.dumps(manifest, indent=2, sort_keys=True) + '\n')
    all_paths = sorted(path for path in STAGE.rglob('*') if path.is_file())
    (STAGE / 'archive-files.sha256').write_text(''.join(sha(path) + '  ' + str(path.relative_to(STAGE)) + '\n' for path in all_paths))
    STAGE.rename(DEST)
    print(f'Archived held tuned CAVLC experiment: {DEST}')
    print(f'{len(FILES)} evidence files; 87 cases, 6 pairs; 22 reductions / 9 regressions / 56 inconclusive')


if __name__ == '__main__':
    try:
        main()
    except BaseException:
        if STAGE.exists():
            (STAGE / 'archive-failure.txt').write_text(traceback.format_exc())
        raise
