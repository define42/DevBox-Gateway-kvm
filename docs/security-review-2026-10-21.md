**Security review — 21 October 2026**

Reviewed commit: `79a82e3df12d5f067d9d6b05e2316c21c98ce6a9`.
Environment: Linux amd64, Go `1.27.2`; vulnerability checker `golang.org/x/vuln v1.9.0`.

This focused review identified one reproducible guest-triggered resource-control
defect, one reachable standard-library advisory in the local toolchain, and a
significant architectural containment limitation. No guest-to-host memory
corruption or arbitrary host-code execution was demonstrated. This conclusion
is limited to the source inspection and bounded tests described below; it is
not an independent security certification.

The review covered KVM exit dispatch and mapping lifetimes; guest-memory range
validation; PCI and selected firmware/PS/2 emulation; virtio transport, block,
network, input, sound and GPU handling; QCOW2 validation and writes; userspace
network forwarding/DNS; worker launch/containment; and selected RDP negotiation
and credential parsing. The assumed attacker controls a guest kernel and its
device requests. RDP clients are separately untrusted. Image import is an
administrator-only capability, and the Linux host/kernel is trusted.

**1. Medium — guest DNS bypasses the network connection budget (confirmed).**

The normal TCP/UDP forwarding paths reserve one of 256 connection slots in
`internal/usernet/forward.go:39`. The built-in DNS handler in
`internal/usernet/dns.go:54` instead calls `dns.Client.ExchangeContext` for
each request without admission control. Both UDP DNS work and accepted TCP DNS
clients lack an explicit concurrency cap. DNS is installed in the managed
`user-isolated` network as well as the standalone network.

A bounded reproduction used the actual guest/network stacks and a silent UDP
resolver bound to host loopback. Sending 300 ordinary guest DNS queries created
300 distinct simultaneous upstream sockets and increased `/proc/self/fd` by
300, while the forwarding semaphore remained **0/256**. Independent rerun:

```text
DNS requests=300, distinct upstream sockets=300, FD increase=300,
forwarding slots=0/256, duration=52.563985ms
PASS
```

A sustained stream can therefore exceed the intended networking budget and
pressure the worker's file descriptors, memory and CPU, as well as the shared
host resolver/network. Timeouts bound each query's lifetime but do not bound
the number alive at once. The worker's memory cgroup mitigates memory impact.
The test did not exhaust resources or demonstrate a host-wide/cross-VM outage;
the strongest proven effect is unbudgeted host sockets in the attacking VM's
worker. This is not a VM escape.

Recommended fix: impose a bounded, nonblocking admission policy for DNS query
work and upstream sockets, with a separate cap on accepted TCP DNS clients.
Reject/drop excess work without accumulating waiting goroutines. A semaphore
inside the query handler alone does not bound idle TCP clients. Add saturation,
timeout, cancellation and recovery regressions. The archived reproduction is
[dns_limits_test.go.txt](security-review-2026-10-21/dns_limits_test.go.txt).

**2. High impact if a worker is compromised — workers lack a separate security boundary (architectural risk).**

`internal/gateway/manager.go:695` launches the VM with the gateway's host
identity. Its process attributes at line 719 set a process group and parent
death signal. `internal/gateway/manager_cgroup.go:621` adds clone-time cgroup
attachment; the controls configured at line 548 are `memory.max`,
`memory.swap.max` and `memory.oom.group`. The inspected launch/runtime path
does not establish a per-worker UID, filesystem namespace/jail or application
syscall sandbox. The worker also inherits the parent's environment.

This is consistent with the shared-account limitation documented in
`README.md:298`. Separate processes and mandatory memory limits are useful
controls, but they do not restrict a compromised worker to its own VM files.
Host code execution in a worker could expose or alter other VM state and
service-account resources. Docker provides an outer boundary for the service;
in the supplied deployment, the gateway and workers still share that boundary
and the state mount.

