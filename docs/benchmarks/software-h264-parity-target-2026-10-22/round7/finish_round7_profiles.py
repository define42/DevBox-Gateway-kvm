from pathlib import Path
import subprocess,json,hashlib,os,sys
r=Path(__file__).resolve().parent
# Capture diagnostic comparison streams separately from the CPU profiles.
# --reuse-streams is for re-profiling an unchanged binary with existing checked streams.
if '--reuse-streams' not in sys.argv:
 for label,file in [('road1','road1080.i420'),('pdf1','pdf1080.i420')]:
  e=os.environ|{'GOMAXPROCS':'4','COMPARE_RUN':'1','COMPARE_FRAMES':'600','COMPARE_SLICES':'1','COMPARE_WIDTH':'1920','COMPARE_HEIGHT':'1080','COMPARE_QP':'20','COMPARE_CASE':label,'COMPARE_I420':'/tmp/h264-native-parity-20261021/'+file,'COMPARE_STREAM':str(r/('round7-'+label+'.264'))}
  subprocess.run(['taskset','-c','2',str(r/'after-avc.test'),'-test.run=^TestOpenH264Comparison$'],env=e,stdout=subprocess.DEVNULL,check=True)
subprocess.run(['python3',str(r/'profile_round.py'),'round7'],check=True)
records=[]
for label in ['road1','pdf1']:
 totals=json.loads((r/('call-count-'+label+'-totals.json')).read_text())
 assert len(totals)==2 and all(x['final'] and x['frames_including_warmup']==616 for x in totals)
 def sha(p):return hashlib.file_digest(p.open('rb'),'sha256').hexdigest()
 normal=sha(r/('round7-'+label+'.264'));diagnostic=sha(r/('call-count-'+label+'.264'));assert normal==diagnostic,(label,normal,diagnostic)
 records.append({'case':label,'frames':616,'normal_sha256':normal,'diagnostic_sha256':diagnostic,'equal':True})
 print(label+' diagnostic stream identical',flush=True)
(r/'call-count-parity.json').write_text(json.dumps(records,indent=2)+'\n')
