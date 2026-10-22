from pathlib import Path
import os,subprocess,sys
r=Path(__file__).resolve().parent;candidate,pkg,pattern=sys.argv[1:];e=os.environ|{'GOMAXPROCS':'4'}
for rep in range(6):
 for kind in (['component-round6-base',candidate] if rep%2==0 else [candidate,'component-round6-base']):
  o=subprocess.check_output(['taskset','-c','2',str(r/(kind+'-'+pkg+'.test')),'-test.run=^$','-test.bench='+pattern,'-test.benchtime=250ms'],env=e,text=True)
  with (r/(candidate+'-'+kind+'.txt')).open('a') as f:f.write(o)
 print(candidate+' extra pair '+str(rep+1),flush=True)
with (r/(candidate+'-benchstat.txt')).open('w') as f:subprocess.run(['/home/define42/go/bin/benchstat',str(r/(candidate+'-component-round6-base.txt')),str(r/(candidate+'-'+candidate+'.txt'))],stdout=f,check=True)
