#!/usr/bin/env python3
"""Before/after complete EncodeDamage calls, including RGB conversion."""
import os
from pathlib import Path
import subprocess
ROOT=Path(__file__).resolve().parent
OUT=ROOT/'full';OUT.mkdir(exist_ok=True)
CASES=[('ShortClip','Clip','/tmp/h264-fullscreen-20261021/clip1080p.rgba'),('Road','Clip',str(ROOT/'road1080.rgba')),('Dense','Dense','')]
for kind in ['before','after']:(OUT/f'{kind}.txt').write_text('')
for repetition in range(6):
 for name,fixture,path in CASES:
  for slices in [1,4]:
   for kind in (['before','after'] if repetition%2==0 else ['after','before']):
    env=os.environ|{'GOMAXPROCS':'4','H264_VIDEO_RGBA':path}
    pattern='^BenchmarkFullscreenVideo$/^'+fixture+'$/^1080p$/^'+str(slices)+'Slices$/^FullDamage$'
    cmd=['taskset','-c','2' if slices==1 else '0-3',str(ROOT/f'{kind}-avc.test'),'-test.run=^$','-test.bench='+pattern,'-test.benchtime=60x','-test.count=1']
    result=subprocess.run(cmd,env=env,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,check=True)
    (OUT/f'{name}-{slices}s-{repetition+1}-{kind}.log').write_text(result.stdout)
    rows=[line.replace('/'+fixture+'/', '/'+name+'/') for line in result.stdout.splitlines() if line.startswith('Benchmark')]
    if len(rows)!=1:raise SystemExit(f'Bad results: {name}, {kind}, {rows}')
    with (OUT/f'{kind}.txt').open('a') as f:f.write(rows[0]+'\n')
   print(f'completed full pair {repetition+1}/6: {name}/{slices}s',flush=True)
