from pathlib import Path
import os,subprocess,shutil
r=Path(__file__).resolve().parent
repo=Path('/home/define42/git/DevBox-Gateway-kvm')
base=r/'quarter-caller-base'
if not base.exists(): shutil.copytree(r/'satd-wrapper-next',base)
f='pkg/h264/internal/encoder/sample_quarter_caller_bench_amd64_test.go'
shutil.copy2(repo/f,base/f)
e=os.environ|{'PATH':'/snap/go/current/bin:'+os.environ['PATH'],'CGO_ENABLED':'0','GOMAXPROCS':'4'}
for kind,src in [('base',base),('after',repo)]:
 print('test/build '+kind,flush=True)
 out=subprocess.check_output(['go','test','./pkg/h264/internal/encoder','-run=Test(SampleAvgSatdFour|QuarterRefine|SampleAvgSatdDispatch|MeRefineFracPixelNoPerBlockAllocs)','-count=1'],cwd=src,env=e,text=True)
 (r/('quarter-caller-'+kind+'-tests.txt')).write_text(out)
 print(out,flush=True)
 subprocess.run(['go','test','-c','-o',str(r/('quarter-caller-'+kind+'.test')),'./pkg/h264/internal/encoder'],cwd=src,env=e,check=True)
 (r/('quarter-caller-'+kind+'.txt')).write_text('')
for rep in range(6):
 for kind in (['base','after'] if rep%2==0 else ['after','base']):
  out=subprocess.check_output(['taskset','-c','2',str(r/('quarter-caller-'+kind+'.test')),'-test.run=^$','-test.bench=^(BenchmarkQuarterRefineLocalParams|BenchmarkMeRefineFracPixelCaller)$','-test.benchtime=200ms','-test.benchmem'],env=e,text=True)
  with (r/('quarter-caller-'+kind+'.txt')).open('a') as f:f.write(out)
 print('caller pair '+str(rep+1),flush=True)
with (r/'quarter-caller-benchstat.txt').open('w') as f:
 subprocess.run(['/home/define42/go/bin/benchstat',str(r/'quarter-caller-base.txt'),str(r/'quarter-caller-after.txt')],stdout=f,check=True)
print('caller benchmark complete',flush=True)
