from pathlib import Path
import os,subprocess,json,hashlib
r=Path(__file__).resolve().parent;e=os.environ|{'PATH':'/snap/go/current/bin:'+os.environ['PATH'],'CGO_ENABLED':'0','GOMAXPROCS':'4'}
# Diagnostic only: this executable is never installed or used for production.
cmd=['go','test','-c','-overlay='+str(r/'round6-overlay.json'),'-gcflags=github.com/define42/DevBox-Gateway-kvm/pkg/h264/...=-B','-o',str(r/'round6-bounds-diagnostic-avc.test'),'./internal/rdp/avc']
(r/'bounds-diagnostic-build.json').write_text(json.dumps(cmd,indent=2)+'\n')
subprocess.run(cmd,cwd=r/'round6',env=e,check=True)
cases=[c for c in json.loads((r/'cases.json').read_text()) if c['name'] in ['Road','PDF'] and c['width']==1920 and c['qp']==20]
assert len(cases)==2
for case in cases:
 name=f"{case['name']}-1920x1080-qp20-1s";stream=r/(name+'-bounds-diagnostic.264')
 ee=e|{'COMPARE_RUN':'1','COMPARE_CASE':case['name'],'COMPARE_I420':case['file'],'COMPARE_WIDTH':'1920','COMPARE_HEIGHT':'1080','COMPARE_QP':'20','COMPARE_SLICES':'1','COMPARE_FRAMES':'80','COMPARE_STREAM':str(stream)}
 subprocess.run(['taskset','-c','2',str(r/'round6-bounds-diagnostic-avc.test'),'-test.run=^TestOpenH264Comparison$'],env=ee,stdout=subprocess.DEVNULL,check=True)
 assert stream.read_bytes()==(r/'parity'/(name+'-after.264')).read_bytes()
 print('Diagnostic stream equal: '+name,flush=True)
for kind in ['current','no-bounds']:(r/('bounds-diagnostic-'+kind+'.txt')).write_text('')
for rep in range(6):
 for case in cases:
  for kind in (['current','no-bounds'] if rep%2==0 else ['no-bounds','current']):
   binary='after-avc.test' if kind=='current' else 'round6-bounds-diagnostic-avc.test'
   ee=e|{'COMPARE_RUN':'1','COMPARE_CASE':case['name'],'COMPARE_I420':case['file'],'COMPARE_WIDTH':'1920','COMPARE_HEIGHT':'1080','COMPARE_QP':'20','COMPARE_SLICES':'1','COMPARE_FRAMES':'100'};ee.pop('COMPARE_STREAM',None)
   result=subprocess.check_output(['taskset','-c','2',str(r/binary),'-test.run=^TestOpenH264Comparison$'],env=ee,text=True)
   row=[x for x in result.splitlines() if x.startswith('Benchmark')];assert len(row)==1
   with (r/('bounds-diagnostic-'+kind+'.txt')).open('a') as f:f.write(row[0]+'\n')
 print('bounds diagnostic pair '+str(rep+1),flush=True)
with (r/'bounds-diagnostic-benchstat.txt').open('w') as f:subprocess.run(['/home/define42/go/bin/benchstat',str(r/'bounds-diagnostic-current.txt'),str(r/'bounds-diagnostic-no-bounds.txt')],stdout=f,check=True)
