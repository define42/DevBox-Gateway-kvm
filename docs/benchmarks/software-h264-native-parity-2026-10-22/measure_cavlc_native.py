#!/usr/bin/env python3
import os
from pathlib import Path
import subprocess
p=Path(__file__).resolve().parent
for kind in ['go','native']:
    (p/f'cavlc-native-final-{kind}.txt').write_text('')
for repetition in range(6):
    for kind in (['go','native'] if repetition%2==0 else ['native','go']):
        if kind=='go':
            cmd=[str(p/'after-encoder.test'),'-test.run=^$','-test.bench=^BenchmarkCavlcExtraction$/.*$/.*$/^scalar=false$','-test.benchtime=100ms','-test.count=1']
        else:
            cmd=[str(p/'cavlc_native_final'),'3000000']
        result=subprocess.run(cmd,env=os.environ|{'GOMAXPROCS':'4'},text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,check=True)
        with (p/f'cavlc-native-final-{kind}.txt').open('a') as f:
            f.write(result.stdout)
    print(f'Native CAVLC pair {repetition+1}/6',flush=True)
