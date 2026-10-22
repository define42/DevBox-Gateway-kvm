from pathlib import Path
import os,subprocess,shutil
r=Path(__file__).resolve().parent
base=r/'plane-round6-base'
if not base.exists(): shutil.copytree(r/'round6',base)
shutil.copy2(r/'plane-next/pkg/h264/internal/encoder/intra_plane_full_bench_test.go',base/'pkg/h264/internal/encoder/intra_plane_full_bench_test.go')
e=os.environ|{'PATH':'/snap/go/current/bin:'+os.environ['PATH'],'CGO_ENABLED':'0','GOMAXPROCS':'4'}
for kind in ['plane-round6-base','plane-next']:
 print('test/build '+kind,flush=True)
 subprocess.run(['go','test','./pkg/h264/internal/encoder','-run=Test(IntraPlaneFull|IntraPlaneSIMD|IntraPredictorSIMD|IntraModeDecisionSIMD|IntraSIMD)','-count=1'],cwd=r/kind,env=e,check=True)
 subprocess.run(['go','test','-c','-o',str(r/(kind+'-encoder.test')),'./pkg/h264/internal/encoder'],cwd=r/kind,env=e,check=True)
 (r/('plane-component-'+kind+'.txt')).write_text('')
for rep in range(6):
 for kind in (['plane-round6-base','plane-next'] if rep%2==0 else ['plane-next','plane-round6-base']):
  o=subprocess.check_output(['taskset','-c','2',str(r/(kind+'-encoder.test')),'-test.run=^$','-test.bench=^BenchmarkIntraPlane(Complete|ModeChain)$','-test.benchtime=200ms'],env=e,text=True)
  with (r/('plane-component-'+kind+'.txt')).open('a') as f:f.write(o)
 print('plane pair '+str(rep+1),flush=True)
with (r/'plane-component-benchstat.txt').open('w') as f:subprocess.run(['/home/define42/go/bin/benchstat',str(r/'plane-component-plane-round6-base.txt'),str(r/'plane-component-plane-next.txt')],stdout=f,check=True)
