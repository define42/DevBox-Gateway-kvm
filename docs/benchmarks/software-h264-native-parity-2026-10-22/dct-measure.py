#!/usr/bin/env python3
"""Reproduce interleaved DCT samples with the already built Go/native binaries.

Build from the repository root:
  GOMAXPROCS=4 go test -c -o /tmp/h264-native-parity-20261021/dct-encoder.test ./pkg/h264/internal/encoder
  g++ -O3 -std=c++17 /tmp/h264-native-parity-20261021/dct_component.cpp /tmp/openh264-comparison-20261021/native/codec/common/x86/dct.o -o /tmp/h264-native-parity-20261021/dct-native

The native harness links OpenH264 2.6.0's existing assembly object directly.
"""
from pathlib import Path
import os
import subprocess

root = Path(__file__).resolve().parent
env = dict(os.environ, GOMAXPROCS="4")
with (root / "dct-component-raw.txt").open("w") as log:
    for sample in range(6):
        paths = ["old-SSE2", "dispatch", "raw-AVX2", "native-AVX2"]
        if sample % 2:
            paths.reverse()
        for stride in (8, 64, 2048):
            for path in paths:
                if path == "native-AVX2":
                    command = [str(root / "dct-native"), str(stride), "14000000"]
                else:
                    command = [str(root / "dct-encoder.test"), "-test.run=^$",
                               "-test.bench=^BenchmarkDCTFourAVX2/stride" + str(stride) + "/" + path + "$",
                               "-test.benchtime=200ms", "-test.count=1"]
                run = subprocess.run(command, env=env, text=True, stdout=subprocess.PIPE,
                                     stderr=subprocess.STDOUT, check=True)
                log.write(f"# sample={sample+1} stride={stride} path={path}\n" + run.stdout)
                log.flush()
raw = (root / "dct-component-raw.txt").read_text()
for path in ("old-SSE2", "dispatch", "raw-AVX2", "native-AVX2"):
    rows = []
    for line in raw.splitlines():
        if line.startswith("BenchmarkDCTFourAVX2/") and "/" + path in line:
            rows.append(line.replace("/" + path + "-4", "-4").replace("/" + path + " ", "-4 "))
    (root / ("dct-" + path + ".txt")).write_text("\n".join(rows) + "\n")
for old, new, name in (("old-SSE2", "dispatch", "validated"), ("native-AVX2", "raw-AVX2", "native")):
    run = subprocess.run(["/home/define42/go/bin/benchstat", str(root / ("dct-" + old + ".txt")),
                          str(root / ("dct-" + new + ".txt"))], text=True, stdout=subprocess.PIPE, check=True)
    (root / ("dct-" + name + "-benchstat.txt")).write_text(run.stdout)
