#!/usr/bin/env python3
import os
from pathlib import Path
import subprocess
ROOT=Path(__file__).resolve().parent
for mode in ['scalar','simd']:
    (ROOT/f'cavlc-confirm-{mode}.txt').write_text('')
for repetition in range(6):
    for mode in (['scalar','simd'] if repetition%2==0 else ['simd','scalar']):
        flag='true' if mode=='scalar' else 'false'
        regex=r'^BenchmarkCavlcExtractionResidual$/^end(3|14|15)$/.*$/^scalar='+flag+'$'
        result=subprocess.run([str(ROOT/'after-encoder.test'),'-test.run=^$','-test.bench='+regex,'-test.benchtime=100ms','-test.count=1'],env=os.environ|{'GOMAXPROCS':'4'},text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,check=True)
        text=result.stdout.replace('/scalar='+flag+'-','-')
        with (ROOT/f'cavlc-confirm-{mode}.txt').open('a') as f:
            f.write(text)
    print(f'Final CAVLC pair {repetition+1}/6',flush=True)
