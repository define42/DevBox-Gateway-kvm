#!/usr/bin/env python3
"""Count distinct clipped five-MVC predictors; diagnostic only, root runs serially.

This prepares a private round11 build with three diagnostic overlay files and the
existing comparison fixtures. The two runs encode 600 measured +16 warmup frames,
but their timing lines are deliberately ignored. Byte hashes must match frozen
uninstrumented round11 streams before the candidate distributions are accepted.
"""
from pathlib import Path
import argparse
import hashlib
import json
import os
import shutil
import subprocess
import time

ROOT = Path(__file__).resolve().parent
GO = '/snap/go/current/bin/go'
GOFMT = '/snap/go/current/bin/gofmt'
BASE_DIGEST = 'c60f1bb69991e68584e2b9512b42acf4a3454c51ec68389da0844825cf95529f'
OVERLAY_MANIFEST_SHA = '894e90faf56ef0821da8c9749a8642d469ae65589f53e6b25ae7504fcc2443d7'
CASES = [
    {'name': 'road1', 'input': '/tmp/h264-native-parity-20261021/road1080.i420',
     'stream_sha256': 'fec3891d0f30674fd132b1cffade2b79b2ff6e9855574c5c233dcd2703e11d31',
     'initial_calls': 10025547, 'eligible': 3770737, 'old_extra_sads': 14507502},
    {'name': 'pdf1', 'input': '/tmp/h264-native-parity-20261021/pdf1080.i420',
     'stream_sha256': 'ee3b0368c76e429a2be6a280c61038e0c50c76170c2c845f0fb2e80298eef9a3',
     'initial_calls': 1073336, 'eligible': 490448, 'old_extra_sads': 1462625},
]


def sha(path):
    with Path(path).open('rb') as f:
        return hashlib.file_digest(f, 'sha256').hexdigest()


def require(condition, message):
    if not condition:
        raise SystemExit(message)


def write_json(path, value):
    Path(path).write_text(json.dumps(value, indent=2) + '\n')


def source_map(tree):
    files = {str(p.relative_to(tree)): sha(p) for folder in ['pkg', 'internal']
             for p in sorted((tree / folder).rglob('*')) if p.is_file()}
    files.update({name: sha(tree / name) for name in ['go.mod', 'go.sum']})
    return files


def digest_map(files):
    return hashlib.sha256(json.dumps(files, sort_keys=True, separators=(',', ':')).encode()).hexdigest()


