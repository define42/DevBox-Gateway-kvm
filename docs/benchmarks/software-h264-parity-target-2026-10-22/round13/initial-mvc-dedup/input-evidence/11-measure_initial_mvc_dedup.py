#!/usr/bin/env python3
"""One serial, predeclared component comparison; no merge or frame claim."""
from pathlib import Path
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
BASE_DIGEST = '0570db217a03beeb4fc44bb202081147b24db54fb1debd1f129e898edd192f8b'
MAIN = Path('/home/define42/git/DevBox-Gateway-kvm')


def sha(path):
    with Path(path).open('rb') as handle:
        return hashlib.file_digest(handle, 'sha256').hexdigest()


def require(condition, message):
    if not condition:
        raise SystemExit(message)


def save(path, value):
    Path(path).write_text(json.dumps(value, indent=2) + '\n')


def source_map(tree):
    files = {str(path.relative_to(tree)): sha(path)
             for prefix in ['pkg', 'internal']
             for path in sorted((tree / prefix).rglob('*')) if path.is_file()}
    files.update({name: sha(tree / name) for name in ['go.mod', 'go.sum']})
    return files


def digest(files):
    return hashlib.sha256(json.dumps(files, sort_keys=True, separators=(',', ':')).encode()).hexdigest()


def hashes(path, files):
    Path(path).write_text(''.join(f'{value}  {name}\n' for name, value in sorted(files.items())))


def index_hash():
    path = subprocess.check_output(['git', 'rev-parse', '--git-path', 'index'], cwd=MAIN, text=True).strip()
    return sha(MAIN / path)


