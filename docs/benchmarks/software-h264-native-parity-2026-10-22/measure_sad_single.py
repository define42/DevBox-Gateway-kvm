import os
from pathlib import Path
import subprocess
p=Path(__file__).resolve().parent
for kind in ['before','after']:
    (p/f'sad-single-{kind}.txt').write_text('')
for repetition in range(6):
    for kind in (['before','after'] if repetition%2==0 else ['after','before']):
        r=subprocess.run([str(p/f'{kind}-common.test'),'-test.run=^$','-test.bench=^BenchmarkSampleSad(8x8|16x16)$','-test.benchtime=150ms','-test.count=1'],env=os.environ|{'GOMAXPROCS':'4'},text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,check=True)
        with (p/f'sad-single-{kind}.txt').open('a') as f:
            f.write(r.stdout)
