# Development

Run the commands below from the repository root. New contributors can start with
[CONTRIBUTING.md](../CONTRIBUTING.md). For operating the application, see the
[gateway guide](gateway.md) and [VM runtime guide](runtime.md).

## Build and contribute

Development targets x86-64 Linux with Go 1.26.0 or newer, as declared in
[`go.mod`](../go.mod). A normal build includes the software H.264 encoder and
needs no C compiler or native codec libraries:

```bash
CGO_ENABLED=0 go build -o devbox-kvm .
./devbox-kvm serve -help
./devbox-kvm boot -help
./devbox-kvm seed -help
```

Booting guests also requires hardware virtualization and read/write access to
`/dev/kvm`. Nested hosts must expose KVM. The optional NVENC build and Go race
detector require cgo and a C compiler; NVENC additionally needs `pkg-config`,
`libavcodec`, and `libavutil` development files. See the
[runtime guide](runtime.md) for encoder setup.

Include a reproduction or focused regression test for behavior changes. Update
the relevant guide and CLI help when changing flags, defaults, guest requirements,
or the web workflow. Describe which checks ran and which hardware-dependent
checks were skipped. Avoid using live gateway disks as test fixtures.

[`make devbox-kvm`](../Makefile) runs code generation before building. `make generate`
installs `stringer` if needed and runs `go generate ./...`; generated KVM enum
strings are committed. `make golangci` runs generation and the linter version
pinned in the Makefile, using [`.golangci.yaml`](../.golangci.yaml).

## Code layout and API boundary

The application entry point is at the repository root. `go build -o devbox-kvm .`
builds the CLI; there is no separate
`cmd/devbox-kvm` module.

| Location | Responsibility |
| --- | --- |
| [`main.go`](../main.go), [`gateway_command.go`](../gateway_command.go), [`seed_command.go`](../seed_command.go) | `boot`, `probe`, `serve`, and `seed` command entry points |
| [`kvm`](../kvm) | Public Go wrappers for Linux KVM ioctls |
| [`pkg/h264`](../pkg/h264), [`pkg/h264/api`](../pkg/h264/api) | Public bundled Go H.264 codec and its API |
| [`internal/gateway`](../internal/gateway) | HTTPS UI, LDAP authentication, image catalog, VM lifecycle, and token-routed RDP |
| [`internal/config`](../internal/config), [`internal/flag`](../internal/flag) | Environment-backed gateway settings and VM CLI options |
| [`internal/vmm`](../internal/vmm) | VM configuration, boot orchestration, and shutdown |
| [`internal/machine`](../internal/machine), [`internal/guestmem`](../internal/guestmem) | KVM machine setup, loaders, platform memory layout, and guest memory validation |
| [`internal/virtio`](../internal/virtio), [`internal/pci`](../internal/pci), [`internal/iodev`](../internal/iodev) | Devices, PCI transport, framebuffer, and guest input |
| [`internal/rdp`](../internal/rdp), [`internal/audio`](../internal/audio) | RDP protocol, graphics encoding, and audio delivery |
| [`internal/disk`](../internal/disk), [`internal/cloudinit`](../internal/cloudinit) | Disk access and NoCloud seed creation and validation |
| [`internal/usernet`](../internal/usernet), [`internal/tap`](../internal/tap) | Built-in guest networking and host-managed TAP access |
| [`scripts`](../scripts), [`tools`](../tools), [`testldap`](../testldap) | Guest helpers, developer tools, and the local test directory service |

Application packages under `internal/` are implementation details, and Go prevents
other modules from importing them. The former public `vmm` and `machine` packages
were moved under `internal/` without compatibility packages. The public `kvm`,
`pkg/h264`, and `pkg/h264/api` packages are available under the
`github.com/define42/DevBox-Gateway-kvm` module path. See the
[codec guide](../pkg/h264/README.md) for its API and the
[porting notes](../pkg/h264/PORTING.md) for its implementation history.

The gateway starts a separate devbox-kvm process for each running desktop. Each worker
opens KVM and its own disk, firmware variables, and seed, then adopts the private
RDP Unix listener created by the gateway. RDP uses TLS, captures the VM's console
framebuffer, and sends input to emulated guest devices;
it does not start a guest XRDP session. The gateway authenticates and routes
connections before proxying them to the selected VM. See
[`Manager.startProcess`](../internal/gateway/manager.go) and the
[machine memory layout](../internal/machine/README.md) when changing these paths.

