#!/usr/bin/env python3
import json
from pathlib import Path
import statistics
ROOT=Path(__file__).resolve().parent

def read(file):
    results={}
    for line in file.read_text().splitlines():
        if not line.startswith('Benchmark'):
            continue
        fields=line.split()
        metrics={fields[i+1]:float(fields[i]) for i in range(2,len(fields)-1,2)}
        results.setdefault(fields[0],[]).append(metrics)
    return {name:{metric:statistics.median(s[metric] for s in values) for metric in values[0]}|{'samples':len(values)} for name,values in results.items()}

codec={kind:read(ROOT/'measure'/f'{kind}.txt') for kind in ['before','after','native']}
rows=[]
for name in codec['before']:
    b,a,n=[codec[kind][name] for kind in ['before','after','native']]
    assert b['bytes/frame']==a['bytes/frame']==n['bytes/frame'],name
    rows.append(dict(name=name,before=b,after=a,native=n,wall_reduction=100*(1-a['ns/op']/b['ns/op']),native_ratio=a['ns/op']/n['ns/op']))
(ROOT/'codec-summary.json').write_text(json.dumps(rows,indent=2)+'\n')
print('| Case | Before ms | After ms | Change | Native ms | Go/native |')
print('| --- | ---: | ---: | ---: | ---: | ---: |')
for r in rows:
    name=r['name'].removeprefix('BenchmarkOpenH264/').removesuffix('-4')
    print(f"| {name} | {r['before']['ns/op']/1e6:.2f} | {r['after']['ns/op']/1e6:.2f} | {-r['wall_reduction']:+.1f}% | {r['native']['ns/op']/1e6:.2f} | {r['native_ratio']:.2f}× |")
if (ROOT/'full'/'after.txt').exists():
    full={kind:read(ROOT/'full'/f'{kind}.txt') for kind in ['before','after']}
    out=[]
    for name in full['before']:
        b,a=[full[kind][name] for kind in ['before','after']]
        assert b['bytes/frame']==a['bytes/frame'],name
        out.append(dict(name=name,before=b,after=a,wall_reduction=100*(1-a['ns/op']/b['ns/op'])))
    (ROOT/'full-summary.json').write_text(json.dumps(out,indent=2)+'\n')
    print('\n| Full EncodeDamage | Before ms | After ms | Change | Encoding before ms | Encoding after ms |')
    print('| --- | ---: | ---: | ---: | ---: | ---: |')
    for r in out:
        name=r['name'].removeprefix('BenchmarkFullscreenVideo/').removesuffix('-4')
        print(f"| {name} | {r['before']['ns/op']/1e6:.2f} | {r['after']['ns/op']/1e6:.2f} | {-r['wall_reduction']:+.1f}% | {r['before']['encode_ns/frame']/1e6:.2f} | {r['after']['encode_ns/frame']/1e6:.2f} |")
