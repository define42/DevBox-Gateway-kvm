#!/usr/bin/env python3
"""Create a Go overlay without modifying the source tree being tested."""
import argparse
import json
from pathlib import Path

parser = argparse.ArgumentParser()
parser.add_argument("tree", type=Path)
parser.add_argument("output", type=Path)
parser.add_argument("--avx2", action="store_true")
args = parser.parse_args()
tree = args.tree.resolve()
if not (tree / "go.mod").is_file():
    parser.error("tree must contain go.mod")
source = Path(__file__).resolve().parent
files = ["vaacalcfuncs_native_oracle_test.go"]
if args.avx2:
    files.append("vaa_avx2_native_oracle_test.go")
replacements = {str(tree / "pkg/h264/internal/processing" / name): str(source / name)
                for name in files}
args.output.write_text(json.dumps({"Replace": replacements}, indent=2) + "\n")
print(args.output.resolve())
