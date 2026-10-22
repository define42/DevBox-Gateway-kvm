from pathlib import Path
import subprocess,os,json,argparse
r=Path(__file__).resolve().parent
p=argparse.ArgumentParser();p.add_argument('candidate');p.add_argument('--baseline',default='round4');p.add_argument('--frames',type=int,default=100);p.add_argument('--repetitions',type=int,default=6);a=p.parse_args()
e=os.environ|{'PATH':'/snap/go/current/bin:'+os.environ['PATH'],'CGO_ENABLED':'0','GOMAXPROCS':'4'}
for kind in [a.baseline,a.candidate]:
 overlay=r/(kind+'-candidate-overlay.json');overlay.write_text(json.dumps({'Replace':{str(r/kind/'internal/rdp/avc/z_openh264_comparison_test.go'):str(r/'compare_test.go')}}))
 subprocess.run(['go','test','-c','-overlay='+str(overlay),'-o',str(r/(kind+'-candidate-avc.test')),'./internal/rdp/avc'],cwd=r/kind,env=e,check=True)
 (r/(a.candidate+'-codec-'+kind+'.txt')).write_text('')
cases=[c for c in json.loads((r/'cases.json').read_text()) if c['width']==1920 and c['qp']==20]
for rep in range(a.repetitions):
 for case in cases:
  for sl in [1,4]:
   for kind in ([a.baseline,a.candidate] if rep%2==0 else [a.candidate,a.baseline]):
    ee=e|{'COMPARE_RUN':'1','COMPARE_CASE':case['name'],'COMPARE_I420':case['file'],'COMPARE_WIDTH':str(case['width']),'COMPARE_HEIGHT':str(case['height']),'COMPARE_QP':str(case['qp']),'COMPARE_SLICES':str(sl),'COMPARE_FRAMES':str(a.frames)}
    ee.pop('COMPARE_STREAM',None)
    o=subprocess.check_output(['taskset','-c','2' if sl==1 else '0-3',str(r/(kind+'-candidate-avc.test')),'-test.run=^TestOpenH264Comparison$'],env=ee,text=True)
    row=[line for line in o.splitlines() if line.startswith('Benchmark')];assert len(row)==1
    with (r/(a.candidate+'-codec-'+kind+'.txt')).open('a') as f:f.write(row[0]+'\n')
  print(a.candidate+' codec pair '+str(rep+1)+' '+case['name'],flush=True)
with (r/(a.candidate+'-codec-benchstat.txt')).open('w') as f:subprocess.run(['/home/define42/go/bin/benchstat',str(r/(a.candidate+'-codec-'+a.baseline+'.txt')),str(r/(a.candidate+'-codec-'+a.candidate+'.txt'))],stdout=f,check=True)
