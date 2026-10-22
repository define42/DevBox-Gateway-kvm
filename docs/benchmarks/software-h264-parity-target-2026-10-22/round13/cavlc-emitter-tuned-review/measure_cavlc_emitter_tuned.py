#!/usr/bin/env python3
"""Prepared, not executed: one tuned emitter versus validated frozen round11.

Reuse only the prior baseline's full encoder/native447 correctness evidence;
add its new density benchmark, rebuild, and sanity-run those20 fixtures. The
candidate gets fresh full/native447/race/CPU-off checks. All87 cases run six
alternating pinned200ms pairs using the EXACT previous333 timing fixture.
The proven driver's side-effect-free helpers are imported; its main is never
called. Both drivers are archived with their hashes. Initial evidence is read
only, and all output/working directories must be new.
"""
from pathlib import Path
import json
import os
import re
import shutil
import traceback

import measure_cavlc_emitter as core

R = Path(__file__).resolve().parent
OUT = R / 'cavlc-emitter-tuned-review'
BASE = R / 'cavlc-emitter-tuned-round11-base'
AFTER = R / 'cavlc-emitter-tuned-measure-next'
FROZEN = R / 'cavlc-emitter-tuned-next'
ROUND11 = R / 'round11'
PRIOR = R / 'cavlc-emitter-review'
PRIOR_BASE = R / 'cavlc-emitter-round11-base'
MANIFEST = R / 'cavlc-emitter-tuned-next-README.txt'
PKG = core.PACKAGE
DENSITY = PKG / 'cavlc_emit_density_bench_test.go'
GROUPS = core.GROUPS + [('density', '^BenchmarkCavlcEmitterDensity$', 20)]
REQUIRED_NATIVE = ['TestCavlcNativeFixtures', 'TestCavlcNativeFullParity',
                   'TestCavlcNativeFullParity/auto', 'TestCavlcNativeFullParity/scalar']
HELPER_SHA256 = '3365c7e5fb97a108aa0c1bd699e650d0708ccb54b4f5e9cedcffc64fcdbd6c35'
created = False
status = {'complete': False}
core.OUT = OUT


def check_cases(log, count):
    names = re.findall(r'^(Benchmark\S+)\s+\d+\s+', log.read_text(), re.MULTILINE)
    if len(names) != count or len(set(names)) != count:
        raise RuntimeError(f'{log.name}: expected{count} unique benchmark cases, got{len(names)}')
    return set(names)


