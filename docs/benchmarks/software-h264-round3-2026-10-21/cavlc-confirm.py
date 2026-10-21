import subprocess,os
from pathlib import Path
names=['Original','Current','Candidate']
env=dict(os.environ,GOMAXPROCS='4')
with Path('confirmation.txt').open('w') as out:
 for repeat in range(6):
  order=names if repeat%2==0 else list(reversed(names))
  for pattern in ['Sparse','Dense','Moderate','Mixed']:
   for name in order:
    r=subprocess.run(['./variant-a.test','-test.run=^$',f'-test.bench=^BenchmarkCavlcIsolation/{pattern}/{name}$','-test.benchtime=200ms','-test.count=1','-test.cpu=4'],env=env,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True,check=True)
    out.write(r.stdout);out.flush()