def clean_environment():
    env = os.environ.copy()
    for key in list(env):
        if key.startswith(('COMPARE_', 'CAVLC_', 'H264_')) or key in {
            'GODEBUG', 'GOFLAGS', 'GOOS', 'GOARCH', 'GOAMD64', 'GOWORK',
        }:
            env.pop(key)
    env.update(PATH='/snap/go/current/bin:' + env.get('PATH', ''),
               CGO_ENABLED='0', GOMAXPROCS='1', GOOS='linux', GOARCH='amd64',
               GOAMD64='v1', GOWORK='off')
    return env


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, default=ROOT / 'initial-mvc-diagnostic-review')
    args = parser.parse_args()
    output = args.output.resolve()
    baseline = ROOT / 'round11'
    overlays = ROOT / 'initial-mvc-diagnostic-overlay'
    manifest_path = overlays / 'manifest.json'
    require(sha(manifest_path) == OVERLAY_MANIFEST_SHA, 'Diagnostic overlay manifest changed')
    manifest = json.loads(manifest_path.read_text())
    require(set(manifest['files']) == {'svc_motion_estimate.go', 'welsEncoderExt.go', 'z_initial_mvc_diagnostic.go'},
            'Expected exactly three diagnostic files')
    for name, expected in manifest['files'].items():
        require(sha(overlays / name) == expected, f'Diagnostic file changed: {name}')
    baseline_files = source_map(baseline)
    require(digest_map(baseline_files) == BASE_DIGEST, 'Frozen round11 source changed')
    for tree in [baseline, overlays, ROOT / 'quarter-plan-next', ROOT / 'quarter-plan-tuned-next',
                 ROOT / 'quarter-plan-review', ROOT / 'quarter-plan-tuned-review']:
        path = tree.resolve()
        require(not (output == path or output.is_relative_to(path) or path.is_relative_to(output)),
                'Output must be disjoint from every frozen source/result directory')
    require(2 in os.sched_getaffinity(0), 'CPU 2 must be available')
    required = [ROOT / 'compare_test.go', ROOT / 'fullscreen_test.go', ROOT / 'round11-avc.test',
                ROOT / 'round11-counter-parity.json']
    for case in CASES:
        required.extend([Path(case['input']), ROOT / f"round11-{case['name']}.264",
                         ROOT / f"call-count-{case['name']}-totals.json"])
    for path in required:
        require(path.is_file(), f'Missing prerequisite: {path}')
    for case in CASES:
        require(sha(ROOT / f"round11-{case['name']}.264") == case['stream_sha256'],
                'Frozen 616-frame baseline stream changed')
        old = json.loads((ROOT / f"call-count-{case['name']}-totals.json").read_text())
        initial = [record for record in old if record['kind'] == 'initial' and record['final']]
        require(len(initial) == 1 and initial[0]['frames_including_warmup'] == 616,
                'Prior diagnostic scope changed')
        counts = initial[0]['counts']
        require(counts['calls'] == case['initial_calls'] and counts['calls_by_mvc_count'][5] == case['eligible']
                and counts['extra_sad_by_mvc_count'][5] == case['old_extra_sads'],
                'Prior diagnostic reference counts changed')

    # Refuse accidental behavior edits beyond the three explicit hook lines.
    hooks = {
        'svc_motion_estimate.go': ['\tmvcDiagnosticRecord(pSad, pFuncList.sSampleDealingFuncs.bSampleSadTwo, pMe, pSlice, sMv, iStrideEnc, iStrideRef)\n'],
        'welsEncoderExt.go': ['\tmvcDiagnosticFinish(p)\n', '\tmvcDiagnosticBeginFrame(p)\n'],
    }
    for name, lines in hooks.items():
        text = (overlays / name).read_text()
        for line in lines:
            require(text.count(line) == 1, f'Unexpected diagnostic hook count: {name}')
            text = text.replace(line, '')
        require(text == (baseline / 'pkg/h264/internal/encoder' / name).read_text(),
                f'Diagnostic replacement has unrelated changes: {name}')

    output.mkdir(exist_ok=False)
    for name in ['source', 'overlay', 'logs', 'streams']:
        (output / name).mkdir()
    source = output / 'source'
    for folder in ['pkg', 'internal']:
        shutil.copytree(baseline / folder, source / folder)
    for name in ['go.mod', 'go.sum']:
        shutil.copy2(baseline / name, source / name)
    require(source_map(source) == baseline_files, 'Source changed while snapshotting')
    (output / 'baseline-sources.sha256').write_text(''.join(f'{digest}  {name}\n' for name, digest in sorted(baseline_files.items())))
    shutil.copy2(Path(__file__), output / Path(__file__).name)
    shutil.copy2(manifest_path, output / 'overlay' / 'manifest.json')
    shutil.copy2(ROOT / 'round11-counter-parity.json', output / 'baseline-counter-parity.json')
    replacements = {}
    for name in manifest['files']:
        dst = output / 'overlay' / name
        shutil.copy2(overlays / name, dst)
        replacements[str(source / 'pkg/h264/internal/encoder' / name)] = str(dst)
    for name, virtual in [('compare_test.go', 'z_openh264_comparison_test.go'),
                          ('fullscreen_test.go', 'z_fullscreen_review_test.go')]:
        dst = output / 'overlay' / name
        shutil.copy2(ROOT / name, dst)
        replacements[str(source / 'internal/rdp/avc' / virtual)] = str(dst)
    overlay_json = output / 'overlay.json'
    write_json(overlay_json, {'Replace': replacements})
    env = clean_environment()
    commands = []

    def run(label, command, extra=None):
        print(label, flush=True)
        start = time.time()
        result = subprocess.run([str(x) for x in command], cwd=source, env=env | (extra or {}),
                                text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        log = output / 'logs' / f'{label}.txt'
        log.write_text(result.stdout)
        commands.append({'name': label, 'command': [str(x) for x in command], 'cwd': str(source),
                         'environment_override': extra or {}, 'exit_code': result.returncode,
                         'elapsed_seconds': time.time() - start, 'log': str(log.relative_to(output))})
        write_json(output / 'commands.json', commands)
        require(result.returncode == 0, f'{label} failed; see {log}')
        return result.stdout

    provenance = {
        'scope': 'Diagnostic counts only; every timing line from this binary is invalid as performance evidence',
        'baseline_source': str(baseline), 'baseline_source_map_sha256': BASE_DIGEST,
        'overlay_manifest_sha256': OVERLAY_MANIFEST_SHA,
        'input_sha256': {str(path): sha(path) for path in [Path(__file__), *required]},
        'environment': {key: env[key] for key in ['CGO_ENABLED', 'GOMAXPROCS', 'GOOS', 'GOARCH', 'GOAMD64', 'GOWORK']},
        'fixture': {'width': 1920, 'height': 1080, 'qp': 20, 'slices': 1,
                    'requested_frames': 600, 'warmup_frames': 16, 'encoded_frames': 616, 'affinity': '2'},
    }
    write_json(output / 'provenance.json', provenance)
    run('go-version', [GO, 'version'])
    formatting = run('overlay-format', [GOFMT, '-l', *[output / 'overlay' / name for name in manifest['files']]])
    require(not formatting.strip(), 'Frozen diagnostic overlay is not gofmt-clean')
    binary = output / 'diagnostic-avc.test'
    run('build', [GO, 'test', '-c', '-overlay=' + str(overlay_json), '-o', binary, './internal/rdp/avc'])
    binary_hash = sha(binary)
    provenance['diagnostic_binary_sha256'] = binary_hash
    write_json(output / 'provenance.json', provenance)
    results = []
    for case in CASES:
        label = case['name']
        stream = output / 'streams' / f'{label}.264'
        extra = {'H264_MVC_UNIQUE_DIAGNOSTIC': '1', 'COMPARE_RUN': '1', 'COMPARE_FRAMES': '600',
                 'COMPARE_SLICES': '1', 'COMPARE_WIDTH': '1920', 'COMPARE_HEIGHT': '1080',
                 'COMPARE_QP': '20', 'COMPARE_CASE': label, 'COMPARE_I420': case['input'],
                 'COMPARE_STREAM': str(stream)}
        text = run(label, ['taskset', '-c', '2', binary, '-test.run=^TestOpenH264Comparison$', '-test.count=1'], extra)
        records = [json.loads(line.removeprefix('H264_MVC_UNIQUE ')) for line in text.splitlines()
                   if line.startswith('H264_MVC_UNIQUE ')]
        require(len(records) == 1, f'Expected one final diagnostic record for {label}')
        record = records[0]
        require(record['final'] and record['case'] == label and record['frames_including_warmup'] == 616,
                f'Wrong frame/lifecycle scope for {label}')
        stream_hash = sha(stream)
        require(stream_hash == case['stream_sha256'], f'Byte stream changed for {label}; discard diagnostic counts')
        c = record['counts']
        ex = c['short_circuit_exclusions']
        require(c['initial_calls'] == case['initial_calls'] and c['eligible_16x16_five_mvc'] == case['eligible'],
                f'Initial-search activity changed for {label}')
        eligible = c['eligible_16x16_five_mvc']
        histogram = c['unique_clipped_mv_histogram']
        require(len(histogram) == 7 and histogram[0] == 0 and sum(histogram) == eligible - ex['short_list'],
                'Unique histogram accounting mismatch')
        require(c['unique_clipped_mv_total'] == sum(i * value for i, value in enumerate(histogram)),
                'Unique total accounting mismatch')
        require(c['duplicate_clipped_mv_total'] == 6 * sum(histogram) - c['unique_clipped_mv_total'],
                'Duplicate total accounting mismatch')
        require(c['initial_calls'] == eligible + ex['other_block'] + ex['other_mvc_count'],
                'Initial exclusion accounting mismatch')
        require(c['preflight_eligible'] + sum(value for key, value in ex.items() if key not in {'other_block', 'other_mvc_count'}) == eligible,
                'Preflight exclusion accounting mismatch')
        require(sum(c['preflight_unique_histogram']) == c['preflight_eligible'], 'Preflight histogram mismatch')
        old_sads = case['eligible'] + case['old_extra_sads']
        results.append({'case': label, 'frames': 616, 'byte_parity': True,
                        'baseline_sha256': case['stream_sha256'], 'diagnostic_sha256': stream_hash,
                        'diagnostic': record, 'prior_five_mvc_sad_evaluations': old_sads,
                        'distinct_mv_evaluations': c['unique_clipped_mv_total'],
                        'potential_repeated_mv_evaluations': old_sads - c['unique_clipped_mv_total'],
                        'interpretation': 'Counts only; savings need a separately benchmarked exact implementation'})
        write_json(output / f'{label}-counts.json', record)
        write_json(output / 'summary.json', {'status': 'partial', 'results': results, 'timings_are_performance_evidence': False})
    require(source_map(source) == baseline_files and source_map(baseline) == baseline_files, 'Frozen/private source changed')
    require(sha(binary) == binary_hash, 'Diagnostic binary changed during runs')
    for name, expected in manifest['files'].items():
        require(sha(overlays / name) == expected and sha(output / 'overlay' / name) == expected, 'Diagnostic overlay changed')
    for path, expected in provenance['input_sha256'].items():
        require(sha(Path(path)) == expected, f'Recorded input changed during diagnostic: {path}')
    require(sha(manifest_path) == OVERLAY_MANIFEST_SHA, 'Diagnostic manifest changed during runs')
    write_json(output / 'summary.json', {'status': 'complete', 'results': results, 'timings_are_performance_evidence': False})
    print(f'Diagnostic counts and 616-frame byte-parity gates complete: {output}', flush=True)


if __name__ == '__main__':
    main()
