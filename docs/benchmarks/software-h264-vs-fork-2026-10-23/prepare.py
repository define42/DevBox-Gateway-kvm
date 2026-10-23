#!/usr/bin/env python3
"""Prepare shared, hashed I420 fixtures outside measurement."""
import argparse
import hashlib
import json
from pathlib import Path
import shutil
import subprocess


def sha(path):
    return hashlib.file_digest(path.open('rb'), 'sha256').hexdigest()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--work', type=Path, required=True)
    parser.add_argument('--res', type=Path, required=True)
    parser.add_argument('--cache', type=Path)
    args = parser.parse_args()
    target = args.work / 'fixtures'
    target.mkdir(parents=True, exist_ok=True)
    specs = [
        ('VID_1920x1080_cabac_temporal_direct.264', 'road1080.i420', 1920, 1080,
         'eca34e7c10664b10662bdbd3a89def3ee8093b02463cc2e99499975f46b2a7e6'),
        ('VID_1920x1080_cabac_temporal_direct.264', 'road720.i420', 1280, 720,
         '1c65ba12ef3b2b21632f4a4d3a7c6d089ae7643ce5ada7a24aada4c64614eef5'),
        ('Adobe_PDF_sample_a_1024x768_50Frms.264', 'pdf1080.i420', 1920, 1080,
         '8a938d0eb90f03c63a7ec8b35247e53033bc6acc683146b74a299767a083ad6b'),
    ]
    manifest = []
    for source, name, width, height, expected in specs:
        dest = target / name
        cmd = ['ffmpeg', '-v', 'error', '-xerror', '-threads', '1', '-i', str(args.res / source),
               '-vf', f'scale={width}:{height}:flags=bilinear', '-threads', '1', '-pix_fmt', 'yuv420p',
               '-fps_mode', 'passthrough', '-f', 'rawvideo', '-y', str(dest)]
        cached = args.cache / name if args.cache else None
        if cached and cached.exists() and sha(cached) == expected:
            shutil.copyfile(cached, dest)
            method = 'hash-verified copy of previous decoded fixture'
        else:
            subprocess.run(cmd, check=True)
            method = 'ffmpeg decode'
        actual = sha(dest)
        if actual != expected:
            raise RuntimeError(f'{name}: fixture differs from recorded reference: {actual}')
        size = dest.stat().st_size
        frame_bytes = width * height * 3 // 2
        assert size and size % frame_bytes == 0
        manifest.append(dict(file=str(dest), sha256=actual, bytes=size, frames=size // frame_bytes,
                             width=width, height=height, method=method, reproduce=cmd,
                             source=str(args.res / source), source_sha256=sha(args.res / source)))

    # Eight deterministic frames: a 128x64 changing widget on a real, static
    # PDF background. Every frame differs from its cyclic predecessor.
    width, height, count = 1920, 1080, 8
    luma = width * height
    frame_bytes = luma * 3 // 2
    with (target / 'pdf1080.i420').open('rb') as source:
        base = source.read(frame_bytes)
    frames = []
    for phase in range(count):
        frame = bytearray(base)
        for y in range(128, 192):
            frame[y * width + 160:y * width + 288] = bytes([40 + 22 * phase]) * 128
        for offset, value in [(luma, 80 + 10 * phase), (luma + luma // 4, 180 - 10 * phase)]:
            for y in range(64, 96):
                frame[offset + y * (width // 2) + 80:offset + y * (width // 2) + 144] = bytes([value]) * 64
        frames.append(bytes(frame))
    masks = bytearray()
    cols, rows = (width + 15) // 16, (height + 15) // 16
    for i, current in enumerate(frames):
        previous = frames[(i - 1) % count]
        for my in range(rows):
            for mx in range(cols):
                unchanged = True
                for offset, stride, block, plane_height in [
                    (0, width, 16, height), (luma, width // 2, 8, height // 2),
                    (luma + luma // 4, width // 2, 8, height // 2),
                ]:
                    for y in range(my * block, min((my + 1) * block, plane_height)):
                        start = offset + y * stride + mx * block
                        end = start + min(block, stride - mx * block)
                        if current[start:end] != previous[start:end]:
                            unchanged = False
                masks.append(int(unchanged))
    assert len(masks) == count * cols * rows
    assert masks.count(0) == count * 32
    widget = target / 'widget1080.i420'
    hints = target / 'widget1080.hints'
    widget.write_bytes(b''.join(frames))
    hints.write_bytes(masks)
    manifest.extend([
        dict(file=str(widget), sha256=sha(widget), bytes=widget.stat().st_size, frames=count,
             width=width, height=height, description='128x64 changing widget over first PDF frame'),
        dict(file=str(hints), sha256=sha(hints), bytes=len(masks), frames=count,
             macroblocks_per_frame=cols * rows, changed_macroblocks_per_frame=32,
             description='Exact per-frame I420 comparisons against cyclic predecessor; frame zero hints disabled by harness'),
    ])
    cases = [
        dict(name='Road', file='road1080.i420', width=1920, height=1080, qp=20, frames=120),
        dict(name='Road', file='road1080.i420', width=1920, height=1080, qp=32, frames=120),
        dict(name='PDF', file='pdf1080.i420', width=1920, height=1080, qp=20, frames=120),
        dict(name='Road', file='road720.i420', width=1280, height=720, qp=20, frames=120),
        dict(name='WidgetNoHints', file='widget1080.i420', width=1920, height=1080, qp=20, frames=480),
        dict(name='WidgetHints', file='widget1080.i420', hints='widget1080.hints', width=1920, height=1080, qp=20, frames=480),
    ]
    for case in cases:
        case['file'] = str(target / case['file'])
        if 'hints' in case:
            case['hints'] = str(target / case['hints'])
    (args.work / 'fixtures.json').write_text(json.dumps(manifest, indent=2) + '\n')
    (args.work / 'cases.json').write_text(json.dumps(cases, indent=2) + '\n')
    print(json.dumps(dict(fixtures=len(manifest), cases=len(cases), changed_widget_macroblocks=32)))


if __name__ == '__main__':
    main()
