from pathlib import Path
import os,shutil,subprocess,sys
r=Path(__file__).resolve().parent;e=os.environ|{'PATH':'/snap/go/current/bin:'+os.environ['PATH'],'CGO_ENABLED':'0','GOMAXPROCS':'4'}
base=r/'component-round6-base'
if not base.exists():shutil.copytree(r/'round5',base)
configs={'center-pairs-next':('common','^BenchmarkMcCenterPairs$'), 'transform-abi-next':('encoder','^Benchmark(TransformABI|IntraReconstruction)$')}
for candidate in sys.argv[1:]:
 pkg,pattern=configs[candidate]
 if candidate=='center-pairs-next':
  name='mc_refine_center_pairs_bench_amd64_test.go';shutil.copy2(r/candidate/'pkg/h264/internal/common'/name,base/'pkg/h264/internal/common'/name)
 else:
  for name in ['intra_reconstruction_bench_test.go','intra_reconstruction_test.go']:
   shutil.copy2(r/candidate/'pkg/h264/internal/encoder'/name,base/'pkg/h264/internal/encoder'/name)
  shutil.copy2(r/'transform-abi-baseline-bench/transform_abi_bench_test.go',base/'pkg/h264/internal/encoder/transform_abi_bench_test.go')
 for kind in ['component-round6-base',candidate]:
  print('test/build '+kind+'/'+pkg,flush=True)
  subprocess.run(['go','test','./pkg/h264/internal/'+pkg],cwd=r/kind,env=e,check=True)
  subprocess.run(['go','test','-c','-o',str(r/(kind+'-'+pkg+'.test')),'./pkg/h264/internal/'+pkg],cwd=r/kind,env=e,check=True)
  (r/(candidate+'-'+kind+'.txt')).write_text('')
 for rep in range(6):
  for kind in (['component-round6-base',candidate] if rep%2==0 else [candidate,'component-round6-base']):
   o=subprocess.check_output(['taskset','-c','2',str(r/(kind+'-'+pkg+'.test')),'-test.run=^$','-test.bench='+pattern,'-test.benchtime=250ms'],env=e,text=True)
   with (r/(candidate+'-'+kind+'.txt')).open('a') as f:f.write(o)
  print(candidate+' pair '+str(rep+1),flush=True)
 with (r/(candidate+'-benchstat.txt')).open('w') as f:subprocess.run(['/home/define42/go/bin/benchstat',str(r/(candidate+'-component-round6-base.txt')),str(r/(candidate+'-'+candidate+'.txt'))],stdout=f,check=True)
