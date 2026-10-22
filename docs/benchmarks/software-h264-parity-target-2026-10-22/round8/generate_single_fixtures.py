#!/usr/bin/env python3
"""Append matched single-coefficient cases to the unchanged native fixtures."""
from pathlib import Path

root = Path(__file__).resolve().parent
original = root / 'pkg/h264/internal/encoder/testdata/cavlc_native_fixtures.txt'
output = root / 'pkg/h264/internal/encoder/testdata/cavlc_native_single_fixtures.txt'
text = original.read_text()
header, body = text.split('\n', 1)
magic, count = header.split()
assert magic == 'cavlc-native-v1'
suites = []


def single_blocks(end, magnitude):
    blocks = []
    for i in range(32):
        coeff = [0] * 16
        coeff[(i // 2) % (end + 1)] = magnitude if i % 2 == 0 else -magnitude
        prop = 3 if end == 3 else (1 if end == 14 else 2)
        if end == 14 and (i // 2) % 2:
            prop = 4
        nc = 17 if end == 3 else (0, 1, 2, 3, 4, 7, 8, 16)[(i // 2) % 8]
        blocks.append([end, prop, nc, 1, 0, *coeff])
    return blocks


def add(name, prefix, timed, blocks):
    value = 0x6B4E29D3 & ((1 << prefix) - 1)
    suites.append((name, prefix, value, int(timed), blocks))


for end in (3, 14, 15):
    for name, magnitude in (('single-one', 1), ('single-level', 2)):
        for prefix in (0, 17, 31):
            add(f'end{end}/{name}/prefix{prefix}', prefix, True, single_blocks(end, magnitude))
    for prefix in range(32):
        add(f'parity-single/end{end}/prefix{prefix}', prefix, False, single_blocks(end, 1))

with output.open('w') as target:
    target.write(f'{magic} {int(count) + len(suites)}\n')
    target.write(body)
    for name, prefix, value, timed, blocks in suites:
        target.write(f'{name} {prefix} {value} {timed} {len(blocks)}\n')
        for block in blocks:
            target.write(' '.join(map(str, block)) + '\n')
