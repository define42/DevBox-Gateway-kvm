#!/usr/bin/env python3
import os
from pathlib import Path
import subprocess
ROOT=Path(__file__).resolve().parent
cases=[('common',r'^BenchmarkSampleSad(8x8|16x16|FourBatched)$/.*$/.*$/^Batched$'),('encoder',r'^BenchmarkQuantScanFour4x4$/.*$/.*$/^Fusedtrue$')]
for kind in ['before','after']:
    (ROOT/f'abi-{kind}.txt').write_text('')
for repetition in range(6):
    for package,regex in cases:
        for kind in (['before','after'] if repetition%2==0 else ['after','before']):
            cmd=[str(ROOT/f'{kind}-{package}.test'),'-test.run=^$','-test.bench='+regex,'-test.benchtime=150ms','-test.count=1']
            result=subprocess.run(cmd,env=os.environ|{'GOMAXPROCS':'4'},text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,check=True)
            with (ROOT/f'abi-{kind}.txt').open('a') as f:
                f.write(result.stdout)
    print(f'ABI pair {repetition+1}/6 complete',flush=True)
