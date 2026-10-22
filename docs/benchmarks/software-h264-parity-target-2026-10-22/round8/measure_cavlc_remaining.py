from pathlib import Path
import os,subprocess,shutil
r=Path(__file__).resolve().parent;review=r/'cavlc-native-review';out=r/'cavlc-remaining-review';out.mkdir(exist_ok=True)
base=r/'cavlc-round7-base'
if not base.exists():shutil.copytree(r/'round7',base)
variants={'base':base,'table128':r/'cavlc-table128-next','packed':r/'cavlc-slow-packed-next'}
shutil.copy2(r/'cavlc-table128-next/pkg/h264/internal/encoder/cavlc_packed_bench_test.go',base/'pkg/h264/internal/encoder/cavlc_packed_bench_test.go')
e=os.environ|{'PATH':'/snap/go/current/bin:'+os.environ['PATH'],'CGO_ENABLED':'0','GOMAXPROCS':'1','CAVLC_NATIVE_BINARY':str(review/'native_cavlc'),'CAVLC_NATIVE_FIXTURES':str(review/'go/pkg/h264/internal/encoder/testdata/cavlc_native_fixtures.txt')}
for kind,src in variants.items():
 for name in ['cavlc_native_full_test.go','cavlc_native_full_bench_test.go']:
  shutil.copy2(review/'go/pkg/h264/internal/encoder'/name,src/'pkg/h264/internal/encoder'/name)
 print('test/build '+kind,flush=True)
 with (out/(kind+'-tests.txt')).open('w') as f:
  subprocess.run(['go','test','./pkg/h264/internal/encoder','-run=TestCavlc','-count=1'],cwd=src,env=e,stdout=f,stderr=subprocess.STDOUT,check=True)
 subprocess.run(['go','test','-c','-o',str(out/(kind+'.test')),'./pkg/h264/internal/encoder'],cwd=src,env=e,check=True)
for kind in [*variants,'native']:(out/(kind+'.txt')).write_text('')
for rep in range(8):
 order=[*variants,'native'];order=order[rep%4:]+order[:rep%4]
 if rep>=4:order.reverse()
 for kind in order:
  if kind=='native':cmd=[str(review/'native_cavlc'),'bench',e['CAVLC_NATIVE_FIXTURES'],'auto','250','1','^end(3|14|15)/(zero|known-zero|sparse|dense|mixed)/prefix17$']
  else:cmd=[str(out/(kind+'.test')),'-test.run=^$','-test.bench=^BenchmarkCavlcNativeFull$/^end(3|14|15)$/^(zero|known-zero|sparse|dense|mixed)$/^prefix17$','-test.cpu=1','-test.benchtime=250ms','-test.benchmem']
  p=subprocess.run(['taskset','-c','2',*cmd],env=e,capture_output=True,text=True,check=True)
  with (out/(kind+'.txt')).open('a') as f:f.write(p.stdout)
  with (out/(kind+'.log')).open('a') as f:f.write(p.stderr)
 print('remaining CAVLC quartet '+str(rep+1),flush=True)
for name,kinds in [('table128-benchstat',['base','table128']),('packed-benchstat',['base','packed']),('native-benchstat',['base','table128','packed','native'])]:
 with (out/(name+'.txt')).open('w') as f:
  subprocess.run(['/home/define42/go/bin/benchstat',*[str(out/(k+'.txt')) for k in kinds]],stdout=f,check=True)

# Independent wider-range Go controls; native fixture timings remain above.
for kind in variants:(out/('levels-'+kind+'.txt')).write_text('')
for rep in range(6):
 order=list(variants);order=order[rep%3:]+order[:rep%3]
 if rep%2:order.reverse()
 for kind in order:
  p=subprocess.run(['taskset','-c','2',str(out/(kind+'.test')),'-test.run=^$','-test.bench=^BenchmarkCavlcLevelWriter$','-test.cpu=1','-test.benchtime=250ms','-test.benchmem'],env=e,capture_output=True,text=True,check=True)
  with (out/('levels-'+kind+'.txt')).open('a') as f:f.write(p.stdout)
 print('level writer triple '+str(rep+1),flush=True)
with (out/'levels-benchstat.txt').open('w') as f:
 subprocess.run(['/home/define42/go/bin/benchstat',*[str(out/('levels-'+k+'.txt')) for k in variants]],stdout=f,check=True)
