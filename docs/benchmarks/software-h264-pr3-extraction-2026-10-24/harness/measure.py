#!/usr/bin/env python3
"""Serial, alternating comparisons of frozen test binaries; no builds during timing."""
import argparse
import hashlib
import itertools
import json
import os
from pathlib import Path
import subprocess
import time


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, required=True)
    parser.add_argument("--variants", nargs="+", required=True)
    parser.add_argument("--name", required=True)
    parser.add_argument("--mode", choices=["codec", "component", "desktop", "parity"], required=True)
    parser.add_argument("--regex", default="")
    parser.add_argument("--repetitions", type=int, default=6)
    parser.add_argument("--frames", type=int, default=64)
    parser.add_argument("--benchtime", default="200ms")
    parser.add_argument("--cpu", default="2")
    parser.add_argument("--godebug", default="")
    args = parser.parse_args()
    root = args.root.resolve()
    out = root / "results" / args.name
    out.mkdir(parents=True, exist_ok=False)
    env = os.environ.copy()
    for key in list(env):
        if key.startswith("COMPARE_") or key in {"GODEBUG", "GOFLAGS"}:
            env.pop(key)
    env.update(GOMAXPROCS="4", GODEBUG=args.godebug)
    suffix = "encoder" if args.mode == "component" else "avc"
    binaries = {v: root / "bin" / f"{v}-{suffix}.test" for v in args.variants}
    manifest = dict(arguments=vars(args) | {"root": str(root)},
                    started=time.strftime("%Y-%m-%dT%H:%M:%S%z"),
                    binaries={v: sha(p) for v, p in binaries.items()},
                    fixtures=json.loads((root / "results/fixtures.json").read_text()),
                    records=[])
    cases = [("ShortClip", "clip1080", 1920, 1080, 20),
             ("Road", "road1080", 1920, 1080, 20),
             ("Road", "road1080", 1920, 1080, 32),
             ("PDF", "pdf1080", 1920, 1080, 20),
             ("Road", "road720", 1280, 720, 20)]
    repetitions = 1 if args.mode == "parity" else args.repetitions
    for rep in range(repetitions):
        order = args.variants[rep % len(args.variants):] + args.variants[:rep % len(args.variants)]
        if len(order) > 2 and (rep // len(order)) % 2:
            order.reverse()
        jobs = list(itertools.product(cases, [1, 4])) if args.mode in {"codec", "parity"} else [(None, None)]
        for case, slices in jobs:
            for variant in order:
                runenv = env.copy()
                if case:
                    label, file, width, height, qp = case
                    key = f"{file}-qp{qp}-s{slices}"
                    runenv.update(COMPARE_RUN="1", COMPARE_CASE=label,
                                  COMPARE_I420=str(root / "fixtures" / f"{file}.i420"),
                                  COMPARE_WIDTH=str(width), COMPARE_HEIGHT=str(height),
                                  COMPARE_QP=str(qp), COMPARE_SLICES=str(slices),
                                  COMPARE_FRAMES=str(args.frames))
                    affinity = args.cpu if slices == 1 else "0-3"
                    command = ["taskset", "-c", affinity, str(binaries[variant]),
                               "-test.run=^TestOpenH264Comparison$", "-test.count=1"]
                    if args.mode == "parity":
                        stream = out / f"{variant}-{key}.264"
                        runenv["COMPARE_STREAM"] = str(stream)
                else:
                    key = args.mode
                    regex = args.regex or "^BenchmarkSlicedDesktop$/1080p/(Scroll|Widget)/(1|4)Slices$"
                    command = ["taskset", "-c", args.cpu, str(binaries[variant]),
                               "-test.run=^$", f"-test.bench={regex}",
                               f"-test.benchtime={args.benchtime}", "-test.count=1", "-test.cpu=4"]
                log = out / f"{rep:02}-{key}-{variant}.log"
                started = time.time()
                with log.open("w") as target:
                    subprocess.run(command, env=runenv, stdout=target, stderr=subprocess.STDOUT, check=True)
                record = dict(repetition=rep, case=key, variant=variant,
                              command=command, seconds=time.time() - started, log=log.name)
                if args.mode == "parity":
                    record["sha256"] = sha(stream)
                    record["bytes"] = stream.stat().st_size
                    decode = ["ffmpeg", "-v", "error", "-xerror", "-threads", "1", "-i", str(stream), "-f", "null", "-"]
                    decoded = subprocess.run(decode, capture_output=True, text=True)
                    record["decode_exit"] = decoded.returncode
                    record["decode_stderr"] = decoded.stderr
                    if decoded.returncode:
                        raise RuntimeError(decoded.stderr)
                    probe = subprocess.check_output(["ffprobe", "-v", "error", "-count_frames", "-select_streams", "v:0", "-show_entries", "stream=nb_read_frames", "-of", "json", str(stream)], text=True)
                    record["decoded_frames"] = int(json.loads(probe)["streams"][0]["nb_read_frames"])
                    assert record["decoded_frames"] == args.frames + 16
                else:
                    lines = [line for line in log.read_text().splitlines() if line.startswith("Benchmark")]
                    if not lines:
                        raise RuntimeError(f"No measurements in {log}")
                    with (out / f"{variant}.txt").open("a") as summary:
                        summary.write("\n".join(lines) + "\n")
                manifest["records"].append(record)
                (out / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
                print(f"{args.name}: {rep+1}/{repetitions} {key} {variant}", flush=True)
    assert manifest["binaries"] == {v: sha(p) for v, p in binaries.items()}, "binaries changed"
    if args.mode == "parity":
        for case in {r["case"] for r in manifest["records"]}:
            assert len({r["sha256"] for r in manifest["records"] if r["case"] == case}) == 1, case
    else:
        benchstat = Path.home() / "go/bin/benchstat"
        with (out / "benchstat.txt").open("w") as output:
            subprocess.run([str(benchstat), *[f"{v}.txt" for v in args.variants]], cwd=out, stdout=output, check=True)
    manifest["completed"] = time.strftime("%Y-%m-%dT%H:%M:%S%z")
    (out / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")


if __name__ == "__main__":
    main()