This finding assumes an additional worker-compromise primitive; none was found
in this review. It must not be described as a demonstrated guest escape or an
authentication bypass. It is nevertheless a major limitation for mutually
untrusted tenants because the custom device code is exposed to their guests.

Recommended fix: introduce an explicit worker sandbox, with a separate
identity and minimal filesystem/device visibility, and grant only that VM's
resources. Keep gateway credentials and the writable cgroup-management tree
outside the worker's accessible view. Add suitable syscall restrictions and
CPU/process/I/O resource controls as defense in depth. Verify containment with
an intentionally compromised-worker test, rather than only guest-crash tests.

**3. Reachable toolchain advisory — Go 1.27.2 certificate verification (deployment-dependent).**

`govulncheck ./...` reported `GO-2026-6809` / `CVE-2026-111943` in
`crypto/x509`, with a reachable path through
`internal/gateway/auth.go:174` (`HandshakeContext` to `Certificate.Verify`).
The advisory concerns distrusting specific compromised intermediate
certificates. It is a build/runtime dependency issue adjacent to the VMM,
not a defect in the KVM device implementation.

Actual exploitability depends on the configured trust anchors and an attack
on a TLS connection. For example, the LDAP CA-file option builds a private
root pool (`internal/gateway/auth.go:93`); a pool containing only an unrelated
private CA does not establish trust in the affected public chains. The scan
proves symbol reachability, not that this deployment accepts such a chain.
No deployed binary/container or trust store was inspected.

