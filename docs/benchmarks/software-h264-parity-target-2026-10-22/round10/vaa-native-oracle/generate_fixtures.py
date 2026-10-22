#!/usr/bin/env python3
"""Write metadata only; C++ and Go independently reproduce every input byte."""
from pathlib import Path

cases = []
for width, height in [(16, 16), (32, 16), (48, 32), (64, 48),
                      (80, 32), (112, 80), (304, 64), (320, 64)]:
    layouts = [("tight", width, 0, 0),
               ("aligned", ((width + 31) // 32) * 32 + 32, 32, 64),
               ("unaligned", width + 13, 3, 11)]
    for layout, stride, cur_off, ref_off in layouts:
        for pattern in range(6):
            name = f"w{width}h{height}_{layout}_p{pattern}"
            seed = (0x26400000 + len(cases) * 0x9E3779B1) & 0xFFFFFFFF
            cases.append((name, width, height, stride, cur_off, ref_off, pattern, seed))

# Full-frame accumulator/order controls: even and odd macroblock columns.
for width in [1920, 1904]:
    cases.append((f"w{width}h1088_unaligned_p4", width, 1088, width + 13,
                  3, 11, 4, 0x26410800 + width))

target = Path(__file__).resolve().with_name("fixtures.txt")
with target.open("w", encoding="ascii") as output:
    output.write(f"vaa-fixtures-v1 {len(cases)}\n")
    for case in cases:
        output.write(" ".join(map(str, case)) + "\n")
print(f"{len(cases)} fixtures -> {target}")
