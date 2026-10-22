#!/usr/bin/env python3
"""Distinct-scope codec gate: frozen round11 versus isolated scratch, no native runs.

Root executes this only through its serialized CPU queue. The completed neutral
full EncodeDamage gate remains intact and is not superseded by this codec scope.
The output directory must be new; no sources, binaries or earlier evidence change.
"""
import hashlib
import json
import os
from pathlib import Path
import re
import statistics
import subprocess

ROOT = Path(__file__).resolve().parent
OUT = ROOT / 'intra4-scratch-codec-review'
FRAME_GATE = ROOT / 'intra4-scratch-frame-review'
BASELINE = ROOT / 'round11-avc.test'
CANDIDATE = FRAME_GATE / 'candidate-avc.test'
BENCHSTAT = '/home/define42/go/bin/benchstat'
REPETITIONS, FRAMES, WARMUP = 6, 80, 16
ENV = os.environ | {'GOMAXPROCS': '4', 'CGO_ENABLED': '0'}
for key in list(ENV):
    if key.startswith('COMPARE_') or key == 'H264_VIDEO_RGBA':
        del ENV[key]


def digest(path):
    with path.open('rb') as f:
        return hashlib.file_digest(f, 'sha256').hexdigest()


def run_logged(command, log, env):
    result = subprocess.run(command, env=env, text=True,
                            stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
    log.write_text(result.stdout)
    if result.returncode:
        raise RuntimeError(f'Command exited {result.returncode}; see {log}: {command}')
    return result.stdout


def summarize():
    values = {}
    for kind in ['before', 'after']:
        rows = {}
        for line in (OUT / f'{kind}.txt').read_text().splitlines():
            parts = line.split()
            name = parts[0].removeprefix('Benchmark')
            metrics = rows.setdefault(name, {})
            for i in range(2, len(parts) - 1, 2):
                metrics.setdefault(parts[i + 1], []).append(float(parts[i]))
        assert len(rows) == 10, (kind, rows.keys())
        assert all(len(samples) == REPETITIONS for metrics in rows.values()
                   for samples in metrics.values())
        values[kind] = {name: {unit: statistics.median(samples)
                              for unit, samples in metrics.items()}
                        for name, metrics in rows.items()}
    pvalues = {}
    for line in (OUT / 'benchstat.txt').read_text().splitlines():
        if line.startswith('OpenH264/'):
            match = re.search(r'p=([0-9.]+)', line)
            if match:
                # Wall sec/op is first; later CPU/bytes/p95 sections are distinct.
                pvalues.setdefault(line.split()[0], float(match.group(1)))
    assert values['before'].keys() == values['after'].keys()
    summary = []
    for name, before in values['before'].items():
        after = values['after'][name]
        summary.append({'case': name, 'samples_per_version': REPETITIONS,
                        'before': before, 'after': after,
                        'wall_change_percent': 100 * (after['ns/op'] / before['ns/op'] - 1),
                        'cpu_change_percent': 100 * (after['cpu_ns/frame'] / before['cpu_ns/frame'] - 1),
                        'p': pvalues[name]})
    (OUT / 'summary.json').write_text(json.dumps(summary, indent=2) + '\n')
    for row in summary:
        print(f"{row['case']}: {row['before']['ns/op']/1e6:.3f} -> "
              f"{row['after']['ns/op']/1e6:.3f} ms "
              f"({row['wall_change_percent']:+.2f}%, p={row['p']:.3f}); "
              f"CPU {row['cpu_change_percent']:+.2f}%", flush=True)


def main():
    cases = json.loads((ROOT / 'cases.json').read_text())
    assert len(cases) == 5
    assert len({(c['name'], c['width'], c['height'], c['qp']) for c in cases}) == 5
    for case in cases:
        assert Path(case['file']).is_file(), case
    assert BASELINE.is_file() and CANDIDATE.is_file()
    prior = json.loads((FRAME_GATE / 'provenance.json').read_text())
    completed = json.loads((FRAME_GATE / 'complete.json').read_text())
    assert completed['parity_cases'] == 10 and completed['decoded_frames'] == 960
    assert completed['full_cases'] == 6 and completed['pairs_per_case'] == 6
    before_hash, after_hash = digest(BASELINE), digest(CANDIDATE)
    assert before_hash == prior['baseline_sha256'], 'Frozen round11 binary changed'
    assert after_hash == prior['candidate_binary_sha256'], 'Use the already validated scratch binary'
    full_summary = FRAME_GATE / 'full-summary.json'
    assert len(json.loads(full_summary.read_text())) == 6
    full_hash = digest(full_summary)
    # Refuse all overwrites, including previous partial runs of this distinct scope.
    OUT.mkdir()
    metadata = {
        'scope': 'Codec encode and owning payload; no RGB conversion, initialization, capture, network or presentation',
        'reason': 'Test transfer of I4 worker gain to codec including PDF/720p/QP32; retain prior neutral full-path results',
        'baseline': str(BASELINE), 'baseline_scope': 'frozen round11, not original',
        'baseline_sha256': before_hash, 'candidate': str(CANDIDATE),
        'candidate_sha256': after_hash, 'repetitions': REPETITIONS,
        'measured_frames': FRAMES, 'warmup_frames': WARMUP,
        'codec_clock_fps': 60, 'GOMAXPROCS': 4, 'CGO_ENABLED': 0,
        'affinity_one_slice': '2', 'affinity_four_slices': '0-3',
        'order': 'before/after on even repetitions, after/before on odd repetitions; adjacent pair for each case',
        'native_reruns': False, 'source_or_binary_mutations': False,
        'prior_parity': str(FRAME_GATE / 'parity/parity.json'),
        'retained_full_summary': str(full_summary), 'retained_full_summary_sha256': full_hash,
        'cases': cases,
    }
    (OUT / 'provenance.json').write_text(json.dumps(metadata, indent=2) + '\n')
    for kind in ['before', 'after']:
        (OUT / f'{kind}.txt').write_text('')
    binaries = {'before': BASELINE, 'after': CANDIDATE}
    for repetition in range(REPETITIONS):
        for case in cases:
            for slices in [1, 4]:
                name = f"{case['name']}-{case['width']}x{case['height']}-qp{case['qp']}-{slices}s"
                env = ENV | {'COMPARE_RUN': '1', 'COMPARE_CASE': case['name'],
                             'COMPARE_I420': case['file'], 'COMPARE_WIDTH': str(case['width']),
                             'COMPARE_HEIGHT': str(case['height']), 'COMPARE_QP': str(case['qp']),
                             'COMPARE_SLICES': str(slices), 'COMPARE_FRAMES': str(FRAMES)}
                for kind in (['before', 'after'] if repetition % 2 == 0 else ['after', 'before']):
                    output = run_logged(['taskset', '-c', '2' if slices == 1 else '0-3',
                                         str(binaries[kind]), '-test.run=^TestOpenH264Comparison$',
                                         '-test.count=1'], OUT / f'{name}-{repetition+1}-{kind}.log', env)
                    rows = [line for line in output.splitlines() if line.startswith('Benchmark')]
                    expected = f"BenchmarkOpenH264/{case['name']}/{case['width']}x{case['height']}/QP{case['qp']}/{slices}Slices-4"
                    assert len(rows) == 1 and rows[0].split()[0] == expected, (name, kind, rows)
                    assert int(rows[0].split()[1]) == FRAMES, rows[0]
                    with (OUT / f'{kind}.txt').open('a') as aggregate:
                        aggregate.write(rows[0] + '\n')
                print(f'Completed codec pair {repetition+1}/{REPETITIONS}: {name}', flush=True)
    run_logged([BENCHSTAT, str(OUT / 'before.txt'), str(OUT / 'after.txt')], OUT / 'benchstat.txt', ENV)
    summarize()
    assert digest(BASELINE) == before_hash and digest(CANDIDATE) == after_hash
    assert digest(full_summary) == full_hash, 'Prior full-path results must remain intact'
    (OUT / 'complete.json').write_text(json.dumps({'codec_cases': 10, 'pairs_per_case': REPETITIONS,
                                                  'native_reruns': 0, 'binaries_unchanged': True,
                                                  'prior_full_results_preserved': True}, indent=2) + '\n')
    print('Distinct-scope scratch codec gate complete; prior full-path evidence preserved', flush=True)


if __name__ == '__main__':
    main()
