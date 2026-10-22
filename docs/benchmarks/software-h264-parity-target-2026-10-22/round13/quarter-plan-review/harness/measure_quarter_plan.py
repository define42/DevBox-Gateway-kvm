#!/usr/bin/env python3
"""Serial component gate for the frozen quarter-plan candidate; root runs it.

Source-only preparation does not execute this module. The driver refuses to
reuse output, verifies both full source fingerprints and the exact eight-file
candidate manifest, and formats/builds/tests private copies only. It does not
merge sources, rerun native, or claim codec/full-frame performance.
"""
from pathlib import Path
import argparse
import hashlib
import json
import math
import os
import re
import shutil
import statistics
import subprocess
import time

ROOT = Path(__file__).resolve().parent
GO = '/snap/go/current/bin/go'
GOFMT = '/snap/go/current/bin/gofmt'
BENCHSTAT = '/home/define42/go/bin/benchstat'
PACKAGE = './pkg/h264/internal/encoder'
REPETITIONS = 6
ENCODER = Path('pkg/h264/internal/encoder')
OVERLAY_NAME = 'quarter_plan_fallback_bench_amd64_test.go'
EXPECTED_BASE_DIGEST = 'c60f1bb69991e68584e2b9512b42acf4a3454c51ec68389da0844825cf95529f'
EXPECTED_AFTER_DIGEST = '1ea041b4427a143af820fa73934b7db5e3e5f912c55be3003467800dd86c440e'
EXPECTED_BASE_MANIFEST = '08c290e150913e99884098980b361a7ae784153659a8dd3d044805c0e795c7e1'
EXPECTED_CANDIDATE_MANIFEST = '91285e4253fe0f8a325c7cfd05cc6d21dde46a90ea426dd8a7751eaad6a5c4d7'
EXPECTED_OVERLAY = '490fef44f3b3d55308e4adbfedecd6dff56415746c506414b774f04d5bb743f5'
SHAPES = ['16x16', '16x8', '8x16', '8x8', '8x4', '4x4', '4x8']
EXPECTED_GROUPS = {
    'BenchmarkMeRefineFracPixelCaller': {
        f'BenchmarkMeRefineFracPixelCaller/{shape}/stride{stride}/motion{motion}'
        for shape in SHAPES for stride in [32, 2048] for motion in ['false', 'true']
    },
    'BenchmarkQuarterRefineLocalParams': {
        f'BenchmarkQuarterRefineLocalParams/{shape}' for shape in SHAPES
    },
    'BenchmarkQuarterPlanFallbackCaller': {
        f'BenchmarkQuarterPlanFallbackCaller/{shape}/stride{stride}/{dispatch}'
        for shape in ['8x8', '16x16'] for stride in [32, 2048]
        for dispatch in ['batch-off', 'validated-off', 'scalar']
    },
}
EXPECTED_CASES = set().union(*EXPECTED_GROUPS.values())
BENCHMARK_PATTERN = '^(' + '|'.join(EXPECTED_GROUPS) + ')$'
FOCUSED_TESTS = '^(TestQuarterPlan|TestHalfSatdPair|TestQuarterRefine|TestSampleAvgSatdFour|TestMeRefineFracPixel)'
ALLOCATION_TESTS = '^(TestMeRefineFracPixelNoPerBlockAllocs|TestQuarterRefinePerInvocationParamsNoAllocs)$'


def sha(path):
    with Path(path).open('rb') as f:
        return hashlib.file_digest(f, 'sha256').hexdigest()


def write_json(path, value):
    Path(path).write_text(json.dumps(value, indent=2) + '\n')


def source_map(tree):
    result = {}
    for prefix in ['pkg', 'internal']:
        for path in sorted((tree / prefix).rglob('*')):
            if path.is_file():
                result[str(path.relative_to(tree))] = sha(path)
    for name in ['go.mod', 'go.sum']:
        result[name] = sha(tree / name)
    return result


def map_digest(files):
    data = json.dumps(files, sort_keys=True, separators=(',', ':')).encode()
    return hashlib.sha256(data).hexdigest()


def write_hashes(path, files):
    Path(path).write_text(''.join(f'{digest}  {name}\n' for name, digest in sorted(files.items())))


def read_hashes(path):
    pairs = re.findall(r'^([0-9a-f]{64})  ([^\r\n]+)$', Path(path).read_text(), re.MULTILINE)
    result = {name: digest for digest, name in pairs}
    if len(result) != len(pairs):
        raise SystemExit(f'Duplicate source manifest paths: {path}')
    return result


