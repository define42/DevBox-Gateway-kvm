#!/usr/bin/env python3
"""Serial round12 validation, parity and timing. Prepared only; root runs it.

No source selection or merge occurs here. Require a finished isolated source
selection, take a private snapshot, and refuse to reuse an output directory.
"""
from pathlib import Path
import argparse
import hashlib
import itertools
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
GOPLS = '/home/define42/go/bin/gopls'
BENCHSTAT = '/home/define42/go/bin/benchstat'
ORIGINAL_COMMIT = '641af9e6216055b6a9d918bdcba4ea7d40020750'
PACKAGES = ['./pkg/h264/internal/common', './pkg/h264/internal/encoder',
            './pkg/h264/internal/processing', './internal/rdp/avc']
REPETITIONS, CODEC_FRAMES, FULL_FRAMES, WARMUP = 6, 80, 60, 16


def sha(path):
    with Path(path).open('rb') as f:
        return hashlib.file_digest(f, 'sha256').hexdigest()


def json_write(path, value):
    Path(path).write_text(json.dumps(value, indent=2) + '\n')


def source_map(path):
    """Fingerprint all copied source/fixture files, not just changed Go files."""
    result = {}
    for prefix in ['pkg', 'internal']:
        for p in sorted((path / prefix).rglob('*')):
            if p.is_file():
                result[str(p.relative_to(path))] = sha(p)
    for name in ['go.mod', 'go.sum']:
        result[name] = sha(path / name)
    return result


def write_source_manifest(path, files):
    Path(path).write_text(''.join(f'{digest}  {name}\n' for name, digest in sorted(files.items())))


def clean_environment():
    env = os.environ.copy()
    for key in list(env):
        if key.startswith(('COMPARE_', 'CAVLC_', 'H264_CAVLC_', 'H264_VIDEO_')) or key in {
            'GODEBUG', 'GOFLAGS', 'GOOS', 'GOARCH', 'GOWORK', 'CAVLC_NATIVE_BINARY',
        }:
            env.pop(key)
    env.update(PATH='/snap/go/current/bin:' + env.get('PATH', ''),
               CGO_ENABLED='0', GOMAXPROCS='4', GOOS='linux', GOARCH='amd64', GOWORK='off')
    return env