def main():
    config_path = ROOT / 'initial-mvc-dedup-measurement.json'
    config = json.loads(config_path.read_text())
    require(config['status'] == 'frozen_for_one_component_comparison', 'Configuration is not frozen')
    require(config['repetitions'] == 6 and config['benchtime'] == '200ms', 'Unexpected timing scope')
    trees = {'base': ROOT / 'round12-next', 'after': ROOT / 'initial-mvc-dedup-next'}
    output = ROOT / 'initial-mvc-dedup-review'
    require(not output.exists(), 'Output already exists; no automatic repeat is permitted')
    require(2 in os.sched_getaffinity(0), 'CPU 2 is unavailable')
    files = {kind: source_map(tree) for kind, tree in trees.items()}
    require(len(files['base']) == 1017 and digest(files['base']) == BASE_DIGEST, 'Baseline changed')
    require(digest(files['after']) == config['candidate_source_digest'], 'Candidate changed')
    changes = {name: files['after'].get(name) for name in sorted(set(files['base']) | set(files['after']))
               if files['base'].get(name) != files['after'].get(name)}
    require(changes == config['changes'], 'Candidate delta differs from frozen configuration')
    overlay_path = Path(config['benchmark_overlay']['path'])
    overlay_name = config['benchmark_overlay']['destination']
    overlay_hash = config['benchmark_overlay']['sha256']
    require(sha(overlay_path) == overlay_hash, 'Benchmark overlay changed')
    require(overlay_name.startswith('pkg/h264/internal/encoder/') and overlay_name.endswith('_test.go') and
            '..' not in Path(overlay_name).parts,
            'Unexpected overlay destination')
    require(all(overlay_name not in mapping for mapping in files.values()), 'Overlay already exists')
    expected_cases = set(config['expected_cases'])
    require(len(expected_cases) == len(config['expected_cases']) > 0, 'Invalid benchmark matrix')
    inputs = {str(config_path): sha(config_path), str(Path(__file__)): sha(__file__),
              str(overlay_path): overlay_hash, **config['evidence_sha256']}
    for path, value in inputs.items():
        require(sha(path) == value, f'Input changed: {path}')
    main_files, initial_index = source_map(MAIN), index_hash()
    require({k: v for k, v in main_files.items() if k.startswith('pkg/h264/')} ==
            {k: v for k, v in files['base'].items() if k.startswith('pkg/h264/')}, 'Main is not round12')

    output.mkdir()
    for name in ['work', 'validation', 'raw', 'bin', 'harness']:
        (output / name).mkdir()
    shutil.copy2(config_path, output / 'harness' / config_path.name)
    shutil.copy2(__file__, output / 'harness' / Path(__file__).name)
    shutil.copy2(overlay_path, output / 'harness' / overlay_path.name)
    work = {kind: output / 'work' / kind for kind in trees}
    for kind, tree in trees.items():
        work[kind].mkdir()
        for prefix in ['pkg', 'internal']:
            shutil.copytree(tree / prefix, work[kind] / prefix)
        for name in ['go.mod', 'go.sum']:
            shutil.copy2(tree / name, work[kind] / name)
        require(source_map(work[kind]) == files[kind], f'{kind} changed during copying')
        hashes(output / f'{kind}-frozen-sources.sha256', files[kind])
        shutil.copy2(overlay_path, work[kind] / overlay_name)
        hashes(output / f'{kind}-measured-sources.sha256', files[kind] | {overlay_name: overlay_hash})
    save(output / 'changes.json', changes)
    env = os.environ.copy()
    for key in list(env):
        if key.startswith(('COMPARE_', 'CAVLC_', 'H264_')) or key in {
                'GODEBUG', 'GOFLAGS', 'GOOS', 'GOARCH', 'GOWORK', 'GOAMD64', 'GOTRACEBACK'}:
            env.pop(key)
    env.update(PATH='/snap/go/current/bin:' + env.get('PATH', ''), CGO_ENABLED='0',
               GOMAXPROCS='4', GOOS='linux', GOARCH='amd64', GOAMD64='v1', GOWORK='off')
    provenance = {'scope': 'Complete initial-search component callers only; no frame or native speed claim',
                  'source_paths': {k: str(v) for k, v in trees.items()},
                  'source_digests': {k: digest(v) for k, v in files.items()},
                  'input_sha256': inputs, 'index_sha256_before': initial_index,
                  'environment': {k: env[k] for k in ['CGO_ENABLED', 'GOMAXPROCS', 'GOOS', 'GOARCH', 'GOAMD64', 'GOWORK']},
                  'timing': {'affinity': '2', 'repetitions': 6, 'benchtime': '200ms',
                             'order': 'base/after on odd pairs, after/base on even pairs'},
                  'decision_rule': config['decision_rule']}
    save(output / 'provenance.json', provenance)
    commands = []

    def run(label, args, kind='after', extra=None, logfile=None):
        print(label, flush=True)
        started = time.time()
        result = subprocess.run(list(map(str, args)), cwd=work[kind], env=env | (extra or {}),
                                text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        log = logfile or output / 'validation' / f'{label}.txt'
        log.write_text(result.stdout)
        commands.append({'label': label, 'args': list(map(str, args)), 'cwd': str(work[kind]),
                         'extra_environment': extra or {}, 'exit_code': result.returncode,
                         'elapsed_seconds': time.time() - started,
                         'log': str(log.relative_to(output)), 'log_sha256': sha(log)})
        save(output / 'commands.json', commands)
        require(result.returncode == 0, f'{label} failed; inspect {log}')
        return result.stdout

    run('go-version', [GO, 'version'])
    run('cpu', ['lscpu'])
    for kind in trees:
        go_files = [overlay_name] + ([name for name in changes if name.endswith('.go') and changes[name]]
                                    if kind == 'after' else [])
        require(not run(f'{kind}-gofmt', [GOFMT, '-l', *go_files], kind).strip(), 'Formatting required')
    run('after-full-encoder-tests', [GO, 'test', PACKAGE, '-count=1'])
    race_log = run('after-focused-race', [GO, 'test', '-json', '-race', PACKAGE, '-run', config['focused_tests'], '-count=1'],
                   extra={'CGO_ENABLED': '1'})
    passed_tests = {event['Test'] for line in race_log.splitlines() if line.startswith('{')
                    for event in [json.loads(line)] if event.get('Action') == 'pass' and 'Test' in event}
    require(len(config['required_race_tests']) > 0 and set(config['required_race_tests']) <= passed_tests,
            'Required focused race tests did not all execute and pass')
    run('after-cpu-all-off', [GO, 'test', PACKAGE, '-count=1'], extra={'GODEBUG': 'cpu.all=off'})
    run('after-arm64-build', [GO, 'test', '-c', '-o', output / 'bin' / 'after-arm64.test', PACKAGE],
        extra={'GOARCH': 'arm64'})
    binaries = {kind: output / 'bin' / f'{kind}.test' for kind in trees}
    for kind, binary in binaries.items():
        run(f'{kind}-build', [GO, 'test', '-c', '-o', binary, PACKAGE], kind)
        run(f'{kind}-initial-search-assembly', [GO, 'tool', 'objdump', '-s',
                                             r'encoder\.(WelsMotionEstimateInitialPoint|meInitial.*)$', binary], kind)
    binary_hashes = {kind: sha(binary) for kind, binary in binaries.items()}
    provenance['binary_sha256'] = binary_hashes
    save(output / 'provenance.json', provenance)
    for kind in trees:
        (output / f'{kind}.txt').write_text('')
    samples = []
    for pair in range(1, 7):
        order = ['base', 'after'] if pair % 2 else ['after', 'base']
        for position, kind in enumerate(order, 1):
            require(sha(binaries[kind]) == binary_hashes[kind], 'Binary changed')
            raw = output / 'raw' / f'pair-{pair:02d}-{position}-{kind}.txt'
            text = run(f'pair-{pair:02d}-{kind}', ['taskset', '-c', '2', binaries[kind], '-test.run=^$',
                       '-test.bench=' + config['benchmark_pattern'], '-test.benchmem',
                       '-test.benchtime=200ms', '-test.count=1'], kind, logfile=raw)
            parsed = {}
            for line in text.splitlines():
                fields = line.split()
                if len(fields) < 8 or not fields[0].startswith('Benchmark') or fields[3] != 'ns/op':
                    continue
                name = re.sub(r'-\d+$', '', fields[0])
                require(name not in parsed, f'Duplicate benchmark: {name}')
                require(fields[5] == 'B/op' and fields[7] == 'allocs/op', 'Unknown benchmark units')
                ns, allocated, allocs = float(fields[2]), float(fields[4]), float(fields[6])
                require(int(fields[1]) > 0 and math.isfinite(ns) and ns > 0, 'Invalid time sample')
                require(allocated == 0 and allocs == 0, f'New allocation: {line}')
                parsed[name] = {'ns_per_op': ns, 'bytes_per_op': allocated, 'allocations_per_op': allocs}
            require(set(parsed) == expected_cases, f'Unexpected benchmark matrix: {set(parsed) ^ expected_cases}')
            with (output / f'{kind}.txt').open('a') as handle:
                handle.write(text)
            samples.extend({'pair': pair, 'position': position, 'kind': kind, 'case': name, **value}
                           for name, value in parsed.items())
            save(output / 'samples.json', samples)
    run('benchstat', [BENCHSTAT, output / 'base.txt', output / 'after.txt'], logfile=output / 'benchstat.txt')
    rows = []
    for name in sorted(expected_cases):
        values = {kind: [item['ns_per_op'] for item in samples if item['kind'] == kind and item['case'] == name]
                  for kind in trees}
        require(all(len(data) == 6 for data in values.values()), 'Wrong sample count')
        before, after = statistics.median(values['base']), statistics.median(values['after'])
        rows.append({'case': name, 'base_median_ns': before, 'after_median_ns': after,
                     'median_change_percent': (after / before - 1) * 100})
    for kind, tree in trees.items():
        require(source_map(tree) == files[kind], f'{kind} frozen source changed')
        require(source_map(work[kind]) == files[kind] | {overlay_name: overlay_hash}, f'{kind} measured source changed')
        require(sha(binaries[kind]) == binary_hashes[kind], f'{kind} binary changed')
    for path, value in inputs.items():
        require(sha(path) == value, f'Input changed: {path}')
    require(source_map(MAIN) == main_files and index_hash() == initial_index, 'Main or index changed')
    save(output / 'summary.json', {'status': 'complete', 'scope': provenance['scope'],
         'cases_per_run': len(expected_cases), 'samples_per_binary': len(expected_cases) * 6,
         'total_samples': len(samples), 'zero_allocations': True,
         'significance': 'See benchstat.txt; median changes are not significance tests', 'cases': rows,
         'source_binary_input_main_index_final_hashes_match': True})
    print(f'Component gate complete: {output}', flush=True)


if __name__ == '__main__':
    main()
