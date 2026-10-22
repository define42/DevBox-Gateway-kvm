import subprocess,os,pathlib
r=pathlib.Path(__file__).resolve().parent
env=os.environ|{'PATH':'/snap/go/current/bin:'+os.environ['PATH'],'CGO_ENABLED':'0','GOMAXPROCS':'4'}
for label in ['before','round1']:
 for package in ['common','encoder']:
  subprocess.run(['go','test','-c','-o',str(r/f'{label}-{package}.test'),f'./pkg/h264/internal/{package}'],cwd=r/label,env=env,check=True)
 subprocess.run(['go','test','-c','-overlay='+str(r/(label+'-overlay.json')),'-o',str(r/f'{label}-avc.test'),'./internal/rdp/avc'],cwd=r/label,env=env,check=True)
for label in ['before','round1']:
 (r/f'{label}-components.txt').write_text('')
for n in range(6):
 for label in (['before','round1'] if n%2==0 else ['round1','before']):
  for package,pattern in [('encoder','^Benchmark(EncodingBlockCopy|IDctReconstruction|QuarterRefineShapes)$'),('common','^BenchmarkIntegerMotionCopy$')]:
   result=subprocess.run(['taskset','-c','2',str(r/f'{label}-{package}.test'),'-test.run=^$','-test.bench='+pattern,'-test.benchtime=200ms','-test.count=1'],env=env,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,check=True)
   with (r/f'{label}-components.txt').open('a') as f:f.write(result.stdout)
 print(f'components pair {n+1}/6',flush=True)
p=subprocess.run(['/home/define42/go/bin/benchstat',str(r/'before-components.txt'),str(r/'round1-components.txt')],text=True,stdout=subprocess.PIPE,check=True)
(r/'round1-components-benchstat.txt').write_text(p.stdout)
print(p.stdout,flush=True)