## Focused checks

The following package selections exercise the gateway, provisioning, graphics,
input, networking, and storage without booting a guest:

```bash
CGO_ENABLED=0 go test -short ./internal/config ./internal/gateway ./internal/cloudinit
CGO_ENABLED=0 go test -short ./internal/rdp/... \
  ./internal/virtio ./internal/vmm ./internal/flag
CGO_ENABLED=0 go test ./internal/audio ./internal/usernet ./internal/disk \
  ./internal/qcow2import ./scripts
```

Some tests bind local TCP listeners. Disk interoperability tests use `qemu-img`
and `qemu-io` when installed; AVC interoperability tests use `ffmpeg`. Those checks
skip when their external tool is unavailable. Production QCOW2 imports and
normalization do not require either QEMU tool. These are image/codec checks, not
QEMU guest runs.

For concurrency changes, run the relevant race checks with cgo enabled:

```bash
CGO_ENABLED=1 go test -race ./internal/gateway ./internal/audio ./internal/rdp/...
CGO_ENABLED=1 go test -race ./internal/virtio ./internal/vmm \
  -run 'Test(RDP|GPU|Audio|Sound|Modern|Framebuffer|Input|FirmwareInput|Blk)'
```

For dashboard changes, use the same JavaScript checks as the Node.js 22 CI job:

```bash
node --check internal/gateway/dashboard.js
node --test internal/gateway/dashboard.test.cjs
```

## Full suite and CI

```bash
make test
```

This runs generation and lints the default packages plus the integration-tagged
VMM, machine, and virtio packages, then runs `make coverage` in a fresh user/network
namespace with loopback enabled. It also runs the required `make test-qcow2`
firmware/disk smoke test and `make test-audio`, then finishes with
`go mod tidy -diff`. It does not download or build Linux kernels or a standalone
initrd.

On Debian/Ubuntu, install the full suite's host tools with:

```bash
sudo apt-get install qemu-utils util-linux iproute2 ovmf \
  build-essential binutils gnu-efi dosfstools mtools
```

The full suite needs KVM access, `/dev/net/tun`, permission to create user and
network namespaces, `unshare`, and `ip`. TAP tests create interfaces in that
namespace. Some machine tests require root as seen inside the namespace.
Running `go test -short ./...` is not a substitute: KVM and TAP tests do not all
honor short mode or skip when devices are unavailable.

### Required QCOW2/UEFI smoke test

```bash
make test-qcow2
```

This builds a tiny EFI application, places it at `EFI/BOOT/BOOTX64.EFI` in a
temporary 64 MiB FAT32 disk, and converts the disk to QCOW2 with `qemu-img`.
The native devbox-kvm KVM runtime boots OVMF with an isolated writable VARS copy.
The test requires the application to report a serial success marker and request
a successful firmware shutdown. QEMU is used only for image conversion, not to
run the guest. No Linux kernel, initrd, downloaded desktop image, or live gateway
disk is used. This checks firmware discovery, QCOW2/virtio-block boot, EFI
execution, and shutdown; it does not establish full OS or desktop readiness.

Use matching plain, non-SMM OVMF CODE and VARS images. The Make target defaults
to `/usr/share/OVMF/OVMF_CODE_4M.fd` and
`/usr/share/OVMF/OVMF_VARS_4M.fd`; override them with `OVMF_CODE` and `OVMF_VARS`:

```bash
make test-qcow2 OVMF_CODE=/path/to/OVMF_CODE.fd OVMF_VARS=/path/to/OVMF_VARS.fd
```

For a direct Go invocation, use the corresponding test environment variables:

```bash
DEVBOX_KVM_REQUIRE_UEFI_TEST=1 \
DEVBOX_KVM_TEST_OVMF_CODE=/usr/share/OVMF/OVMF_CODE_4M.fd \
DEVBOX_KVM_TEST_OVMF_VARS=/usr/share/OVMF/OVMF_VARS_4M.fd \
  go test -tags=integration ./internal/vmm \
  -run '^TestUEFIQCOW2Boot$' -count=1 -timeout 3m -v
```

`DEVBOX_KVM_REQUIRE_UEFI_TEST=1` makes missing tools, firmware, or KVM access a test
failure instead of a skip. `make test-qcow2` sets it automatically, including
when called by `make test` and CI.

### Audio integration checks

```bash
make test-audio
```

