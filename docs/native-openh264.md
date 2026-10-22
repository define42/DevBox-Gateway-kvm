# Native Cisco OpenH264 experiment

The container on this branch selects Cisco OpenH264 (the native C++ codec) instead
of the Go port for the RDP software AVC encoder. GPU NVENC and automatic fallback
continue to use the established encoder interface. The existing Go port stays
available without the `openh264_native` build tag.

## Native Linux build

On Debian, install `libopenh264-dev`, `pkg-config`, `g++`, and `nasm`.
Then run `make devbox-kvm-openh264` or
`CGO_ENABLED=1 go build -tags openh264_native ./...`.

The distribution OpenH264 library must itself be built with NASM to benefit from
assembly optimizations: merely installing `nasm` next to a non-optimized binary
does not change that binary. Check the distribution build flags or build Cisco
OpenH264 from source with NASM before linking.

The container build links to Debian's OpenH264 package and bundles the runtime
library. The software codec remains H.264 Baseline, fixed QP, CAVLC, full-range
BT.709, without periodic IDRs. The native codec currently does **not** consume
the Go encoder's unchanged-macroblock hints; compare sparse desktop updates as
well as full-frame workloads before merging into main.

## Validation

Run `make test-openh264` and validate color, SPS VUI metadata, forced IDRs,
slice counts, P-frame continuity and FreeRDP behavior at 1080p60 and 4K60.
Compare median and P99 encode latency, CPU, output size, and measured end-to-end
RDP frame delivery against main. The branch has not yet been performance-qualified.
