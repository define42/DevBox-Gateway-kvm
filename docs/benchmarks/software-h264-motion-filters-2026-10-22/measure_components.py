#!/usr/bin/env python3
import os,subprocess
from pathlib import Path
r=Path(__file__).resolve().parent
env=os.environ|{'GOMAXPROCS':'4'}
for kind in ['before','after']: (r/f'component-{kind}.txt').write_text('')
for rep in range(6):
    for kind in (['before','after'] if rep%2==0 else ['after','before']):
        out=subprocess.check_output([str(r/f'{kind}-common.test'),'-test.run=^$','-test.bench=^BenchmarkMcAVX2Refinement$','-test.benchtime=200ms','-test.count=1'],env=env,text=True)
        with (r/f'component-{kind}.txt').open('a') as f:f.write(out)
    print(f'Component pair {rep+1}/6',flush=True)
with (r/'component-benchstat.txt').open('w') as f:
    subprocess.run(['/home/define42/go/bin/benchstat',str(r/'component-before.txt'),str(r/'component-after.txt')],stdout=f,check=True)