def require(condition, message):
    if not condition:
        raise SystemExit(message)


def clean_environment():
    env = os.environ.copy()
    for key in list(env):
        if key.startswith(('COMPARE_', 'CAVLC_', 'H264_CAVLC_', 'H264_VIDEO_')) or key in {
            'GODEBUG', 'GOFLAGS', 'GOOS', 'GOARCH', 'GOWORK', 'GOAMD64', 'GOTRACEBACK',
        }:
            env.pop(key)
    env.update(PATH='/snap/go/current/bin:' + env.get('PATH', ''),
               CGO_ENABLED='0', GOMAXPROCS='4', GOOS='linux', GOARCH='amd64',
               GOAMD64='v1', GOWORK='off')
    return env


def parse_benchmarks(text):
    result = {}
    for line in text.splitlines():
        fields = line.split()
        if not fields or not fields[0].startswith('Benchmark'):
            continue
        # Ignore verbose test headings; require every timed leaf's usual units.
        if len(fields) < 8 or fields[3] != 'ns/op':
            continue
        name = re.sub(r'-\d+$', '', fields[0])
        require(name not in result, f'Duplicate benchmark leaf: {name}')
        require(fields[5] == 'B/op' and fields[7] == 'allocs/op', f'Unexpected benchmark units: {line}')
        entry = {'iterations': int(fields[1]), 'ns_per_op': float(fields[2]),
                 'bytes_per_op': float(fields[4]), 'allocations_per_op': float(fields[6])}
        require(entry['iterations'] > 0 and math.isfinite(entry['ns_per_op']) and entry['ns_per_op'] > 0,
                f'Invalid benchmark sample: {line}')
        require(entry['bytes_per_op'] == 0 and entry['allocations_per_op'] == 0,
                f'Unexpected per-call allocation: {line}')
        result[name] = entry
    require(set(result) == EXPECTED_CASES,
            f'Benchmark matrix changed: missing={sorted(EXPECTED_CASES - set(result))}; '
            f'extra={sorted(set(result) - EXPECTED_CASES)}')
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, default=ROOT / 'quarter-plan-review')
    args = parser.parse_args()
    output = args.output.resolve()
    selected = {'base': ROOT / 'round11', 'after': ROOT / 'quarter-plan-next'}
    baseline_manifest = ROOT / 'round11-sources.sha256'
    candidate_manifest = ROOT / 'quarter-plan-next-README.txt'
    overlay = ROOT / OVERLAY_NAME
    for path, expected in [(baseline_manifest, EXPECTED_BASE_MANIFEST),
                           (candidate_manifest, EXPECTED_CANDIDATE_MANIFEST),
                           (overlay, EXPECTED_OVERLAY)]:
        require(path.is_file() and sha(path) == expected, f'Frozen input changed: {path}')
    for tree in selected.values():
        resolved = tree.resolve()
        require(not (output == resolved or output.is_relative_to(resolved) or resolved.is_relative_to(output)),
                'Output must be disjoint from both frozen source trees')
    require(hasattr(os, 'sched_getaffinity') and 2 in os.sched_getaffinity(0),
            'CPU 2 must be available for the fixed-affinity comparison')
    for command in [GO, GOFMT, BENCHSTAT]:
        require(Path(command).is_file(), f'Missing tool: {command}')
    require(shutil.which('taskset') is not None, 'taskset is required')

    files = {kind: source_map(tree) for kind, tree in selected.items()}
    require(map_digest(files['base']) == EXPECTED_BASE_DIGEST, 'Frozen round11 source fingerprint changed')
    require(map_digest(files['after']) == EXPECTED_AFTER_DIGEST, 'Frozen quarter-plan source fingerprint changed')
    archived_base = read_hashes(baseline_manifest)
    require(len(archived_base) == 566, 'Unexpected frozen round11 source manifest size')
    require(all(files['base'].get(name) == digest for name, digest in archived_base.items()),
            'Round11 source no longer matches its archived 566-file manifest')
    changes = {name: files['after'].get(name) for name in sorted(set(files['base']) | set(files['after']))
               if files['base'].get(name) != files['after'].get(name)}
    declared_changes = read_hashes(candidate_manifest)
    require(len(declared_changes) == 8 and changes == declared_changes,
            'Actual delta differs from the frozen eight-file candidate manifest')
    benchmark = str(ENCODER / 'sample_quarter_caller_bench_amd64_test.go')
    require(files['base'][benchmark] == files['after'][benchmark], 'Existing caller benchmarks changed')
    require(str(ENCODER / OVERLAY_NAME) not in files['base'] and str(ENCODER / OVERLAY_NAME) not in files['after'],
            'Benchmark-only overlay unexpectedly exists in a frozen tree')

    output.mkdir(exist_ok=False)
    for name in ['validation', 'bin', 'raw', 'harness', 'work']:
        (output / name).mkdir()
    for path in [Path(__file__), baseline_manifest, candidate_manifest, overlay]:
        shutil.copy2(path, output / 'harness' / path.name)
    write_json(output / 'changes-vs-round11.json', [
        {'path': name, 'before_sha256': files['base'].get(name), 'after_sha256': digest}
        for name, digest in changes.items()])
    write_json(output / 'expected-cases.json', {
        'groups': {group: sorted(names) for group, names in EXPECTED_GROUPS.items()},
        'case_count_per_run': len(EXPECTED_CASES), 'runs_per_binary': REPETITIONS,
        'expected_samples_per_binary': REPETITIONS * len(EXPECTED_CASES),
    })
    work = {kind: output / 'work' / kind for kind in selected}
    for kind, tree in selected.items():
        work[kind].mkdir()
        for prefix in ['pkg', 'internal']:
            shutil.copytree(tree / prefix, work[kind] / prefix)
        for name in ['go.mod', 'go.sum']:
            shutil.copy2(tree / name, work[kind] / name)
        require(source_map(work[kind]) == files[kind], f'{kind} source changed while snapshotting')
        write_hashes(output / f'{kind}-frozen-sources.sha256', files[kind])
        shutil.copy2(overlay, work[kind] / ENCODER / OVERLAY_NAME)

    env = clean_environment()
    commands = []

    def run(label, command, *, kind='after', extra=None, logfile=None):
        cwd = work[kind]
        effective = env | (extra or {})
        print(label, flush=True)
        started = time.time()
        result = subprocess.run([str(x) for x in command], cwd=cwd, env=effective,
                                text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        log = logfile or output / 'validation' / f'{label}.txt'
        log.write_text(result.stdout)
        commands.append({'name': label, 'command': [str(x) for x in command], 'cwd': str(cwd),
                         'environment_override': extra or {}, 'exit_code': result.returncode,
                         'elapsed_seconds': time.time() - started,
                         'log': str(log.relative_to(output))})
        write_json(output / 'commands.json', commands)
        require(result.returncode == 0, f'{label} failed; see {log}')
        return result.stdout

    provenance = {
        'scope': 'Component gate only; no native, codec/frame, stream, or live RDP claim',
        'source_paths': {kind: str(tree) for kind, tree in selected.items()},
        'working_paths': {kind: str(tree) for kind, tree in work.items()},
        'frozen_source_map_sha256': {kind: map_digest(data) for kind, data in files.items()},
        'frozen_source_file_counts': {kind: len(data) for kind, data in files.items()},
        'input_sha256': {str(path): sha(path) for path in [Path(__file__), baseline_manifest, candidate_manifest, overlay]},
        'base_environment': {key: env[key] for key in ['CGO_ENABLED', 'GOMAXPROCS', 'GOOS', 'GOARCH', 'GOAMD64', 'GOWORK']},
        'timing': {'repetitions': REPETITIONS, 'cpu_affinity': '2', 'benchtime': '200ms',
                   'order': 'base/after on odd pairs, after/base on even pairs',
                   'groups': {group: len(names) for group, names in EXPECTED_GROUPS.items()},
                   'case_count_per_run': len(EXPECTED_CASES), 'warmup': 'Go benchmark calibration'},
        'benchmark_overlay': 'The external fallback-only fixture is identical in both working copies; frozen trees stay untouched',
    }
    write_json(output / 'provenance.json', provenance)
    run('go-version', [GO, 'version'])
    run('host', ['uname', '-a'])
    run('cpu', ['lscpu'])
    changed_go = [name for name in changes if name.endswith('.go')]
    for kind in ['base', 'after']:
        formatting_paths = ([str(ENCODER / OVERLAY_NAME)] if kind == 'base'
                            else [*changed_go, str(ENCODER / OVERLAY_NAME)])
        run(f'{kind}-gofmt', [GOFMT, '-w', *formatting_paths], kind=kind)
        expected = files[kind] | {str(ENCODER / OVERLAY_NAME): EXPECTED_OVERLAY}
        formatted = source_map(work[kind])
        require(formatted == expected, f'{kind} formatting altered frozen bytes; review before measurement')
        write_hashes(output / f'{kind}-measured-sources.sha256', formatted)

    run('after-full-encoder-tests', [GO, 'test', PACKAGE, '-count=1'])
    run('after-focused-race', [GO, 'test', '-race', PACKAGE, '-run', FOCUSED_TESTS, '-count=1'],
        extra={'CGO_ENABLED': '1'})
    run('after-cpu-all-off', [GO, 'test', PACKAGE, '-count=1'], extra={'GODEBUG': 'cpu.all=off'})
    run('after-arm64-build', [GO, 'test', '-c', '-o', output / 'bin' / 'after-arm64.test', PACKAGE],
        extra={'GOARCH': 'arm64'})
    binaries = {kind: output / 'bin' / f'{kind}.test' for kind in selected}
    for kind, binary in binaries.items():
        run(f'{kind}-build', [GO, 'test', '-c', '-o', binary, PACKAGE], kind=kind)
        run(f'{kind}-allocation-tests', ['taskset', '-c', '2', binary, '-test.run=' + ALLOCATION_TESTS,
                                        '-test.count=1', '-test.v'], kind=kind)
        run(f'{kind}-refinement-assembly', [GO, 'tool', 'objdump', '-s',
                                           r'encoder\.(MeRefineFracPixel|meRefineQuarterPlan.*)$', binary], kind=kind)
    binary_hashes = {kind: sha(binary) for kind, binary in binaries.items()}
    provenance['binary_sha256'] = binary_hashes
    write_json(output / 'provenance.json', provenance)
    for kind in selected:
        (output / f'{kind}.txt').write_text('')

    samples = []
    for rep in range(REPETITIONS):
        order = ['base', 'after'] if rep % 2 == 0 else ['after', 'base']
        for position, kind in enumerate(order):
            require(sha(binaries[kind]) == binary_hashes[kind], 'Measured binary changed')
            log = output / 'raw' / f'pair-{rep + 1:02d}-{position + 1}-{kind}.txt'
            text = run(f'pair-{rep + 1:02d}-{kind}',
                       ['taskset', '-c', '2', binaries[kind], '-test.run=^$',
                        '-test.bench=' + BENCHMARK_PATTERN, '-test.benchmem',
                        '-test.benchtime=200ms', '-test.count=1'], kind=kind, logfile=log)
            parsed = parse_benchmarks(text)
            with (output / f'{kind}.txt').open('a') as f:
                f.write(text)
            samples.extend({'pair': rep + 1, 'order_in_pair': position + 1, 'kind': kind,
                            'case': name, **sample} for name, sample in parsed.items())
            write_json(output / 'samples.json', samples)
        print(f'Quarter-plan component pair {rep + 1}/{REPETITIONS} complete', flush=True)

    run('benchstat', [BENCHSTAT, output / 'base.txt', output / 'after.txt'], logfile=output / 'benchstat.txt')
    cases = []
    for name in sorted(EXPECTED_CASES):
        values = {kind: [item['ns_per_op'] for item in samples if item['kind'] == kind and item['case'] == name]
                  for kind in selected}
        require(all(len(data) == REPETITIONS for data in values.values()), f'Wrong sample count for {name}')
        before, after = statistics.median(values['base']), statistics.median(values['after'])
        cases.append({'case': name, 'samples_per_binary': REPETITIONS,
                      'base_median_ns': before, 'after_median_ns': after,
                      'median_change_percent': (after / before - 1) * 100,
                      'allocations_per_op': 0, 'bytes_per_op': 0})
    for kind, tree in selected.items():
        require(source_map(tree) == files[kind], f'{kind} frozen source changed during measurement')
        require(source_map(work[kind]) == files[kind] | {str(ENCODER / OVERLAY_NAME): EXPECTED_OVERLAY},
                f'{kind} measured source changed after formatting')
        require(sha(binaries[kind]) == binary_hashes[kind], f'{kind} binary changed after timing')
    write_json(output / 'summary.json', {
        'status': 'complete', 'scope': provenance['scope'],
        'repetitions': REPETITIONS, 'cases_per_run': len(EXPECTED_CASES),
        'group_case_counts': {group: len(names) for group, names in EXPECTED_GROUPS.items()},
        'samples_per_binary': REPETITIONS * len(EXPECTED_CASES),
        'total_samples': len(samples), 'all_samples_zero_allocations': True,
        'statistical_evidence': 'benchstat.txt; median changes alone are not significance claims',
        'cases': cases,
    })
    print(f'Quarter-plan component gate complete: {output}', flush=True)


if __name__ == '__main__':
    main()
