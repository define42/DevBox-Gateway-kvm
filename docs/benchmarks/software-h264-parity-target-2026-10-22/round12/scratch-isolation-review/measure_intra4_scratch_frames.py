#!/usr/bin/env python3
"""Isolated scratch frame gate: saved-stream parity, then round11/full-path pairs.

Run only from the root agent's serialized CPU queue. This script never writes the
main tree, round11 snapshot/binary, or existing parity/full/measure directories.
It refuses to reuse its output directory so prior evidence cannot be overwritten.
"""
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import statistics
import subprocess

ROOT = Path(__file__).resolve().parent
OUT = ROOT / 'intra4-scratch-frame-review'
CANDIDATE = ROOT / 'intra4-scratch-isolation-next'
BASELINE = ROOT / 'round11-avc.test'
GO = '/snap/go/current/bin/go'
BENCHSTAT = '/home/define42/go/bin/benchstat'
REPETITIONS, FULL_FRAMES, WARMUP, PARITY_FRAMES = 6, 60, 16, 80
FULL_CASES = [
    ('ShortClip', 'Clip', '/tmp/h264-fullscreen-20261021/clip1080p.rgba'),
    ('Road', 'Clip', str(ROOT / 'road1080.rgba')),
    ('Dense', 'Dense', ''),
]
ENV = os.environ | {'PATH': '/snap/go/current/bin:' + os.environ['PATH'],
                    'CGO_ENABLED': '0', 'GOMAXPROCS': '4'}
# Prevent inherited stream capture or comparison selectors contaminating timing.
for key in list(ENV):
    if key.startswith('COMPARE_') or key == 'H264_VIDEO_RGBA':
        del ENV[key]


def digest(path):
    with path.open('rb') as f:
        return hashlib.file_digest(f, 'sha256').hexdigest()


