from pathlib import Path
import os, shutil, subprocess
r=Path(__file__).resolve().parent
out=r/'center-stack-tuned-review';out.mkdir(exist_ok=True)
candidate=r/'center-filter-stack-tuned-next';base=r/'center-stack-round9-base'
if not base.exists():shutil.copytree(r/'round9',base)
rel=Path('pkg/h264/internal/common/mc_center_stack_bench_amd64_test.go')
shutil.copy2(candidate/rel,base/rel)
env=os.environ|{'PATH':'/snap/go/current/bin:'+os.environ['PATH'],'CGO_ENABLED':'0','GOMAXPROCS':'4'}
packages=[('common','^(TestMcCenterStack|TestMcCenterPairs|TestMcAVX2Refinement)','^BenchmarkMcCenterWholeCaller$'),('encoder','^(TestHalf|TestQuarter|TestMeRefine|TestFractional)','^BenchmarkMeRefineFracPixelCaller$')]
for kind,src in [('base',base),('after',candidate)]:
 print('test/build '+kind,flush=True)
 for package,tests,bench in packages:
  with (out/(kind+'-'+package+'-tests.txt')).open('w') as f:
   subprocess.run(['go','test','./pkg/h264/internal/'+package,'-run='+('.' if kind=='after' else tests),'-count=1'],cwd=src,env=env,stdout=f,stderr=subprocess.STDOUT,check=True)
  subprocess.run(['go','test','-c','-o',str(out/(kind+'-'+package+'.test')),'./pkg/h264/internal/'+package],cwd=src,env=env,check=True)
 (out/(kind+'.txt')).write_text('')
for rep in range(6):
 for kind in (['base','after'] if rep%2==0 else ['after','base']):
  for package,tests,bench in packages:
   res=subprocess.run(['taskset','-c','2',str(out/(kind+'-'+package+'.test')),'-test.run=^$','-test.bench='+bench,'-test.benchtime=200ms','-test.benchmem'],env=env,capture_output=True,text=True,check=True)
   with (out/(kind+'.txt')).open('a') as f:f.write(res.stdout)
 print('center/complete-refinement pair '+str(rep+1),flush=True)
with (out/'benchstat.txt').open('w') as f:subprocess.run(['/home/define42/go/bin/benchstat',str(out/'base.txt'),str(out/'after.txt')],stdout=f,check=True)
