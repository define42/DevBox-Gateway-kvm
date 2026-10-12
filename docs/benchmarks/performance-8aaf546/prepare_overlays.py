"""Materialize the read-only review experiments as Go test overlays."""

import json
from pathlib import Path
import sys

if len(sys.argv) != 2:
    raise SystemExit("usage: python3 prepare_overlays.py OUTPUT_DIRECTORY")

evidence = Path(__file__).resolve().parent
repository = evidence.parents[2]
previous = evidence.parent / "performance-2026-10-11" / "experiments"
output = Path(sys.argv[1]).resolve()
output.mkdir(parents=True, exist_ok=True)
experiments = {
    "gateway-current": (
        "internal/gateway/performance_review_overlay_test.go",
        evidence / "experiments/gateway.go.txt",
    ),
    "runtime-current": (
        "internal/virtio/runtime_review_test.go",
        evidence / "experiments/runtime.go.txt",
    ),
    "encoders-cpu": (
        "internal/rdp/avc/perf_concurrency_bench_test.go",
        evidence / "experiments/encoders-cpu.go.txt",
    ),
    "encoders": (
        "internal/rdp/avc/perf_concurrency_bench_test.go",
        previous / "concurrency_bench_test.go.txt",
    ),
    "gpu": (
        "internal/virtio/review_gpu_bench_test.go",
        previous / "gpu_bench_test.go.txt",
    ),
    "idle": (
        "internal/virtio/perf_idle_test.go",
        previous / "idle_test.go.txt",
    ),
}
for name, (virtual, source) in experiments.items():
    virtual_path = repository / virtual
    if virtual_path.exists():
        raise SystemExit(f"Refusing to shadow an existing file: {virtual_path}")
    if not source.is_file():
        raise SystemExit(f"Missing experiment: {source}")
    manifest = {"Replace": {str(virtual_path): str(source)}}
    (output / f"{name}.json").write_text(json.dumps(manifest, indent=2) + "\n")
print(output)