def run_logged(command, log, *, env=ENV, cwd=None):
    result = subprocess.run(command, cwd=cwd, env=env, text=True,
                            stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
    log.write_text(result.stdout)
    if result.returncode:
        raise RuntimeError(f'Command exited {result.returncode}; see {log}: {command}')
    return result.stdout


def summarize_full(full, statistics_file):
    values = {}
    for kind in ['before', 'after']:
        rows = {}
        for line in (full / f'{kind}.txt').read_text().splitlines():
            parts = line.split()
            name = parts[0].removeprefix('Benchmark')
            bucket = rows.setdefault(name, {})
            for i in range(2, len(parts) - 1, 2):
                bucket.setdefault(parts[i + 1], []).append(float(parts[i]))
        assert len(rows) == 6, (kind, rows.keys())
        assert all(len(samples) == REPETITIONS for metrics in rows.values()
                   for samples in metrics.values())
        values[kind] = {name: {unit: statistics.median(samples)
                              for unit, samples in metrics.items()}
                        for name, metrics in rows.items()}
    pvalues = {}
    for line in statistics_file.read_text().splitlines():
        if line.startswith('FullscreenVideo/'):
            match = re.search(r'p=([0-9.]+)', line)
            if match:
                # The first section is wall sec/op, subsequent sections other metrics.
                pvalues.setdefault(line.split()[0], float(match.group(1)))
    summary = []
    assert values['before'].keys() == values['after'].keys()
    for name, before in values['before'].items():
        after = values['after'][name]
        summary.append({'case': name, 'samples_per_version': REPETITIONS,
                        'before': before, 'after': after,
                        'wall_change_percent': 100 * (after['ns/op'] / before['ns/op'] - 1),
                        'p': pvalues[name]})
    (OUT / 'full-summary.json').write_text(json.dumps(summary, indent=2) + '\n')
    for row in summary:
        print(f"{row['case']}: {row['before']['ns/op']/1e6:.3f} -> "
              f"{row['after']['ns/op']/1e6:.3f} ms "
              f"({row['wall_change_percent']:+.2f}%, p={row['p']:.3f})", flush=True)


def main():
    assert BASELINE.is_file(), 'Frozen round11 AVC binary is required; never use original here'
    assert CANDIDATE.is_dir()
    cases = json.loads((ROOT / 'cases.json').read_text())
    assert len(cases) == 5
    expected_parity = {}
    for line in (ROOT / 'parity/parity.txt').read_text().splitlines():
        match = re.fullmatch(r'([^:]+): before=after=native bytes=(\d+) '
                             r'sha256=([a-f0-9]{64}) decoded_frames=(\d+)', line)
        assert match, line
        expected_parity[match[1]] = {'bytes': int(match[2]), 'sha256': match[3],
                                    'frames': int(match[4])}
    assert len(expected_parity) == 10
    for case in cases:
        assert Path(case['file']).is_file(), case
        for slices in [1, 4]:
            name = f"{case['name']}-{case['width']}x{case['height']}-qp{case['qp']}-{slices}s"
            assert expected_parity[name]['frames'] == PARITY_FRAMES + WARMUP
            for kind in ['before', 'native']:
                assert (ROOT / 'parity' / f'{name}-{kind}.264').is_file()
    for _, _, fixture in FULL_CASES:
        if fixture:
            assert Path(fixture).is_file(), fixture
    # mkdir without exist_ok intentionally protects any previous gate evidence.
    OUT.mkdir()
    parity, full = OUT / 'parity', OUT / 'full'
    parity.mkdir()
    full.mkdir()
    baseline_hash = digest(BASELINE)
    overlay = {'Replace': {}}
    overlay_hashes = {}
    exact_overlay = json.loads((ROOT / 'round11-overlay.json').read_text())['Replace']
    assert len(exact_overlay) == 2
    for old_target, source in exact_overlay.items():
        source = Path(source)
        assert source.name in {'compare_test.go', 'fullscreen_test.go'}
        destination = OUT / source.name
        shutil.copyfile(source, destination)
        overlay['Replace'][str(CANDIDATE / 'internal/rdp/avc' / Path(old_target).name)] = str(destination)
        overlay_hashes[source.name] = digest(destination)
    assert set(overlay_hashes) == {'compare_test.go', 'fullscreen_test.go'}
    overlay_file = OUT / 'candidate-overlay.json'
    overlay_file.write_text(json.dumps(overlay, indent=2) + '\n')
    candidate_binary = OUT / 'candidate-avc.test'
    print('Build isolated scratch candidate with unchanged round11 overlays', flush=True)
    run_logged([GO, 'test', '-c', '-overlay=' + str(overlay_file), '-o',
                str(candidate_binary), './internal/rdp/avc'], OUT / 'build.txt', cwd=CANDIDATE)
    source_rows = [digest(f) + '  ' + str(f.relative_to(CANDIDATE))
                   for f in sorted((CANDIDATE / 'pkg/h264').rglob('*')) if f.is_file()]
    (OUT / 'candidate-sources.sha256').write_text('\n'.join(source_rows) + '\n')
    metadata = {
        'baseline': str(BASELINE), 'baseline_scope': 'frozen round11, not original',
        'baseline_sha256': baseline_hash, 'candidate': str(CANDIDATE),
        'candidate_binary_sha256': digest(candidate_binary),
        'overlay_sha256': overlay_hashes, 'source_files': len(source_rows),
        'repetitions': REPETITIONS, 'full_frames': FULL_FRAMES, 'warmup_frames': WARMUP,
        'parity_frames_per_case': PARITY_FRAMES + WARMUP,
        'full_framerate': 30, 'codec_preflight_framerate': 60,
        'GOMAXPROCS': 4, 'CGO_ENABLED': 0, 'affinity_one_slice': '2',
        'affinity_four_slices': '0-3', 'saved_streams': str(ROOT / 'parity'),
        'preflight': 'candidate matches saved original/native SHA256 and FFmpeg+ffprobe960frames',
    }
    (OUT / 'provenance.json').write_text(json.dumps(metadata, indent=2) + '\n')
    print('Preflight candidate against saved original/native streams; decode960frames', flush=True)
    parity_records = []
    with (parity / 'parity.txt').open('w') as report:
        for case in cases:
            for slices in [1, 4]:
                name = f"{case['name']}-{case['width']}x{case['height']}-qp{case['qp']}-{slices}s"
                expected = expected_parity[name]
                old_streams = {kind: ROOT / 'parity' / f'{name}-{kind}.264'
                               for kind in ['before', 'native']}
                hashes = {kind: digest(path) for kind, path in old_streams.items()}
                assert all(value == expected['sha256'] for value in hashes.values()), (name, hashes)
                assert all(path.stat().st_size == expected['bytes'] for path in old_streams.values())
                stream = parity / f'{name}-after.264'
                env = ENV | {'COMPARE_RUN': '1', 'COMPARE_CASE': case['name'],
                             'COMPARE_I420': case['file'], 'COMPARE_WIDTH': str(case['width']),
                             'COMPARE_HEIGHT': str(case['height']), 'COMPARE_QP': str(case['qp']),
                             'COMPARE_SLICES': str(slices), 'COMPARE_FRAMES': str(PARITY_FRAMES),
                             'COMPARE_STREAM': str(stream)}
                run_logged(['taskset', '-c', '2' if slices == 1 else '0-3', str(candidate_binary),
                            '-test.run=^TestOpenH264Comparison$', '-test.count=1'],
                           parity / f'{name}-after.log', env=env)
                hashes['after'] = digest(stream)
                assert hashes['after'] == expected['sha256'] and stream.stat().st_size == expected['bytes'], (name, hashes)
                output = run_logged(['ffmpeg', '-v', 'error', '-xerror', '-threads', '1', '-i',
                                     str(stream), '-f', 'null', '-'], parity / f'{name}-decode.log')
                assert not output, f'Unexpected decoder output: {name}: {output}'
                probe = run_logged(['ffprobe', '-v', 'error', '-count_frames', '-select_streams', 'v:0',
                                    '-show_entries', 'stream=nb_read_frames', '-of', 'csv=p=0', str(stream)],
                                   parity / f'{name}-probe.log').strip()
                assert int(probe) == PARITY_FRAMES + WARMUP, (name, probe)
                parity_records.append({'case': name, 'hashes': hashes, 'bytes': stream.stat().st_size,
                                       'decoded_frames': int(probe),
                                       'reference_paths': {k: str(v) for k, v in old_streams.items()}})
                line = f"{name}: before=after=native bytes={expected['bytes']} sha256={hashes['after']} decoded_frames={probe}\n"
                report.write(line)
                report.flush()
                print(line, end='', flush=True)
    assert sum(row['decoded_frames'] for row in parity_records) == 960
    (parity / 'parity.json').write_text(json.dumps(parity_records, indent=2) + '\n')
    for kind in ['before', 'after']:
        (full / f'{kind}.txt').write_text('')
    binaries = {'before': BASELINE, 'after': candidate_binary}
    print('Six alternating complete EncodeDamage pairs, round11 versus scratch', flush=True)
    for repetition in range(REPETITIONS):
        for name, fixture, path in FULL_CASES:
            for slices in [1, 4]:
                for kind in (['before', 'after'] if repetition % 2 == 0 else ['after', 'before']):
                    pattern = f'^BenchmarkFullscreenVideo$/^{fixture}$/^1080p$/^{slices}Slices$/^FullDamage$'
                    env = ENV | {'H264_VIDEO_RGBA': path}
                    output = run_logged(['taskset', '-c', '2' if slices == 1 else '0-3', str(binaries[kind]),
                                         '-test.run=^$', '-test.bench=' + pattern,
                                         f'-test.benchtime={FULL_FRAMES}x', '-test.count=1'],
                                        full / f'{name}-{slices}s-{repetition+1}-{kind}.log', env=env)
                    rows = [line.replace('/' + fixture + '/', '/' + name + '/')
                            for line in output.splitlines() if line.startswith('Benchmark')]
                    assert len(rows) == 1, (name, slices, kind, rows)
                    expected_name = f'BenchmarkFullscreenVideo/{name}/1080p/{slices}Slices/FullDamage-4'
                    assert rows[0].split()[0] == expected_name and int(rows[0].split()[1]) == FULL_FRAMES, rows
                    with (full / f'{kind}.txt').open('a') as aggregate:
                        aggregate.write(rows[0] + '\n')
                print(f'Completed full pair {repetition+1}/{REPETITIONS}: {name}/{slices}s', flush=True)
    statistics_file = OUT / 'full-benchstat.txt'
    run_logged([BENCHSTAT, str(full / 'before.txt'), str(full / 'after.txt')], statistics_file)
    summarize_full(full, statistics_file)
    assert digest(BASELINE) == baseline_hash, 'Frozen baseline changed during the gate'
    (OUT / 'complete.json').write_text(json.dumps({'parity_cases': len(parity_records),
                                                  'decoded_frames': 960, 'full_cases': 6,
                                                  'pairs_per_case': REPETITIONS,
                                                  'baseline_unchanged': True}, indent=2) + '\n')
    print('Scratch full-frame gate complete; original round11 evidence untouched', flush=True)


if __name__ == '__main__':
    main()
