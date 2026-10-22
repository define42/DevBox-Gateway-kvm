#!/usr/bin/env python3
"""Run isolated before/after/native encoders serially with rotating order."""
import argparse
import hashlib
import itertools
import json
import os
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parent
CASES = json.loads((ROOT / 'cases.json').read_text())
parser = argparse.ArgumentParser()
parser.add_argument('--preflight', action='store_true')
parser.add_argument('--after-only', action='store_true', help='Reuse saved before/native parity streams')
parser.add_argument('--repetitions', type=int, default=6)
parser.add_argument('--frames', type=int, default=80)
parser.add_argument('--case', default='')
args = parser.parse_args()
selected = [c for c in CASES if not args.case or c['name'] == args.case]
logs = ROOT / ('parity' if args.preflight else 'measure')
logs.mkdir(exist_ok=True)
if not args.preflight:
    for kind in ['before', 'after', 'native']:
        (logs / f'{kind}.txt').write_text('')

def call(kind, case, slices, count, stream=None):
    env = os.environ | {
        'GOMAXPROCS': '4', 'COMPARE_RUN': '1',
        'COMPARE_CASE': case['name'], 'COMPARE_I420': case['file'],
        'COMPARE_WIDTH': str(case['width']), 'COMPARE_HEIGHT': str(case['height']),
        'COMPARE_QP': str(case['qp']), 'COMPARE_SLICES': str(slices),
        'COMPARE_FRAMES': str(count),
    }
    env.pop('COMPARE_STREAM', None)
    if kind == 'native':
        cmd = [str(ROOT / 'native_bench'), case['file'], str(slices), str(count)]
        if stream:
            cmd.append(str(stream))
    else:
        cmd = [str(ROOT / f'{kind}-avc.test'), '-test.run=^TestOpenH264Comparison$', '-test.count=1']
        if stream:
            env['COMPARE_STREAM'] = str(stream)
    result = subprocess.run(cmd, env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, check=True)
    return result.stdout

if args.preflight:
    with (logs / 'parity.txt').open('w') as report:
        for case in selected:
            for slices in [1, 4]:
                name = f"{case['name']}-{case['width']}x{case['height']}-qp{case['qp']}-{slices}s"
                hashes = []
                sizes = []
                for kind in ['before', 'after', 'native']:
                    stream = logs / f'{name}-{kind}.264'
                    if not args.after_only or kind == 'after':
                        output = call(kind, case, slices, args.frames, stream)
                        (logs / f'{name}-{kind}.log').write_text(output)
                    data = stream.read_bytes()
                    hashes.append(hashlib.sha256(data).hexdigest())
                    sizes.append(len(data))
                if len(set(hashes)) != 1:
                    raise SystemExit(f'PARITY FAIL {name}: {list(zip(hashes,sizes))}')
                stream = logs / f'{name}-after.264'
                decode = subprocess.run(['ffmpeg','-v','error','-xerror','-threads','1','-i',str(stream),'-f','null','-'],text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT)
                if decode.returncode != 0 or decode.stdout:
                    raise SystemExit(f'DECODE FAIL {name}: {decode.stdout}')
                probe = subprocess.check_output(['ffprobe','-v','error','-count_frames','-select_streams','v:0','-show_entries','stream=nb_read_frames','-of','csv=p=0',str(stream)],text=True).strip()
                if int(probe) != args.frames+16:
                    raise SystemExit(f'FRAME COUNT FAIL {name}: {probe}')
                line=f'{name}: before=after=native bytes={sizes[0]} sha256={hashes[0]} decoded_frames={probe}\n'
                report.write(line)
                report.flush()
                print(line,end='',flush=True)
else:
    orders=list(itertools.permutations(['before','after','native']))
    for repetition in range(args.repetitions):
        for case in selected:
            for slices in [1,4]:
                name=f"{case['name']}-{case['width']}x{case['height']}-qp{case['qp']}-{slices}s"
                for kind in orders[repetition % len(orders)]:
                    output=call(kind,case,slices,args.frames)
                    (logs/f'{name}-{repetition+1}-{kind}.log').write_text(output)
                    rows=[line for line in output.splitlines() if line.startswith('Benchmark')]
                    if len(rows)!=1:
                        raise SystemExit(f'Bad benchmark result for {name}/{kind}')
                    with (logs/f'{kind}.txt').open('a') as f:
                        f.write(rows[0]+'\n')
                print(f'completed pair {repetition+1}/{args.repetitions}: {name}',flush=True)
