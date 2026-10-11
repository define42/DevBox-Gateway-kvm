#!/usr/bin/env python3
"""Summarize repetitions without treating individual IOs as independent runs."""
import collections
import json
import statistics
import sys

groups = collections.defaultdict(list)
with open(sys.argv[1]) as stream:
    for line in stream:
        row = json.loads(line)
        groups[row["label"], row["workload"]].append(row)

print("| Target | Workload | Runs | Median IOPS | Mean latency, median [min, max] µs | Median p99 µs |")
print("|---|---|---:|---:|---:|---:|")
for (label, workload), rows in sorted(groups.items()):
    means = [row["mean_us"] for row in rows]
    print(f"| {label} | {workload} | {len(rows)} | "
          f"{statistics.median(row['iops'] for row in rows):.0f} | "
          f"{statistics.median(means):.2f} [{min(means):.2f}, {max(means):.2f}] | "
          f"{statistics.median(row['p99_us'] for row in rows):.2f} |")
