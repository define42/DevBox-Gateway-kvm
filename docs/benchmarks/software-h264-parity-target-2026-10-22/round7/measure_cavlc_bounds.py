from pathlib import Path
import os,subprocess
r=Path(__file__).resolve().parent
e=os.environ|{'PATH':'/snap/go/current/bin:'+os.environ['PATH'],'CGO_ENABLED':'0','GOMAXPROCS':'4'}
for kind in ['round6','cavlc-bounds-next']:
 print('test/build '+kind,flush=True)
 subprocess.run(['go','test','./pkg/h264/internal/encoder','-run=Test(Cavlc|BsFlush)','-count=1'],cwd=r/kind,env=e,check=True)
 subprocess.run(['go','test','-c','-o',str(r/(kind+'-cavlc.test')),'./pkg/h264/internal/encoder'],cwd=r/kind,env=e,check=True)
 (r/('cavlc-bounds-'+kind+'.txt')).write_text('')
for rep in range(6):
 for kind in (['round6','cavlc-bounds-next'] if rep%2==0 else ['cavlc-bounds-next','round6']):
  o=subprocess.check_output(['taskset','-c','2',str(r/(kind+'-cavlc.test')),'-test.run=^$','-test.bench=^BenchmarkCavlc(LevelWriter|Residual)$','-test.benchtime=200ms'],env=e,text=True)
  with (r/('cavlc-bounds-'+kind+'.txt')).open('a') as f:f.write(o)
 print('CAVLC bounds pair '+str(rep+1),flush=True)
with (r/'cavlc-bounds-benchstat.txt').open('w') as f:subprocess.run(['/home/define42/go/bin/benchstat',str(r/'cavlc-bounds-round6.txt'),str(r/'cavlc-bounds-cavlc-bounds-next.txt')],stdout=f,check=True)
