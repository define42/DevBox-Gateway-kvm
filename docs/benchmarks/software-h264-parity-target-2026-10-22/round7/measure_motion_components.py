from pathlib import Path
import os,subprocess,shutil
r=Path(__file__).resolve().parent
base=r/'motion-round6-base'
if not base.exists(): shutil.copytree(r/'round6',base)
shutil.copy2(r/'motion-batch-next/pkg/h264/internal/encoder/sample_quarter_four_bench_amd64_test.go',base/'pkg/h264/internal/encoder/sample_quarter_four_bench_amd64_test.go')
e=os.environ|{'PATH':'/snap/go/current/bin:'+os.environ['PATH'],'CGO_ENABLED':'0','GOMAXPROCS':'4'}
for kind in ['motion-round6-base','motion-batch-next']:
 print('test/build '+kind,flush=True)
 subprocess.run(['go','test','./pkg/h264/internal/encoder','-run=Test(SampleAvgSatdFour|QuarterRefine|SampleAvgSatdDispatch)','-count=1'],cwd=r/kind,env=e,check=True)
 subprocess.run(['go','test','-c','-o',str(r/(kind+'-encoder.test')),'./pkg/h264/internal/encoder'],cwd=r/kind,env=e,check=True)
 (r/('motion-component-'+kind+'.txt')).write_text('')
for rep in range(6):
 for kind in (['motion-round6-base','motion-batch-next'] if rep%2==0 else ['motion-batch-next','motion-round6-base']):
  o=subprocess.check_output(['taskset','-c','2',str(r/(kind+'-encoder.test')),'-test.run=^$','-test.bench=^BenchmarkQuarterRefine(Shapes|FourCandidates)$','-test.benchtime=200ms'],env=e,text=True)
  with (r/('motion-component-'+kind+'.txt')).open('a') as f:f.write(o)
 print('motion pair '+str(rep+1),flush=True)
with (r/'motion-component-benchstat.txt').open('w') as f:subprocess.run(['/home/define42/go/bin/benchstat',str(r/'motion-component-motion-round6-base.txt'),str(r/'motion-component-motion-batch-next.txt')],stdout=f,check=True)
