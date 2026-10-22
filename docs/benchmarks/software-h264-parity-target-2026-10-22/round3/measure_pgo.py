import os,json,subprocess,pathlib
r=pathlib.Path(__file__).resolve().parent
cases=[json.loads((r/'cases.json').read_text())[i] for i in [0,1,3]]
for label in ['round1','round1-pgo']:(r/(label+'-pgo-check.txt')).write_text('')
for rep in range(6):
 for case in cases:
  for label in (['round1','round1-pgo'] if rep%2==0 else ['round1-pgo','round1']):
   env=os.environ|{'GOMAXPROCS':'4','COMPARE_RUN':'1','COMPARE_CASE':case['name'],'COMPARE_I420':case['file'],'COMPARE_WIDTH':str(case['width']),'COMPARE_HEIGHT':str(case['height']),'COMPARE_QP':str(case['qp']),'COMPARE_SLICES':'1','COMPARE_FRAMES':'80'}
   result=subprocess.run(['taskset','-c','2',str(r/(label+'-avc.test')),'-test.run=^TestOpenH264Comparison$','-test.count=1'],env=env,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,check=True)
   with (r/(label+'-pgo-check.txt')).open('a') as f:f.write('\n'.join(x for x in result.stdout.splitlines() if x.startswith('Benchmark'))+'\n')
 print(f'PGO pair {rep+1}/6',flush=True)
p=subprocess.run(['/home/define42/go/bin/benchstat',str(r/'round1-pgo-check.txt'),str(r/'round1-pgo-pgo-check.txt')],text=True,stdout=subprocess.PIPE,check=True)
(r/'pgo-check-benchstat.txt').write_text(p.stdout)
print(p.stdout,flush=True)
