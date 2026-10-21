#!/usr/bin/env python3
import os
from pathlib import Path
import subprocess
import sys
run=Path(sys.argv[1]).resolve()
run.mkdir(parents=True,exist_ok=True)
binary="/tmp/h264-fullscreen-20261021/after.test"
env=dict(os.environ,GOMAXPROCS="4",H264_VIDEO_RGBA="/tmp/h264-fullscreen-20261021/clip1080p.rgba")
for name,pattern,frames in [
 ("clip-1","BenchmarkFullscreenVideo/Clip/1080p/1Slices/FullDamage$",300),
 ("clip-4","BenchmarkFullscreenVideo/Clip/1080p/4Slices/FullDamage$",300),
 ("dense-1","BenchmarkFullscreenVideo/Dense/1080p/1Slices/FullDamage$",120),
]:
 with (run/f"{name}-profile-run.txt").open("w") as output:
  subprocess.run([binary,"-test.run=^$",f"-test.bench={pattern}",f"-test.benchtime={frames}x","-test.cpu=4",f"-test.cpuprofile={run/name}.cpu.pprof","-test.benchmem"],env=env,stdout=output,stderr=subprocess.STDOUT,check=True)
