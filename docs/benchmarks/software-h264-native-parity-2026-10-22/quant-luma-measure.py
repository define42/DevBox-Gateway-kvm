#!/usr/bin/env python3
"""Measure validated complete-luma batching against four current group calls."""
from pathlib import Path
import os
import subprocess

root = Path(__file__).resolve().parent
env = dict(os.environ, GOMAXPROCS="4")
go = "/snap/go/current/bin/go"
command = [go, "test", "./pkg/h264/internal/encoder", "-run", "Test(QuantScan|CoefficientEncodingStateParity)", "-count=1"]
for feature in ("normal", "cpu.ssse3=off", "cpu.all=off"):
    runenv = dict(env)
    if feature != "normal":
        runenv["GODEBUG"] = feature
    run = subprocess.run(command, env=runenv, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, check=True)
    (root / ("quant-luma-tests-" + feature.replace("=", "-") + ".txt")).write_text(run.stdout)
    print(feature, run.stdout.strip(), flush=True)
binary = root / "quant-luma-encoder.test"
subprocess.run([go, "test", "-c", "-o", str(binary), "./pkg/h264/internal/encoder"], env=env, check=True)
with (root / "quant-luma-component-raw.txt").open("w") as log:
    for sample in range(6):
        paths = ["FourCalls", "Batched"]
        if sample % 2:
            paths.reverse()
        for density in ("Zero", "Sparse", "Dense"):
            for path in paths:
                cmd = [str(binary), "-test.run=^$", "-test.bench=^BenchmarkQuantScanLuma/" + density + "/" + path + "$",
                       "-test.benchtime=200ms", "-test.count=1"]
                run = subprocess.run(cmd, env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, check=True)
                log.write(f"# sample={sample+1} density={density} path={path}\n" + run.stdout)
                log.flush()
raw = (root / "quant-luma-component-raw.txt").read_text()
for path in ("FourCalls", "Batched"):
    rows = []
    for line in raw.splitlines():
        if line.startswith("BenchmarkQuantScanLuma/") and "/" + path in line:
            rows.append(line.replace("/" + path + "-4", "-4"))
    (root / ("quant-luma-" + path + ".txt")).write_text("\n".join(rows) + "\n")
run = subprocess.run(["/home/define42/go/bin/benchstat", str(root / "quant-luma-FourCalls.txt"), str(root / "quant-luma-Batched.txt")],
                     text=True, stdout=subprocess.PIPE, check=True)
(root / "quant-luma-benchstat.txt").write_text(run.stdout)
print(run.stdout)
