from pathlib import Path
import os
import subprocess

base = Path(__file__).resolve().parent
binaries = {"before": Path("/tmp/h264-rgb-parallel-20261021/after.test"), "after": base / "after.test"}
env = dict(os.environ, GOMAXPROCS="4", H264_VIDEO_RGBA="/tmp/h264-fullscreen-20261021/clip1080p.rgba")
cases = [
    ("control", "BenchmarkFullscreenVideo/Clip/1080p/4Slices/FullDamage$", "360x"),
    ("control", "BenchmarkFullscreenVideo/Clip/1080p/1Slices/FullDamage$", "120x"),
    ("control", "BenchmarkSlicedDesktop/1080p/Scroll/4Slices$", "240x"),
    ("control", "BenchmarkSlicedDesktop/1080p/Widget/1Slices$", "960x"),
]
for group in ("control",):
    for version in binaries:
        (base / (group + "-" + version + ".txt")).write_text("")
for pair in range(6):
    order = ("before", "after") if pair % 2 == 0 else ("after", "before")
    for group, pattern, count in cases:
        for version in order:
            cmd = [str(binaries[version]), "-test.run=^$", "-test.cpu=4", "-test.bench=" + pattern,
                   "-test.benchtime=" + count, "-test.benchmem"]
            result = subprocess.run(cmd, env=env, text=True, capture_output=True, check=True)
            with (base / (group + "-" + version + ".txt")).open("a") as out:
                out.write(result.stdout)
        print("pair", pair + 1, pattern, "complete", flush=True)
for group in ("control",):
    result = subprocess.run(["/home/define42/go/bin/benchstat", str(base / (group + "-before.txt")),
                             str(base / (group + "-after.txt"))], text=True, capture_output=True, check=True)
    (base / (group + "-benchstat.txt")).write_text(result.stdout)
print("all comparisons complete", flush=True)
