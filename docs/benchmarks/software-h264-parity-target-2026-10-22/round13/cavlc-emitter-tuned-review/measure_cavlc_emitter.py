#!/usr/bin/env python3
"""Root-run, serialized CAVLC emitter validation and matched component timing.

Prepared only: the author has not executed this driver. All output/build trees
must be fresh. Frozen round11 and the candidate remain read-only; formatting
and the permitted old assembly EOF normalization happen in working copies.
No native timing is run. Native447 dump parity is part of the full Go tests.
"""
from pathlib import Path
from datetime import datetime, timezone
import hashlib
import json
import os
import platform
import re
import shutil
import subprocess
import time
import traceback

ROOT = Path(__file__).resolve().parent
OUT = ROOT / 'cavlc-emitter-review'
FROZEN_BASE = ROOT / 'round11'
FROZEN_CANDIDATE = ROOT / 'cavlc-emitter-next'
BASE = ROOT / 'cavlc-emitter-round11-base'
AFTER = ROOT / 'cavlc-emitter-measure-next'
MANIFEST = ROOT / 'cavlc-emitter-next-README.txt'
PACKAGE = Path('pkg/h264/internal/encoder')
COPY_ASM = Path('pkg/h264/internal/common/copy_simd_amd64.s')
GO = '/snap/go/current/bin/go'
GOFMT = '/snap/go/current/bin/gofmt'
BENCHSTAT = '/home/define42/go/bin/benchstat'
NATIVE = ROOT / 'cavlc-native-review/native_cavlc'
TIMING_SOURCE = ROOT / 'cavlc-native-review/go/pkg/h264/internal/encoder/testdata/cavlc_native_fixtures.txt'
CORRECTNESS_REL = PACKAGE / 'testdata/cavlc_native_fixtures.txt'
BENCH_IMPORTS = [
    PACKAGE / 'cavlc_scratch_bench_test.go',
    PACKAGE / 'cavlc_native_full_test.go',
    PACKAGE / 'cavlc_native_full_bench_test.go',
    CORRECTNESS_REL,
]
GROUPS = [
    ('callers', '^Benchmark(CavlcMacroblock|CavlcLevelWriter)$', 52),
    ('public', '^BenchmarkCavlcNativeFull$/^end(3|14|15)$/^(zero|known-zero|sparse|dense|mixed)$/^prefix17$', 15),
]
ENV_KEYS = ['PATH', 'CGO_ENABLED', 'GOMAXPROCS', 'GODEBUG', 'GOFLAGS',
            'GOOS', 'GOARCH', 'GOTOOLCHAIN', 'CAVLC_NATIVE_BINARY', 'CAVLC_NATIVE_FIXTURES']
STAGE = 'setup'
OUTPUT_CREATED = False
STATUS = {'complete': False, 'started_utc': None}


def utc():
    return datetime.now(timezone.utc).isoformat()


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def write_json(path, value):
    path.write_text(json.dumps(value, indent=2, sort_keys=True) + '\n')


def source_hashes(tree):
    # Build inputs and test fixture data; docs are deliberately not build inputs.
    suffixes = {'.go', '.s', '.h', '.c', '.cc', '.cpp', '.mod', '.sum'}
    return {str(p.relative_to(tree)): sha(p) for p in sorted(tree.rglob('*'))
            if p.is_file() and (p.suffix in suffixes or 'testdata' in p.parts)}


def env_record(env):
    return {key: env[key] for key in ENV_KEYS if key in env}


def run(name, args, cwd, env):
    global STAGE
    STAGE = name
    log = OUT / (name + '.txt')
    start = time.monotonic()
    record = {'name': name, 'argv': [str(v) for v in args], 'cwd': str(cwd),
              'environment': env_record(env), 'started_utc': utc(), 'log': str(log)}
    print(name, flush=True)
    with log.open('w') as output:
        process = subprocess.run(args, cwd=cwd, env=env, stdout=output, stderr=subprocess.STDOUT)
    record.update(returncode=process.returncode, duration_seconds=time.monotonic() - start,
                  finished_utc=utc(), log_sha256=sha(log))
    with (OUT / 'commands.jsonl').open('a') as output:
        output.write(json.dumps(record, sort_keys=True) + '\n')
    if process.returncode:
        raise RuntimeError(f'{name} exited {process.returncode}; see {log}')
    return log


