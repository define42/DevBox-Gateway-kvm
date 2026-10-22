import os,subprocess,pathlib,shutil
r=pathlib.Path(__file__).resolve().parent
env=os.environ|{'PATH':'/snap/go/current/bin:'+os.environ['PATH'],'CGO_ENABLED':'0','GOMAXPROCS':'4'}
# Keep round3's frozen source fingerprint intact.
base=r/'component-next-base';shutil.copytree(r/'round3',base,dirs_exist_ok=True)
for f in ['inter_luma_bookkeeping_bench_test.go','inter_luma_bookkeeping_test.go']:
 shutil.copy2(r/'inter-next/pkg/h264/internal/encoder'/f,base/'pkg/h264/internal/encoder'/f)
for kind,package in [('component-next-base','encoder'),('component-next-base','common'),('quant-next','encoder'),('inter-next','encoder'),('sad-next','common')]:
 print(f'test/build {kind}/{package}',flush=True)
 subprocess.run(['go','test',f'./pkg/h264/internal/{package}'],cwd=r/kind,env=env,check=True)
 subprocess.run(['go','test','-c','-o',str(r/f'{kind}-{package}.test'),f'./pkg/h264/internal/{package}'],cwd=r/kind,env=env,check=True)
experiments=[('quant-next','encoder',['^BenchmarkQuantScanFour4x4$/././Fusedtrue$','^BenchmarkQuantScanLuma$/./Batched$']),('inter-next','encoder',['^BenchmarkInterLumaBookkeeping$']),('sad-next','common',['^BenchmarkSampleSadPackedRows$'])]
for candidate,package,patterns in experiments:
 for kind in ['component-next-base',candidate]:(r/f'{candidate}-{kind}-results.txt').write_text('')
 for rep in range(6):
  for kind in (['component-next-base',candidate] if rep%2==0 else [candidate,'component-next-base']):
   for pattern in patterns:
    output=subprocess.check_output(['taskset','-c','2',str(r/f'{kind}-{package}.test'),'-test.run=^$','-test.bench='+pattern,'-test.benchtime=250ms'],env=env,text=True)
    with (r/f'{candidate}-{kind}-results.txt').open('a') as f:f.write(output)
  print(f'{candidate} pair {rep+1}/6',flush=True)
 with (r/f'{candidate}-benchstat.txt').open('w') as f:subprocess.run(['/home/define42/go/bin/benchstat',str(r/f'{candidate}-component-next-base-results.txt'),str(r/f'{candidate}-{candidate}-results.txt')],stdout=f,check=True)
print('next candidates measured',flush=True)
