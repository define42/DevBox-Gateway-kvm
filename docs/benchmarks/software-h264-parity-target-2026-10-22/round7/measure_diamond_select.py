from pathlib import Path
import os,subprocess,shutil
r=Path(__file__).resolve().parent;base=r/'component-round7-base';e=os.environ|{'PATH':'/snap/go/current/bin:'+os.environ['PATH'],'CGO_ENABLED':'0','GOMAXPROCS':'4'}
if not base.exists():shutil.copytree(r/'round6',base)
for name in ['diamond_cost_select_test.go','diamond_cost_select_bench_test.go']:
 shutil.copy2(r/'diamond-select-next/pkg/h264/internal/encoder'/name,base/'pkg/h264/internal/encoder'/name)
for kind in ['component-round7-base','diamond-select-next']:
 print('test/build '+kind,flush=True)
 subprocess.run(['go','test','./pkg/h264/internal/encoder','-run=TestDiamondCostSelect','-count=1'],cwd=r/kind,env=e,check=True)
 subprocess.run(['go','test','-c','-o',str(r/(kind+'-encoder.test')),'./pkg/h264/internal/encoder'],cwd=r/kind,env=e,check=True)
 (r/('diamond-select-'+kind+'.txt')).write_text('')
for rep in range(6):
 for kind in (['component-round7-base','diamond-select-next'] if rep%2==0 else ['diamond-select-next','component-round7-base']):
  o=subprocess.check_output(['taskset','-c','2',str(r/(kind+'-encoder.test')),'-test.run=^$','-test.bench=^BenchmarkDiamondCostSelect$','-test.benchtime=250ms'],env=e,text=True)
  with (r/('diamond-select-'+kind+'.txt')).open('a') as f:f.write(o)
 print('diamond select pair '+str(rep+1),flush=True)
with (r/'diamond-select-benchstat.txt').open('w') as f:subprocess.run(['/home/define42/go/bin/benchstat',str(r/'diamond-select-component-round7-base.txt'),str(r/'diamond-select-diamond-select-next.txt')],stdout=f,check=True)
