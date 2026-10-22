from pathlib import Path
import os,subprocess,json,hashlib
r=Path(__file__).resolve().parent
e=os.environ|{'CGO_ENABLED':'0','GOMAXPROCS':'4'}
subprocess.run(['/snap/go/current/bin/go','test','-c','-overlay='+str(r/'sparse-count-diagnostic-overlay.json'),'-o',str(r/'sparse-count-diagnostic-avc.test'),'./internal/rdp/avc'],cwd=r/'sparse-count-diagnostic',env=e,check=True)
records=[]
for label,file in [('road1','road1080.i420'),('pdf1','pdf1080.i420')]:
 env=e|{'H264_CALL_COUNT_DIAGNOSTIC':'1','COMPARE_RUN':'1','COMPARE_FRAMES':'600','COMPARE_SLICES':'1','COMPARE_WIDTH':'1920','COMPARE_HEIGHT':'1080','COMPARE_QP':'20','COMPARE_CASE':label,'COMPARE_I420':'/tmp/h264-native-parity-20261021/'+file,'COMPARE_STREAM':str(r/('sparse-count-'+label+'.264'))}
 with (r/('sparse-count-'+label+'.log')).open('w') as f:subprocess.run(['taskset','-c','2',str(r/'sparse-count-diagnostic-avc.test'),'-test.run=^TestOpenH264Comparison$'],env=env,stdout=f,stderr=subprocess.STDOUT,check=True)
 log=(r/('sparse-count-'+label+'.log')).read_text()
 assert '\nPASS\n' in log, label+' did not pass'
 totals=[json.loads(line.removeprefix('H264_CALL_COUNTS ')) for line in log.splitlines() if line.startswith('H264_CALL_COUNTS ')]
 totals=[record for record in totals if record['final']]
 assert len(totals)==2 and {record['kind'] for record in totals}=={'cavlc','initial'},label
 assert all(record['frames_including_warmup']==616 for record in totals),label
 (r/('sparse-count-'+label+'-totals.json')).write_text(json.dumps(totals,indent=2)+'\n')
 print(label+' diagnostic counts saved',flush=True)

for label in ["road1","pdf1"]:
 def sha(p):
  with p.open("rb") as f:return hashlib.file_digest(f,"sha256").hexdigest()
 assert sha(r/("sparse-count-"+label+".264"))==sha(r/("call-count-"+label+".264")),label
 print(label+" sparse diagnostic stream parity passed",flush=True)
