"""Summarize each benchmark series independently; never pool separate runs."""

import json
from pathlib import Path
import statistics

evidence = Path(__file__).resolve().parent
result = {}
for path in sorted(evidence.glob("*.txt")):
    samples = {}
    for line in path.read_text().splitlines():
        fields = line.split()
        if len(fields) < 4 or not fields[0].startswith("Benchmark"):
            continue
        for index in range(2, len(fields) - 1, 2):
            try:
                value = float(fields[index])
            except ValueError:
                continue
            samples.setdefault(fields[0], {}).setdefault(fields[index + 1], []).append(value)
    if samples:
        result[path.name] = {
            name: {
                unit: {
                    "median": statistics.median(values),
                    "min": min(values),
                    "max": max(values),
                    "samples": len(values),
                }
                for unit, values in units.items()
            }
            for name, units in samples.items()
        }
(evidence / "summary.json").write_text(json.dumps(result, indent=2) + "\n")
print(f"Summarized {sum(map(len, result.values()))} benchmark cases in {len(result)} series")