This is part of `make test`. It checks UEFI virtio-sound PCI/ACPI registration,
interrupt acknowledgement, and worker cleanup using KVM, then verifies RDP audio
negotiation, stereo PCM delivery across reconnects, and desktop input/output when
the client has no audio channel. It does not boot a guest OS, require a sound
card, or prove that a particular desktop image has a working audio driver.
The machine check skips when KVM is absent or inaccessible; inspect the verbose
output when running this target on its own. The preceding `make test-qcow2` step
in the full suite treats missing KVM as a failure.

The RDP-only checks need no KVM device and also run under the race detector in CI:

```bash
CGO_ENABLED=1 go test -race -tags=integration -count=1 ./internal/virtio \
  -run '^TestRDPDisplayAudio'
```

### CI coverage

```bash
make coverage                     # Run the default suite and enforce 80% coverage.
make coverage-check               # Check the existing c.out without rerunning tests.
go tool cover -func=c.out          # Inspect function-level coverage.
go tool cover -html=c.out          # Browse uncovered source statements.
```

`make coverage` uses `go test -count=1 -timeout 30m -covermode=atomic
-coverpkg=./... -coverprofile c.out ./...`. Cross-package instrumentation counts
code reached through public APIs and end-to-end package tests, not only the
package's own tests. Atomic counters also support concurrent execution.

The gate requires at least **80% of statements across all module packages**;
it does not average package percentages or exclude bundled codec, generated Go,
or command code. It compares the unrounded statement totals, so 79.999% fails.
Per-package lines printed by `go test -coverpkg=./...` show each test binary's
contribution, not the aggregate; use the gate's summary or `go tool cover` for
the combined result. Integration-tagged tests run separately and do not count
toward this default-suite threshold.

The self-contained codec tests run without the optional upstream `res/` corpus.
Missing optional fixtures do not bypass the coverage gate. The coverage command
still needs the full suite's KVM/network prerequisites listed above.

The [CI workflow](../.github/workflows/ci.yml) runs the default Go suite and the
QCOW2/UEFI smoke test ten times, with lint, coverage, and `make test-audio` on the
first run. `make test` enforces the threshold before CI uploads `c.out` to
Coveralls. Separate jobs check the pure-Go and NVENC-tagged builds, selected race
tests (including RDP audio), and the dashboard JavaScript.
The [vulnerability workflow](../.github/workflows/govulncheck.yml) runs
`govulncheck` against the module. Other integration-tagged tests and physical GPU
checks below remain opt-in; the NVENC CI build does not prove that a host GPU can
encode.

## Optional integration checks

Use the check that matches the change. These commands can launch guests or a
local LDAP service, and some need several GiB of free RAM.

### Image import

The optional QCOW2 interoperability tests use `qemu-img` to generate independent
compressed and alternative-layout fixtures, then import them through the Go
normalizer. They need no KVM device; ordinary import coverage does not require
an external image tool:

```bash
go test -tags integration ./internal/gateway \
  -run '^TestImageCatalogNormalizes(CompressedQCOW2|QCOW2Layouts)$' -count=1 -v
```

### LDAP

```bash
make gateway-ldap-test
make ldap-stop
```

The first target starts the bundled directory with Docker Compose and verifies
authentication and role membership over LDAPS. It requires Docker Compose and
OpenSSL. See the [LDAP guide](../testldap/README.md) for its public demo accounts,
certificate setup, and `DEVBOX_KVM_TEST_LDAP_URL` / `DEVBOX_KVM_TEST_LDAP_CA` overrides.

### Hardware H.264

With the NVENC development dependencies installed, build and test the optional
backend without requiring a GPU:

```bash
CGO_ENABLED=1 go build -tags nvenc ./...
CGO_ENABLED=1 go test -short -tags nvenc ./internal/rdp/... \
  ./internal/virtio ./internal/vmm ./internal/flag
```

With an NVIDIA H.264 NVENC-capable GPU, a working driver, and the `ffmpeg` decoder
installed, opt into actual hardware encoding:

```bash
DEVBOX_KVM_TEST_NVENC=1 CGO_ENABLED=1 go test -tags 'nvenc integration' \
  ./internal/rdp/avc -run '^TestNVENCHardware' -count=1 -timeout 2m -v
```

This covers independent decoding, forced IDR refreshes, unusual frame dimensions,
color metadata, output ownership, and software fallback after a hardware error.

