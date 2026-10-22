from pathlib import Path
import os,subprocess,shutil
r=Path(__file__).resolve().parent;review=r/'cavlc-native-review';candidate=r/'cavlc-single-next';out=r/'cavlc-single-review';out.mkdir(exist_ok=True)
for name in ['cavlc_native_full_test.go','cavlc_native_full_bench_test.go']:
 shutil.copy2(review/'go/pkg/h264/internal/encoder'/name,candidate/'pkg/h264/internal/encoder'/name)
e=os.environ|{'PATH':'/snap/go/current/bin:'+os.environ['PATH'],'CGO_ENABLED':'0','GOMAXPROCS':'1','CAVLC_NATIVE_BINARY':str(review/'native_cavlc'),'CAVLC_NATIVE_FIXTURES':str(candidate/'pkg/h264/internal/encoder/testdata/cavlc_native_single_fixtures.txt')}
with (out/'tests.txt').open('w') as f:
 subprocess.run(['go','test','./pkg/h264/internal/encoder','-run=TestCavlc','-count=1'],cwd=candidate,env=e,stdout=f,stderr=subprocess.STDOUT,check=True)
subprocess.run(['go','test','-c','-o',str(out/'after.test'),'./pkg/h264/internal/encoder'],cwd=candidate,env=e,check=True)
for kind in ['base','candidate','native']:(out/(kind+'.txt')).write_text('')
for rep in range(6):
 order=['base','candidate','native'];order=order[rep%3:]+order[:rep%3]
 if rep%2:order.reverse()
 for kind in order:
  if kind=='native':cmd=[str(review/'native_cavlc'),'bench',e['CAVLC_NATIVE_FIXTURES'],'auto','250','1','^end(3|14|15)/(zero|known-zero|sparse|dense|mixed|single-one|single-level)/prefix17$']
  else:cmd=[str(r/'cavlc-remaining-review/base.test' if kind=='base' else out/'after.test'),'-test.run=^$','-test.bench=^BenchmarkCavlcNativeFull$/^end(3|14|15)$/^(zero|known-zero|sparse|dense|mixed|single-one|single-level)$/^prefix17$','-test.cpu=1','-test.benchtime=250ms','-test.benchmem']
  p=subprocess.run(['taskset','-c','2',*cmd],env=e,capture_output=True,text=True,check=True)
  with (out/(kind+'.txt')).open('a') as f:f.write(p.stdout)
  with (out/'run.log').open('a') as f:f.write(p.stderr)
 print('single trailing-one CAVLC triple '+str(rep+1),flush=True)
for name,kinds in [('benchstat',['base','candidate','native']),('native-benchstat',['candidate','native'])]:
 with (out/(name+'.txt')).open('w') as f:subprocess.run(['/home/define42/go/bin/benchstat',*[str(out/(k+'.txt')) for k in kinds]],stdout=f,check=True)
