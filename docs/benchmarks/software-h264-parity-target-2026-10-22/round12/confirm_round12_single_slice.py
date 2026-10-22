#!/usr/bin/env python3
"""One bounded confirmation for the mixed round12 one-slice result; no native reruns."""
from pathlib import Path
import hashlib,json,os,subprocess,statistics,re
r=Path(__file__).resolve().parent;out=r/'round12-single-confirmation';run=r/'round12-review'
assert json.loads((run/'complete.json').read_text())['complete']
record=json.loads((run/'binary-hashes.json').read_text())
binaries={k:Path(record[k]['path']) for k in ['round11','current']}
def sha(p):return hashlib.sha256(p.read_bytes()).hexdigest()
for k,p in binaries.items():assert sha(p)==record[k]['sha256'],k
cases=json.loads((r/'cases.json').read_text());assert len(cases)==5
out.mkdir()
prior={str(p):sha(p) for p in [run/'codec/benchstat.txt',run/'full/incremental-benchstat.txt',run/'codec/summary.json',run/'full/summary.json']}
meta={'reason':'Resolve original ShortClip/1 regression +1.44% p=.026 versus Road20/1 improvement -1.52% p=.041; all other codec/full observations remain archived. One confirmation only, all5 one-slice settings, not only the regression case.','binaries':{k:record[k] for k in binaries},'cases':cases,'measured_frames':240,'warmup_frames':16,'pairs':6,'affinity':'2','GOMAXPROCS':4,'native_reruns':0,'prior_artifact_sha256':prior}
(out/'provenance.json').write_text(json.dumps(meta,indent=2)+'\n')
env=os.environ.copy()
for key in list(env):
 if key.startswith(('COMPARE_','CAVLC_','H264_')) or key in ['GODEBUG','GOFLAGS']:env.pop(key)
env.update(GOMAXPROCS='4',CGO_ENABLED='0')
for k in binaries:(out/(k+'.txt')).write_text('')
for rep in range(6):
 for case in cases:
  key=f"{case['name']}-{case['width']}x{case['height']}-qp{case['qp']}"
  settings=env|{'COMPARE_RUN':'1','COMPARE_CASE':case['name'],'COMPARE_I420':case['file'],'COMPARE_WIDTH':str(case['width']),'COMPARE_HEIGHT':str(case['height']),'COMPARE_QP':str(case['qp']),'COMPARE_SLICES':'1','COMPARE_FRAMES':'240'}
  for k in (['round11','current'] if rep%2==0 else ['current','round11']):
   result=subprocess.run(['taskset','-c','2',str(binaries[k]),'-test.run=^TestOpenH264Comparison$','-test.count=1'],env=settings,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT)
   (out/f'{key}-{rep+1}-{k}.log').write_text(result.stdout)
   assert result.returncode==0,(key,k,result.returncode)
   rows=[line for line in result.stdout.splitlines() if line.startswith('Benchmark')]
   assert len(rows)==1 and int(rows[0].split()[1])==240,rows
   with (out/(k+'.txt')).open('a') as f:f.write(rows[0]+'\n')
  print(f'Confirmation pair {rep+1}/6: {key}',flush=True)
with (out/'benchstat.txt').open('w') as f:subprocess.run(['/home/define42/go/bin/benchstat',str(out/'round11.txt'),str(out/'current.txt')],stdout=f,check=True)
data={}
for k in binaries:
 rows={}
 for line in (out/(k+'.txt')).read_text().splitlines():
  fields=line.split();metrics={fields[i+1]:float(fields[i]) for i in range(2,len(fields)-1,2)}
  rows.setdefault(fields[0],[]).append(metrics)
 assert len(rows)==5 and all(len(v)==6 for v in rows.values())
 data[k]={name:{unit:statistics.median(x[unit] for x in values) for unit in values[0]} for name,values in rows.items()}
ps={}
for line in (out/'benchstat.txt').read_text().splitlines():
 if line.startswith('geomean'):break
 if line.startswith('OpenH264/'):
  ps['Benchmark'+line.split()[0]]=float(re.search(r'p=([0-9.]+)',line)[1])
summary=[]
for name,b in data['round11'].items():
 a=data['current'][name];row={'name':name,'round11':b,'current':a,'wall_percent':100*(a['ns/op']/b['ns/op']-1),'cpu_percent':100*(a['cpu_ns/frame']/b['cpu_ns/frame']-1),'p':ps[name]}
 summary.append(row);print(name,f"wall {row['wall_percent']:+.2f}% CPU {row['cpu_percent']:+.2f}% p={row['p']:.3f}",flush=True)
(out/'summary.json').write_text(json.dumps(summary,indent=2)+'\n')
for k,p in binaries.items():assert sha(p)==record[k]['sha256']
for p,h in prior.items():assert sha(Path(p))==h
(out/'complete.json').write_text(json.dumps({'complete':True,'cases':5,'pairs_per_case':6,'native_reruns':0,'prior_evidence_preserved':True},indent=2)+'\n')
