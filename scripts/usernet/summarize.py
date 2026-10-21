#!/usr/bin/env python3
"""Summarize repeated Go forwarding benchmarks without inferring significance."""
import json
import re
import statistics
import sys
from collections import defaultdict
from pathlib import Path

pattern = re.compile(
    r"^(BenchmarkNetworkTCP/\S+)-\d+\s+\d+\s+([\d.]+) ns/op\s+"
    r"([\d.]+) MB/s\s+(\d+) B/op\s+(\d+) allocs/op$"
)
results = {}
for filename in sys.argv[1:]:
    cases = defaultdict(list)
    for line in Path(filename).read_text().splitlines():
        match = pattern.match(line.strip())
        if match:
            cases[match[1]].append([float(value) for value in match.groups()[1:]])
    if not cases:
        raise SystemExit(f"no forwarding benchmark samples in {filename}")
    report = {}
    for case, samples in cases.items():
        metrics = {}
        for index, name in enumerate(("ns/op", "MB/s", "B/op", "allocs/op")):
            values = [sample[index] for sample in samples]
            metrics[name] = {"median": statistics.median(values), "min": min(values), "max": max(values)}
        report[case] = {"samples": len(samples), "metrics": metrics}
    results[filename] = report
print(json.dumps(results, indent=2, sort_keys=True))
