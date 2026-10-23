#!/usr/bin/env python3
"""Validate and measure the two frozen binaries serially; never build here."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import time


def digest(path):
    with path.open('rb') as file:
        return hashlib.file_digest(file, 'sha256').hexdigest()


def save(path, value):
    path.write_text(json.dumps(value, indent=2) + '\n')


def host():
    return dict(time=time.time(), load=os.getloadavg(),
                cpu_stat=Path('/proc/stat').read_text().splitlines()[:9],
                temperature={str(p): p.read_text().strip() for p in Path('/sys/class/thermal').glob('thermal_zone*/temp')})


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--work', type=Path, required=True)
    parser.add_argument('--mode', choices=['preflight', 'measure'], required=True)
    parser.add_argument('--repetitions', type=int, default=6)
    args = parser.parse_args()
    work = args.work.resolve()
    cases = json.loads((work / 'cases.json').read_text())
    fixtures = json.loads((work / 'fixtures.json').read_text())
    binaries = {'go': work / 'go.test', 'native': work / 'native_bench'}
    expected = {str(p): digest(p) for p in binaries.values()}
    expected.update({item['file']: item['sha256'] for item in fixtures})

    def verify():
        for path, wanted in expected.items():
            if digest(Path(path)) != wanted:
                raise RuntimeError(f'input/binary changed: {path}')

    verify()
    out = work / args.mode
    out.mkdir(exist_ok=True)
    if any(out.iterdir()):
        raise RuntimeError(f'refusing to overwrite existing results in {out}')
    save(out / 'protocol.json', dict(mode=args.mode, repetitions=args.repetitions,
         cases=cases, hashes=expected, warmup=16, frame_rate=60,
         affinity={'1': '2', '4': '0-3'}, gomaxprocs=4,
         order='Alternating Go/native and native/Go each repetition; offset by case and slice',
         host_before=host()))

    def call(kind, case, slices, prefix, stream=False):
        env = dict(os.environ)
        for key in list(env):
            if key.startswith('COMPARE_') or key in ('GODEBUG', 'GOGC', 'GOMEMLIMIT'):
                env.pop(key)
        env.update(GOMAXPROCS='4', COMPARE_RUN='1', COMPARE_CASE=case['name'],
                   COMPARE_I420=case['file'], COMPARE_WIDTH=str(case['width']),
                   COMPARE_HEIGHT=str(case['height']), COMPARE_QP=str(case['qp']),
                   COMPARE_SLICES=str(slices), COMPARE_FRAMES=str(case['frames']))
        if 'hints' in case:
            env['COMPARE_HINTS'] = case['hints']
        if stream:
            env['COMPARE_STREAM'] = str(prefix.with_suffix('.264'))
        else:
            env['COMPARE_DURATIONS'] = str(prefix.with_suffix('.csv'))
        cmd = ['taskset', '-c', '2' if slices == 1 else '0-3', str(binaries[kind])]
        if kind == 'go':
            cmd += ['-test.run=^TestOpenH264Comparison$', '-test.count=1', '-test.v']
        else:
            cmd += [case['file'], str(slices), str(case['frames'])]
        before = host()
        result = subprocess.run(cmd, env=env, text=True, stdout=subprocess.PIPE,
                                stderr=subprocess.STDOUT, timeout=180)
        after = host()
        prefix.with_suffix('.log').write_text(result.stdout)
        save(prefix.with_suffix('.json'), dict(command=cmd, environment={key: value for key, value in env.items()
             if key.startswith('COMPARE_') or key == 'GOMAXPROCS'}, returncode=result.returncode,
             host_before=before, host_after=after))
        if result.returncode:
            raise RuntimeError(f'{kind} failed, see {prefix}.log')
        rows = [line for line in result.stdout.splitlines() if line.startswith('Benchmark')]
        params = [json.loads(line[len('PARAMS '):]) for line in result.stdout.splitlines() if line.startswith('PARAMS ')]
        if len(rows) != 1 or len(params) != 1:
            raise RuntimeError(f'expected one benchmark/parameters row: {prefix}')
        return rows[0], params[0]

    validated_params = {}
    if args.mode == 'measure':
        validation = json.loads((work / 'preflight' / 'validation.json').read_text())
        for item in validation:
            if not item['decoded_identical']:
                raise RuntimeError('decoded output differs; review quality before measuring')
            validated_params[item['name']] = item['params']
        prior = json.loads((work / 'preflight' / 'protocol.json').read_text())
        if prior['hashes'] != expected or prior['cases'] != cases:
            raise RuntimeError('measurement inputs/binaries differ from preflight')

    validations = []
    for repetition in range(1 if args.mode == 'preflight' else args.repetitions):
        for case_index, case in enumerate(cases):
            for slice_index, slices in enumerate([1, 4]):
                name = f"{case['name']}-{case['width']}x{case['height']}-qp{case['qp']}-{slices}s"
                order = ['go', 'native'] if (repetition + case_index + slice_index) % 2 == 0 else ['native', 'go']
                params, streams = {}, {}
                for kind in order:
                    prefix = out / f'{name}-{repetition + 1}-{kind}'
                    row, params[kind] = call(kind, case, slices, prefix, args.mode == 'preflight')
                    if args.mode == 'measure':
                        if params[kind] != validated_params[name]:
                            raise RuntimeError(f'effective parameters changed: {name}/{kind}')
                        with (out / f'{kind}.txt').open('a') as file:
                            file.write(row + '\n')
                    else:
                        streams[kind] = prefix.with_suffix('.264')
                if params['go'] != params['native']:
                    save(out / 'parameter-mismatch.json', params)
                    raise RuntimeError(f'effective parameters differ: {name}')
                if args.mode == 'preflight':
                    decoded = {}
                    for kind, stream in streams.items():
                        md5 = stream.with_suffix('.framemd5')
                        cmd = ['ffmpeg', '-v', 'error', '-xerror', '-threads', '1', '-i', str(stream),
                               '-map', '0:v:0', '-fps_mode', 'passthrough', '-pix_fmt', 'yuv420p',
                               '-f', 'framemd5', '-y', str(md5)]
                        result = subprocess.run(cmd, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
                        stream.with_suffix('.decode.log').write_text(result.stdout)
                        if result.returncode != 0 or result.stdout.strip():
                            raise RuntimeError(f'decode failed: {stream}')
                        decoded[kind] = [line for line in md5.read_text().splitlines() if not line.startswith('#')]
                        if len(decoded[kind]) != case['frames'] + 16:
                            raise RuntimeError(f'incorrect decoded frame count: {stream}')
                    item = dict(name=name, params=params['go'], frames=case['frames'] + 16,
                         streams={kind: dict(sha256=digest(path), bytes=path.stat().st_size) for kind, path in streams.items()},
                         bitstreams_identical=digest(streams['go']) == digest(streams['native']),
                         decoded_identical=decoded['go'] == decoded['native'])
                    validations.append(item)
                    save(out / 'validation.json', validations)
                    print(f"validated {name}: bitstream_equal={item['bitstreams_identical']} decoded_equal={item['decoded_identical']}", flush=True)
                else:
                    print(f'completed pair {repetition + 1}/{args.repetitions}: {name}', flush=True)
    verify()
    save(out / 'complete.json', dict(host_after=host(), hashes_verified_after=True,
         pairs=len(cases) * 2 * (1 if args.mode == 'preflight' else args.repetitions)))


if __name__ == '__main__':
    main()
