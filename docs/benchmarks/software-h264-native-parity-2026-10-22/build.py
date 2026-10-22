#!/usr/bin/env python3
import os
from pathlib import Path
import subprocess
ROOT=Path(__file__).resolve().parent
REPO=Path('/home/define42/git/DevBox-Gateway-kvm')
GO='/snap/go/current/bin/go'
NATIVE=Path('/tmp/openh264-comparison-20261021/native')
steps=[
 ([GO,'test','-overlay='+str(ROOT/'before-overlay.json'),'-c','-o',str(ROOT/'before-avc.test'),'./internal/rdp/avc'],ROOT/'before','build-before-avc'),
 ([GO,'test','-overlay='+str(ROOT/'after-overlay.json'),'-c','-o',str(ROOT/'after-avc.test'),'./internal/rdp/avc'],REPO,'build-after-avc'),
 ([GO,'test','-c','-o',str(ROOT/'after-common.test'),'./pkg/h264/internal/common'],REPO,'build-after-common'),
 ([GO,'test','-c','-o',str(ROOT/'after-encoder.test'),'./pkg/h264/internal/encoder'],REPO,'build-after-encoder'),
 (['g++','-O3','-std=c++17','-I',str(NATIVE/'codec/api/wels'),str(ROOT/'native_bench.cpp'),str(NATIVE/'libopenh264.a'),'-pthread','-o',str(ROOT/'native_bench')],ROOT,'build-native'),
]
for cmd,cwd,name in steps:
    print(name,flush=True)
    with (ROOT/f'{name}.txt').open('w') as f:
        f.write(' '.join(cmd)+'\n')
        f.flush()
        subprocess.run(cmd,cwd=cwd,env=os.environ|{'PATH':'/snap/go/current/bin:'+os.environ['PATH'],'CGO_ENABLED':'0'},stdout=f,stderr=subprocess.STDOUT,check=True)
