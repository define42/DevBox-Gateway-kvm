# Gateway configuration

`devbox-kvm serve` reads all gateway settings from environment variables through
[`internal/config/config.go`](../internal/config/config.go), following the
[DevBox-Gateway configuration design](https://github.com/define42/DevBox-Gateway/blob/main/internal/config/config.go).
Set exported variables in your service environment or pass them before the command:

```bash
LDAP_URL=ldaps://directory.example.com \
LDAP_BASE_DN=dc=example,dc=com \
LDAP_USER_DOMAIN=example.com \
ADMIN_GROUP=devbox-admins \
./devbox-kvm serve
```

Exactly one of `LDAP_URL` and `AUTH_FILE` must be set. Authentication, TLS,
paths, and supported value ranges are validated during startup. See the
[gateway guide](gateway.md) for LDAP, certificates, VM admission, and storage.
This example starts the management UI; starting desktops additionally requires
the delegated `VM_CGROUP_ROOT` described below.

Precedence is **explicit command-line flag → environment variable → default**.
Existing flags remain supported, including explicit false and empty overrides.
Surrounding whitespace is trimmed from string environment values; an explicitly
empty value remains empty. Malformed integer, boolean, or duration environment
values fall back to their registered defaults. Parsed values still undergo the
gateway's normal startup validation and default handling. For example, zero
selects component defaults for CPU count, memory, quotas, session limits, and
several timeouts. `VM_DISK_MAX_SIZE` is stricter: a blank, zero, negative,
fractional, malformed, or overflowing value fails startup. It is parsed after
flags, so a valid explicit flag can override a malformed environment value.
Durations use Go syntax such as `30s`, `10m`, and `8h`.
Settings are read at startup; restart the gateway after changing them.

The native executable does **not** automatically load a `.env` file. Docker
Compose loads `.env` and passes the listed settings into the container. For
`docker run`, supply `--env NAME=value` or `--env-file /path/to/gateway.env`.
An env file for `docker run` should contain literal `NAME=value` assignments,
without shell expansion or quote characters around values.

## Storage and desktops

Defaults below are for the native executable. The Docker image changes
`DATA_ROOT_DIR` to `/var/lib/devbox-kvm` and `LISTEN_ADDR` to `0.0.0.0:8443`.
Docker Compose bind-mounts the complete gateway state tree there from the host
path selected by `GATEWAY_STORAGE_PATH`, which defaults to
`/srv/devbox-storage`.

| Environment variable | Default | Flag override | Purpose |
| --- | --- | --- | --- |
| `DATA_ROOT_DIR` | `.devbox-kvm-gateway` | `-state` | Private persistent state directory. |
| `BASE_IMAGE` | Empty | `-image` | Optional immutable raw or supported uncompressed QCOW2 default template. Omit to use administrator uploads. |
| `VM_BINARY` | Empty | `-vm-binary` | Executable for managed VMs; empty uses the current executable. |
| `VM_GPU_3D` | `false` | `-gpu-3d` | Enable guest OpenGL via virglrenderer; requires a `virgl` build and host EGL. `false` retains the 2D device without initializing a renderer. Applies when each VM starts. |
| `VM_NETWORK_POLICY` | `internet` | `-vm-network-policy` | Managed IPv4 TCP/UDP access: `internet` permits public destinations, while `allowlist` permits only explicit CIDRs. Visible host interface subnets, private and special ranges require an exception. DHCP/DNS remain available; see [networking](runtime.md#networking) for routed/NAT LAN limits. |
| `VM_NETWORK_ALLOW_CIDRS` | Empty | `-vm-network-allow-cidrs` | Up to 64 comma-separated canonical IPv4 CIDRs allowed in addition to the policy. No `/0` or IPv6. Applies to newly started workers; host alias access requires `127.0.0.1/32`. See [networking](runtime.md#networking). |
| `UEFI_CODE` | `/usr/share/OVMF/OVMF_CODE_4M.fd` | `-uefi` | OVMF CODE firmware. |
| `UEFI_VARS_TEMPLATE` | `/usr/share/OVMF/OVMF_VARS_4M.fd` | `-uefi-vars-template` | Matching pristine variables template, copied per VM. |
| `H264_ENCODER` | `auto` | `-encoder` | `auto`, `nvenc`, or `software`. |
| `H264_QP` | `20` | `-h264-qp` | H.264 quantization parameter, minimum `0`, maximum `51` (inclusive). Lower values improve quality and increase traffic; higher values compress more and reduce quality. Applies when a managed VM starts. |
| `RDP_PROFILE` | `office` | `-rdp-profile` | AVC420 cap: `office` (30 FPS) or `smooth` (60 FPS); bitmap fallback stays at 30 FPS. Applies when a managed VM starts. |
| `GATEWAY_VM_PROFILING` | `false` | `-vm-profiling` | Enable private Unix sockets for bounded administrator CPU profiles. Compose defaults to `true`; no public profiling port is opened. |
| `VM_KEYBOARD` | `us` | `-keyboard` | Keyboard layout provisioned in new guests. |
| `VM_VCPU_COUNT` | `2` | `-cpus` | vCPUs per desktop, from 1 to 64. |
| `VM_MEMORY_MIB` | `4096` | `-memory` | MiB per desktop, from 256 to 1048576. |
| `VM_MEMORY_BUDGET_MIB` | `0` | `-memory-budget` | Maximum MiB reserved for running desktops, including per-worker overhead; 0 selects automatic capacity. An explicit value can lower that budget. |
| `VM_MEMORY_OVERHEAD_MIB` | `512` | `-memory-overhead` | Host-memory allowance above guest RAM in each worker's reservation and hard cgroup limit, from 64 to 16384 MiB. |
| `VM_CGROUP_ROOT` | Empty | `-vm-cgroup-root` | Clean absolute path to the private delegated cgroup v2 subtree used for VM workers. Empty permits management but refuses every VM start. |
| `VM_DISK_MAX_SIZE` | `500GB` | `-vm-disk-max-size` | Maximum virtual capacity of each template and managed VM disk. |
| `MAX_VDI_PER_USER` | `3` | `-max-vms-per-user` | Desktop quota per account. |
| `MAX_CONCURRENT_CREATIONS` | `2` | `-max-creations` | Gateway-wide simultaneous desktop provisioning limit, from `1` to `64`. Further requests wait within `VM_CREATE_TIMEOUT`. |
| `MAX_RUNNING_VMS` | `4` | `-max-running` | Global running desktop limit. |
| `IMAGE_UPLOAD_MAX_BYTES` | `21474836480` | `-image-upload-limit` | Maximum bytes per uploaded or normalized catalog file (20 GiB), up to 1 TiB. |
| `IMAGE_UPLOAD_TIMEOUT` | `30m` | `-image-upload-timeout` | Overall upload/import timeout, from `1m` to `24h`. |
| `VM_CREATE_TIMEOUT` | `30m` | `-vm-create-timeout` | Overall desktop provisioning timeout including queue wait, from `1m` to `24h`. |
| `VM_START_TIMEOUT` | `30s` | `-vm-start-timeout` | Time allowed for a managed VM to start its RDP console. |
| `VM_STOP_TIMEOUT` | `30s` | `-vm-stop-timeout` | Guest ACPI shutdown wait; a dashboard stop timeout leaves the guest running. Gateway exit also has separate forced-termination and host-cleanup waits. |

Set `VM_GPU_3D=false` to disable guest 3D. After changing it, restart the gateway
and start new VM workers; reconnecting RDP or rebooting within a running guest
does not apply a gateway configuration change. See [guest graphics](runtime.md#guest-graphics)
for builds, host/guest requirements, and renderer verification.

`H264_QP` is shared by software and NVENC encoding, including automatic fallback.
Zero is valid and selects the highest supported quality; AVC420 still uses chroma
subsampling, so it does not promise lossless RGB. QP controls compression, not a
guaranteed Mbps limit. Set `H264_QP=28` in Compose's `.env`, or run
`H264_QP=28 make nvidia`. Recreate/restart the gateway and start new VM processes
to apply the change; reconnecting RDP does not apply it.

Managed desktops enable virtio-snd playback over RDP automatically; there is no
separate gateway audio setting. Guests need the `snd_virtio` driver and an audio
stack, and clients must enable sound. See [audio requirements](runtime.md#audio).

`GATEWAY_VM_PROFILING` takes effect for newly started workers after a gateway
restart. Set it to `false` to disable the private profiling sockets in Compose.
See [administrator CPU profiling](performance.md#optional-administrator-cpu-profiling)
for access requirements and capture limits.

During gateway exit, VMs receive their ACPI shutdown request concurrently. After
`VM_STOP_TIMEOUT`, each remaining worker receives SIGTERM and then SIGKILL if
needed, with up to two seconds after each signal (or `VM_STOP_TIMEOUT` when it
is shorter). Final host cleanup gets a separate, fixed two-second wait, including
pending metadata writes. A timeout reports an error while the state lock remains
held until cleanup finishes. Keep the service manager's stop timeout longer than
these waits; the supplied Compose configuration allows 45 seconds with the
default 30-second guest timeout.

Every managed VM start requires `VM_CGROUP_ROOT`. Its directory must be a
privately controlled, writable cgroup v2 domain subtree, reached without
symlinks and owned by the gateway's effective UID, with no resident processes.
Both `memory` and `pids` controllers must appear in `cgroup.controllers` and
`cgroup.subtree_control`; at gateway startup,
the root's recursive `cgroup.events` state must report `populated 0`. Children
created there must provide writable `cgroup.kill`. These cgroup features require
Linux 5.14 or newer with memory and pids support and clone-time cgroup attachment
available to the process. Managed workers additionally require an unprivileged
service account, Linux 6.12+ with Landlock ABI 6 enabled, and seccomp; see
[worker confinement](worker-sandbox.md).

For every launch, the gateway creates a child cgroup, sets and verifies
`memory.max` to guest RAM plus `VM_MEMORY_OVERHEAD_MIB`, disables swap with
`memory.swap.max=0`, enables group OOM handling with `memory.oom.group=1`, caps
host tasks (including threads) with `pids.max=512`, and attaches the worker at
clone time. The task ceiling is fixed and applies to the worker and descendants.
Cleanup checks the child's recursive
`populated` state, uses `cgroup.kill` when descendants remain, and releases the
reservation only after verifying `populated 0` and removing the child. A
cleanup failure blocks all later VM starts in that gateway process until the
operator recovers the subtree and restarts it. An empty
`VM_CGROUP_ROOT` still permits gateway and stopped-desktop management but starts
no VM workers; an invalid configured root fails gateway startup. A kernel or
runtime policy may still deny the clone-time `clone3` attachment after these
checks pass, in which case that VM start is refused.

`VM_DISK_MAX_SIZE` accepts a positive integer in bytes, optionally followed by
`B`, `KB`, `MB`, `GB`, `TB`, `KiB`, `MiB`, `GiB`, or `TiB` (case-insensitive).
Decimal units use powers of 1000, so the default `500GB` is exactly
500,000,000,000 bytes. Binary units use powers of 1024, so `500GiB` is
536,870,912,000 bytes. The setting is a per-disk virtual-capacity guard, not an
allocated-block limit or aggregate physical-storage quota. It does not
preallocate, grow, or shrink an image. See
[shared base images and administration](gateway.md#shared-base-images-and-administration)
for import and existing-VM behavior. H04 remains an operator-managed residual
shared-storage-exhaustion risk; see
[storage capacity responsibility](storage-isolation.md).

## Listener and TLS

RDP access is provided only by the authenticated gateway. Users sign in to the
UI and open its downloaded RDP file; standalone `boot -rdp <address>` listeners
are not supported. Managed VM workers inherit private Unix listeners from the
gateway and verify the gateway process identity. There is no per-VM RDP bind
address or unsafe override to configure.

| Environment variable | Default | Flag override | Purpose |
| --- | --- | --- | --- |
| `LISTEN_ADDR` | `127.0.0.1:8443` | `-listen` | Shared HTTPS/RDP TCP listener. |
| `PUBLIC_ADDRESS` | `localhost:8443` | `-public-address` | Hostname and port used by the gateway, its certificate, and downloaded RDP files. |
| `CERT_FILE` | Empty | `-tls-cert` | Optional PEM certificate; supply together with `KEY_FILE`. |
| `KEY_FILE` | Empty | `-tls-key` | Matching private PEM key. |
| `ACME_ENABLE` | `false` | `-acme` | Obtain and renew a public certificate with TLS-ALPN-01 on public TCP 443. Excludes supplied PEM files. |
| `ACME_EMAIL` | Empty | `-acme-email` | ACME account contact email. |
| `ACME_CA` | `production` | `-acme-ca` | `production`/`prod`, `staging`, or custom ACME directory URL. |

Without supplied PEM files or ACME, the gateway generates a persistent
self-signed certificate. Enabling ACME accepts the selected CA's subscriber
agreement. See [certificates and ACME](gateway.md#gateway-certificates-and-acme).

## Authentication and sessions

| Environment variable | Default | Flag override | Purpose |
| --- | --- | --- | --- |
| `AUTH_FILE` | Empty | `-auth-file` | Private bcrypt JSON file for loopback development. Excludes LDAP and grants no admin role. |
| `LDAP_URL` | Empty | `-ldap-url` | Directory URL, `ldaps://` or `ldap://` (StartTLS). |
| `LDAP_BASE_DN` | Empty | `-ldap-base-dn` | Required LDAP search base DN. |
| `LDAP_USER_DOMAIN` | Empty | `-ldap-domain` | Domain appended to the submitted username when binding, without a leading `@`. |
| `LDAP_USER_FILTER` | Empty | `-ldap-filter` | Search filter. Empty selects `(uid=%s)`, or `(mail=%s)` when a domain is configured. |
| `LDAP_USERNAME_ATTRIBUTE` | `uid` | `-ldap-username-attribute` | Canonical VM-owner account attribute. |
| `LDAP_REQUIRED_GROUP` | Empty | `-ldap-required-group` | Optional direct group membership required for login. |
| `ADMIN_GROUP` | Empty | `-ldap-admin-group` | Optional direct group membership granting the administrator role. |
| `LDAP_CA_FILE` | Empty | `-ldap-ca` | Private CA PEM file; empty uses system trust. |
| `LDAP_INSECURE_LOOPBACK` | `false` | `-ldap-insecure-loopback` | Permit plaintext LDAP only with a loopback IP, for testing. |
| `LDAP_AUTH_TIMEOUT` | `10s` | `-ldap-timeout` | LDAP authentication timeout. |
| `SESSION_TTL` | `8h` | `-session-ttl` | Browser session lifetime, from `1m` to `24h`. |
| `MAX_SESSIONS` | `1024` | `-max-sessions` | Maximum browser sessions, from 1 to 10000. |

## Connection admission

| Environment variable | Default | Flag override | Purpose |
| --- | --- | --- | --- |
| `MAX_CONCURRENT_CONNECTIONS` | `128` | `-max-connections` | Maximum authenticated HTTPS/RDP connections, from 1 to 4096; excludes the pending pool. |
| `MAX_HANDSHAKES` | `64` | `-max-handshakes` | Maximum pending, unauthenticated connections, from 1 to 4096. |
| `MAX_CONNECTIONS_PER_IP` | `32` | `-max-connections-per-ip` | Maximum pending and authenticated connections combined per source, from 1 to 4096. |
| `MAX_HANDSHAKES_PER_IP` | `8` | `-max-handshakes-per-ip` | Maximum pending connections per source, from 1 to 4096. |
| `CONNECTION_RATE_LIMIT` | `128` | `-connection-rate-limit` | Global TCP admission rate per second, from 1 to 100000, with an equal-size burst. |
| `CONNECTION_RATE_LIMIT_PER_IP` | `8` | `-connection-rate-limit-per-ip` | TCP admission rate per source per second, from 1 to 100000, with an equal-size burst. |
| `HANDSHAKE_TIMEOUT` | `10s` | `-handshake-timeout` | Total time from TCP acceptance to application authentication, from `1s` to `1m`. |

Pending connections have separate capacity from authenticated connections. With
the defaults, the global ceiling is 64 pending plus 128 authenticated connections.
Completing TLS alone does not authenticate a connection: HTTPS must present a
valid browser session or complete login, and RDP must validate its credentials.
The pending timeout covers protocol negotiation, TLS, and application
authentication together; sending more bytes does not extend it. Zero selects the
default for these settings; it does not disable a limit.

Source limits use the actual TCP peer, not forwarded HTTP headers. IPv4-mapped
IPv6 addresses share their IPv4 bucket; IPv6 addresses share a bucket per /64.
Tune per-source limits for offices behind one NAT or IPv6 prefix, allowing for
browser parallel connections as well as RDP. The separate password-attempt
limits still apply. See [connection admission and edge protection](gateway.md#connection-admission-and-edge-protection)
for deployment requirements.

## Compose and existing deployments

The root [`compose.yaml`](../compose.yaml) passes every setting above to the
gateway and starts it with `serve`. [`.env.example`](../.env.example) includes
the deployment defaults and configures the bundled development LDAP directory.
Its LDAP address, CA path, account attributes, and group settings differ from the native
executable's empty-directory defaults. File paths in Compose are container paths;
add corresponding mounts when supplying certificates or an external base image.
Compose also enables `GATEWAY_VM_PROFILING` by default. It passes
`VM_CGROUP_ROOT` into the container but does not delegate or mount a subtree;
see the [required container cgroup setup](docker.md#required-cgroup-v2-delegation-for-vm-starts).

`GATEWAY_BIND`, `GATEWAY_HOST`, `GATEWAY_PORT`, `KVM_GID`, and
`GATEWAY_STORAGE_PATH` are Compose host-deployment controls.
`GATEWAY_STORAGE_PATH` defaults to `/srv/devbox-storage` and bind-mounts that
host directory as the container's entire `/var/lib/devbox-kvm` state tree, including
VM disks, catalog and import data, certificates, ACME state, and metadata. It is
not read by the native executable and does not change the native
`DATA_ROOT_DIR` default. Keep `DATA_ROOT_DIR=/var/lib/devbox-kvm` with the shipped
Compose mount; an override requires updating the mount target as well.

Compose does not configure or verify aggregate block or inode capacity,
filesystem quotas, or a protected free-space reserve. The administrator must
prepare the host path and owns capacity planning, optional quotas and reserves,
monitoring, recovery, and backups; no particular filesystem type is required.
See [storage capacity responsibility](storage-isolation.md).

`PUBLIC_ADDRESS` defaults to
`GATEWAY_HOST:GATEWAY_PORT` when it is unset. Keep the internal `LISTEN_ADDR`
port aligned with the Compose port mapping and the state directory aligned with
its bind mount.

Existing Compose `.env` files continue to work. These legacy Compose variables
are used only when the corresponding canonical variable is unset:

| Legacy Compose variable | Canonical environment variable |
| --- | --- |
| `GATEWAY_ENCODER` | `H264_ENCODER` |
| `GATEWAY_CPUS` | `VM_VCPU_COUNT` |
| `GATEWAY_MEMORY` | `VM_MEMORY_MIB` |
| `GATEWAY_MAX_RUNNING` | `MAX_RUNNING_VMS` |
| `GATEWAY_ACME` | `ACME_ENABLE` |
| `GATEWAY_ACME_EMAIL` | `ACME_EMAIL` |
| `GATEWAY_ACME_CA` | `ACME_CA` |

Canonical variables take precedence even when explicitly empty. Rename legacy
entries when migrating; avoid retaining conflicting values. The executable
itself reads the canonical names only. The native `make gateway` workflow also
retains its existing Make helpers and aliases; see the
[development setup](gateway.md#local-development-setup).
