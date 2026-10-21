#!/usr/bin/env python3
import os
from pathlib import Path
import subprocess
import sys
root=Path(sys.argv[1]).resolve()
env=dict(os.environ,GOMAXPROCS="4",H264_VIDEO_RGBA=str(root/"clip1080p.rgba"))
for label,pattern,iterations in [
 ("clip","BenchmarkFullscreenVideo/Clip/1080p/(1|4)Slices/(FullDamage|NilDamage)$","60x"),
 ("dense","BenchmarkFullscreenVideo/Dense/1080p/(1|4)Slices/FullDamage$","16x"),
]:
 for side in ["before","after"]: (root/f"{label}-{side}.txt").write_text("")
 for repetition in range(6):
  sides=["before","after"] if repetition%2==0 else ["after","before"]
  for side in sides:
   print(f"{label} pair={repetition+1}/6 {side}",flush=True)
   command=[str(root/f"{side}.test"),"-test.run=^$","-test.cpu=4",f"-test.bench={pattern}",f"-test.benchtime={iterations}","-test.benchmem","-test.count=1"]
   result=subprocess.run(command,env=env,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,check=True)
   with (root/f"{label}-{side}.txt").open("a") as output: output.write(result.stdout)
