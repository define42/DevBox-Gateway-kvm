# QCOW2 import reader

The descriptor types, bit extraction logic, extent model and read design were
adapted from [lima-vm/go-qcow2reader](https://github.com/lima-vm/go-qcow2reader),
commit [`b4a622473d3b7fefea093b20bfd79bbc39181112`](https://github.com/lima-vm/go-qcow2reader/tree/b4a622473d3b7fefea093b20bfd79bbc39181112),
specifically `image/qcow2/qcow2.go` and `image/image.go`.
The upstream Apache-2.0 license is retained in [LICENSE](LICENSE). Upstream has
no NOTICE file. This adaptation is modified from upstream, not a verbatim copy.

The import-only API removes backing-file access, snapshot support, format
autodetection, logging and JSON inspection helpers. Those restrictions eliminate
the upstream helper-package dependencies. Parsing, validation, caching and read
orchestration were rewritten for untrusted uploads. The caller retains ownership
of the input file, which must not change while the reader is in use.

Supported inputs are standalone QCOW2 v2/v3 images, up to 1 TiB, with 512-byte
through 2-MiB clusters. Standard and extended L2 entries, shared clusters, all
QCOW2 refcount widths, zero/preallocated regions, and consistent bitmap metadata
are accepted. Conversion preserves guest-visible data and discards bitmap state.
Backings, snapshots, encryption, dirty/corrupt flags, external data and unknown
incompatible/autoclear features are rejected.

Both raw DEFLATE and Zstd decoding use `github.com/klauspost/compress` without
CGO. Each stream must decode to exactly one cluster. Zstd checksums remain
enabled, windows and decoder memory are limited to 8 MiB, and dictionaries and
skippable frames are rejected. QCOW2's final compressed sector may include
padding or the start of another cluster's stream; those bytes are not decoded.

Validation checks table geometry and file bounds, referenced refcounts, copied
flags, reserved bits, subcluster bitmaps, and metadata/data overlap. It is not a
replacement for every `qemu-img check` diagnostic: unreachable storage and leaked
refcounts are not repaired or audited. Metadata traversal is capped at 128 MiB,
with separate 32-MiB L1 and 8-MiB refcount-table limits. Shared-reference
counters use at most 64 MiB in sparse pages. Cancellation is checked
during traversal and reads. Active caches hold one L2 table, one refcount block
and one decoded cluster.

The format reference is the [QEMU QCOW2 specification](https://www.qemu.org/docs/master/interop/qcow2.html).