def parse_json_stream(text):
    decoder = json.JSONDecoder()
    while text.strip():
        text = text.lstrip()
        item, end = decoder.raw_decode(text)
        yield item
        text = text[end:]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source', type=Path, default=ROOT / 'round12-next')
    parser.add_argument('--output', type=Path, default=ROOT / 'round12-review')
    args = parser.parse_args()
    selected, output = args.source.resolve(), args.output.resolve()
    prior_source, original_source = ROOT / 'round11', ROOT / 'before'
    prior_binary, original_binary, native_binary = ROOT / 'round11-avc.test', ROOT / 'before-avc.test', ROOT / 'native_bench'
    native_build_root = Path('/tmp/openh264-comparison-20261021/native')
    native_archive = native_build_root / 'libopenh264.a'
    native_harness = Path('/tmp/h264-native-parity-20261021/native_bench.cpp')
    native_build_command = Path('/tmp/h264-native-parity-20261021/build-native.txt')
    cases = json.loads((ROOT / 'cases.json').read_text())
    actual_cases = [(c['name'], c['width'], c['height'], c['qp']) for c in cases]
    assert actual_cases == [('ShortClip', 1920, 1080, 20), ('Road', 1920, 1080, 20),
                            ('Road', 1920, 1080, 32), ('PDF', 1920, 1080, 20),
                            ('Road', 1280, 720, 20)], 'Expected the five frozen configurations'
    full_cases = [('ShortClip', 'Clip', Path('/tmp/h264-fullscreen-20261021/clip1080p.rgba')),
                  ('Road', 'Clip', ROOT / 'road1080.rgba'), ('Dense', 'Dense', None)]
    required = [selected / 'go.mod', prior_source / 'go.mod', original_source / 'go.mod',
                prior_binary, original_binary, native_binary, native_archive, native_harness, native_build_command,
                ROOT / 'compare_test.go', ROOT / 'fullscreen_test.go']
    required += [Path(case['file']) for case in cases]
    required += [path for _, _, path in full_cases if path is not None]
    for case in cases:
        for slices in [1, 4]:
            key = case_key(case, slices)
            required += [ROOT / 'parity' / f'{key}-{kind}.264' for kind in ['before', 'native']]
    for path in required:
        if not path.is_file():
            raise SystemExit(f'Missing prerequisite: {path}')
    with native_binary.open('rb') as f:
        if f.read(4) != b'\x7fELF':
            raise SystemExit('Native benchmark must resolve to the frozen ELF; inspect any new wrapper explicitly')
    if str(native_archive) not in native_build_command.read_text():
        raise SystemExit('Native archive does not match recorded link command')
    if output == selected or output.is_relative_to(selected) or selected.is_relative_to(output):
        raise SystemExit('Output and selected source must be disjoint directories')
    output.mkdir(exist_ok=False)
    for name in ['validation', 'parity', 'codec', 'full', 'bin', 'harness']:
        (output / name).mkdir()
    shutil.copy2(Path(__file__), output / 'measure_round12.py')
    shutil.copy2(ROOT / 'cases.json', output / 'cases.json')
    for name in ['compare_test.go', 'fullscreen_test.go']:
        shutil.copy2(ROOT / name, output / 'harness' / name)
    shutil.copy2(native_harness, output / 'harness/native_bench.cpp')
    shutil.copy2(native_build_command, output / 'harness/native-build-command.txt')
    for name in ['round11-baseline-audit.json', 'round11-whitespace-cleanup.json']:
        if (ROOT / name).is_file():
            shutil.copy2(ROOT / name, output / name)
    native_source_files = {str(p.relative_to(native_build_root)): sha(p)
                           for p in sorted((native_build_root / 'codec').rglob('*'))
                           if p.is_file() and p.suffix in {'.c', '.cc', '.cpp', '.h', '.hpp', '.asm', '.S', '.inc'}}
    for p in [native_build_root / 'Makefile', *sorted((native_build_root / 'build').glob('*.mk'))]:
        native_source_files[str(p.relative_to(native_build_root))] = sha(p)
    write_source_manifest(output / 'native-sources.sha256', native_source_files)
    source = output / 'source'
    source.mkdir()
    selected_files = source_map(selected)
    for name in ['pkg', 'internal']:
        shutil.copytree(selected / name, source / name)
    for name in ['go.mod', 'go.sum']:
        shutil.copy2(selected / name, source / name)
    current_files = source_map(source)
    assert current_files == selected_files, 'Source changed while snapshotting'
    prior_files, original_files = source_map(prior_source), source_map(original_source)
    for name, files in [('current', current_files), ('round11', prior_files), ('original', original_files)]:
        write_source_manifest(output / f'{name}-sources.sha256', files)
    changes = [{'path': name, 'status': 'added' if name not in prior_files else 'deleted' if name not in current_files else 'modified',
                'before_sha256': prior_files.get(name), 'after_sha256': current_files.get(name)}
               for name in sorted(set(prior_files) | set(current_files)) if prior_files.get(name) != current_files.get(name)]
    json_write(output / 'changes-vs-round11.json', changes)
    changed_go = [x['path'] for x in changes if x['path'].endswith('.go') and x['status'] != 'deleted']
    env = clean_environment()
    commands = []

    def run(label, command, *, cwd=source, extra=None, logfile=None):
        effective = env | (extra or {})
        print(label, flush=True)
        started = time.time()
        result = subprocess.run([str(x) for x in command], cwd=cwd, env=effective,
                                text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        log = logfile or output / 'validation' / f'{label}.txt'
        Path(log).write_text(result.stdout)
        record = {'name': label, 'command': [str(x) for x in command], 'cwd': str(cwd),
                  'environment_override': extra or {}, 'exit_code': result.returncode,
                  'elapsed_seconds': time.time() - started, 'log': str(Path(log).relative_to(output))}
        commands.append(record)
        json_write(output / 'commands.json', commands)
        if result.returncode:
            raise SystemExit(f'{label} failed; see {log}')
        return result.stdout

    tracked = {str(path): sha(path) for path in sorted({p.resolve() for p in required})}
    tracked[str(ROOT / 'cases.json')] = sha(ROOT / 'cases.json')
    provenance = {'selected_source': str(selected), 'snapshot': str(source),
                  'original_source': str(original_source), 'original_commit': ORIGINAL_COMMIT,
                  'round11_source': str(prior_source), 'original_binary': str(original_binary),
                  'round11_binary': str(prior_binary), 'native_binary': str(native_binary.resolve()),
                  'native_entry_path': str(native_binary),
                  'native_source_tree': str(native_build_root),
                  'native_static_archive': {'path': str(native_archive), 'sha256': sha(native_archive)},
                  'native_linkage': 'OpenH264 statically linked from the recorded archive; system C/C++ runtime remains dynamic',
                  'input_sha256': tracked, 'base_environment': {key: env[key] for key in ['CGO_ENABLED', 'GOMAXPROCS', 'GOOS', 'GOARCH', 'GOWORK']},
                  'timing': {'repetitions': REPETITIONS, 'codec_measured_frames': CODEC_FRAMES,
                             'full_measured_frames': FULL_FRAMES, 'warmup_frames': WARMUP,
                             'one_slice_cpu': '2', 'four_slice_cpus': '0-3', 'stream_capture_in_timing': False},
                  'scope': {'codec': 'round11/current/native, ten cases',
                            'full': 'original/round11/current, six FullDamage EncodeDamage cases',
                            'full_excludes': ['capture', 'socket write', 'ACK wait', 'presentation']}}
    json_write(output / 'provenance.json', provenance)
    run('go-version', [GO, 'version'])
    run('host', ['uname', '-a'])
    run('native-commit', ['git', '-C', '/home/define42/git/openh264', 'rev-parse', 'HEAD'])
    dynamic = run('native-dynamic-dependencies', ['readelf', '-d', native_binary.resolve()])
    if 'Shared library: [libopenh264' in dynamic:
        raise SystemExit('Native OpenH264 linkage changed; record the actual shared library before timing')
    run('ffmpeg-version', ['ffmpeg', '-version'])
    run('ffprobe-version', ['ffprobe', '-version'])
    if changed_go:
        formatting = run('gofmt-changed', ['/snap/go/current/bin/gofmt', '-l', *changed_go])
        if formatting.strip():
            raise SystemExit('Selected changed Go files need formatting; no source was modified')
    run('tests', [GO, 'test', '-count=1', './pkg/h264/...', './internal/rdp/avc'])
    run('race', [GO, 'test', '-race', '-count=1', *PACKAGES], extra={'CGO_ENABLED': '1'})
    run('fallback-avx2', [GO, 'test', '-count=1', *PACKAGES], extra={'GODEBUG': 'cpu.avx2=off'})
    run('fallback-all', [GO, 'test', '-count=1', *PACKAGES], extra={'GODEBUG': 'cpu.all=off'})
    for package in PACKAGES:
        name = package.rsplit('/', 1)[-1]
        run('arm64-' + name, [GO, 'test', '-c', '-o', output / 'bin' / f'arm64-{name}.test', package],
            extra={'GOARCH': 'arm64', 'CGO_ENABLED': '0'})

    # Source comparison, not Git staging, determines this round's actual files.
    # Check each changed Go file in a build context that includes it; common
    # files need only one check. All package tests above still cover the tree.
    remaining = set(changed_go)
    gopls_records = []
    for arch in ['amd64', 'arm64']:
        listed = run('go-list-' + arch, [GO, 'list', '-json', './pkg/h264/...', './internal/rdp/avc'], extra={'GOARCH': arch})
        visible = set()
        for package in parse_json_stream(listed):
            for field in ['GoFiles', 'CgoFiles', 'TestGoFiles', 'XTestGoFiles']:
                for name in package.get(field, []):
                    visible.add(str((Path(package['Dir']) / name).relative_to(source)))
        chosen = sorted(remaining & visible)
        if chosen:
            run('gopls-' + arch, [GOPLS, 'check', *chosen], extra={'GOARCH': arch})
        gopls_records.append({'architecture': arch, 'files': chosen})
        remaining -= set(chosen)
    json_write(output / 'gopls-scope.json', {'changed_go': changed_go, 'checks': gopls_records, 'uncovered': sorted(remaining)})
    if remaining:
        raise SystemExit('Changed Go files outside checked build contexts: ' + ', '.join(sorted(remaining)))

    current_binary = output / 'bin' / 'current-avc.test'
    overlay = {'Replace': {str(source / 'internal/rdp/avc/z_openh264_comparison_test.go'): str(output / 'harness/compare_test.go'),
                           str(source / 'internal/rdp/avc/z_fullscreen_review_test.go'): str(output / 'harness/fullscreen_test.go')}}
    json_write(output / 'overlay.json', overlay)
    run('build-comparison', [GO, 'test', '-c', '-overlay=' + str(output / 'overlay.json'), '-o', current_binary, './internal/rdp/avc'])
    binaries = {'original': original_binary, 'round11': prior_binary, 'current': current_binary, 'native': native_binary}
    binary_hashes = {kind: {'path': str(path.resolve()), 'sha256': sha(path)} for kind, path in binaries.items()}
    json_write(output / 'binary-hashes.json', binary_hashes)
    json_write(output / 'validation.json', commands.copy())

    def codec_call(kind, case, slices, count, log, stream=None):
        settings = {'COMPARE_RUN': '1', 'COMPARE_CASE': case['name'], 'COMPARE_I420': case['file'],
                    'COMPARE_WIDTH': str(case['width']), 'COMPARE_HEIGHT': str(case['height']),
                    'COMPARE_QP': str(case['qp']), 'COMPARE_SLICES': str(slices), 'COMPARE_FRAMES': str(count)}
        if kind == 'native':
            command = [native_binary, case['file'], str(slices), str(count)]
            if stream is not None:
                command.append(stream)
        else:
            command = [binaries[kind], '-test.run=^TestOpenH264Comparison$', '-test.count=1']
            if stream is not None:
                settings['COMPARE_STREAM'] = str(stream)
        command = ['taskset', '-c', '2' if slices == 1 else '0-3', *command]
        return run(log.stem, command, extra=settings, logfile=log)

    parity = []
    for case in cases:
        for slices in [1, 4]:
            key = case_key(case, slices)
            stream = output / 'parity' / f'{key}-current.264'
            codec_call('current', case, slices, CODEC_FRAMES, output / 'parity' / f'{key}-current.log', stream)
            references = {kind: ROOT / 'parity' / f'{key}-{old}.264' for kind, old in [('original', 'before'), ('native', 'native')]}
            hashes = {kind: sha(path) for kind, path in references.items()} | {'current': sha(stream)}
            if len(set(hashes.values())) != 1:
                raise SystemExit(f'Stream parity failed for {key}: {hashes}')
            decoded = run('decode-' + key, ['ffmpeg', '-v', 'error', '-xerror', '-threads', '1', '-i', stream, '-f', 'null', '-'],
                          logfile=output / 'parity' / f'{key}-decode.log')
            if decoded.strip():
                raise SystemExit(f'FFmpeg reported decoder errors for {key}')
            count = run('probe-' + key, ['ffprobe', '-v', 'error', '-count_frames', '-select_streams', 'v:0',
                        '-show_entries', 'stream=nb_read_frames', '-of', 'csv=p=0', stream],
                        logfile=output / 'parity' / f'{key}-probe.log').strip()
            if int(count) != CODEC_FRAMES + WARMUP:
                raise SystemExit(f'Wrong decoded frame count for {key}: {count}')
            parity.append({'case': key, 'frames': int(count), 'bytes': stream.stat().st_size, 'sha256': hashes,
                           'saved_reference_paths': {k: str(v) for k, v in references.items()}, 'equal': True})
            json_write(output / 'parity' / 'parity.json', parity)
    assert len(parity) == 10 and sum(row['frames'] for row in parity) == 960
    (output / 'parity' / 'parity.txt').write_text(''.join(f"{row['case']}: original=current=native bytes={row['bytes']} sha256={row['sha256']['current']} decoded_frames={row['frames']}\n" for row in parity))

    codec_rows = {kind: [] for kind in ['round11', 'current', 'native']}
    orders = list(itertools.permutations(codec_rows))
    for rep in range(REPETITIONS):
        for case in cases:
            for slices in [1, 4]:
                key = case_key(case, slices)
                for kind in orders[rep]:
                    log = output / 'codec' / f'{key}-{rep + 1}-{kind}.log'
                    result = codec_call(kind, case, slices, CODEC_FRAMES, log)
                    row = benchmark_row(result, 'BenchmarkOpenH264/', CODEC_FRAMES)
                    expected = f"BenchmarkOpenH264/{case['name']}/{case['width']}x{case['height']}/QP{case['qp']}/{slices}Slices-4"
                    assert row.split()[0] == expected, f'Unexpected case/actual slices: {row}'
                    codec_rows[kind].append(row)
                    with (output / 'codec' / f'{kind}.txt').open('a') as f:
                        f.write(row + '\n')
                print(f'Codec triple {rep + 1}/6: {key}', flush=True)

    full_rows = {kind: [] for kind in ['original', 'round11', 'current']}
    orders = list(itertools.permutations(full_rows))
    for rep in range(REPETITIONS):
        for name, fixture, path in full_cases:
            for slices in [1, 4]:
                for kind in orders[rep]:
                    pattern = f'^BenchmarkFullscreenVideo$/^{fixture}$/^1080p$/^{slices}Slices$/^FullDamage$'
                    command = ['taskset', '-c', '2' if slices == 1 else '0-3', binaries[kind], '-test.run=^$',
                               '-test.bench=' + pattern, '-test.benchtime=60x', '-test.count=1']
                    log = output / 'full' / f'{name}-{slices}s-{rep + 1}-{kind}.log'
                    result = run(log.stem, command, extra={'H264_VIDEO_RGBA': str(path) if path else ''}, logfile=log)
                    row = benchmark_row(result, 'BenchmarkFullscreenVideo/', FULL_FRAMES).replace('/' + fixture + '/', '/' + name + '/')
                    expected = f'BenchmarkFullscreenVideo/{name}/1080p/{slices}Slices/FullDamage-4'
                    assert row.split()[0] == expected, f'Unexpected full case/actual slices: {row}'
                    full_rows[kind].append(row)
                    with (output / 'full' / f'{kind}.txt').open('a') as f:
                        f.write(row + '\n')
                print(f'Full EncodeDamage triple {rep + 1}/6: {name}/{slices}s', flush=True)

    for group, rows, expected in [('codec', codec_rows, 10), ('full', full_rows, 6)]:
        summary = summarize(rows, expected)
        json_write(output / group / 'summary.json', summary)
    comparisons = [('codec', 'benchstat', ['round11', 'current', 'native']),
                   ('codec', 'native-benchstat', ['current', 'native']),
                   ('full', 'benchstat', ['original', 'round11', 'current']),
                   ('full', 'incremental-benchstat', ['round11', 'current']),
                   ('full', 'cumulative-benchstat', ['original', 'current'])]
    for group, label, kinds in comparisons:
        run(group + '-' + label, [BENCHSTAT, *[output / group / f'{kind}.txt' for kind in kinds]],
            logfile=output / group / (label + '.txt'))
    assert source_map(source) == current_files, 'Snapshot source changed during run'
    assert source_map(selected) == selected_files, 'Selected source changed during run'
    assert source_map(prior_source) == prior_files, 'Frozen round11 source changed during run'
    assert source_map(original_source) == original_files, 'Original source changed during run'
    for relative, digest in native_source_files.items():
        assert sha(native_build_root / relative) == digest, f'Native source changed: {relative}'
    for path, digest in tracked.items():
        assert sha(path) == digest, f'Input changed during run: {path}'
    for kind, path in binaries.items():
        assert sha(path) == binary_hashes[kind]['sha256'], f'Binary changed during run: {kind}'
    json_write(output / 'complete.json', {'complete': True, 'decoded_frames': 960,
               'codec_cases': 10, 'codec_samples_per_kind': 60, 'full_cases': 6, 'full_samples_per_kind': 36,
               'source_and_inputs_stable': True, 'selected_source': str(selected), 'no_main_merge': True})
    print('Round12 isolated validation/parity/timing complete: ' + str(output), flush=True)


def case_key(case, slices):
    return f"{case['name']}-{case['width']}x{case['height']}-qp{case['qp']}-{slices}s"


def benchmark_row(text, prefix, iterations):
    rows = [line for line in text.splitlines() if line.startswith('Benchmark')]
    if len(rows) != 1 or not rows[0].startswith(prefix):
        raise SystemExit(f'Unexpected benchmark row: {rows}')
    fields = rows[0].split()
    if int(fields[1]) != iterations:
        raise SystemExit(f'Unexpected measured frame count: {rows[0]}')
    return rows[0]


def summarize(rows, expected_cases):
    parsed = {}
    for kind, lines in rows.items():
        per_case = {}
        for line in lines:
            fields = line.split()
            metrics = {fields[i + 1]: float(fields[i]) for i in range(2, len(fields) - 1, 2)}
            if any(not math.isfinite(value) for value in metrics.values()):
                raise SystemExit(f'Non-finite metric: {line}')
            per_case.setdefault(fields[0], []).append(metrics)
        assert len(per_case) == expected_cases and all(len(values) == REPETITIONS for values in per_case.values())
        parsed[kind] = per_case
    names = list(next(iter(parsed.values())))
    assert all(set(values) == set(names) for values in parsed.values())
    result = []
    for name in names:
        medians = {kind: {unit: statistics.median(sample[unit] for sample in values[name]) for unit in values[name][0]}
                   for kind, values in parsed.items()}
        # All retained changes preserve output; byte counts must remain equal.
        sizes = {sample['bytes/frame'] for values in parsed.values() for sample in values[name]}
        assert len(sizes) == 1, f'Output byte counts differ for {name}: {sizes}'
        current, prior = medians['current'], medians['round11']
        record = {'name': name, 'samples': {kind: len(values[name]) for kind, values in parsed.items()}, 'medians': medians,
                  'current_vs_round11_wall_percent': 100 * (current['ns/op'] / prior['ns/op'] - 1),
                  'current_vs_round11_cpu_percent': 100 * (current['cpu_ns/frame'] / prior['cpu_ns/frame'] - 1)}
        if 'native' in medians:
            record['current_vs_native_wall_ratio'] = current['ns/op'] / medians['native']['ns/op']
            record['current_vs_native_cpu_ratio'] = current['cpu_ns/frame'] / medians['native']['cpu_ns/frame']
        if 'original' in medians:
            record['current_vs_original_wall_percent'] = 100 * (current['ns/op'] / medians['original']['ns/op'] - 1)
        result.append(record)
    return result


if __name__ == '__main__':
    main()
