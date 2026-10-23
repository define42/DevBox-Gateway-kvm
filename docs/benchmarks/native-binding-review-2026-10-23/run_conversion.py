import os, subprocess, json
from pathlib import Path
root=Path('/tmp/devbox-native-review')
results=root/'results-asm-800'
with (results/'conversion.jsonl').open('w') as collected:
 for repeat in range(6):
  for width,height in [(800,600),(1920,1080)]:
   name=f'conversion_{width}_{repeat+1}'
   env=os.environ.copy();env.update(GOMAXPROCS='4',COMPARE_RUN='1',COMPARE_WIDTH=str(width),COMPARE_HEIGHT=str(height),COMPARE_FRAMES='200')
   proc=subprocess.run(['taskset','-c','0-3',str(root/'native-asm.test'),'-test.run','^TestNativeReviewConversion$','-test.v'],env=env,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT)
   (results/(name+'.log')).write_text(proc.stdout)
   if proc.returncode:raise SystemExit(proc.stdout)
   for line in proc.stdout.splitlines():
    if not line.startswith('RESULT '):continue
    record=json.loads(line[7:]);record.update(repeat=repeat+1,load_after=os.getloadavg(),affinity='0-3')
    collected.write(json.dumps(record)+'\n');collected.flush()
    print(f"{repeat+1}/6 {width}x{height} {record['format']} workers={record['threads']}: mean {record['mean_ns']/1e6:.3f} ms, p95 {record['p95_ns']/1e6:.3f} ms",flush=True)
