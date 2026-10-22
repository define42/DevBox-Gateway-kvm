import subprocess,os,pathlib
r=pathlib.Path(__file__).resolve().parent
repo=pathlib.Path('/home/define42/git/DevBox-Gateway-kvm')
env=os.environ|{'PATH':'/snap/go/current/bin:'+os.environ['PATH'],'CGO_ENABLED':'0','GOMAXPROCS':'4'}
for label,root in [('before',r/'before'),('round2',repo)]:
 for package in ['common','encoder']:
  subprocess.run(['go','test','-c','-o',str(r/f'{label}-{package}.test'),f'./pkg/h264/internal/{package}'],cwd=root,env=env,check=True)
 (r/f'{label}-round2-components.txt').write_text('')
for n in range(6):
 for label in (['before','round2'] if n%2==0 else ['round2','before']):
  for package,pattern in [('encoder','^BenchmarkEncodingBlockCopy$'),('encoder','^BenchmarkCavlcExtractionResidual$/^end(3|14|15)$/./^scalar=false$'),('encoder','^BenchmarkCavlcBitWriter$/./^prior=false$'),('common','^Benchmark(IntegerMotionCopy|SampleSadPackedRows|SampleSadFourRows)$')]:
   result=subprocess.run(['taskset','-c','2',str(r/f'{label}-{package}.test'),'-test.run=^$','-test.bench='+pattern,'-test.benchtime=150ms','-test.count=1'],env=env,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,check=True)
   with (r/f'{label}-round2-components.txt').open('a') as f:f.write(result.stdout)
 print(f'components pair {n+1}/6',flush=True)
p=subprocess.run(['/home/define42/go/bin/benchstat',str(r/'before-round2-components.txt'),str(r/'round2-round2-components.txt')],text=True,stdout=subprocess.PIPE,check=True)
(r/'round2-components-benchstat.txt').write_text(p.stdout)
print('round2 benchmarks complete',flush=True)
