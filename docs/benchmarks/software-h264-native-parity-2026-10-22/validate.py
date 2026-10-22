#!/usr/bin/env python3
import json
import os
from pathlib import Path
import subprocess
ROOT=Path(__file__).resolve().parent
REPO=Path('/home/define42/git/DevBox-Gateway-kvm')
GO='/snap/go/current/bin/go'
env=os.environ|{'PATH':'/snap/go/current/bin:'+os.environ['PATH'],'GOMAXPROCS':'4'}
steps=[
 ('tests',[GO,'test','./pkg/h264/...','./internal/rdp/...'],{}),
 ('race',[GO,'test','-race','./pkg/h264/internal/common','./pkg/h264/internal/encoder','./internal/rdp/avc'],{'CGO_ENABLED':'1'}),
]
for feature in ['avx2','ssse3','popcnt','all']:
    steps.append((f'fallback-{feature}',[GO,'test','./pkg/h264/internal/common','./pkg/h264/internal/encoder','./internal/rdp/avc'],{'GODEBUG':f'cpu.{feature}=off'}))
for package in ['common','encoder']:
    steps.append((f'arm64-{package}',[GO,'test','-c','-o',str(ROOT/f'arm64-{package}.test'),f'./pkg/h264/internal/{package}'],{'GOARCH':'arm64','CGO_ENABLED':'0'}))
steps.append(('arm64-avc',[GO,'test','-c','-o',str(ROOT/'arm64-avc.test'),'./internal/rdp/avc'],{'GOARCH':'arm64','CGO_ENABLED':'0'}))
checks=['pkg/h264/internal/common/sad_simd_amd64.go','pkg/h264/internal/common/mc_avx2_amd64.go']
checks += [str(f.relative_to(REPO)) for pattern in ['dct_avx2_*.go','cavlc_extract_*.go','quant_scan_luma*.go'] for f in (REPO/'pkg/h264/internal/encoder').glob(pattern)]
checks += ['pkg/h264/internal/encoder/'+f for f in ['dct_simd_amd64.go','set_mb_syn_cavlc.go','sample_simd_amd64.go','quant_scan_amd64.go','svc_encode_mb.go']]
steps.append(('gopls',['/home/define42/go/bin/gopls','check',*checks],{}))
steps.append(('lint',['./golangci-lint','run','./pkg/h264/...','./internal/rdp/avc/...'],{'GOTOOLCHAIN':'go1.26.9'}))
results=[]
for name,cmd,extra in steps:
    print('validating '+name,flush=True)
    with (ROOT/f'validation-{name}.txt').open('w') as f:
        f.write(' '.join(cmd)+'\n')
        f.write('environment overrides: '+repr(extra)+'\n')
        f.flush()
        result=subprocess.run(cmd,cwd=REPO,env=env|extra,stdout=f,stderr=subprocess.STDOUT)
    results.append(dict(name=name,command=cmd,environment=extra,exit_code=result.returncode))
    (ROOT/'validation.json').write_text(json.dumps(results,indent=2)+'\n')
    if result.returncode:
        print((ROOT/f'validation-{name}.txt').read_text(),flush=True)
        raise SystemExit(result.returncode)
