from pathlib import Path
import subprocess
import sys

root=Path(sys.argv[1]).resolve() if len(sys.argv)>1 else Path(__file__).resolve().parent
suites=[
    ("desktop", "before.test", "after.test", "^BenchmarkSlicedDesktop/1080p/(Widget|Scroll)/(1|4)Slices$", "200x"),
    ("conversion", "before.test", "after.test", "^BenchmarkI420$", "500ms"),
    ("vaa", "vaa-before.test", "vaa-after.test", "^BenchmarkVAACalcSad/(320x192|1920x1088)/SSE2$", "500ms"),
]
for name, before, after, pattern, duration in suites:
    paths=[root/(name+"-"+variant+".txt") for variant in ("before","after")]
    for path in paths:
        path.write_text("")
    for sample in range(6):
        order=[("before",before),("after",after)]
        if sample%2:
            order.reverse()
        for variant,binary in order:
            command=[str(root/binary),"-test.run=^$","-test.bench="+pattern,
                     "-test.cpu=4","-test.benchmem","-test.benchtime="+duration,"-test.count=1"]
            result=subprocess.run(command,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT)
            with (root/(name+"-"+variant+".txt")).open("a") as stream:
                stream.write(result.stdout)
            if result.returncode:
                print(result.stdout,flush=True)
                raise SystemExit(result.returncode)
        print(f"{name}: completed interleaved pair {sample+1}/6",flush=True)
