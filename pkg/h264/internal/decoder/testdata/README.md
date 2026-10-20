# Synthetic decoder conformance clips

These base64-encoded Annex-B streams contain 12 frames of FFmpeg's generated
`testsrc2` pattern. They contain no third-party video. Tests decode them without
FFmpeg, network access, or the optional upstream `res/` corpus. Expected SHA-256
values cover the complete tightly packed I420 output in presentation order and
were obtained independently with FFmpeg's H.264 decoder, not this Go decoder.

Generated with Ubuntu FFmpeg `6.1.1-3ubuntu5`, libx264, on amd64. Commands below
describe provenance; newer encoder versions may produce different streams.

| File | Size | Decoded bytes | Features | Extra x264 parameters |
| --- | --- | --- | --- | --- |
| `baseline.h264.b64` | 62×46 | 51,336 | Cropping, CAVLC, I/P pictures, two slices | `cabac=0:bframes=0:slices=2` |
| `main_ip.h264.b64` | 96×64 | 110,592 | CABAC, I/P pictures, VUI color/aspect metadata | `cabac=1:bframes=0` |
| `high_ip.h264.b64` | 96×64 | 110,592 | CABAC, I/P pictures, VUI metadata, 8×8 transform | `cabac=1:bframes=0:8x8dct=1:weightp=2` |

For each row, substitute its profile (`baseline`, `main`, or `high`), size and
extra parameters. Main and High also use
`-aspect 4:3 -color_primaries bt709 -color_trc bt709 -colorspace bt709`:

```sh
ffmpeg -hide_banner -loglevel error -f lavfi -i "testsrc2=size=$size:rate=12" \
  -frames:v 12 -pix_fmt yuv420p -c:v libx264 -profile:v "$profile" \
  -preset medium -crf 22 \
  -x264-params "threads=1:lookahead-threads=1:scenecut=0:keyint=12:min-keyint=12:ref=3:aud=1:$extra" \
  -f h264 pipe:1 | base64 > "$name.h264.b64"

base64 -d "$name.h264.b64" |
  ffmpeg -hide_banner -loglevel error -i pipe:0 -fps_mode passthrough \
    -pix_fmt yuv420p -f rawvideo pipe:1 |
  sha256sum
```

The reference decoder must use `-fps_mode passthrough`: FFmpeg's default
frame-rate conversion duplicates frames for these raw streams. Replacing
`sha256sum` with `wc -c` must produce the decoded byte count above, equal to
`12 * width * height * 3 / 2`.

The test checks output dimensions, exact frame count, error-free decoding and
flushing, and reference checksums. A separate parser-only test checks the cropped
dimensions, Annex-B NAL boundaries, two slices per frame, and initial IDR picture.
These fixtures intentionally cover I/P pictures, not B-picture reordering.
