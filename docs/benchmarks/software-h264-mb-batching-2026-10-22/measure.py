#!/usr/bin/env python3
"""Go (HEAD) vs native OpenH264 2.6.0 / 2.7.0, serial, rotating order."""
import argparse, hashlib, itertools, json, os, subprocess
from pathlib import Path
ROOT = Path(__file__).resolve().parent
CASES = json.loads((ROOT / 'cases.json').read_text())
KINDS = {'base': 'go-base.test', 'final': 'go-final.test', 'native26': 'native_bench'}
p = argparse.ArgumentParser()
p.add_argument('--preflight', action='store_true')
p.add_argument('--repetitions', type=int, default=6)
p.add_argument('--frames', type=int, default=80)
p.add_argument('--kinds', default='base,final,native26')
a = p.parse_args()
kinds = a.kinds.split(',')
out = ROOT / ('parity2' if a.preflight else 'measure2'); out.mkdir(exist_ok=True)

def call(kind, c, slices, count, stream=None):
    env = os.environ | {'GOMAXPROCS': '4', 'COMPARE_RUN': '1', 'COMPARE_CASE': c['name'],
        'COMPARE_I420': c['file'], 'COMPARE_WIDTH': str(c['width']), 'COMPARE_HEIGHT': str(c['height']),
        'COMPARE_QP': str(c['qp']), 'COMPARE_SLICES': str(slices), 'COMPARE_FRAMES': str(count)}
    env.pop('COMPARE_STREAM', None)
    if not KINDS[kind].endswith('.test'):
        cmd = [str(ROOT / KINDS[kind]), c['file'], str(slices), str(count)] + ([str(stream)] if stream else [])
    else:
        cmd = [str(ROOT / KINDS[kind]), '-test.run=^TestOpenH264Comparison$', '-test.count=1']
        if stream: env['COMPARE_STREAM'] = str(stream)
    return subprocess.run(cmd, env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, check=True).stdout

def name(c, s): return f"{c['name']}-{c['width']}x{c['height']}-qp{c['qp']}-{s}s"
if a.preflight:
    with (out / 'parity.txt').open('w') as rep:
        for c in CASES:
            for s in [1, 4]:
                n = name(c, s); res = {}
                for k in kinds:
                    st = out / f'{n}-{k}.264'; call(k, c, s, a.frames, st)
                    d = st.read_bytes(); res[k] = (len(d), hashlib.sha256(d).hexdigest())
                st = out / f'{n}-go.264'
                dec = subprocess.run(['ffmpeg','-v','error','-xerror','-threads','1','-i',str(st),'-f','null','-'],text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT)
                fr = subprocess.check_output(['ffprobe','-v','error','-count_frames','-select_streams','v:0','-show_entries','stream=nb_read_frames','-of','csv=p=0',str(st)],text=True).strip()
                line = f"{n}: " + ' '.join(f"{k}=bytes:{v[0]},sha256:{v[1]}" for k, v in res.items()) + f" go_decode_ok={dec.returncode==0 and not dec.stdout} decoded_frames={fr}\n"
                rep.write(line); print(line, end='', flush=True)
else:
    for k in kinds: (out / f'{k}.txt').write_text('')
    orders = list(itertools.permutations(kinds))
    for r in range(a.repetitions):
        for c in CASES:
            for s in [1, 4]:
                for k in orders[r % len(orders)]:
                    o = call(k, c, s, a.frames)
                    rows = [l for l in o.splitlines() if l.startswith('Benchmark')]
                    assert len(rows) == 1, o
                    with (out / f'{k}.txt').open('a') as f: f.write(rows[0] + '\n')
        print(f'rep {r+1}/{a.repetitions} done', flush=True)
