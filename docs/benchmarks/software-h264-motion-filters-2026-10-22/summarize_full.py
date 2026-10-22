#!/usr/bin/env python3
import json,re,statistics
from pathlib import Path
r=Path(__file__).resolve().parent
values={}
for kind in ['before','after']:
    rows={}
    for line in (r/'full'/f'{kind}.txt').read_text().splitlines():
        if not line.startswith('Benchmark'):continue
        parts=line.split();name=parts[0].removeprefix('Benchmark')
        bucket=rows.setdefault(name,{})
        for i in range(2,len(parts)-1,2):bucket.setdefault(parts[i+1],[]).append(float(parts[i]))
    values[kind]={name:{unit:statistics.median(data) for unit,data in metrics.items()} for name,metrics in rows.items()}
pvalues={}
for line in (r/'full-benchstat.txt').read_text().splitlines():
    if not line.startswith('FullscreenVideo/'):continue
    name=line.split()[0]
    match=re.search(r'p=([0-9.]+)',line)
    if match:pvalues.setdefault(name,float(match.group(1)))
summary=[]
for name,before in values['before'].items():
    after=values['after'][name]
    summary.append({'case':name,'before':before,'after':after,'wall_change_percent':100*(after['ns/op']/before['ns/op']-1),'p':pvalues[name]})
(r/'full-summary.json').write_text(json.dumps(summary,indent=2)+'\n')
for row in summary:
    print(f"{row['case']}: {row['before']['ns/op']/1e6:.3f} -> {row['after']['ns/op']/1e6:.3f} ms ({row['wall_change_percent']:+.2f}%, p={row['p']:.3f})")
