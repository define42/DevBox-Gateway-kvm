#!/usr/bin/env python3
"""Sum cachegrind Ir per function. usage: cgfn.py FILE [N] [FILE2 to diff]"""
import collections,re,sys
def load(p):
    d=collections.Counter(); fn=None
    for l in open(p):
        if l.startswith('fn='): fn=l[3:].strip(); continue
        if l[:1].isdigit():
            parts=l.split()
            if len(parts)>=2: d[fn]+=int(parts[1])
    return d
def short(f): return re.sub(r'github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/','',f)
a=load(sys.argv[1]); n=int(sys.argv[2]) if len(sys.argv)>2 else 40
if len(sys.argv)>3:
    b=load(sys.argv[3]); diff={k:b[k]-a[k] for k in set(a)|set(b)}
    print(f'total {sum(a.values()):,} -> {sum(b.values()):,}')
    for k,v in sorted(diff.items(),key=lambda x:x[1])[:n//2]+sorted(diff.items(),key=lambda x:-x[1])[:n//2]:
        print(f'{v:+15,} {a[k]:15,} {short(k)[:100]}')
else:
    t=sum(a.values())
    for k,v in a.most_common(n): print(f'{v:15,} {100*v/t:5.1f}% {short(k)[:110]}')
