# Development

Run the commands below from the repository root. For operating the application,
see the [gateway guide](gateway.md) and [VM runtime guide](runtime.md).

## Build and contribute

Development targets x86-64 Linux with Go 1.26.0 or newer, as declared in
[`go.mod`](../go.mod). A normal build includes the software H.264 encoder and
needs no C compiler or native codec libraries:

```bash
CGO_ENABLED=0 go build -o gokvm .
./gokvm serve -help
./gokvm boot -help
./gokvm seed -help
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

[`make gokvm`](../Makefile) runs code generation before building. `make generate`
installs `stringer` if needed and runs `go generate ./...`; generated KVM enum
strings are committed. `make golangci` runs generation and the linter version
pinned in the Makefile, using [`.golangci.yaml`](../.golangci.yaml).

## Code layout and API boundary

The application entry point remains at the repository root. `go build -o gokvm .`
builds the CLI with its existing executable name; there is no separate
`cmd/gokvm` module.

| Location | Responsibility |
| --- | --- |
| [`main.go`](../main.go), [`gateway_command.go`](../gateway_command.go), [`seed_command.go`](../seed_command.go) | `boot`, `probe`, `serve`, and `seed` command entry points |
| [`kvm`](../kvm) | Public Go wrappers for Linux KVM ioctls |
| [`pkg/h264`](../pkg/h264), [`pkg/h264/api`](../pkg/h264/api) | Public bundled Go H.264 codec and its API |
| [`internal/gateway`](../internal/gateway) | HTTPS UI, LDAP authentication, image catalog, VM lifecycle, and token-routed RDP |
| [`internal/vmm`](../internal/vmm) | VM configuration, boot orchestration, and shutdown |
| [`internal/machine`](../internal/machine), [`internal/guestmem`](../internal/guestmem) | KVM machine setup, loaders, platform memory layout, and guest memory validation |
| [`internal/virtio`](../internal/virtio), [`internal/pci`](../internal/pci), [`internal/iodev`](../internal/iodev) | Devices, PCI transport, framebuffer, and guest input |
| [`internal/rdp`](../internal/rdp), [`internal/audio`](../internal/audio) | RDP protocol, graphics encoding, and audio delivery |
| [`internal/disk`](../internal/disk), [`internal/cloudinit`](../internal/cloudinit), [`internal/iso9660`](../internal/iso9660), [`internal/uki`](../internal/uki) | Disk access, NoCloud seeds, ISO discovery, and UKI parsing |
| [`internal/usernet`](../internal/usernet), [`internal/tap`](../internal/tap) | Built-in guest networking and host-managed TAP access |
| [`scripts`](../scripts), [`tools`](../tools), [`testldap`](../testldap) | Guest helpers, developer tools, and the local test directory service |

Application packages under `internal/` are implementation details, and Go prevents
other modules from importing them. The former public `vmm` and `machine` packages
were moved under `internal/` without compatibility packages. The public `kvm`,
`pkg/h264`, and `pkg/h264/api` packages are available under the
`github.com/define42/DevBox-Gateway-kvm` module path. See the
[codec guide](../pkg/h264/README.md) for its API and the
[porting notes](../pkg/h264/PORTING.md) for its implementation history.

The gateway starts a separate gokvm process for each running desktop. Each process
opens KVM and its own disk, firmware variables, seed, and TLS RDP listener. RDP
captures the VM's console framebuffer and sends input to emulated guest devices;
it does not start a guest XRDP session. The gateway authenticates and routes
connections before proxying them to the selected VM. See
[`Manager.startProcess`](../internal/gateway/manager.go) and the
[machine memory layout](../internal/machine/README.md) when changing these paths.

## Focused checks

The following package selections exercise the gateway, provisioning, graphics,
input, networking, storage, and parsers without booting a guest:

```bash
CGO_ENABLED=0 go test -short ./internal/gateway ./internal/cloudinit
CGO_ENABLED=0 go test -short ./internal/rdp/... \
  ./internal/virtio ./internal/vmm ./internal/flag
CGO_ENABLED=0 go test ./internal/audio ./internal/usernet ./internal/disk \
  ./internal/iso9660 ./scripts
```

Some tests bind local TCP listeners. Disk interoperability tests use `qemu-img`
and `qemu-io` when installed; AVC interoperability tests use `ffmpeg`. Those checks
skip when their external tool is unavailable. These are image/codec checks, not
QEMU guest runs.

For concurrency changes, run the relevant race checks with cgo enabled:

```bash
CGO_ENABLED=1 go test -race ./internal/gateway ./internal/audio ./internal/rdp/...
CGO_ENABLED=1 go test -race ./internal/virtio ./internal/vmm \
  -run 'Test(RDP|GPU|MultiDisplay|Audio|Sound|Modern|Framebuffer|SerialMirror)'
```

## Full suite and CI

```bash
make test
```

This runs generation and lints the default packages plus the integration-tagged
VMM code, then runs
`go test -count=1 -timeout 30m -coverprofile c.out ./...` in a fresh user/network
namespace with loopback enabled. It also runs the required `make test-qcow2`
firmware/disk smoke test and finishes with `go mod tidy -diff`. It does not
download or build Linux kernels or a standalone initrd.

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
The native gokvm KVM runtime boots OVMF with an isolated writable VARS copy.
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
GOKVM_REQUIRE_UEFI_TEST=1 \
GOKVM_TEST_OVMF_CODE=/usr/share/OVMF/OVMF_CODE_4M.fd \
GOKVM_TEST_OVMF_VARS=/usr/share/OVMF/OVMF_VARS_4M.fd \
  go test -tags=integration ./internal/vmm \
  -run '^TestUEFIQCOW2Boot$' -count=1 -timeout 3m -v
```