def require_pass(log, names):
    passed, skipped = set(), set()
    for line in log.read_text().splitlines():
        try:
            event = json.loads(line)
        except json.JSONDecodeError:
            continue
        if event.get('Action') == 'pass':
            passed.add(event.get('Test'))
        if event.get('Action') == 'skip':
            skipped.add(event.get('Test'))
    absent = set(names) - passed
    if absent or set(names) & skipped:
        raise RuntimeError(f'{log.name}: required tests absent/skipped: {absent}, {set(names) & skipped}')
    return sorted(name for name in passed if isinstance(name, str))


def fixture_header(path, count):
    header = path.read_text().splitlines()[0].split()
    if header != ['cavlc-native-v1', str(count)]:
        raise RuntimeError(f'{path}: expected {count} fixture suites, got {header}')


def main():
    global STAGE, OUTPUT_CREATED
    for fresh in (OUT, BASE, AFTER):
        if fresh.exists():
            raise RuntimeError(f'refusing to reuse existing path: {fresh}')
    OUT.mkdir()
    OUTPUT_CREATED = True
    STATUS['started_utc'] = utc()
    write_json(OUT / 'completion.json', STATUS)
    shutil.copy2(Path(__file__), OUT / Path(__file__).name)
    shutil.copy2(MANIFEST, OUT / MANIFEST.name)

    # Verify the entire published candidate manifest before formatting anything.
    STAGE = 'manifest verification'
    entries = []
    relative = {}
    for line in MANIFEST.read_text().splitlines():
        match = re.fullmatch(r'([0-9a-f]{64})  (.+)', line)
        if match is None:
            continue
        expected, name = match.groups()
        rel = Path(name)
        path = rel if rel.is_absolute() else FROZEN_CANDIDATE / rel
        actual = sha(path)
        entries.append({'path': str(path), 'manifest_sha256': expected, 'actual_sha256': actual})
        if actual != expected:
            write_json(OUT / 'manifest-verification.json', entries)
            raise RuntimeError(f'frozen candidate manifest mismatch: {path}')
        if not rel.is_absolute():
            relative[name] = actual
    if len(relative) != 16:
        raise RuntimeError(f'expected16 candidate entries, got {len(relative)}')
    write_json(OUT / 'manifest-verification.json', entries)
    base_input = source_hashes(FROZEN_BASE)
    candidate_input = source_hashes(FROZEN_CANDIDATE)
    write_json(OUT / 'frozen-base-source-sha256.json', base_input)
    write_json(OUT / 'frozen-candidate-source-sha256.json', candidate_input)
    differences = {name for name in base_input.keys() | candidate_input.keys()
                   if base_input.get(name) != candidate_input.get(name)}
    unexpected = differences - set(relative) - {str(COPY_ASM)}
    if unexpected:
        raise RuntimeError(f'unmanifested build-input differences versus frozen round11: {sorted(unexpected)}')

    shutil.copytree(FROZEN_BASE, BASE)
    shutil.copytree(FROZEN_CANDIDATE, AFTER)
    # Some earlier copies omitted one final newline from this existing file.
    # Only trailing LF differences are permitted; never normalize body content.
    old, desired = (AFTER / COPY_ASM).read_bytes(), (BASE / COPY_ASM).read_bytes()
    aligned = old != desired
    if aligned:
        if old.rstrip(b'\n') != desired.rstrip(b'\n'):
            raise RuntimeError('copy_simd_amd64.s differs beyond trailing LF; refusing normalization')
        (AFTER / COPY_ASM).write_bytes(desired)
    write_json(OUT / 'copy-asm-eof-alignment.json', {
        'path': str(COPY_ASM), 'changed': aligned,
        'candidate_frozen_sha256': hashlib.sha256(old).hexdigest(),
        'round11_sha256': hashlib.sha256(desired).hexdigest(),
        'candidate_working_sha256': sha(AFTER / COPY_ASM),
        'rule': 'only trailing LF differences; exact frozen round11 bytes copied to working candidate',
    })
    imports = []
    for rel in BENCH_IMPORTS:
        (BASE / rel).parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(FROZEN_CANDIDATE / rel, BASE / rel)
        expected = sha(FROZEN_CANDIDATE / rel)
        if sha(BASE / rel) != expected:
            raise RuntimeError(f'fixture copy changed bytes: {rel}')
        imports.append({'path': str(rel), 'source_sha256': expected, 'baseline_sha256': sha(BASE / rel)})
    write_json(OUT / 'baseline-test-imports.json', imports)
    correctness = OUT / 'correctness-fixtures447.txt'
    timing = OUT / 'timing-fixtures333.txt'
    shutil.copy2(FROZEN_CANDIDATE / CORRECTNESS_REL, correctness)
    shutil.copy2(TIMING_SOURCE, timing)
    fixture_header(correctness, 447)
    fixture_header(timing, 333)

    # Clear inherited CPU/fixture switches; record every relevant effective value.
    env = os.environ.copy()
    for key in ('GODEBUG', 'GOFLAGS', 'GOOS', 'GOARCH', 'CAVLC_NATIVE_BINARY', 'CAVLC_NATIVE_FIXTURES'):
        env.pop(key, None)
    env.update(PATH='/snap/go/current/bin:' + os.environ['PATH'], CGO_ENABLED='0', GOMAXPROCS='4',
               CAVLC_NATIVE_BINARY=str(NATIVE), CAVLC_NATIVE_FIXTURES=str(correctness))
    provenance = {
        'started_utc': STATUS['started_utc'], 'driver_sha256': sha(Path(__file__)),
        'manifest_sha256': sha(MANIFEST), 'frozen_base': str(FROZEN_BASE),
        'frozen_candidate': str(FROZEN_CANDIDATE), 'working_base': str(BASE),
        'working_candidate': str(AFTER), 'host': platform.uname()._asdict(),
        'cpu_affinity_available': sorted(os.sched_getaffinity(0)), 'benchmark_cpu': 2,
        'validation_env': env_record(env), 'native_binary': str(NATIVE),
        'native_binary_sha256': sha(NATIVE), 'correctness_fixtures': str(correctness),
        'correctness_sha256': sha(correctness), 'correctness_suites': 447,
        'timing_fixtures': str(timing), 'timing_sha256': sha(timing), 'timing_suites': 333,
        'pairs': 6, 'benchtime': '200ms', 'groups': GROUPS, 'native_timing_runs': 0,
    }
    write_json(OUT / 'provenance.json', provenance)
    if 2 not in os.sched_getaffinity(0):
        raise RuntimeError('CPU2 is not in the available affinity mask')
    (OUT / 'cpuinfo.txt').write_text(Path('/proc/cpuinfo').read_text())
    run('go-version', [GO, 'version'], BASE, env)
    run('go-env', [GO, 'env', '-json'], BASE, env)

    for kind, tree in [('base', BASE), ('after', AFTER)]:
        before = source_hashes(tree)
        format_paths = [rel for rel in (BENCH_IMPORTS if kind == 'base' else map(Path, relative))
                        if rel.suffix == '.go']
        run(kind + '-gofmt', [GOFMT, '-w'] + [str(tree / rel) for rel in sorted(format_paths)], tree, env)
        post = source_hashes(tree)
        write_json(OUT / (kind + '-postformat-source-sha256.json'), post)
        write_json(OUT / (kind + '-gofmt-delta.json'), {
            name: {'before': before.get(name), 'after': post.get(name)}
            for name in before.keys() | post.keys() if before.get(name) != post.get(name)
        })
        # Full package includes native auto/scalar dump parity. JSON pass events
        # prove that native checks actually ran rather than silently skipping.
        log = run(kind + '-full-encoder-tests', [GO, 'test', '-json', './' + str(PACKAGE), '-count=1'], tree, env)
        required = ['TestCavlcNativeFixtures', 'TestCavlcNativeFullParity',
                    'TestCavlcNativeFullParity/auto', 'TestCavlcNativeFullParity/scalar']
        passed = require_pass(log, required)
        write_json(OUT / (kind + '-native447-validation.json'), {
            'required_passed': required, 'fixture_sha256': sha(correctness), 'suites': 447,
            'all_passed_tests': passed, 'native_binary_sha256': sha(NATIVE),
        })
        if kind == 'after':
            focused = '^TestCavlcEmitter'
            run('after-emitter-race', [GO, 'test', '-race', './' + str(PACKAGE), '-run=' + focused, '-count=1'],
                tree, env | {'CGO_ENABLED': '1'})
            run('after-emitter-cpu-all-off', [GO, 'test', './' + str(PACKAGE), '-run=' + focused, '-count=1'],
                tree, env | {'GODEBUG': 'cpu.all=off', 'GOMAXPROCS': '1'})
        run(kind + '-build', [GO, 'test', '-c', '-o', str(OUT / (kind + '.test')), './' + str(PACKAGE)], tree, env)
        run(kind + '-build-info', [GO, 'version', '-m', str(OUT / (kind + '.test'))], tree, env)
        write_json(OUT / (kind + '-binary-sha256.json'), {'binary': str(OUT / (kind + '.test')), 'sha256': sha(OUT / (kind + '.test'))})
        (OUT / (kind + '.txt')).write_text('')

    # No tests run during timing; use the unchanged default333 fixture and one P.
    timing_env = env | {'GOMAXPROCS': '1', 'CAVLC_NATIVE_FIXTURES': str(timing)}
    timing_env.pop('CAVLC_NATIVE_BINARY', None)
    write_json(OUT / 'timing-environment.json', env_record(timing_env))
    expected_names = {}
    for rep in range(6):
        order = ['base', 'after'] if rep % 2 == 0 else ['after', 'base']
        for kind in order:
            tree = BASE if kind == 'base' else AFTER
            for group, pattern, count in GROUPS:
                name = f'pair{rep + 1:02d}-{kind}-{group}'
                log = run(name, ['taskset', '-c', '2', str(OUT / (kind + '.test')),
                                '-test.run=^$', '-test.bench=' + pattern, '-test.benchtime=200ms',
                                '-test.benchmem', '-test.count=1'], tree, timing_env)
                text = log.read_text()
                names = re.findall(r'^(Benchmark\S+)\s+\d+\s+', text, re.MULTILINE)
                if len(names) != count or len(set(names)) != count:
                    raise RuntimeError(f'{name}: expected{count} unique benchmark cases, got{len(names)}')
                if group in expected_names and set(names) != expected_names[group]:
                    raise RuntimeError(f'{name}: benchmark case names changed between binaries/repetitions')
                expected_names[group] = set(names)
                with (OUT / (kind + '.txt')).open('a') as aggregate:
                    aggregate.write(text)
        print(f'CAVLC emitter caller/public pair {rep + 1}/6 complete', flush=True)
    run('benchstat', [BENCHSTAT, str(OUT / 'base.txt'), str(OUT / 'after.txt')], OUT, timing_env)
    # The working build sources must not have drifted while measurements ran.
    for kind, tree in [('base', BASE), ('after', AFTER)]:
        saved = json.loads((OUT / (kind + '-postformat-source-sha256.json')).read_text())
        if source_hashes(tree) != saved:
            raise RuntimeError(f'{kind}: source drift after formatting')
    STATUS.update(complete=True, finished_utc=utc(), pairs_completed=6,
                  matched_cases_per_pair=sum(group[2] for group in GROUPS),
                  native_timing_runs=0, result=str(OUT / 'benchstat.txt'))
    write_json(OUT / 'completion.json', STATUS)
    print('CAVLC emitter validation and six matched caller/public pairs complete', flush=True)


if __name__ == '__main__':
    try:
        main()
    except BaseException as error:
        if OUTPUT_CREATED:
            STATUS.update(complete=False, failed_stage=STAGE, finished_utc=utc(), error=str(error))
            write_json(OUT / 'completion.json', STATUS)
            (OUT / 'failure.txt').write_text(traceback.format_exc())
        raise
