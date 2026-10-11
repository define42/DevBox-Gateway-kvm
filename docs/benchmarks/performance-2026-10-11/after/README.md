# Performance fixes and validation

Implements the six prioritized findings in the [review](../../../performance-review-2026-10-11.md). This directory records the uncommitted implementation against base revision `3decb2ff9b0684269eda0b1ebeabbe73414e5862`; [source hashes](implementation.json) identify the measured Go files.

| Change | Result and regression coverage |
|---|---|
| VM state reads separated from lifecycle operations | Inventory and existing endpoints stay readable during cloning; queued lifecycle operations honor cancellation; shutdown cancels active and queued creation. |
| Short image-catalog locking with open source descriptors | An in-flight clone survives template deletion; unrelated catalog reads/imports continue. |
| RDP state locks released during frame/graphics-resize writes | Keyboard delivery is required before releasing the test's blocked writer, including ACK-first traffic and resize. Tests also cover early ACK, suspension, partial failure, and close ordering. |
| Sparse non-reflink copy | Extent traversal skips holes; zero-block scanning supports filesystems without extent queries. Tests verify full content, file size, sparse allocation, unaligned extents, trailing/all holes, cancellation, and changed source size. |
| Bounded creation and independent deadlines | `MAX_CONCURRENT_CREATIONS=2` limits this gateway's provisioning across owners. `VM_CREATE_TIMEOUT=30m` includes queue wait. Each progress write is bounded at 30 seconds, with the total deadline restored between writes. Ordinary HTTP timeouts remain unchanged. |
| Adaptive idle device watchdogs | Idle polling backs off to 100 ms; normal kicks/queue setup remain immediate and pending IRQs keep original retry intervals. Virtual-time tests cover missing notifications, early interrupts, acknowledgement/reset, burst coalescing, and shutdown. |
| GPU backing traversal | A transfer-local cursor advances across rows, eliminating repeated searches through preceding backing entries. Correctness tests cover fragmentation, irregular entries, offsets, clipping, and missing/invalid backing. |

`MAX_CONCURRENT_CREATIONS` is per gateway manager, not a host-wide lock shared by separate gateway processes. A genuinely missing guest notification can now wait up to the 100 ms idle watchdog; this does not affect ordinary kicks. Optional sound polling remains unchanged because its timer also schedules playback, and default gateway workers do not enable sound.

## Measured changes

Same preserved fixtures and settings as the baseline, run sequentially after all test/build activity completed: Go 1.26.9, `GOMAXPROCS=4`, six 300 ms GPU samples, three three-second idle samples per group. The shared interactive host and component-only scope remain limitations.

| GPU transfer | Before | After | Time reduction |
|---|---:|---:|---:|
| 1080p partial browser rectangle | 1.773 ms | 1.139 ms | 35.75% |
| 4K partial browser rectangle | 10.875 ms | 5.203 ms | 52.16% |
| 1080p full frame | 1.326 ms | 1.294 ms | No significant difference |
| 4K full frame | 5.538 ms | 5.594 ms | No significant difference |

Both partial-transfer comparisons had benchstat `p=0.002`, six samples per side. All paths retained zero Go allocations per operation. [Raw after samples](gpu-transfer.txt), [complete comparison](gpu-comparison.txt).

| Synthetic idle device groups | Before median CPU, % of one core | After median CPU, % of one core |
|---|---:|---:|
| 0 | 0.022 | 0.017 |
| 1 | 17.117 | 0.311 |
| 4 | 27.631 | 0.440 |

After one-group samples ranged 0.272–0.350%; four-group samples ranged 0.440–0.442%. These are synthetic empty devices in one Go process, not complete VM processes or guest office workloads. The figures do not establish desktop density. [Raw after samples](idle-devices.txt), [sample/median comparison](idle-comparison.json).

## Validation

- Full repository test suite passed using the CI user/network namespace with loopback enabled. On this host the Snap launcher cannot run inside that namespace, so the actual Go 1.26.9 toolchain executable was used. [Output](tests.txt).
- Full race tests passed for gateway, RDP/AVC, virtio and VMM. [Output](race.txt).
- Pure-Go (`CGO_ENABLED=0`) and optional NVENC (`-tags=nvenc`) short suites passed. [Pure-Go](purego.txt), [NVENC](nvenc.txt).
- Repository lint passed with zero issues. Focused regression output is retained in `regressions.txt`.

To reproduce GPU/idle measurements, prepare the overlays using the [baseline instructions](../README.md), then run the same GPU and idle commands against the fixed tree. Do not run the old adverse-behavior gateway/backpressure experiments as assertions against fixed code; the repository's new regression tests expect the corrected behavior.

Memory admission, guest defaults, codec quality and encoder-thread policy are unchanged. End-to-end simultaneous-office validation is still needed before selecting a supported desktop count.
