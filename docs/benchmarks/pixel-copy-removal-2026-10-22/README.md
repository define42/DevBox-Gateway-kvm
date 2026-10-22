# BGRX publication and direct I420 capture — 2026-10-22

The 2D BGRX scanout now publishes directly into framebuffer-owned packed pixels. H.264 readers convert the published pixels directly into their retained I420 planes. This removes the GPU's intermediate RGBA desktop and each H.264 reader's packed-pixel snapshot. The guest-to-resource transfer and the codec's internal source-picture copy remain.

`PrepareDamage` and `PrepareBGRXDamage` consume pixels and damage metadata synchronously. `EncodePrepared` uses only encoder-owned state. The framebuffer mutex covers preparation and is released before codec execution or socket writes. This keeps the published image stable during conversion without holding the producer behind encoding or transport waits. Conversion can still delay concurrent publication; this experiment does not measure live-guest contention or presentation latency.

Only flushed regions become visible. Slow readers accumulate tile damage independently; format switches preserve previously published pixels outside damage. Bitmap readers receive RGBA, other resource formats and native renderer readbacks retain their compatible publication path, and cursors remain separate. BGRX conversion preserves existing full-range BT.709 arithmetic, chroma averaging, scaling, change detection, SIMD tails and scalar fallback.

## Measurement

`BenchmarkFramebufferCapture` compares the retained copied path with the direct path in the same binary. The copied display deliberately omits the optional BGRX capability. Both paths use the same final encoder implementation, so this isolates publication/capture routing rather than comparing entire commits.

Six serial repetitions, 300 ms per case, `GOMAXPROCS=4`, one software encoder slice per reader, Go 1.26.9, linux/amd64, Intel Core i7-7700HQ. Every repetition runs copied then direct for each case. Readers run sequentially. The host is an interactive workstation; other activity was not disabled.

Pixels are initialized and encoded before timing. Repeated identical pixels with explicit damage then exercise all publication, capture and conversion passes, while unchanged-frame detection skips codec work. Guest transfer, active codec work, transport, ACK waits and client presentation are excluded. The small rectangle touches four 64x64 capture tiles. Three readers exercise independent framebuffer consumers, not three simultaneous live RDP connections.

| Update | Readers | Copied | Direct | Change |
|---|---:|---:|---:|---:|
| Full 1920x1080 | 1 | 5.940 ms | 3.120 ms | −47.46% |
| Full 1920x1080 | 3 | 12.177 ms | 6.416 ms | −47.31% |
| 64x32 region | 1 | 19.65 µs | 14.42 µs | −26.61% |
| 64x32 region | 3 | 56.68 µs | 41.56 µs | −26.69% |

All four timing comparisons have p=0.002, n=6. The one-reader full-frame saving is 2.82 ms for these preparation stages; it is not an end-to-end frame-latency measurement.

Retained packed storage (GPU publication image + shared framebuffer + reader RGB snapshots) falls from 24,883,200 to 8,294,400 bytes for one reader, or from 41,472,000 to 8,294,400 bytes for three readers. This count excludes the GPU resource, I420 planes and codec buffers. Per-iteration allocations remain 240 bytes / 4 allocations for one reader and 432 bytes / 8 allocations for three.

[Raw measurements](preparation.txt), [copied samples](copied.txt), [direct samples](direct.txt), [benchstat comparison](comparison.txt).

Reproduce from the repository root:

```sh
GOTOOLCHAIN=go1.26.9 go test -c -o /tmp/pixel-copy-virtio.test ./internal/virtio
for run in 1 2 3 4 5 6; do
  GOMAXPROCS=4 /tmp/pixel-copy-virtio.test \
    -test.run '^$' -test.bench '^BenchmarkFramebufferCapture$' \
    -test.benchmem -test.benchtime=300ms -test.count=1
done
```

The copied/direct sample files retain their raw rows with the final mode name removed, allowing benchstat to match cases.

## Validation

- Go 1.26.9: `go test ./internal/rdp/... ./internal/virtio -count=1` and `CGO_ENABLED=0 go build ./...` pass.
- Race detector: all AVC tests and the RDP/GPU/framebuffer virtio tests pass.
- Both affected packages compile for linux/arm64.
- AVX2-disabled preparation, BGRX conversion, GPU publication and framebuffer capture tests pass.
- Encoded bytes match the copied path for native/scaled output, partial and forced updates, source resize, format changes and independently paced readers.
- Publication after preparation cannot change the pending frame; concurrent publication/capture preserves coherent generations.
- Repository golangci-lint v2.12.2, using Go 1.26.9: affected packages pass.

The race and arm64 checks used the host Go 1.27.3 toolchain. Test and build logs are preserved in [validation.txt](validation.txt). Hardware encoding and live guest playback were not benchmarked.
