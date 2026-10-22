#!/usr/bin/env python3
import os
from pathlib import Path
import subprocess
ROOT=Path(__file__).resolve().parent
GO='/snap/go/current/bin/go'
env=os.environ|{'GOMAXPROCS':'4','COMPARE_RUN':'1','COMPARE_CASE':'Road','COMPARE_I420':str(ROOT/'road1080.i420'),'COMPARE_SLICES':'1','COMPARE_FRAMES':'300','COMPARE_WIDTH':'1920','COMPARE_HEIGHT':'1080','COMPARE_QP':'20'}
with (ROOT/'after-profile-run.txt').open('w') as f:
    subprocess.run([str(ROOT/'after-avc.test'),'-test.run=^TestOpenH264Comparison$','-test.cpuprofile='+str(ROOT/'after.pprof')],env=env,stdout=f,stderr=subprocess.STDOUT,check=True)
for flag,name in [('-flat','after-profile-top.txt'),('-cum','after-profile-cum.txt')]:
    with (ROOT/name).open('w') as f:
        subprocess.run([GO,'tool','pprof','-top',flag,'-nodecount=40',str(ROOT/'after-avc.test'),str(ROOT/'after.pprof')],stdout=f,stderr=subprocess.STDOUT,check=True)
print((ROOT/'after-profile-top.txt').read_text())
