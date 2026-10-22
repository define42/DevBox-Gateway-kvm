#!/usr/bin/env python3
"""Generate one deterministic, native-valid full-CAVLC fixture manifest."""
from pathlib import Path

ROOT = Path(__file__).resolve().parent
DEST = ROOT / "go/pkg/h264/internal/encoder/testdata/cavlc_native_fixtures.txt"
KINDS = ("zero", "known-zero", "sparse", "dense", "mixed")
SMALL = (2, -2, 3, -3, 6, -6, 12, -12, 24, -24, 48, -48, 2, -4, 8, -16)
MIXED = (2, -3, 4, -7, 12, -13, 24, -25, 48, -49, 63, -64, 64, -65, 511, -511)


def block(end, kind, index):
    coeff = [0] * 16
    if kind == "sparse":
        coeff[index % (end + 1)] = SMALL[index % 16]
        coeff[(index * 5 + 2) % (end + 1)] = SMALL[(index + 3) % 16]
    elif kind in ("dense", "mixed"):
        values = SMALL if kind == "dense" else MIXED
        for i in range(end + 1):
            coeff[i] = values[(i + index) % 16]
    if kind not in ("zero", "known-zero"):
        for i in range(index % 4):
            coeff[end - i] = 1 if i % 2 == 0 else -1
    prop = 3 if end == 3 else (1 if end == 14 else 2)
    if end == 14 and index % 2:
        prop = 4  # Both luma and chroma AC use end 14.
    nc = 17 if end == 3 else (0, 1, 2, 3, 4, 7, 8, 16)[index % 8]
    return [end, prop, nc, int(kind != "known-zero"), 0, *coeff]


def suite(name, prefix, timed, blocks):
    value = 0x6B4E29D3 & ((1 << prefix) - 1)
    return (name, prefix, value, int(timed), blocks)


suites = []
for end in (3, 14, 15):
    for kind in KINDS:
        for prefix in (0, 17, 31):
            suites.append(suite(f"end{end}/{kind}/prefix{prefix}", prefix, True,
                                [block(end, kind, i) for i in range(32)]))
# Exercise every partial accumulator position with mixed, success-only blocks.
for end in (3, 14, 15):
    for prefix in range(32):
        suites.append(suite(f"parity/end{end}/prefix{prefix}", prefix, False,
                            [block(end, KINDS[i % len(KINDS)], i) for i in range(16)]))
# Overflow is defined for these legal coefficient arrays. Put it after a valid
# block and after small coefficients within its own block to preserve/check any
# output words written before the writer returns without committing local state.
for end in (3, 14, 15):
    for prefix in range(32):
        for value in (-32768, 32767):
            failure = block(end, "dense", 0)
            failure[4] = 64
            failure[5] = value
            suites.append(suite(f"overflow/end{end}/prefix{prefix}/value{value}",
                                prefix, False, [block(end, "dense", 2), failure]))

DEST.parent.mkdir(parents=True, exist_ok=True)
with DEST.open("w") as output:
    output.write(f"cavlc-native-v1 {len(suites)}\n")
    for name, prefix, value, timed, blocks in suites:
        output.write(f"{name} {prefix} {value} {timed} {len(blocks)}\n")
        for item in blocks:
            output.write(" ".join(map(str, item)) + "\n")
print(f"{DEST}: {len(suites)} suites, {sum(len(s[4]) for s in suites)} blocks, "
      f"{sum(s[3] for s in suites)} timed suites")
