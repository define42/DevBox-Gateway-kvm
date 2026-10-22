import subprocess,os,pathlib,shutil
r=pathlib.Path(__file__).resolve().parent
env=os.environ|{'PATH':'/snap/go/current/bin:'+os.environ['PATH'],'CGO_ENABLED':'0','GOMAXPROCS':'4'}
for package in ['common','encoder']:
 subprocess.run(['go','test','-c','-o',str(r/f'after-{package}.test'),f'./pkg/h264/internal/{package}'],cwd=r/'round3',env=env,check=True)
subprocess.run(['go','test','-c','-overlay='+str(r/'round3-overlay.json'),'-o',str(r/'after-avc.test'),'./internal/rdp/avc'],cwd=r/'round3',env=env,check=True)
for label in ['before','after']:(r/f'{label}-final-components.txt').write_text('')
for rep in range(6):
 for label in (['before','after'] if rep%2==0 else ['after','before']):
  for package,pattern in [('encoder','^BenchmarkEncodingBlockCopy$'),('common','^Benchmark(IntegerMotionCopy|SampleSadFourRows)$')]:
   output=subprocess.check_output(['taskset','-c','2',str(r/f'{label}-{package}.test'),'-test.run=^$','-test.bench='+pattern,'-test.benchtime=200ms'],env=env,text=True)
   with (r/f'{label}-final-components.txt').open('a') as f:f.write(output)
 print(f'final component pair {rep+1}/6',flush=True)
with (r/'final-components-benchstat.txt').open('w') as f:subprocess.run(['/home/define42/go/bin/benchstat',str(r/'before-final-components.txt'),str(r/'after-final-components.txt')],stdout=f,check=True)
print('complete-stream preflight',flush=True)
subprocess.run(['python3',str(r/'measure.py'),'--preflight'],env=env,check=True)
print('complete native comparison',flush=True)
subprocess.run(['python3',str(r/'measure.py')],env=env,check=True)
with (r/'codec-benchstat.txt').open('w') as f:subprocess.run(['/home/define42/go/bin/benchstat',str(r/'measure/before.txt'),str(r/'measure/after.txt'),str(r/'measure/native.txt')],stdout=f,check=True)
print('round3 measurements complete',flush=True)
