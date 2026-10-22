from pathlib import Path
import os,subprocess,json,hashlib
r=Path(__file__).resolve().parent
e=os.environ|{'CGO_ENABLED':'0','GOMAXPROCS':'4'}
subprocess.run(['/snap/go/current/bin/go','test','-c','-overlay='+str(r/'call-count-diagnostic-overlay.json'),'-o',str(r/'call-count-diagnostic-avc.test'),'./internal/rdp/avc'],cwd=r/'call-count-diagnostic',env=e,check=True)
records=[]
for label,file in [('road1','road1080.i420'),('pdf1','pdf1080.i420')]:
 env=e|{'H264_CALL_COUNT_DIAGNOSTIC':'1','COMPARE_RUN':'1','COMPARE_FRAMES':'600','COMPARE_SLICES':'1','COMPARE_WIDTH':'1920','COMPARE_HEIGHT':'1080','COMPARE_QP':'20','COMPARE_CASE':label,'COMPARE_I420':'/tmp/h264-native-parity-20261021/'+file,'COMPARE_STREAM':str(r/('call-count-'+label+'.264'))}
 with (r/('call-count-'+label+'.log')).open('w') as f:subprocess.run(['taskset','-c','2',str(r/'call-count-diagnostic-avc.test'),'-test.run=^TestOpenH264Comparison$'],env=env,stdout=f,stderr=subprocess.STDOUT,check=True)
 log=(r/('call-count-'+label+'.log')).read_text()
 assert '\nPASS\n' in log, label+' did not pass'
 totals=[json.loads(line.removeprefix('H264_CALL_COUNTS ')) for line in log.splitlines() if line.startswith('H264_CALL_COUNTS ')]
 totals=[record for record in totals if record['final']]
 assert len(totals)==2 and {record['kind'] for record in totals}=={'cavlc','initial'},label
 assert all(record['frames_including_warmup']==616 for record in totals),label
 (r/('call-count-'+label+'-totals.json')).write_text(json.dumps(totals,indent=2)+'\n')
 print(label+' diagnostic counts saved',flush=True)