`GOKVM_REQUIRE_UEFI_TEST=1` makes missing tools, firmware, or KVM access a test
failure instead of a skip. `make test-qcow2` sets it automatically, including
when called by `make test` and CI.

The [CI workflow](../.github/workflows/ci.yml) runs the default Go suite and the
QCOW2/UEFI smoke test ten times, with lint and coverage on the first run. A
separate job checks the pure-Go and NVENC-tagged builds plus selected race tests.
The [vulnerability workflow](../.github/workflows/govulncheck.yml) runs
`govulncheck` against the module. Other integration-tagged tests and physical GPU
checks below remain opt-in; the NVENC CI build does not prove that a host GPU can
encode.

## Optional integration checks

Use the check that matches the change. These commands can launch guests or a
local LDAP service, and some need several GiB of free RAM.

### Image import and ISO discovery

The compressed QCOW2 import test needs `qemu-img`, but no KVM device:

```bash
go test -tags integration ./internal/gateway \
  -run '^TestImageCatalogNormalizesCompressedQCOW2$' -count=1 -v
```

To exercise ISO discovery with a local image without booting it:

```bash
GOKVM_TEST_ISO=/path/to/linux.iso go test ./internal/iso9660 \
  -run '^TestLoadBootFilesFromExternalISO$' -count=1 -v
```

### LDAP

```bash
make gateway-ldap-test
make ldap-stop
```

The first target starts the bundled directory with Docker Compose and verifies
authentication and role membership over LDAPS. It requires Docker Compose and
OpenSSL. See the [LDAP guide](../testldap/README.md) for its public demo accounts,
certificate setup, and `GOKVM_TEST_LDAP_URL` / `GOKVM_TEST_LDAP_CA` overrides.

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
GOKVM_TEST_NVENC=1 CGO_ENABLED=1 go test -tags 'nvenc integration' \
  ./internal/rdp/avc -run '^TestNVENCHardware' -count=1 -timeout 2m -v
```

This covers independent decoding, forced IDR refreshes, unusual frame dimensions,
color metadata, output ownership, and software fallback after a hardware error.

### Guest lifecycle and desktops

The untagged `TestTinyCoreVNCDesktopBoot` is also included in the default Go
suite. It uses the repository-root `TinyCore-current.iso` and skips in short
mode, without root/KVM, or when the ISO is missing. The ISO is tracked, so a
normal CI checkout includes it. This existing conditional desktop check is
separate from the required QCOW2/UEFI firmware smoke test above.

The tagged lifecycle tests require `/dev/kvm`; the headless boot test also needs
non-terminal stdin. They use synthetic guest code and do not need a Linux kernel
or initrd:

```bash
go test -tags integration ./internal/machine ./internal/vmm \
  -run '^Test(CloseStopsRunningVCPUs|CloseReleasesResources|RunImmediateExitReturnsEINTR|HeadlessBootReturnsCPUFailure|SetupFailureClosesMachineAndConsole|RDPWaitCancellationPrecedesVCPURun)$' \
  -count=1 -v </dev/null
```

The NetDesk test boots a 6 GiB guest and checks desktop readiness, graphics,
pointer/keyboard input, and Chromium networking. Run it separately from other
heavy boots:

```bash
GOKVM_NETDESK_UKI=/path/to/netdesk.efi go test -tags integration ./internal/vmm \
  -run '^TestNetDeskUKIDesktopBoot$' -count=1 -timeout 6m -v
```

For native UEFI graphics, persistent variables across reset, and shutdown:

```bash
OVMF_CODE=/usr/share/OVMF/OVMF_CODE_4M.fd \
OVMF_VARS=/usr/share/OVMF/OVMF_VARS_4M.fd tools/test-uefi.sh
```

This needs matching plain non-SMM OVMF firmware, KVM, GNU-EFI development files,
a C compiler/binutils, mtools, `qemu-img`, `timeout`, `truncate`, and Python 3.
It builds an EFI test application and boots isolated raw and QCOW2 disks.
Artifacts are retained on failure. Set `UEFI_KEEP_ARTIFACTS=1` to retain successful
runs, `UEFI_CPUS` to vary vCPU count, `UEFI_TIMEOUT` to change the per-boot timeout,
or `GOKVM_BINARY` to use an existing binary. `EFI_INCLUDE_DIR` and `EFI_LIB_DIR`
select nonstandard GNU-EFI installations. The script tests firmware/platform
behavior; it does not establish compatibility with every guest OS.

## Debugging and performance

Set these variables to any nonempty value for device traces:

| Variable | Output |
| --- | --- |
| `GOKVM_TRACE_IO` | Guest port I/O exits |
| `GOKVM_TRACE_MMIO` | Unhandled guest MMIO accesses |
| `GOKVM_TRACE_ATAPI` | Commands received by the emulated ATAPI device |

`boot -T N` enables instruction tracing with a skip interval. `boot -rdp-stats`
reports framebuffer, conversion, encoding, transport, and pacing measurements
for an RDP session. Use the same guest workload, client dimensions, and encoder
when comparing changes; an RDP connection alone does not prove desktop readiness.
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
