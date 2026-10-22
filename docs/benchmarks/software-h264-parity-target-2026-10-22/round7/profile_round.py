from pathlib import Path
import os,subprocess,sys
r=Path(__file__).resolve().parent;round_name=sys.argv[1]
for label,file in [('road1','road1080.i420'),('pdf1','pdf1080.i420')]:
 e=os.environ.copy();e.update(GOMAXPROCS='4',COMPARE_RUN='1',COMPARE_FRAMES='600',COMPARE_SLICES='1',COMPARE_WIDTH='1920',COMPARE_HEIGHT='1080',COMPARE_QP='20',COMPARE_CASE=label,COMPARE_I420='/tmp/h264-native-parity-20261021/'+file)
 stem=round_name+'-'+label
 # Stream accumulation is harness work outside codec timing and must not pollute CPU profiles.
 e.pop('COMPARE_STREAM',None)
 with (r/(stem+'-profile.txt')).open('w') as f:subprocess.run(['taskset','-c','2',str(r/'after-avc.test'),'-test.run=^TestOpenH264Comparison$','-test.cpuprofile='+str(r/(stem+'.pprof'))],env=e,stdout=f,stderr=subprocess.STDOUT,check=True)
 for suffix,args in [('top',[]),('cum',['-cum'])]:
  with (r/(stem+'-'+suffix+'.txt')).open('w') as f:subprocess.run(['/snap/go/current/bin/go','tool','pprof','-top','-nodecount=90',*args,str(r/'after-avc.test'),str(r/(stem+'.pprof'))],stdout=f,check=True)
 print(stem+' profile saved',flush=True)
