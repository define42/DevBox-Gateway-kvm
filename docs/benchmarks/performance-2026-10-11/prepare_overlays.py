"""Materialize Go test overlays for the preserved review experiments."""
import json
from pathlib import Path
import sys

if len(sys.argv) != 2:
    raise SystemExit("usage: python3 prepare_overlays.py OUTPUT_DIRECTORY")

evidence = Path(__file__).resolve().parent
repository = evidence.parents[2]
output = Path(sys.argv[1]).resolve()
output.mkdir(parents=True, exist_ok=True)
experiments = {
    "gateway": ("internal/gateway/performance_review_test.go", "gateway_review_test.go.txt"),
    "idle": ("internal/virtio/perf_idle_test.go", "idle_test.go.txt"),
    "encoders": ("internal/rdp/avc/perf_concurrency_bench_test.go", "concurrency_bench_test.go.txt"),
    "gpu": ("internal/virtio/review_gpu_bench_test.go", "gpu_bench_test.go.txt"),
    "backpressure": ("internal/rdp/perf_backpressure_test.go", "backpressure_test.go.txt"),
}
for name, (virtual, source) in experiments.items():
    virtual_path = repository / virtual
    source_path = evidence / "experiments" / source
    if virtual_path.exists():
        raise SystemExit(f"Refusing to shadow an existing file: {virtual_path}")
    if not source_path.is_file():
        raise SystemExit(f"Missing experiment: {source_path}")
    manifest = {"Replace": {str(virtual_path): str(source_path)}}
    (output / f"{name}.json").write_text(json.dumps(manifest, indent=2) + "\n")
print(output)
