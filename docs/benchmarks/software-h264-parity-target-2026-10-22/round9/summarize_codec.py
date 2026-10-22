from pathlib import Path
import statistics,json
r=Path(__file__).resolve().parent
allrows={}
for kind in ['before','after','native']:
 rows={}
 for line in (r/'measure'/f'{kind}.txt').read_text().splitlines():
  fields=line.split();name=fields[0].removeprefix('BenchmarkOpenH264/').removesuffix('-4')
  values={fields[i+1]:float(fields[i]) for i in range(2,len(fields)-1,2)}
  rows.setdefault(name,[]).append(values)
 allrows[kind]=rows
out=[]
for name in allrows['before']:
 record={'name':name,'samples':{kind:len(allrows[kind][name]) for kind in allrows},'medians':{kind:{unit:statistics.median(v[unit] for v in allrows[kind][name]) for unit in allrows[kind][name][0]} for kind in allrows}}
 b,a,n=(record['medians'][kind]['ns/op'] for kind in ['before','after','native'])
 record.update(time_change_percent=(a/b-1)*100,native_ratio=a/n)
 out.append(record)
 print(f'{name}: {b/1e6:.3f} -> {a/1e6:.3f} ms ({(a/b-1)*100:+.2f}%); native {n/1e6:.3f} ms; Go/native {a/n:.3f}x')
(r/'codec-summary.json').write_text(json.dumps(out,indent=2)+'\n')
