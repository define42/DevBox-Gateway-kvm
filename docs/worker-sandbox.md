# Managed worker confinement

Managed VM starts now require an unprivileged service account, Landlock ABI 6
or newer (Linux 6.12+ with Landlock enabled), seccomp filter support, and the
existing cgroup delegation with both memory and pids controllers. The supplied
Compose deployment drops all capabilities and sets no-new-privileges. There is
no automatic fallback to an unconfined managed worker. The standalone `boot`
command remains an operator-controlled development entrypoint.

The gateway invokes an internal `worker` stage with a minimal environment.
Only locale/timezone and NVIDIA device-selection settings are inherited; gateway
authentication settings, arbitrary VM overrides, and dynamic-loader injection
variables are excluded. Launch arguments supply VM configuration.

Before guest processing, the launcher creates a Landlock ruleset granting:

- Read/write access to this VM's existing disk and UEFI variables.
- Read-only access to its firmware, seed, RDP certificate and key.
- Access to KVM and basic runtime devices; GPU devices when guest 3D or the
  `auto`/`nvenc` H.264 encoder is selected. `GPU3D=false` alone does not disable
  hardware video encoding; use the `software` encoder to exclude GPU access.
- Read-only runtime libraries and a small set of public resolver/driver files.
- Socket creation/removal only in its dedicated profiling directory.

The ruleset grants no access to the gateway state directory, other VM files,
service secrets, or the writable cgroup-management tree. Signal and abstract Unix
socket scopes prevent reaching processes outside this worker's Landlock domain.
The implementation locks its OS thread, applies the restrictions, and re-executes
the binary on that thread. Every new Go and native thread therefore inherits the
same domain; applying Landlock to one thread of an already-running Go program
would be insufficient. See the [kernel Landlock documentation](https://docs.kernel.org/userspace-api/landlock.html).

Telemetry uses a connected Unix sequenced-packet socketpair, which preserves
message boundaries and cannot be redirected like a datagram socket.

After inherited control sockets and optional profiling are initialized, a
process-wide seccomp filter is synchronized across all threads before VM setup.
It blocks additional program execution, process forks, namespace changes,
ptrace/process-memory access, descriptor stealing, io_uring, System V IPC,
filesystem metadata changes and creation of new Unix/raw sockets. Ordinary
TCP/UDP sockets and route-netlink queries remain available to userspace
networking. Thread creation remains allowed. Core dumps are disabled, the hard
file-descriptor limit is at most 4096, and the cgroup limits each worker to 512
processes/threads. Existing memory limits remain mandatory.

This is additional containment, not a claim that a compromised worker has no
host attack surface. It retains access to KVM, assigned device ioctls, permitted
libraries, its own VM state and outbound TCP/UDP sockets. The destination policy
is enforced by the userspace forwarder; arbitrary code execution in the worker
could bypass that policy. Deploy host firewall rules if egress restrictions must
survive a worker compromise. Native GPU drivers and the Linux kernel remain
trusted. Landlock does not hide all filesystem metadata like a mount namespace.
There is no new per-worker CPU quota; the memory, descriptor and task limits do
not prevent a worker from consuming its available CPU time.

## Validation

Run the disposable compromised-worker test on a host with Landlock enabled:

```sh
go test -race -tags=integration -count=1 -timeout=45s ./internal/workersandbox
```

It checks access to the assigned resource while attempting reads/writes of
sibling VM data, gateway secrets and a cgroup-control fixture, signaling the
parent, executing a program, opening Unix/raw sockets and accessing another
process. It also verifies restrictions from multiple OS threads. Tests skip
when kernel prerequisites are absent; deployment failure remains closed.

On 2026-10-21, the race-enabled tests passed on Linux 7.0 with Landlock ABI 8.
The KVM integration test also initialized, ran and closed two successive UEFI
VM instances using the same confined process, including userspace networking
and profiling resources. This exercises lifecycle compatibility, not a full
guest OS workload. Native GPU initialization under the new restrictions has
not been validated on this host.

The networking measurements and sustained traffic experiments are recorded
separately in the [network-hardening report](network-hardening-2026-10-21.md). Run the ordinary gateway lifecycle
and telemetry suites after changing any inherited descriptor format.
