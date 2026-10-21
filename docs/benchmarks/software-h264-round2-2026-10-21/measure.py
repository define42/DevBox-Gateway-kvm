#!/usr/bin/env python3
"""Run precompiled before/after binaries serially with alternating order."""
import os
from pathlib import Path
import subprocess
import sys

before, after, output = map(Path, sys.argv[1:4])
output.mkdir(parents=True, exist_ok=True)
env = dict(os.environ, GOMAXPROCS="4")
workloads = [
    ("desktop", "BenchmarkSlicedDesktop/1080p/(Widget|Scroll)/(1|4)Slices$", "200x"),
    ("motion", "^BenchmarkEncode$", "100x"),
    ("sessions", "BenchmarkOfficeProfileBudget/profile=office30$", "1x"),
    ("construction", "^BenchmarkSoftwareEncoderConstruction$", "10x"),
    ("startup", "^BenchmarkSoftwareEncoderStartup$", "10x"),
]
selected = set(sys.argv[4:])
for name, pattern, iterations in workloads:
    if selected and name not in selected:
        continue
    for side in ("before", "after"):
        (output / f"{name}-{side}.txt").write_text("")
    for repetition in range(6):
        order = [("before", before), ("after", after)]
        if repetition % 2:
            order.reverse()
        for side, binary in order:
            command = [str(binary.resolve()), "-test.run=^$", "-test.cpu=4",
                       f"-test.bench={pattern}", f"-test.benchtime={iterations}",
                       "-test.count=1", "-test.benchmem"]
            print(f"{name} pair={repetition + 1}/6 {side}", flush=True)
            result = subprocess.run(command, env=env, text=True, stdout=subprocess.PIPE,
                                    stderr=subprocess.STDOUT, check=True)
            with (output / f"{name}-{side}.txt").open("a") as log:
                log.write(result.stdout)