### Guest lifecycle and desktops

The tagged lifecycle tests require `/dev/kvm`; the headless boot test also needs
non-terminal stdin. They use synthetic guest code and do not need a Linux kernel
or initrd:

```bash
go test -tags integration ./internal/machine ./internal/vmm \
  -run '^Test(CloseStopsRunningVCPUs|CloseReleasesResources|RunImmediateExitReturnsEINTR|HeadlessBootReturnsCPUFailure|SetupFailureClosesMachineAndConsole|RDPWaitCancellationPrecedesVCPURun)$' \
  -count=1 -v </dev/null
```

For native UEFI disk boot, persistent variables across reset, and shutdown:

```bash
OVMF_CODE=/usr/share/OVMF/OVMF_CODE_4M.fd \
OVMF_VARS=/usr/share/OVMF/OVMF_VARS_4M.fd tools/test-uefi.sh
```

This needs matching plain non-SMM OVMF firmware, KVM, GNU-EFI development files,
a C compiler/binutils, mtools, `qemu-img`, `timeout`, `truncate`, and Python 3.
It builds an EFI test application and boots an isolated QCOW2 disk.
Artifacts are retained on failure. Set `UEFI_KEEP_ARTIFACTS=1` to retain successful
runs, `UEFI_CPUS` to vary vCPU count, `UEFI_TIMEOUT` to change the per-boot timeout,
or `DEVBOX_KVM_BINARY` to use an existing binary. `EFI_INCLUDE_DIR` and `EFI_LIB_DIR`
select nonstandard GNU-EFI installations. The script tests firmware/platform
behavior; it does not establish compatibility with every guest OS.

## Debugging and performance

Set these variables to any nonempty value for device traces:

| Variable | Output |
| --- | --- |
| `DEVBOX_KVM_TRACE_IO` | Guest port I/O exits |
| `DEVBOX_KVM_TRACE_MMIO` | Unhandled guest MMIO accesses |

Use these traces for direct developer boots. Managed workers discard standard
output and error; use gateway telemetry and diagnostics for those desktops.

`boot -T N` enables instruction tracing with a skip interval. Managed VM
telemetry reports framebuffer, conversion, encoding, transport, and pacing
measurements for gateway RDP sessions. Use the same guest workload, client
dimensions, and encoder when comparing changes; an RDP connection alone does
not prove desktop readiness. Standalone RDP listeners are not supported.
Detailed performance options and display limitations are in the
[runtime guide](runtime.md).

The software AVC path passes complete damage information into guarded unchanged-block
encoding. See the [sparse-encoding benchmark](benchmarks/rdp-sparse-2026-10-18/README.md)
for before/after measurements, reproduction commands, and remaining full-picture costs.

### Disk benchmarks

The [isolated host/guest disk harness](../scripts/diskbench/README.md) runs the
same 4 KiB random-read and durable-write workloads against temporary host files
and raw/QCOW2 disks in a minimal Linux guest. It requires an externally supplied
compatible kernel via `KERNEL` and builds its own minimal initramfs. It records
latency and IOPS, checks written data, and compares disk contents after reopening.
Run baseline and candidate sequentially on the same filesystem; distinguish warm
host-cache reads from physical storage access.

Focused Go benchmarks cover QCOW2 metadata and the block worker:

```bash
go test ./internal/disk -run '^$' -bench '^BenchmarkQCOW2' -benchmem -count=10
go test ./internal/virtio -run '^$' -bench '^BenchmarkBlkWrite' -benchmem -count=10
```

`BenchmarkBlkWriteFlushGroup` compares 32 individually submitted writes per
durability boundary in writethrough and negotiated writeback modes. These Go
benchmarks do not include a running guest or KVM notification overhead.
The [2026-10-11 evaluation](benchmarks/disk-2026-10-11.md) records the measured
flush and QCOW2 cache changes, including workload limits and raw samples.

## Protocol and implementation references

- [Linux KVM API](https://docs.kernel.org/virt/kvm/api.html)
- [Linux/x86 boot protocol](https://docs.kernel.org/arch/x86/boot.html)
- [Virtio specification](https://docs.oasis-open.org/virtio/virtio/v1.2/virtio-v1.2.html)
- [Machine memory layout and loaders](../internal/machine/README.md)
- [Bundled H.264 implementation](../pkg/h264/README.md)
- [Gateway static asset provenance](../internal/gateway/static/README.md)
