from pathlib import Path
import subprocess,os
r=Path(__file__).resolve().parent;e=os.environ|{'PATH':'/snap/go/current/bin:'+os.environ['PATH'],'CGO_ENABLED':'0','GOMAXPROCS':'4'}
for kind in ['vaa-next','satd-schedule-next']:
 print('test/build '+kind,flush=True)
 subprocess.run(['go','test','./pkg/h264/internal/encoder'],cwd=r/kind,env=e,check=True)
 subprocess.run(['go','test','-c','-o',str(r/(kind+'-encoder.test')),'./pkg/h264/internal/encoder'],cwd=r/kind,env=e,check=True)
for cand,pattern in [('vaa-next','^BenchmarkIntraVaaDispatch$'),('satd-schedule-next','^BenchmarkSampleSatd8Rows$')]:
 for kind in ['component-round5-base',cand]:(r/(cand+'-'+kind+'.txt')).write_text('')
 for rep in range(6):
  for kind in (['component-round5-base',cand] if rep%2==0 else [cand,'component-round5-base']):
   o=subprocess.check_output(['taskset','-c','2',str(r/(kind+'-encoder.test')),'-test.run=^$','-test.bench='+pattern,'-test.benchtime=250ms'],env=e,text=True)
   with (r/(cand+'-'+kind+'.txt')).open('a') as f:f.write(o)
  print(cand+' pair '+str(rep+1),flush=True)
 with (r/(cand+'-benchstat.txt')).open('w') as f:subprocess.run(['/home/define42/go/bin/benchstat',str(r/(cand+'-component-round5-base.txt')),str(r/(cand+'-'+cand+'.txt'))],stdout=f,check=True)
