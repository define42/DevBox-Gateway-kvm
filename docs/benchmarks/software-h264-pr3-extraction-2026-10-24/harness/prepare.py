#!/usr/bin/env python3
"""Freeze baseline/current source, prepare local video fixtures, and build test binaries.

Run outside benchmark timing. Requires Go, Git, FFmpeg and local OpenH264 res files.
No native codec library or network access is required.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source", type=Path, required=True)
    parser.add_argument("--work", type=Path, required=True)
    parser.add_argument("--fixtures", type=Path, required=True, help="OpenH264 res directory")
    parser.add_argument("--baseline", default="cc750a847437e50d54d360dd4f2d63dd43df4791")
    args = parser.parse_args()
    source, root = args.source.resolve(), args.work.resolve()
    fixtures_source = args.fixtures.resolve()
    if source == root or source in root.parents:
        parser.error("--work must be outside --source to avoid copying its own snapshots")
    if not fixtures_source.is_dir():
        parser.error("--fixtures must name an existing OpenH264 res directory")
    root.mkdir(parents=True, exist_ok=False)
    here = Path(__file__).resolve().parent
    for name in ["baseline", "after", "harness", "fixtures", "bin", "results"]:
        (root / name).mkdir()
    archive = subprocess.Popen(["git", "archive", args.baseline], cwd=source, stdout=subprocess.PIPE)
    subprocess.run(["tar", "-x", "-C", str(root / "baseline")], stdin=archive.stdout, check=True)
    archive.stdout.close()
    if archive.wait():
        raise RuntimeError("git archive failed")
    paths = subprocess.check_output(["git", "ls-files", "-z", "--cached", "--others", "--exclude-standard"], cwd=source).split(b"\0")
    for path in paths:
        if not path:
            continue
        rel = Path(os.fsdecode(path))
        if (source / rel).is_file():
            (root / "after" / rel).parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(source / rel, root / "after" / rel)
    # The encoder conformance tests resolve ../../../../res from their
    # package directory. Link the supplied corpus so validation runs its cases
    # instead of silently skipping them in the frozen source snapshots.
    for variant in ["baseline", "after"]:
        res = root / variant / "res"
        if not res.exists() and not res.is_symlink():
            res.symlink_to(fixtures_source, target_is_directory=True)
    shutil.copy2(here / "compare_test.go.txt", root / "harness/compare_test.go")
    steps = [("road1080", "VID_1920x1080_cabac_temporal_direct.264", 1920, 1080),
             ("road720", "VID_1920x1080_cabac_temporal_direct.264", 1280, 720),
             ("pdf1080", "Adobe_PDF_sample_a_1024x768_50Frms.264", 1920, 1080),
             ("clip1080", "Cisco_Absolute_Power_1280x720_30fps.yuv", 1920, 1080)]
    fixtures = []
    for name, filename, width, height in steps:
        raw = ["-f", "rawvideo", "-pixel_format", "yuv420p", "-video_size", "1280x720"] if filename.endswith(".yuv") else []
        target = root / "fixtures" / f"{name}.i420"
        command = ["ffmpeg", "-v", "error", "-xerror", "-threads", "1", *raw,
                   "-i", str(fixtures_source / filename), "-vf", f"scale={width}:{height}:flags=bilinear",
                   "-threads", "1", "-pix_fmt", "yuv420p", "-fps_mode", "passthrough", "-f", "rawvideo", str(target)]
        subprocess.run(command, check=True)
        data = target.read_bytes()
        assert len(data) % (width * height * 3 // 2) == 0
        fixtures.append(dict(name=name, source=str(fixtures_source / filename), width=width, height=height,
                             frames=len(data) // (width * height * 3 // 2),
                             sha256=hashlib.sha256(data).hexdigest(), command=command))
    (root / "results/fixtures.json").write_text(json.dumps(fixtures, indent=2) + "\n")
    env = os.environ | {"CGO_ENABLED": "0", "GOCACHE": str(root / "gocache"), "GOWORK": "off"}
    for variant in ["baseline", "after"]:
        mapping = {str(root / variant / "internal/rdp/avc/z_pr3_comparison_test.go"): str(root / "harness/compare_test.go")}
        for filename in ["dct_mb_bench_test.go", "cavlc_mb_bench_test.go", "svc_motion_estimate_fast_bench_amd64_test.go", "md_refine_fast_bench_amd64_test.go"]:
            harness = root / "after/pkg/h264/internal/encoder" / filename
            if harness.exists():
                mapping[str(root / variant / "pkg/h264/internal/encoder" / filename)] = str(harness)
        overlay = root / f"{variant}-overlay.json"
        overlay.write_text(json.dumps({"Replace": mapping}))
        for package, suffix in [("internal/rdp/avc", "avc"), ("pkg/h264/internal/encoder", "encoder")]:
            subprocess.run(["go", "test", f"-overlay={overlay}", "-c", "-o", str(root / "bin" / f"{variant}-{suffix}.test"), f"./{package}"], cwd=root / variant, env=env, check=True)
    print(f"Prepared frozen comparisons in {root}")


if __name__ == "__main__":
    main()
