#!/usr/bin/env python3
import os
from pathlib import Path
import subprocess
import sys
run=Path(sys.argv[1]).resolve()
env=dict(os.environ,GOMAXPROCS="4",H264_VIDEO_RGBA="/tmp/h264-fullscreen-20261021/clip1080p.rgba")
binaries={"standard":Path("/tmp/h264-fullscreen-20261021/after.test"),"pgo":run/"pgo.test"}
for label,pattern,frames in [
 ("clip","BenchmarkFullscreenVideo/Clip/1080p/1Slices/FullDamage$",60),
 ("dense","BenchmarkFullscreenVideo/Dense/1080p/1Slices/FullDamage$",16),
]:
 for side in binaries:(run/f"{label}-{side}.txt").write_text("")
 for repetition in range(6):
  order=["standard","pgo"] if repetition%2==0 else ["pgo","standard"]
  for side in order:
   print(label,repetition+1,side,flush=True)
   result=subprocess.run([str(binaries[side]),"-test.run=^$",f"-test.bench={pattern}",f"-test.benchtime={frames}x","-test.cpu=4","-test.benchmem"],env=env,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,check=True)
   with (run/f"{label}-{side}.txt").open("a") as output:output.write(result.stdout)