def main():
    global created
    for path in (OUT, BASE, AFTER):
        if path.exists():
            raise RuntimeError(f'refusing to reuse existing path: {path}')
    OUT.mkdir()
    created = True
    status['started_utc'] = core.utc()
    core.write_json(OUT / 'completion.json', status)
    helper = Path(core.__file__).resolve()
    if core.sha(helper) != HELPER_SHA256:
        raise RuntimeError('proven helper driver changed after tuned driver preparation')
    for path in (Path(__file__), helper, MANIFEST):
        shutil.copy2(path, OUT / path.name)

    # Verify every published hash before any formatting or working-tree edit.
    relative, checked = {}, []
    for line in MANIFEST.read_text().splitlines():
        match = re.fullmatch(r'([0-9a-f]{64})  (.+)', line)
        if not match:
            continue
        expected, name = match.groups()
        rel = Path(name)
        path = rel if rel.is_absolute() else FROZEN / rel
        actual = core.sha(path)
        checked.append({'path': str(path), 'expected': expected, 'actual': actual})
        if actual != expected:
            raise RuntimeError(f'tuned manifest mismatch: {path}')
        if not rel.is_absolute():
            relative[name] = actual
    if len(relative) != 18:
        raise RuntimeError(f'expected18 unique tuned source entries, got{len(relative)}')
    core.write_json(OUT / 'manifest-verification.json', checked)
    round11_hashes, tuned_hashes = core.source_hashes(ROUND11), core.source_hashes(FROZEN)
    differences = {name for name in round11_hashes.keys() | tuned_hashes.keys()
                   if round11_hashes.get(name) != tuned_hashes.get(name)}
    if differences - set(relative) - {str(core.COPY_ASM)}:
        raise RuntimeError('unmanifested tuned build-input difference versus frozen round11')
    core.write_json(OUT / 'frozen-round11-source-sha256.json', round11_hashes)
    core.write_json(OUT / 'frozen-tuned-source-sha256.json', tuned_hashes)

    # Prior baseline is reused only if its exact source and successful native
    # pass events still match the completed initial experiment's provenance.
    previous = json.loads((PRIOR / 'provenance.json').read_text())
    if not json.loads((PRIOR / 'completion.json').read_text()).get('complete'):
        raise RuntimeError('initial experiment did not complete')
    recorded_base = json.loads((PRIOR / 'base-postformat-source-sha256.json').read_text())
    if core.source_hashes(PRIOR_BASE) != recorded_base:
        raise RuntimeError('validated prior baseline source drifted')
    prior_log = PRIOR / 'base-full-encoder-tests.txt'
    core.require_pass(prior_log, REQUIRED_NATIVE)
    prior_commands = [json.loads(line) for line in (PRIOR / 'commands.jsonl').read_text().splitlines()]
    evidence = [item for item in prior_commands if item['name'] == 'base-full-encoder-tests']
    if not evidence or evidence[-1]['returncode'] != 0 or evidence[-1]['log_sha256'] != core.sha(prior_log):
        raise RuntimeError('prior baseline full-test log does not match recorded successful command')
    if core.sha(core.NATIVE) != previous['native_binary_sha256']:
        raise RuntimeError('native correctness binary changed')
    reuse = OUT / 'reused-baseline-evidence'
    reuse.mkdir()
    for name in ['completion.json', 'provenance.json', 'commands.jsonl',
                 'base-full-encoder-tests.txt', 'base-native447-validation.json',
                 'base-postformat-source-sha256.json']:
        shutil.copy2(PRIOR / name, reuse / name)
    shutil.copy2(R / 'cavlc-emitter-next-README.txt', reuse / 'initial-candidate-manifest.txt')
    core.write_json(OUT / 'baseline-validation-reuse.json', {
        'source': str(PRIOR_BASE), 'evidence': str(reuse), 'required_native_passes': REQUIRED_NATIVE,
        'full_test_log_sha256': core.sha(prior_log), 'new_source_only': str(DENSITY),
        'reason': 'unchanged validated baseline production/correctness fixtures; one new baseline-compatible benchmark',
    })

    shutil.copytree(PRIOR_BASE, BASE)
    shutil.copytree(FROZEN, AFTER)
    shutil.copy2(FROZEN / DENSITY, BASE / DENSITY)
    core.write_json(OUT / 'baseline-import.json', {'path': str(DENSITY), 'sha256': core.sha(BASE / DENSITY)})
    old, desired = (AFTER / core.COPY_ASM).read_bytes(), (ROUND11 / core.COPY_ASM).read_bytes()
    if old != desired:
        if old.rstrip(b'\n') != desired.rstrip(b'\n'):
            raise RuntimeError('copy assembly differs beyond trailing LF')
        (AFTER / core.COPY_ASM).write_bytes(desired)
    core.write_json(OUT / 'copy-asm-eof-alignment.json', {
        'changed': old != desired, 'frozen_candidate_sha256': core.sha(FROZEN / core.COPY_ASM),
        'round11_sha256': core.sha(ROUND11 / core.COPY_ASM), 'working_candidate_sha256': core.sha(AFTER / core.COPY_ASM),
    })
    correctness, timing = OUT / 'correctness-fixtures447.txt', OUT / 'timing-fixtures333.txt'
    shutil.copy2(PRIOR / 'correctness-fixtures447.txt', correctness)
    shutil.copy2(PRIOR / 'timing-fixtures333.txt', timing)
    core.fixture_header(correctness, 447)
    core.fixture_header(timing, 333)
    if core.sha(correctness) != previous['correctness_sha256'] or core.sha(timing) != previous['timing_sha256']:
        raise RuntimeError('prior correctness/timing fixture changed')
    for tree in (BASE, AFTER):
        if core.sha(tree / core.CORRECTNESS_REL) != core.sha(correctness):
            raise RuntimeError('working source native447 fixture differs from reused exact fixture')

    env = os.environ.copy()
    for key in ('GODEBUG', 'GOFLAGS', 'GOOS', 'GOARCH', 'CAVLC_NATIVE_BINARY', 'CAVLC_NATIVE_FIXTURES'):
        env.pop(key, None)
    env.update(PATH='/snap/go/current/bin:' + os.environ['PATH'], CGO_ENABLED='0', GOMAXPROCS='4',
               CAVLC_NATIVE_BINARY=str(core.NATIVE), CAVLC_NATIVE_FIXTURES=str(correctness))
    if 2 not in os.sched_getaffinity(0):
        raise RuntimeError('CPU2 unavailable')
    core.write_json(OUT / 'provenance.json', {
        'driver_sha256': core.sha(Path(__file__)), 'helper_sha256': core.sha(helper),
        'tuned_manifest_sha256': core.sha(MANIFEST), 'initial_evidence': str(PRIOR),
        'baseline_source': str(PRIOR_BASE), 'candidate_source': str(FROZEN),
        'correctness_sha256': core.sha(correctness), 'correctness_suites': 447,
        'timing_sha256': core.sha(timing), 'timing_suites': 333,
        'native_binary_sha256': core.sha(core.NATIVE), 'native_timing_runs': 0,
        'pairs': 6, 'benchtime': '200ms', 'groups': GROUPS,
        'validation_env': core.env_record(env), 'benchmark_cpu': 2,
    })
    core.run('go-version', [core.GO, 'version'], BASE, env)
    core.run('go-env', [core.GO, 'env', '-json'], BASE, env)
    for kind, tree in [('base', BASE), ('after', AFTER)]:
        before = core.source_hashes(tree)
        paths = [DENSITY] if kind == 'base' else [Path(p) for p in relative if p.endswith('.go')]
        core.run(kind + '-gofmt', [core.GOFMT, '-w'] + [str(tree / p) for p in sorted(paths)], tree, env)
        post = core.source_hashes(tree)
        core.write_json(OUT / (kind + '-postformat-source-sha256.json'), post)
        core.write_json(OUT / (kind + '-gofmt-delta.json'), {
            p: {'before': before.get(p), 'after': post.get(p)} for p in before.keys() | post.keys()
            if before.get(p) != post.get(p)
        })
        if kind == 'after':
            log = core.run('after-full-encoder-tests', [core.GO, 'test', '-json', './' + str(PKG), '-count=1'], tree, env)
            required = REQUIRED_NATIVE + ['TestCavlcEmitterDensityThreshold', 'TestCavlcEmitterSignedDomain']
            passed = core.require_pass(log, required)
            core.write_json(OUT / 'after-native447-validation.json', {
                'required_passes': required, 'all_passed_tests': passed, 'suites': 447,
                'fixture_sha256': core.sha(correctness), 'native_binary_sha256': core.sha(core.NATIVE),
            })
            core.run('after-emitter-race', [core.GO, 'test', '-race', './' + str(PKG), '-run=^TestCavlcEmitter', '-count=1'], tree, env | {'CGO_ENABLED': '1'})
            core.run('after-emitter-cpu-all-off', [core.GO, 'test', './' + str(PKG), '-run=^TestCavlcEmitter', '-count=1'], tree, env | {'GODEBUG': 'cpu.all=off', 'GOMAXPROCS': '1'})
        core.run(kind + '-build', [core.GO, 'test', '-c', '-o', str(OUT / (kind + '.test')), './' + str(PKG)], tree, env)
        core.run(kind + '-build-info', [core.GO, 'version', '-m', str(OUT / (kind + '.test'))], tree, env)
        core.write_json(OUT / (kind + '-binary-sha256.json'), {'sha256': core.sha(OUT / (kind + '.test'))})
        sanity = core.run(kind + '-density-fixture-sanity', [str(OUT / (kind + '.test')), '-test.run=^$',
                         '-test.bench=^BenchmarkCavlcEmitterDensity$', '-test.benchtime=1x', '-test.count=1'], tree, env)
        check_cases(sanity, 20)  # Setup validates the complete caller outside timing; ignore these one-shot times.
        (OUT / (kind + '.txt')).write_text('')

    timing_env = env | {'GOMAXPROCS': '1', 'CAVLC_NATIVE_FIXTURES': str(timing)}
    timing_env.pop('CAVLC_NATIVE_BINARY', None)
    core.write_json(OUT / 'timing-environment.json', core.env_record(timing_env))
    reference_names = {}
    for rep in range(6):
        for kind in (['base', 'after'] if rep % 2 == 0 else ['after', 'base']):
            tree = BASE if kind == 'base' else AFTER
            for group, pattern, count in GROUPS:
                log = core.run(f'pair{rep + 1:02d}-{kind}-{group}', ['taskset', '-c', '2', str(OUT / (kind + '.test')),
                               '-test.run=^$', '-test.bench=' + pattern, '-test.benchtime=200ms',
                               '-test.benchmem', '-test.count=1'], tree, timing_env)
                names = check_cases(log, count)
                if group in reference_names and names != reference_names[group]:
                    raise RuntimeError('benchmark case mismatch between binaries/repetitions')
                reference_names[group] = names
                with (OUT / (kind + '.txt')).open('a') as aggregate:
                    aggregate.write(log.read_text())
        print(f'CAVLC tuned emitter caller/public/density pair {rep + 1}/6 complete', flush=True)
    core.run('benchstat', [core.BENCHSTAT, str(OUT / 'base.txt'), str(OUT / 'after.txt')], OUT, timing_env)
    for kind, tree in [('base', BASE), ('after', AFTER)]:
        if core.source_hashes(tree) != json.loads((OUT / (kind + '-postformat-source-sha256.json')).read_text()):
            raise RuntimeError(f'{kind}: source drift during validation/measurement')
    status.update(complete=True, finished_utc=core.utc(), pairs_completed=6, matched_cases_per_pair=87,
                  native_timing_runs=0, baseline_validation_reused=True, result=str(OUT / 'benchstat.txt'))
    core.write_json(OUT / 'completion.json', status)
    print('Tuned emitter validation and six87-case matched pairs complete', flush=True)


if __name__ == '__main__':
    try:
        main()
    except BaseException as error:
        if created:
            status.update(complete=False, failed_stage=core.STAGE, finished_utc=core.utc(), error=str(error))
            core.write_json(OUT / 'completion.json', status)
            (OUT / 'failure.txt').write_text(traceback.format_exc())
        raise
