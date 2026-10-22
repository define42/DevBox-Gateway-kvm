#!/usr/bin/env python3
import json,re,statistics
from pathlib import Path
r=Path(__file__).resolve().parent
values={}
for kind in ['before','after','native']:
    rows={}
    for line in (r/f'component-{kind}.txt').read_text().splitlines():
        if not line.startswith('Benchmark'):continue
        parts=line.split()
        name=re.sub(r'-\d+$','',parts[0].removeprefix('Benchmark'))
        rows.setdefault(name,[]).append(float(parts[2]))
    assert all(len(data)==6 for data in rows.values())
    values[kind]={name:statistics.median(data) for name,data in rows.items()}
pvalues={}
for line in (r/'component-benchstat.txt').read_text().splitlines():
    if not line.startswith('SampleSatd8Rows/'):continue
    name=re.sub(r'-\d+$','',line.split()[0])
    m=re.search(r'p=([0-9.]+)',line)
    if m:pvalues.setdefault(name,float(m.group(1)))
summary=[]
for name,before in values['before'].items():
    after=values['after'][name]
    summary.append(dict(case=name,before_ns=before,after_ns=after,change_percent=100*(after/before-1),p=pvalues[name],native_ns=values['native'].get(name)))
(r/'component-summary.json').write_text(json.dumps(summary,indent=2)+'\n')
for x in summary:
    print(f"{x['case']}: {x['before_ns']:.3f} -> {x['after_ns']:.3f} ns ({x['change_percent']:+.2f}%, p={x['p']:.3f}), native={x['native_ns']}")