Rebuild using **Go 1.27.3 or later in that series**, or **Go 1.26.10 or later
in that series** where compatible with the selected dependencies, and rerun
the scan against the release build configuration. Merely changing `go.mod`
does not patch an already built executable. Fixed releases are documented in
the [Go release history](https://go.dev/doc/devel/release); affected conditions
and ranges are in the [Go Project CVE record](https://www.cve.org/CVERecord?id=CVE-2026-111943).

The scan found no other reachable advisories. It reported seven advisories in
required modules whose vulnerable code was not reached. Those are not counted
as confirmed application vulnerabilities here. The initial installed scanner
could not parse Go 1.27; a current checker was built in `/tmp` and the completed
scan above used that checker. Native C libraries and optional build tags were
not covered by this scan.

**Controls verified during review.**

- Modern virtqueues validate the full size and alignment of unsafe queue views
  before mapping them (`internal/virtio/modern.go:545`). Shared guest-memory
  range checks reject overflow, out-of-RAM addresses and the MMIO hole.
- Block handling snapshots bounded descriptor chains and validates capacity
  before host disk I/O. Network, input and sound paths bound descriptor walks
  and validate guest buffers. No cross-boundary defect was confirmed in these
  paths.
- QCOW2 validation checks metadata bounds/alignment, ownership/overlap and
  refcounts; caches are bounded. Unsupported backing files, external data,
  encryption and snapshots are rejected. Administrator image checks precede
  `qemu-img` processing. No QCOW2 isolation defect was confirmed.
- KVM shutdown joins active vCPUs and device workers before releasing guest
  memory. Targeted real-KVM lifecycle tests passed.
- RDP uses private backend sockets and kernel peer identity checks, with
  gateway routing grants, ownership and fresh password checks. Bounded MCS
  setup reaches the backend before password validation; ordinary desktop
  traffic does not. Do not claim all backend parsing is post-authentication.

**Validation performed.**

The following existing tests passed:

```bash
GOMAXPROCS=4 go test -race -short -count=1 -p 2 -timeout 5m \
  ./internal/guestmem ./internal/pci ./internal/iodev ./internal/machine \
  ./internal/virtio ./internal/disk ./internal/usernet ./internal/vmm \
  ./internal/rdp/...

GOMAXPROCS=4 go test -race -short -count=1 -timeout=3m ./internal/gateway \
  -run 'Test(RDP|AuthenticatedClientInfo|ClientInfo|RoutingToken|PasswordGate|AuthenticationPacketReader|PrepareVMLaunchCgroup|ValidateVMCgroup|VMLaunchCgroup|LinuxCgroup|ManagerMemory|ManagerConcurrentMemory|ManagerAccountsForUncommittedMemory|ManagerFailedLaunch|MemoryBudget)'

GOMAXPROCS=4 go test -tags=integration -count=1 -timeout=2m \
  ./internal/machine ./internal/vmm \
  -run 'Test(CloseStopsRunningVCPUs|CloseReleasesResources|RunImmediateExitReturnsEINTR|NewGuestMemoryStartsZeroed|HeadlessBootReturnsCPUFailure|SetupFailureClosesMachineAndConsole)$' -v
```

The six explicitly selected KVM integration tests executed and passed. Some
other existing tests skip under nonroot or short-mode conditions; the broader
suite pass should not be interpreted as full integration coverage.

Two existing coverage-guided fuzz targets ran for 15 seconds each with two
workers: `FuzzProtocolParsers` completed 284,721 executions and
`FuzzClientInfoCredentials` completed 92,359 executions, with no failure.

Supplemental experiments used Go overlays without editing production source:
100,000 deterministic hostile virtqueue cases and 100,000 deterministic GPU
command mutations/truncations (half using a stub 3D backend) completed without
failure. These are bounded smoke tests, not coverage-guided device fuzzing.
The GPU experiment checked resource accounting after every command.
Harnesses are archived beside the DNS reproduction in
[security-review-2026-10-21/](security-review-2026-10-21/).

To run the archived experiments from the repository root, create an overlay
in a temporary directory, then invoke the named tests:

```bash
review_tmp=$(mktemp -d /tmp/devbox-review-repro.XXXXXX)
python3 - "$review_tmp" <<'PY'
import json, pathlib, sys
root = pathlib.Path.cwd()
archive = root / "docs/security-review-2026-10-21"
mapping = {
    root / "internal/usernet/security_review_dns_limits_test.go": archive / "dns_limits_test.go.txt",
    root / "internal/virtio/security_review_descriptor_bounds_test.go": archive / "descriptor_bounds_test.go.txt",
    root / "internal/virtio/security_review_gpu_sequences_test.go": archive / "gpu_sequences_test.go.txt",
}
path = pathlib.Path(sys.argv[1]) / "overlay.json"
path.write_text(json.dumps({"Replace": {str(k): str(v) for k, v in mapping.items()}}))
PY
GOMAXPROCS=2 go test -overlay="$review_tmp/overlay.json" ./internal/usernet \
  -run '^TestSecurityReviewDNSBypassesConnectionBudget$' -count=1 -timeout=30s -v
GOMAXPROCS=2 go test -overlay="$review_tmp/overlay.json" ./internal/virtio \
  -run '^TestReviewGuestDescriptorBounds$' -count=1 -timeout=60s -v
GOMAXPROCS=2 go test -race -overlay="$review_tmp/overlay.json" ./internal/virtio \
  -run '^TestSecurityReviewGPUSequences$' -count=1 -timeout=90s -v
```

**Remaining assurance gaps.**

Optional virgl rendering passes guest command contents into in-process native
code (`internal/virgl/native_linux.go:346`). This environment lacks usable
`libvirglrenderer` support, so the native renderer, host graphics drivers,
sanitizer runs and real 3D integration were not validated. A stub test cannot
establish native memory safety. Keep this separate from the pure-Go results.

No persistent device/QCOW2 fuzz targets or sustained fuzz jobs were found in
the inspected packages/CI configuration. Prioritize stateful descriptor and
reset fuzzing, simultaneous malicious guest-memory mutation, malformed QCOW2
corpora, and native transfer/fence/backing lifetime testing. The Go race
detector does not observe KVM guest writes as ordinary instrumented Go writes.

The Linux kernel/KVM implementation, full third-party networking and codec
code, hardware side channels, and a full gateway/web audit were outside this
review. Production source and configuration were not changed; this report and
archived experiments are the only repository additions.
