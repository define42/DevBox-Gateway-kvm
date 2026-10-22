#!/usr/bin/env python3
import os,subprocess
from pathlib import Path
r=Path(__file__).resolve().parent
env=os.environ|{'GOMAXPROCS':'4'}
prefix=['taskset','-c','2']
for kind in ['before','after','native']:(r/f'component-{kind}.txt').write_text('')
for rep in range(6):
    for kind in (['before','after'] if rep%2==0 else ['after','before']):
        out=subprocess.check_output(prefix+[str(r/f'{kind}-encoder.test'),'-test.run=^$','-test.bench=^BenchmarkSampleSatd8Rows$','-test.benchtime=500ms','-test.count=1'],env=env,text=True)
        with (r/f'component-{kind}.txt').open('a') as f:f.write(out)
    out=subprocess.check_output(prefix+[str(r/'native_satd8'),'20000000'],text=True)
    with (r/'component-native.txt').open('a') as f:f.write(out)
    print(f'Component pair {rep+1}/6',flush=True)
with (r/'component-benchstat.txt').open('w') as f:
    subprocess.run(['/home/define42/go/bin/benchstat',str(r/'component-before.txt'),str(r/'component-after.txt')],stdout=f,check=True)
print((r/'component-benchstat.txt').read_text())
