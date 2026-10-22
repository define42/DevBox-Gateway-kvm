"""Bounded confirmation after high-variance four-slice results; no samples discarded."""
from pathlib import Path
import itertools
import json
import os
import subprocess

r = Path(__file__).resolve().parent
out = r / 'round10-timing-repeat'
out.mkdir(exist_ok=True)
cases = json.loads((r / 'cases.json').read_text())
selected = [(c, 4) for c in cases]
# One unchanged-affinity one-slice control, selected before seeing repeat results.
selected += [(c, 1) for c in cases if c['name'] == 'Road' and c['width'] == 1920 and c['qp'] == 20]
(out / 'scope.json').write_text(json.dumps({
    'reason': 'First run four-slice confidence ranges were very wide for all three binaries',
    'frames': 80, 'warmup': 16, 'repetitions': 6,
    'baseline': 'round9-avc.test', 'candidate': 'round10-avc.test',
    'cases': selected, 'affinity': {'1': '2', '4': '0-3'},
    'host_settings_changed': False, 'samples_discarded': False,
}, indent=2) + '\n')
for kind in ['before', 'after', 'native']:
    (out / (kind + '.txt')).write_text('')
for rep, order in enumerate(itertools.permutations(['before', 'after', 'native'])):
    for case, slices in selected:
        name = f"{case['name']}-{case['width']}x{case['height']}-qp{case['qp']}-{slices}s"
        env = os.environ | {'GOMAXPROCS': '4', 'COMPARE_RUN': '1', 'COMPARE_CASE': case['name'],
            'COMPARE_I420': case['file'], 'COMPARE_WIDTH': str(case['width']),
            'COMPARE_HEIGHT': str(case['height']), 'COMPARE_QP': str(case['qp']),
            'COMPARE_SLICES': str(slices), 'COMPARE_FRAMES': '80'}
        env.pop('COMPARE_STREAM', None)
        for kind in order:
            if kind == 'native':
                cmd = [str(r / 'native_bench'), case['file'], str(slices), '80']
            else:
                binary = r / ('round9-avc.test' if kind == 'before' else 'round10-avc.test')
                cmd = [str(binary), '-test.run=^TestOpenH264Comparison$', '-test.count=1']
            result = subprocess.run(['taskset', '-c', '2' if slices == 1 else '0-3', *cmd],
                env=env, capture_output=True, text=True, check=True)
            (out / f'{name}-{rep+1}-{kind}.log').write_text(result.stdout + result.stderr)
            rows = [line for line in result.stdout.splitlines() if line.startswith('Benchmark')]
            assert len(rows) == 1
            with (out / (kind + '.txt')).open('a') as f:
                f.write(rows[0] + '\n')
        print(f'confirmation {rep+1}/6 {name}', flush=True)
for name, kinds in [('benchstat', ['before', 'after', 'native']), ('native-benchstat', ['after', 'native'])]:
    with (out / (name + '.txt')).open('w') as f:
        subprocess.run(['/home/define42/go/bin/benchstat', *[str(out / (k + '.txt')) for k in kinds]],
            stdout=f, check=True)
print('Bounded timing confirmation complete', flush=True)
