import os, subprocess, json, time
from pathlib import Path
root=Path('/tmp/devbox-native-review')
results=root/'results-asm-800'
results.mkdir(exist_ok=True)
configs=[('native_adapter','native',4,'adapter'),('purego_four','purego',4,'adapter'),('native_raw','native',4,'raw_binding'),('purego_one','purego',1,'adapter')]
with (results/'encoding.jsonl').open('w') as collected:
 for repeat in range(6):
  order=configs if repeat%2==0 else list(reversed(configs))
  for label,impl,slices,boundary in order:
   name=f'road800_qp32_{label}_{repeat+1}'
   env=os.environ.copy()
   env.update(GOMAXPROCS='4',COMPARE_RUN='1',COMPARE_IMPL=impl,COMPARE_I420='/tmp/devbox-native-review/road800.i420',COMPARE_CASE='Road800',COMPARE_WIDTH='800',COMPARE_HEIGHT='600',COMPARE_FRAMES='240',COMPARE_QP='32',COMPARE_SLICES=str(slices),COMPARE_BOUNDARY=boundary,COMPARE_DURATIONS=str(results/(name+'.csv')))
   load_before=os.getloadavg()
   proc=subprocess.run(['taskset','-c','0-3',str(root/('native-asm.test' if impl=='native' else 'purego.test')),'-test.run','^TestNativeReviewEncode$','-test.v'],env=env,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT)
   (results/(name+'.log')).write_text(proc.stdout)
   if proc.returncode: raise SystemExit(proc.stdout)
   record=json.loads(next(line[7:] for line in proc.stdout.splitlines() if line.startswith('RESULT ')))
   record.update(repeat=repeat+1,label=label,load_before=load_before,load_after=os.getloadavg(),affinity='0-3')
   collected.write(json.dumps(record)+'\n'); collected.flush()
   print(f"{repeat+1}/6 {label}: mean {record['mean_ns']/1e6:.2f} ms, p95 {record['p95_ns']/1e6:.2f} ms, CPU {record['mean_cpu_ns']/1e6:.2f} ms",flush=True)
