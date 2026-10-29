# Desktop gateway operations

The gateway serves the web dashboard, authenticates users, provisions persistent
Linux desktops, and routes downloaded RDP files to the correct VM. Each desktop
runs in a separate devbox-kvm process using KVM, UEFI firmware, and devbox-kvm's own RDP
console. The RDP connection shows the guest's shared screen, including firmware
and its graphical login. It does not create a separate guest XRDP session.

For installation, use the [native DEB/RPM packages](packages.md). The packaged
service reads `/etc/devbox-kvm/devbox-kvm.env`, stores state in
`/var/lib/devbox-kvm`, and supplies its own private cgroup delegation. Configure
your LDAP directory before enabling the service.

The local development commands below run from the repository root. See the
[README](../README.md) for host requirements and the project overview.

## Local development setup

The example uses embedded OVMF firmware, Docker with the Compose plugin,
and OpenSSL. Image imports use the built-in Go QCOW2 normalizer.
No desktop image is required at startup. The `make gateway` target builds with
the bundled pure Go H.264 encoder and requires no C compiler or native codec SDK.

Prepare the private cgroup v2 subtree described under
[`serve` options](#serve-options), then pass its path when launching the gateway
as a non-root user:

```bash
export VM_CGROUP_ROOT=/path/to/delegated-subtree
make gateway
```

This helper does not create or delegate the subtree. An unset or invalid
`VM_CGROUP_ROOT` prevents gateway startup, including the management dashboard.

Open **https://localhost:8443** and accept the locally generated self-signed
certificate for this development instance. The bundled directory provides these
public demo credentials:

| Account | Password | Access |
| --- | --- | --- |
| `johndoe` | `dogood` | User |
| `testuser` | `dogood` | User |
| `administrator` | `dogood` | Administrator |

The bundled GLAuth service listens on `127.0.0.1:1636` using verified LDAPS.
Stopping the gateway leaves this directory running. Use `make ldap` to start it
separately and `make ldap-stop` to stop it. See the
[LDAP setup](../testldap/README.md) for every demo account, certificate handling,
external-directory variables, and authentication tests.

A new gateway starts with an empty image catalog in `.devbox-kvm-gateway/images`.
Sign in as `administrator`, open **Admin → Base Images**, and upload a compatible
QCOW2 desktop image. Users can then select it when creating a desktop. Templates
and desktop disks persist across restarts. Compressed QCOW2 uploads are
normalized without an external conversion tool.

| Make variable | Default | Purpose |
| --- | --- | --- |
| `QCOW2_IMAGE` | `./rocky9-xfce-v2.4.16.qcow2` | Source used only when an explicit `GATEWAY_IMAGE` needs creating. |
| `GATEWAY_STATE` | `./.devbox-kvm-gateway` | Private persistent state. |
| `GATEWAY_IMAGE` | Empty | Optional protected default image; if set and absent, converted from `QCOW2_IMAGE`. |
| `GATEWAY_ARGS` | Empty | Additional `serve` flags. |
| `GATEWAY_LDAP_LOCAL` | `1` | Set to `0` to skip the bundled directory. |
| `LDAP_PORT` | `1636` | Bundled directory's host port. |

The executable reads [environment configuration](configuration.md). These Make
variables remain helper aliases for the local setup; explicit `serve` flags
override environment values. The gateway always uses its embedded firmware.
To opt into a preconfigured protected default image, for example:

```bash
make gateway GATEWAY_IMAGE=./.devbox-kvm-gateway/base.qcow2 QCOW2_IMAGE=/path/to/desktop.qcow2
```

This prepares the destination once if absent, then reuses it. The original
source and any existing developer working disks are preserved. This optional
Make helper still uses `qemu-img` from `qemu-utils` to prepare the default image.
Without `GATEWAY_IMAGE`, the gateway uses only its uploaded catalog and needs no
external conversion tool.

### Run the gateway directly

To build the application and start LDAP separately:

```bash
make devbox-kvm
make ldap
VM_CGROUP_ROOT=/path/to/delegated-subtree \
LDAP_URL=ldaps://127.0.0.1:1636 LDAP_BASE_DN=dc=glauth,dc=com \
LDAP_USER_DOMAIN=example.com LDAP_USER_FILTER='(mail=%s)' \
LDAP_USERNAME_ATTRIBUTE=mail \
LDAP_REQUIRED_GROUP=ou=team10_r,ou=groups,dc=glauth,dc=com \
ADMIN_GROUP=devbox-admins LDAP_CA_FILE=./testldap/.tls/cert.pem \
./devbox-kvm serve
```

Open the same **https://localhost:8443** page and use the demo accounts above.
Sign in as `administrator` to upload the first template if the catalog is empty.
The gateway extracts its embedded OVMF pair under `DATA_ROOT_DIR/firmware`. No
host OVMF installation or firmware configuration is required.
`CGO_ENABLED=0 go build -o devbox-kvm .` is also supported; catalog
imports, including compressed QCOW2 images, work in that build too.
As with `make gateway`, prepare the [required VM cgroup delegation](#serve-options)
before launching `serve`. Startup checks verify kernel confinement, KVM access,
and cgroup delegation before initializing gateway TLS or desktop state.

## Create and connect to a desktop

Once an administrator has uploaded at least one template and the gateway has a
valid `VM_CGROUP_ROOT`:

1. Sign in, select **Create DevBox**, enter a name, and choose a **Base Image**.
   Names use lowercase letters, digits, and interior hyphens, up to 63 characters.
   Names must be unique within your account.
2. Wait for creation to finish. The dialog reports disk-copy progress, then
   configuration and finalization. A filesystem clone can complete immediately;
   reaching 100% copy progress does not by itself mean finalization has finished.
3. Select **Start**. A `running` state means the private RDP console responds;
   firmware, cloud-init, and the desktop may still be booting.
4. Select **RDP** and open the downloaded file within two minutes, while keeping
   your browser session signed in. Enter your current gateway password at the RDP
   client's credential prompt. For a locally built FreeRDP client:

   ```bash
   ./tools/freerdp/bin/xfreerdp /path/to/desktop.rdp
   ```

5. At the guest's graphical login, use the account username and password used to
   provision this desktop.

Creation leaves the VM stopped. The dashboard refreshes status automatically.
If the progress connection is interrupted, check the dashboard before retrying;
creation may have finished before the browser lost its response.

Each VM receives an independent clone of the selected image, a private firmware
variables file, a NoCloud seed, and a private TLS identity. Existing VMs keep
their owners and provisioned guest passwords. Renaming or changing a directory
account does not migrate VM ownership or update an existing guest's password.

Managed UEFI desktops display through virtio-gpu. Keyboard input uses PS/2;
pointer input uses PS/2 during firmware boot and switches to an absolute
virtio-input tablet when the guest driver is ready. Include the virtio-input
driver for absolute pointer input; PS/2 remains the fallback until it is ready.

### View VM performance

Select **Performance** beside one of your desktops. This read-only panel updates
every five seconds while open and visible, including when the desktop is idle.
It uses measurements from the host-side VM process; no guest agent, QCOW2 image
change, or RDP client extension is needed. The panel is in the web dashboard,
not overlaid on the native RDP session.

| Measurement | Meaning |
| --- | --- |
| VM process CPU | CPU time used by the VM process, including vCPU and encoding work. 100% means one host CPU core; values can exceed 100%. |
| Host resident memory | RAM resident in the VM process, not the guest's used/free RAM. Configured guest memory is shown separately. |
| RDP download / upload | VM → client and client → VM traffic over the measured sampling interval, in decimal Mbps (1 Mbps = 1,000,000 bits/s). |
| Data sent / received | Cumulative VM → client and client → VM bytes since this VM process started, displayed as B, KiB, MiB, GiB, or TiB. |
| Encoder | The active encoding backend: pure Go software H.264 or bitmap fallback. No active connection means no active encoder. |
| Frames sent / sent FPS | Successful server frame sends since this VM process started, and the recent send rate. These are not frames displayed by the client; an idle screen can correctly show zero FPS. |
| Encoding | Average H.264 codec time during the latest sample interval; excludes capture and color conversion. Unavailable for bitmap-only intervals. |
| Transport write | Average time writing graphics data to the local RDP transport during the latest sample interval. Can include backpressure; it is not end-to-end network latency. |
| ACK turnaround | The latest graphics acknowledgment turnaround, including network and client processing. It is not input latency or a pure network RTT. |

RDP traffic includes negotiation, TLS, and all RDP channels measured at the worker
endpoint. It excludes TCP/IP overhead and guest internet traffic, so it is not an
exact measurement of external network usage. Totals combine all connections and
survive reconnects; restarting the VM resets them. No persistent traffic history
is stored. Initial bandwidth rates remain unmeasured until two valid samples are
available; older workers show the traffic measurements as unavailable.

Missing measurements are shown as unavailable, not as zero. Samples become stale
after 15 seconds without an update. Restarting a desktop resets its process
counters. Telemetry is kept only in memory and is collected through a private,
bounded channel; a telemetry failure does not stop the VM or block RDP output.

The authenticated `GET /vms/{id}/performance` endpoint checks VM ownership and
disables caching. Users, including administrators, can see performance only for
their own desktops. It does not expose host-wide usage or other users' sessions.
After upgrading, rebuild/restart the gateway and start the desktops with the new
binary to enable collection. No additional Docker ports or guest packages are
required.

The authenticated `GET /perf` endpoint exports an owner-scoped JSON report for
diagnosis, with the active encoder, configured vCPUs, a fixed pacing
label, CPU/RSS, frame and payload rates, RDP bandwidth and transfer totals, and
encoding/write/ACK timings. Each current worker can also expose its four latest
terminal RDP session records as `VMPerformance.rdpSessions`. A record contains a
worker-local `sequence`, UTC `timestamp`, last `stage`, `outcome`, sanitized
`error`, and `durationMs`, plus an optional fixed `reason` for dynamic-channel
capability rejection checks. Reasons contain no submitted values or raw error
text. A `frame_write` stage may be a transport closure during transmission rather
than an encoding failure. Each record remains in memory until one of the next sessions
evicts it or that worker restarts. Worker sequence values are not gateway RDP
attempt IDs. Use `/perf?download=1` to save the report, or
`/perf?vm=<id>&download=1` for one owned desktop. See
[performance diagnostics](performance.md) for collection and interpretation.

Its additive `pipeline` object reports virtio transfer/flush work, framebuffer
capture and colour conversion, outstanding graphics ACKs, and ACK-blocked time.
Optional `GATEWAY_VM_PROFILING=true` adds private Unix-socket CPU profiling for
host operators, not a public debug route or a capability granted by web login.
See [profiling setup and limitations](performance.md#optional-administrator-cpu-profiling).

### RDP identity and routing

The downloaded file contains your LDAP login name in `username` and a random
routing token in `loadbalanceinfo`. The RDP client must submit your current gateway
password. The gateway validates it with the configured LDAP authenticator (or the
development authentication file) on every connection, including required-group
membership, and requires the resulting account to own the selected VM. A client
`Domain` hint does not override the gateway's configured directory. Login aliases
remain usable when the LDAP login differs from the canonical VM owner name.

The routing token is sent in the initial RDP negotiation before TLS, so it is
visible on the wire. It only selects the desktop and browser session; possession
of it is insufficient to authenticate. Username and password are sent inside TLS.
Trust the gateway's certificate in the RDP client. The file contains no saved
password, and the gateway removes the password before forwarding Client Info to
the VM console. This gateway check is separate from the guest's graphical login;
existing guest passwords do not change when an LDAP password changes.

A grant is single use, expires after two minutes or when its browser session
expires, and is bound to the session, client IP, VM, and owner. Downloading a new
file for the same VM in that session invalidates the previous unused grant.
Successful password validation consumes the grant; rejected credentials do not.
Download again after a successful connection closes or the grant expires. Browser
and RDP connections must reach the gateway from the same source IP. Web and RDP
password checks share the gateway's limit of 10 attempts per source IP per minute
(including successes), and at most eight simultaneous authentication checks. A web
login followed by an RDP connection uses two attempts; allow for this when several
users share an office NAT.

Browser sessions last eight hours. Logging out revokes that session's unused
grants and active RDP connections; session expiry also ends its RDP connections.
The gateway checks ownership again when connecting and pins the private VM
server's certificate. Even administrators can download RDP files only for their
own VMs.

The downloaded profile sets `enablecredsspsupport:i:1`, allowing the client to
advertise CredSSP and use its normal credential prompt. The gateway does not
implement server-side NLA: protocol negotiation selects TLS, and the client sends
the entered password in the TLS-protected Client Info packet. The profile retains
`authentication level:i:2` so the client continues to authenticate the gateway's
certificate; it contains neither a password nor a `public mode` setting.

The guest console accepts one RDP connection at a time. Managed VMs enable H.264,
with bitmap fallback for clients without AVC420 support. AVC420 pacing is fixed
at 30 FPS, rising to 60 during left-button dragging when client feedback allows
it. The boost ends on release or after 150 ms without drag movement. Bitmap
fallback stays at 30 FPS.
Display H.264 encoding uses four slices; the codec can use fewer for very small
pictures. The cap is applied before copying the framebuffer for encoding,
converting colors, and invoking the full-picture H.264 codec.
Changes between frames are coalesced without losing pending damage; idle screens
are not encoded. This controls graphics only, not input, cursor updates, or audio.

`H264_QP` (or `serve -h264-qp`) sets H.264 quantization: default `20`, minimum `0`,
maximum `51`, inclusive. Lower values improve quality and increase traffic;
higher values compress more and reduce quality. QP does not impose a guaranteed
Mbps cap, and even QP `0` retains AVC420 chroma subsampling. Restart the gateway
and start new VM processes to apply a change; see the
[configuration reference](configuration.md#storage-and-desktops).

Four continuously changing AVC420 sessions target at most 120 encoded frames
per second at the normal rate, rising to 240 if all four are actively dragging.
These are workload caps, not measured throughput or guarantees of session density;
CPU, resolution, content, and the active encoding path still determine actual
performance. See the
[runtime pacing reference](runtime.md#rdp-graphics-pacing) for details.

Managed VMs enable audio playback by default: 48 kHz stereo, signed 16-bit PCM
over RDP. The guest needs the `snd_virtio` driver and a working desktop audio
stack. The downloaded file requests sound on the client and disables microphone
capture. Enable local sound in the client; an audio-disabled or unsupported
client can still use graphics and input. Restart existing VMs after upgrading
to an audio-enabled worker. See [audio requirements and troubleshooting](runtime.md#audio).

Clipboard redirection and microphone capture are not implemented. The
downloaded file's clipboard preference does not add server-side support.

RDP access must use the dashboard's downloaded file and authenticate through the
gateway. Standalone `boot -rdp` listeners and the direct `make rdp` helper are
removed. Each VM's backend uses a private Unix socket with kernel-verified
gateway process identity; it exposes no per-VM TCP port. The inherited `-rdp-fd`
worker option is internal and is not a standalone connection mode.

## Connection admission and edge protection

Unauthenticated sockets use a separate pending pool, so idle clients cannot
consume the authenticated HTTPS/RDP connection pool. Defaults allow 64 pending
and 128 authenticated connections globally, at most 32 total and eight pending
per source. TCP admissions use global and per-source token buckets: 128 and eight
connections per second respectively, each with an equal-size burst. Excess
connections are closed before protocol negotiation or TLS work.

A connection must authenticate within ten seconds of TCP acceptance. Completing
TLS, fetching the login page, or sending partial requests does not reset this
budget. HTTPS requires a valid browser session or a successful login; RDP requires
credential validation. Browser clients can reconnect if a login-page connection
expires while the user is entering credentials. Existing password-attempt limits
are independent of these connection limits.

Per-source limits use the TCP peer address, normalize IPv4-mapped addresses, and
group IPv6 addresses by /64. Forwarded headers do not change this identity. Shared
NATs and IPv6 prefixes share their allowance: size per-source capacity and rates
for legitimate concurrent browsers and RDP clients. All limits and the pending
timeout are configurable through [connection admission settings](configuration.md#connection-admission).

For Internet-facing deployments, also configure SYN/new-connection rate limiting
and connection caps at the source-preserving layer-4 firewall or network edge.
The in-process admission limits run after TCP acceptance; they cannot protect the
kernel's SYN backlog, upstream bandwidth, or a saturated network link. A
distributed attack can still fill the pending pool and prevent new logins even
though existing authenticated connections keep their separate capacity.

Apply edge protection to both IPv4 and IPv6 on the published gateway port, and
verify it covers the actual packet path, including container port forwarding.
Preserve the client source address and allow both HTTPS and native RDP on the
same TCP listener. Do not place an ordinary HTTP reverse proxy in front of it.
The [opt-in nftables example and deployment guide](edge-rate-limiting.md) cover
native listeners and Docker's published port before destination NAT. No rules
are installed automatically; adapt and validate them for your deployment, then
test legitimate browser/RDP access before exposure.

## Shared base images and administration

Administrators open **Admin** to view all owners' VMs, start or stop them, delete
stopped VMs, and manage shared base images. The role does not grant access to
another owner's RDP console.

The catalog starts empty and may remain empty until the first upload. It is
stored under `<state>/images`; a template has its own QCOW2 file and metadata.
Users cannot create a new desktop until at least one template is available.

On **Admin → Base Images**, choose a file under **Upload Base Image**, then
select **Upload**. Keep the page open through both transfer and preparation.
The transfer percentage measures upload bytes; validation and conversion follow
as a separate phase. **Cancel upload** cancels the request and refreshes the
catalog in case publication already completed. Logging out or losing the session
also cancels an in-progress import.

A usable template must contain an x86-64 UEFI bootloader, systemd, cloud-init,
a graphical desktop, a display manager, and virtio disk, network, and display
drivers, plus PS/2 keyboard and virtio-input tablet support. Audio playback also
requires `snd_virtio` and a desktop audio stack. Upload validation checks storage
format, not whether the installed guest can boot or provision successfully.

- Uploads must contain standalone QCOW2 version 2 or 3 data. The file picker
  accepts `.qcow2`, `.img`, and `.raw` names, but raw-format uploads are rejected.
- Backing files, encryption, internal snapshots, external data files, dirty or
  corrupt flags, and unsupported incompatible features are rejected.
- Images using DEFLATE or Zstd compression, extended L2 entries, shared clusters,
  or alternate refcount widths are normalized by the built-in Go importer. It
  also rebuilds layouts whose refcount tables cannot cover their full virtual
  capacity. Consistent bitmap metadata is discarded during normalization.
  Conversion preserves virtual capacity and guest data, producing a standalone
  uncompressed QCOW2 image supported by the VM disk backend. No external
  conversion tool is required. Existing unsupported templates or VM disks are
  not modified automatically; import the source as a new catalog template.
- Every template and managed VM disk has a default maximum virtual capacity of
  `500GB`, controlled by `VM_DISK_MAX_SIZE` (`-vm-disk-max-size`).
- Administrator QCOW2 imports retain an independent 1 TiB virtual-capacity
  ceiling even when `VM_DISK_MAX_SIZE` is raised. A configured QCOW2
  `BASE_IMAGE` may be larger when the policy is raised.
- The upload file and the normalized file each have a default limit of 20 GiB;
  this is separate from virtual capacity.
- Only one upload/import runs at a time. Image names must be unique, ignoring
  case. An incomplete import is not published as a selectable template.

The disk limit accepts a positive integer byte count, with optional `B`, `KB`,
`MB`, `GB`, `TB`, `KiB`, `MiB`, `GiB`, or `TiB` units. Units are
case-insensitive. Decimal `GB` uses powers of 1000, so the default `500GB` is
exactly 500,000,000,000 bytes; binary `500GiB` is 536,870,912,000 bytes.

An oversized administrator upload is rejected before publication, and an
oversized configured `BASE_IMAGE` prevents gateway startup. Capacity is checked
again when a desktop is created and whenever it starts. Lowering the limit does
not delete existing templates or VMs: they remain visible and removable, but an
oversized template cannot create a desktop and an oversized stopped VM cannot
start. The gateway never grows or shrinks a disk to meet the setting.

`VM_DISK_MAX_SIZE` applies to each virtual disk; it is not an aggregate physical
storage quota. Sparse QCOW2 allocation, filesystem reflinks, subsequent guest
writes, and QCOW2 metadata overhead mean actual state-directory use can differ
from virtual capacity. The administrator is responsible for aggregate capacity,
filesystem quotas, reserved space, and monitoring. H-04 remains an
operator-managed shared-storage-exhaustion risk, not a vulnerability closed by
the 500 GB default; see [storage responsibilities](storage-isolation.md).

Upload and import requests default to a 30-minute timeout. To permit a larger
file and a longer overall request:

```bash
IMAGE_UPLOAD_MAX_BYTES=42949672960 IMAGE_UPLOAD_TIMEOUT=1h make gateway
```

Image normalization has its own 30-minute cap, even when the overall
request timeout is longer. Session expiry can also end the operation. Reserve
space for the upload, converted output, and guest clones; the per-file limit is
not a total state-directory quota.

Validated images become available to every allowed user in **Create DevBox**.
If explicitly configured, the optional `BASE_IMAGE` template (`GATEWAY_IMAGE` in
the Make helper) is marked **Default** and **Protected**; it cannot be removed through the UI.
Removing an uploaded image, including the last template, removes it from
the catalog but preserves existing VMs, whose disks are independent. A
filesystem reflink may initially share storage blocks without creating a QCOW2
backing-file dependency.

## Gateway certificates and ACME

Without TLS settings, the gateway generates a self-signed certificate for the host
in `PUBLIC_ADDRESS` and persists it as `<state>/gateway.crt` and
`<state>/gateway.key`. This works on loopback and LAN listeners. Trust the
certificate in your browser and RDP client before using it. A generated certificate
is refreshed at startup when it expires or the public hostname changes, preserving
its private key.

Set `CERT_FILE=/path/to/fullchain.pem` and `KEY_FILE=/path/to/key.pem` to use your
own matching PEM identity. The gateway reads these regular files without modifying
them; symlinks are rejected. The private key must be owned by the gateway user
with no group or other permissions (for example, mode `0600`).

For automatic public certificates, set `ACME_ENABLE=true` with a public DNS
hostname in `PUBLIC_ADDRESS`. `ACME_EMAIL` sets the account contact address and `ACME_CA`
selects `production` (the default), `staging`, or a custom ACME directory URL.
Enabling ACME accepts the CA's subscriber agreement. ACME and supplied PEM files
are mutually exclusive.

Public TCP **443** must reach the shared gateway listener for the TLS-ALPN-01
challenge. Port 80 is not required. Certificate management starts after the
listener is bound; a self-signed certificate serves as fallback while initial
issuance is pending or retrying. Certificates switch automatically after issuance
and renew while the gateway is running. The hostname allowlist contains only
`PUBLIC_ADDRESS`'s hostname; RDP clients without SNI use that same identity.

ACME accounts, cached certificates, and renewal state persist under
`<state>/acme`. Keep that directory across restarts and include it in private
state backups. See the [Docker TLS guide](docker.md#tls-and-acme) for Compose
settings and a migration from the former certificate helper service.

## LDAP and remote access

For a remote deployment, enable ACME or supply a certificate trusted by both web
browsers and RDP clients. The executable supplies the OVMF firmware.
An administrator uploads image templates after startup:

```bash
DATA_ROOT_DIR=/var/lib/devbox-kvm \
VM_CGROUP_ROOT=/path/to/delegated-subtree \
LISTEN_ADDR=0.0.0.0:8443 PUBLIC_ADDRESS=desktops.example.com:8443 \
CERT_FILE=/etc/devbox-kvm/fullchain.pem KEY_FILE=/etc/devbox-kvm/key.pem \
LDAP_URL=ldaps://directory.example.com LDAP_BASE_DN=dc=example,dc=com \
LDAP_USER_DOMAIN=example.com LDAP_USERNAME_ATTRIBUTE=sAMAccountName \
LDAP_USER_FILTER='(userPrincipalName=%s)' \
LDAP_REQUIRED_GROUP='CN=Desktop Users,OU=Groups,DC=example,DC=com' \
ADMIN_GROUP='CN=Desktop Admins,OU=Groups,DC=example,DC=com' \
./devbox-kvm serve
```

Run as a non-root account with access to the state, certificates, and `/dev/kvm`.
Prepare `VM_CGROUP_ROOT` as described under [`serve` options](#serve-options)
before launching this command; missing delegation prevents startup.

Use the configured public hostname in the browser and downloaded RDP files.
HTTPS and RDP share one TCP port. An ordinary HTTP reverse proxy cannot carry
the RDP connection; deployment must preserve direct TCP access and client source
addresses. The gateway does not use forwarded HTTP headers to determine the
session's client IP.

The gateway binds to LDAP with the submitted username and password, appending
`@domain` when configured. It then searches for exactly one matching entry. The
filter contains one `%s` placeholder, filled with the escaped bind identifier.
The canonical username attribute must contain one supported lowercase Linux
account name, or an email address matching `LDAP_USER_DOMAIN`; the local part then
identifies the VM owner. Sign in with the bare account name.

`LDAP_REQUIRED_GROUP` restricts login using direct `memberOf` membership.
`ADMIN_GROUP` grants the admin role using the same direct membership check.
Both accept a group name or full DN and compare case-insensitively. Nested groups
are not expanded, and administrators must still satisfy the login restriction.
An empty admin-group setting disables administrator access. An account named
`administrator` has no special privilege by name alone.

Membership and roles are captured at login. Sign out and in after membership
changes. New desktops use a salted SHA-512 crypt hash of the password supplied at
login; plaintext LDAP passwords are not persisted. Existing guest passwords do
not track subsequent directory changes.

`ldaps://` negotiates verified TLS immediately. `ldap://` upgrades with StartTLS.
Use `LDAP_CA_FILE` for a private CA; otherwise system trust is used. The explicit
`LDAP_INSECURE_LOOPBACK=true` test option permits plaintext only for an `ldap://`
URL with a loopback IP.

The bundled setup requires `team10_r` and grants admin to `devbox-admins`. It uses
no generated-auth-file fallback. Old `auth.json` and
`auth.json.credentials.json` files are not read by the LDAP setup, and old
`developer` workspaces are not reassigned to `johndoe`. For all
`GATEWAY_LDAP_*` Make variables and an external-directory example, see the
[LDAP reference](../testldap/README.md).

### `serve` options

Exactly one of `LDAP_URL` and `AUTH_FILE` is required. All gateway settings are
read from environment variables; explicit flags remain available as overrides.
See the [configuration reference](configuration.md) for every variable, default,
and corresponding flag, or run `./devbox-kvm serve -help`.

CPU, RAM, and H.264 quality settings apply when a VM process starts; they are
gateway-wide settings, not per-image sizing. Restart running VMs after changing
these settings. Count quotas and `VM_DISK_MAX_SIZE` are admission controls, not
an aggregate physical-storage quota.

Before initializing gateway TLS or desktop state, `serve` verifies Linux amd64,
a non-root effective user, actual Landlock ABI 6 or newer, seccomp availability,
and KVM access. The KVM check opens `/dev/kvm` read/write, verifies API version 12,
and creates then closes an empty VM without booting a guest. It checks actual
kernel capabilities, not just the kernel version string. Failures exit with
status 78 and explain the unmet requirement on standard error, or in the systemd
journal for the packaged service. The unit's `RestartPreventExitStatus=78`
prevents a restart loop; correct the requirement and start the service again.
These checks apply to direct `serve` commands as well as the packaged service.
An empty image catalog remains valid; memory admission still happens separately
for each desktop start.

`VM_CGROUP_ROOT` is required for `serve` and is validated during these startup
checks. The packaged systemd service supplies it automatically; direct launches
must set it explicitly. The value must be a clean absolute path to a writable
cgroup v2 domain subtree that:

- is owned by the gateway's effective UID and is not group or other writable;
- has no processes in `cgroup.procs`;
- reports recursive `populated 0` in `cgroup.events` at gateway startup;
- lists both `memory` and `pids` in `cgroup.controllers` and
  `cgroup.subtree_control`; and
- provides writable `cgroup.kill` controls in gateway-created children.

A recursive `populated 1` rejects startup instead of accepting live orphan
processes from a previous run. Managed VMs require Linux 6.12+ with Landlock ABI 6
enabled, seccomp, cgroup v2 memory and pids controllers, `cgroup.kill`, and
clone-time cgroup attachment support. Even after the
filesystem checks pass, the kernel or container runtime policy may deny the
required `clone3` cgroup attachment; that VM launch is refused rather than
started without its limits.

For containers, mount only the dedicated host-delegated subtree at that path;
do not expose all of `/sys` or `/sys/fs/cgroup` read-write, add
`CAP_SYS_ADMIN`, or run the gateway as privileged. The Docker guide describes
the [scoped delegation requirements](docker.md#required-cgroup-v2-delegation-for-vm-starts).

For every launch, the gateway creates a private child cgroup and sets
`memory.max` to `VM_MEMORY_MIB + VM_MEMORY_OVERHEAD_MIB`,
`memory.swap.max=0`, `memory.oom.group=1`, and `pids.max=512`. The process limit
counts host tasks, including threads, across the worker and its descendants.
It verifies each value, then attaches the worker during clone so the process cannot run first and be moved
into the cgroup later. Failure to prepare, configure, verify, or attach the
cgroup leaves the desktop stopped. On exit, cleanup checks the recursive
`populated` state, writes `cgroup.kill` if residual descendants remain, waits
for `populated 0`, and removes the exact child before releasing its reservation.
If cleanup cannot be verified, the management plane remains available, but
every later VM start is blocked until the operator recovers the dedicated
subtree and restarts the gateway. `VM_MEMORY_OVERHEAD_MIB` defaults to 512 MiB
and accepts 64 through 16384 MiB; setting it too low can cause the worker cgroup
to be OOM-killed under load.

Each opened QCOW2 image retains at most 8 MiB of L2 table payload in a FIFO
cache, independently of the image's virtual size. The cache's Go map and FIFO
ring metadata add memory beyond that payload budget, but their entry counts are
bounded by the same table limit. An accepted L2 metadata write reaches the image
before the cached table changes, so eviction cannot discard the only updated
copy. Image opening rejects geometry whose single L2 table exceeds the cache
budget, in addition to the existing header and metadata-table geometry limits.
The 8 MiB value is not a total worker RSS limit; the separate hard worker limit
remains guest RAM plus `VM_MEMORY_OVERHEAD_MIB` as described above.

Before starting the first desktop, the gateway sets its automatic memory budget
from Linux `MemAvailable`, leaves 1 GiB for the host, and applies any smaller
`VM_MEMORY_BUDGET_MIB` limit. Visible cgroup v1/v2 memory limits and usage also
constrain available capacity, including ancestor limits. Each starting or
running desktop reserves its full configured RAM plus
`VM_MEMORY_OVERHEAD_MIB`, matching its hard cgroup limit, even when the guest
has not touched all of its RAM yet. Concurrent starts share this budget; launch
failure or child exit releases the reservation only after cgroup cleanup is
verified. The automatic budget is refreshed once all desktops have stopped and
another start is requested.

Every start also checks current available memory and effective memory limits,
including existing reservations that are not yet backed by resident anonymous
pages. Anonymous proportional resident memory from each child's Linux
`smaps_rollup` is credited so committed RAM is not counted twice; unreadable
child accounting receives no credit.
Insufficient capacity leaves the desktop stopped and displays a memory-specific
error. If host memory cannot be measured, the start is refused. For example:

```bash
VM_CGROUP_ROOT=/path/to/delegated-subtree \
VM_MEMORY_MIB=4096 VM_MEMORY_OVERHEAD_MIB=512 \
VM_MEMORY_BUDGET_MIB=10240 make gateway
```

This caps desktop reservations at 10 GiB, allowing at most two 4 GiB desktops
(each charged 4608 MiB), subject to available host memory and `MAX_RUNNING_VMS`.
The capacity checks are admission control rather than an OS reservation: other
applications can allocate memory afterward. The per-worker cgroup is the hard
limit and can terminate the worker if actual overhead exceeds its allowance.
Guest RAM is fixed while running; there is no memory ballooning. Standalone
`boot` commands are outside both the gateway budget and these managed-worker
cgroup controls.

For an explicit local-auth development setup:

```bash
./devbox-kvm auth-user -user developer -out auth.json
```

This creates a new bcrypt account file and a private
`auth.json.credentials.json` with a generated password. Supply
`AUTH_FILE=auth.json` with `LDAP_URL` unset when starting `serve`. Existing
output files are never overwritten. This mode requires a loopback listener and
does not provide administrator accounts. Use an already populated image catalog
or set `BASE_IMAGE=/path/to/prepared.qcow2` with a supported uncompressed default
template; this mode cannot upload the first image through Admin.

## State, networking, and shutdown

The gateway requires state directories owned by its host account with no access
for other users, normally mode 0700. State files are private regular files,
normally mode 0600. Only one gateway may own a state directory at a time.

| Path under `DATA_ROOT_DIR` | Contents |
| --- | --- |
| `manager.lock` | Exclusive manager lock. |
| `gateway.crt`, `gateway.key` | Persistent self-signed identity and ACME fallback. |
| `acme/` | ACME account credentials, certificate cache, and renewal state. |
| `firmware/` | Embedded firmware files extracted for gateway use. |
| `images/<image-id>/image.json` | Uploaded template metadata. |
| `images/<image-id>/disk.qcow2` | Validated, normalized template. |
| `vms/<vm-id>/vm.json` | Persistent name, owner, state, and source-image identity. |
| `vms/<vm-id>/disk.img` | Writable guest disk; the format follows the source despite the filename. |
| `vms/<vm-id>/vars.fd` | Writable firmware settings for this VM. |
| `vms/<vm-id>/seed.iso` | NoCloud provisioning, including the password hash. |
| `vms/<vm-id>/rdp.crt`, `rdp.key` | Private VM-console TLS identity. |

The `images` directory is created automatically and starts empty for a new
state directory without `BASE_IMAGE`. The Compose stack binds the host directory
selected by `GATEWAY_STORAGE_PATH` (default `/srv/devbox-storage`) to
`/var/lib/devbox-kvm`. Templates therefore live at `/srv/devbox-storage/images` on the
host by default, and VM disks at `/srv/devbox-storage/vms/<id>/disk.img`.
This mount holds all gateway state, not just QCOW2 files. See the
[Docker storage setup](docker.md) for directory preparation and ownership.

Back up the state with the gateway and VMs stopped; include any optional base
image configured outside the state directory. Keep seeds and private keys
protected. The VM's private Unix RDP listener accepts only the gateway process,
verified using kernel peer credentials. This does not isolate the service from
a compromised host account or a privileged host administrator.

Managed worker standard output and error are discarded rather than written to
persistent per-VM logs, so guest-controlled output cannot fill the state disk.
The diagnostic export contains only structured, sanitized worker records, not
raw standard error. Use the performance panel and gateway diagnostics for
host-side troubleshooting. After deploying diagnostic changes, restart affected
VMs so their workers run the new binary.

Managed VMs use `-net user-isolated`. They retain IPv4 DHCP/DNS and default to
public internet TCP/UDP destinations. Visible host interface subnets, loopback,
private, shared, link-local and other special ranges require an explicit
`VM_NETWORK_ALLOW_CIDRS` exception. `VM_NETWORK_POLICY=allowlist` permits only
those exceptions. The DNS proxy alone may contact configured host resolvers
without an exception. See the [destination policy](runtime.md#networking) for
CIDR validation, host-alias translation and deployment boundaries.

Desktop creation preserves sparse image holes when reflinks are unavailable.
`MAX_CONCURRENT_CREATIONS` limits simultaneous provisioning across all accounts
(default `2`); additional requests wait without blocking inventory or existing
RDP connections. `VM_CREATE_TIMEOUT` bounds creation including queue wait
(default `30m`). Progress writes have a separate 30-second network deadline.

**Stop** requests the guest's ACPI power button and waits up to `VM_STOP_TIMEOUT`
(30 seconds by default). If the guest needs confirmation or does not shut down, it remains running; use its
graphical power menu and refresh status. **Delete** requires a stopped VM and
permanently removes its disk, firmware variables, seed, and metadata.
Deletion first moves the VM into a private `.deleting-<id>` directory and syncs
the parent directory before removing files. Once moved, the desktop leaves the
inventory even if file cleanup returns an error. Startup retries these pending
deletions; cleanup failures are logged without preventing other desktops from
loading. Resolve the reported filesystem error and restart the gateway to retry
cleanup. Pending files continue to consume storage until removal succeeds.

Stopping the gateway requests ACPI shutdown of running VMs. After its
`VM_STOP_TIMEOUT` wait it escalates to SIGTERM, then SIGKILL if necessary. Shut
down desktops first when preserving guest application state matters. Restarting the gateway loads
saved VMs as stopped; it does not resume them automatically. Browser sessions and
RDP grants are held in memory and do not survive a gateway restart.

## Cloud-init provisioning

Gateway desktop creation automatically builds a per-VM NoCloud seed. Its
cloud-config creates the account with password-protected sudo, locks root,
disables SSH password login, stops and masks guest XRDP and the serial getty,
selects graphical boot, configures the keyboard, and enables IPv4 DHCP. The image
must already include systemd, cloud-init, a desktop, a display manager, and
virtio drivers; provisioning
does not install them.

For headless developer disk boot, generate a seed explicitly. Start with an
existing supported UEFI disk and create a separate writable variables copy.
This example exposes the host serial console; guest output depends on the
installed bootloader and OS serial configuration. Use the dashboard workflow
for interactive desktop access. Unlike `serve`, the standalone `boot` command
requires explicit firmware paths; this example uses the committed firmware files:

```bash
cp internal/gateway/firmware/OVMF_VARS_4M.fd ./guest_VARS.fd
./devbox-kvm seed -user rocky -hostname workstation -keyboard us -out seed.iso
# Read seed.iso.credentials.json for the generated guest login.
./devbox-kvm boot -uefi internal/gateway/firmware/OVMF_CODE_4M.fd \
  -uefi-vars ./guest_VARS.fd -d ./guest.qcow2 -cloud-init ./seed.iso \
  -m 4G -c 2 -net user
```

Account mode supports `-password-stdin` or `-password-hash-file` instead of a
generated password. Standard-input passwords must be 12–72 bytes with no embedded
line breaks. The hash-file mode expects SHA-512 crypt and does not write a
plaintext credentials file. `-credentials` selects another credentials output
path when a plaintext password is available. The default hostname is `desktop`,
keyboard is `us`, and `-instance-id` defaults to a generated identity.

To supply your own documents instead of the generated account configuration:

```bash
./devbox-kvm seed -user-data ./user-data -meta-data ./meta-data \
  -network-config ./network-config -out seed.iso
```

`-user-data` and `-meta-data` are required together; `-network-config` is optional.
These raw-document options cannot be combined with account mode. User-data is
preserved, supporting cloud-config and cloud-init scripts. Metadata must contain
a nonempty `instance-id`. The network-config is a separate root document; version
2 uses top-level `version: 2` and `ethernets:` keys.

Seeds are generated in Go as CIDATA ISOs and attached read-only after the primary
disk, which remains `/dev/vda`. A `-cloud-init` seed requires `-d`; invalid seeds
are rejected before opening KVM. Seed and credentials files are created with mode
0600 and never overwrite existing files.

Keep the instance ID stable for a VM's lifetime. Cloud-init normally applies
per-instance configuration once; use a fresh VM disk for a new account rather
than trying to reprovision an existing guest by changing its seed identity.

## Scope

The gateway implements the desktop provisioning, login, RDP-download, and admin
image/VM-management workflow. Its assets are bundled and served locally. The
previous DevBox-Gateway application's libvirt, Splunk, and Sauron integrations
have not been migrated. See [gateway source](../internal/gateway) and the
[CLI entry point](../gateway_command.go) for implementation details.
