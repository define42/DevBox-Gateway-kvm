#!/usr/bin/env python3
"""Summarize every completed run; benchstat remains the statistical comparison."""
import argparse
from collections import defaultdict
import json
from pathlib import Path
import statistics
import subprocess


def read(path):
    result = defaultdict(list)
    for line in path.read_text().splitlines():
        fields = line.split()
        if not fields or not fields[0].startswith('Benchmark'):
            continue
        result[fields[0]].append({fields[i + 1]: float(fields[i]) for i in range(2, len(fields), 2)})
    return result


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--work', type=Path, required=True)
    parser.add_argument('--benchstat', default='benchstat')
    args = parser.parse_args()
    directory = args.work / 'measure'
    if not (directory / 'complete.json').exists():
        raise RuntimeError('measurement is incomplete')
    go, native = read(directory / 'go.txt'), read(directory / 'native.txt')
    assert go.keys() == native.keys()
    summary = []
    for name in go:
        assert len(go[name]) == len(native[name])
        gm = {metric: statistics.median(row[metric] for row in go[name]) for metric in go[name][0]}
        nm = {metric: statistics.median(row[metric] for row in native[name]) for metric in native[name][0]}
        summary.append(dict(name=name, repetitions=len(go[name]), go=gm, native=nm,
             go_over_native=gm['ns/op'] / nm['ns/op'],
             native_time_reduction_percent=100 * (1 - nm['ns/op'] / gm['ns/op']),
             go_cpu_over_native=gm['cpu_ns/frame'] / nm['cpu_ns/frame'],
             paired_wall_ratios=[g['ns/op'] / n['ns/op'] for g, n in zip(go[name], native[name])]))
    (args.work / 'summary.json').write_text(json.dumps(summary, indent=2) + '\n')
    command = [args.benchstat, str(directory / 'go.txt'), str(directory / 'native.txt')]
    result = subprocess.run(command, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, check=True)
    (args.work / 'benchstat.txt').write_text(result.stdout)
    lines = ['| Case | Ours ms/frame | Fork ms/frame | Ours/fork | Ours CPU ms/frame | Fork CPU ms/frame |',
             '|---|---:|---:|---:|---:|---:|']
    for row in summary:
        g, n = row['go'], row['native']
        lines.append(f"| {row['name'].removeprefix('BenchmarkOpenH264/').removesuffix('-4')} | "
                     f"{g['ns/op']/1e6:.3f} | {n['ns/op']/1e6:.3f} | {row['go_over_native']:.2f}× | "
                     f"{g['cpu_ns/frame']/1e6:.3f} | {n['cpu_ns/frame']/1e6:.3f} |")
    table = '\n'.join(lines) + '\n'
    (args.work / 'table.md').write_text(table)
    print(table)


if __name__ == '__main__':
    main()
