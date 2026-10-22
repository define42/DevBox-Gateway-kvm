from pathlib import Path
import os
import subprocess
import json
import re

base=Path(__file__).resolve().parent
env=dict(os.environ,GOMAXPROCS='4',COMPARE_RUN='1',COMPARE_I420=str(base/'clip1080p.i420'),COMPARE_FRAMES='120')
env.pop('COMPARE_STREAM', None)
for label in ('go','native'):
    (base/(label+'.txt')).write_text('goos: linux\ngoarch: amd64\ncpu: Intel(R) Core(TM) i7-7700HQ CPU @ 2.80GHz\n')
results=[]
for pair in range(6):
    for slices in (1,4):
        for label in (('native','go') if pair%2==0 else ('go','native')):
            runenv=dict(env,COMPARE_SLICES=str(slices))
            if label=='go':
                cmd=[str(base/'go.test'),'-test.run=^TestOpenH264Comparison$','-test.v']
            else:
                cmd=[str(base/'native_bench'),str(base/'clip1080p.i420'),str(slices),'120']
            out=subprocess.run(cmd,env=runenv,capture_output=True,text=True,check=True)
            (base/f'pair-{pair+1}-{slices}-{label}.log').write_text(out.stdout+out.stderr)
            rows=[line for line in out.stdout.splitlines() if line.startswith('Benchmark')]
            if len(rows)!=1: raise RuntimeError(out.stdout+out.stderr)
            if label=='native':
                total=int(re.search(r'timed_bytes=(\d+)',out.stdout)[1])
                rows[0]=re.sub(r'\S+ bytes/frame',f'{total/120:.3f} bytes/frame',rows[0])
            with (base/(label+'.txt')).open('a') as f:f.write(rows[0]+'\n')
            results.append(dict(pair=pair+1,slices=slices,label=label,row=rows[0]))
            print(pair+1,slices,label,rows[0],flush=True)
(base/'results.json').write_text(json.dumps(results,indent=2))
out=subprocess.run(['/home/define42/go/bin/benchstat',str(base/'native.txt'),str(base/'go.txt')],capture_output=True,text=True,check=True)
(base/'benchstat.txt').write_text(out.stdout)
print(out.stdout,flush=True)
