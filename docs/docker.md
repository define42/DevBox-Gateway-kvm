# Docker

The default Docker image starts from `scratch` and includes DevBox-Gateway-kvm,
OVMF, `qemu-img`, CA certificates, and the shared libraries needed for NVENC and
image conversion. The executable uses dynamic linking; NVENC also loads NVIDIA
driver libraries supplied by the host. The runtime has no shell, package manager,
or FFmpeg command-line tool.

It runs as UID/GID **10001**. The host needs native **x86-64 Linux**, Make, Docker with Compose,
and working `/dev/kvm` access. If Linux itself runs in a VM, nested virtualization
must be enabled.

## Start the gateway and LDAP with Compose

The root Compose stack builds the gateway and starts the bundled development
LDAP directory. No desktop image is needed to start it. Compose bind-mounts one
private host directory at `/var/lib/gokvm` for all gateway state and deliberately
does not create the source directory.

For a fresh deployment, use a new, empty, dedicated directory. On rootful Docker
without user-namespace remapping, create the default path with:

```bash
sudo install -d -m 0700 -o 10001 -g 10001 /srv/devbox-storage
```

That ownership command is only correct for rootful Docker without UID remapping.
For rootless Docker or a daemon using `userns-remap`, determine the host UID/GID
to which container UID/GID 10001 maps and use those mapped IDs instead. To use a
different empty directory, set its absolute path in `.env` before starting:

```dotenv
GATEWAY_STORAGE_PATH=/path/to/private/devbox-storage
```

Do not point this setting at unrelated or non-empty data. An old
`gateway-state` named volume is left untouched, but Compose no longer mounts or
migrates it automatically; selecting this host path starts with the state found
at that path.

Then run from the repository root:

```bash
make gateway-docker
docker compose logs -f gateway ldap
```

The target runs `./docker/configure.sh`, then
`docker compose up --build -d gateway`. The setup helper detects `/dev/kvm`'s
group and writes `KVM_GID` to `.env`, which Compose loads automatically. It
creates `.env` from `.env.example` when needed and preserves your other settings
on later runs. The gateway continues to run as UID 10001. State already stored
in the selected host directory is preserved.

The default Compose deployment leaves `VM_CGROUP_ROOT` empty and therefore
refuses every VM start. Catalog, creation, and stopped-desktop management remain
available. Before running a desktop, complete the
[required cgroup v2 delegation](#required-cgroup-v2-delegation-for-vm-starts).

After updating this checkout, run `make gateway-docker` again to rebuild the
gateway image and recreate its container. `docker compose restart`
reuses the existing binary. If an older image reports
`choose either -ldap-url or -auth-file: invalid command arguments` after the
environment configuration migration, it may be ignoring the new variables;
that message does not necessarily mean both authentication methods are set.

Open **https://localhost:8443** and accept the locally generated self-signed
certificate in your browser and RDP client. Sign in as **`administrator`** with
password **`dogood`**, open **Admin → Base Images**, and upload a compatible QCOW2
desktop image. The image catalog starts empty; VM creation becomes available once
an administrator has uploaded a template. Uploads that use compression are
validated and converted by the included `qemu-img`.

Then sign in as `johndoe` or `testuser` (also password **`dogood`**), select
**Create DevBox**, and choose an uploaded image. See the
[sample desktop](../README.md#prepare-the-sample-desktop) and
[image requirements](gateway.md#shared-base-images-and-administration) for
compatible templates. These are public development credentials; the gateway is
published only on host loopback by default, and LDAP has no published host port.

Select **Performance** beside your desktop to view its host-process CPU/memory,
active encoder, sent frames, and encoding/transport timings. The panel refreshes
every five seconds while visible and is restricted to the desktop's owner.
It needs no extra ports, guest agent, or image changes; see the
[measurement definitions](gateway.md#view-vm-performance).

For a shareable JSON snapshot, use **Download diagnostics** on the dashboard or
open **https://localhost:8443/perf?download=1** after signing in as the desktop's
owner (use your configured address if different). Keep the RDP session connected
and reproduce the lag before downloading. See [performance diagnostics](performance.md)
for the report fields and limitations. The endpoint is private; share the downloaded
report, never your login cookie.

The report also includes virtio transfer/flush, framebuffer capture/conversion,
and RDP ACK-wait measurements. For function-level CPU investigation, optional
`GATEWAY_VM_PROFILING=true` enables private, bounded worker profiles without
opening a port. This requires a planned gateway recreation and new worker starts;
see [administrator profiling](performance.md#optional-administrator-cpu-profiling).

The gateway manages its own certificate: by default it generates and persists a
self-signed identity for the hostname in `PUBLIC_ADDRESS` (by default,
`GATEWAY_HOST`). It can instead load a supplied PEM pair or obtain and renew an
ACME certificate. See [TLS and ACME](#tls-and-acme) below.
The generated certificate is refreshed at startup when it expires or the
configured public hostname changes, preserving its private key. There is no separate certificate-initialization
service.

LDAP generates its own private TLS identity before starting. Its public
certificate is shared with the gateway through a separate read-only mount; the
LDAP private key stays in the LDAP container's volume. Compose waits for a
verified LDAP TLS handshake before starting the gateway.

The stack uses one host bind mount for gateway state and two named volumes for
LDAP:

| Storage | Contents |
| --- | --- |
| `${GATEWAY_STORAGE_PATH:-/srv/devbox-storage}` mounted at `/var/lib/gokvm` | Desktop state, uploaded images, gateway TLS identity, and ACME storage |
| `ldap-tls` | LDAP certificate/key and directory configuration |
| `ldap-ca` | LDAP public certificate shared with the gateway |

Compose prefixes the two LDAP volume names with its project name,
`devbox-gateway-kvm`. All gateway state lives beneath the selected host storage
root. Uploaded QCOW2 templates and their metadata are under `images/`; each VM's
independent QCOW2 disk is `vms/<id>/disk.img`, alongside its other per-VM state.
The root also holds the generated gateway TLS identity and `acme/` state. The
catalog and desktop state survive container rebuilds and restarts. Removing a
template, including the last one, does not remove existing desktops. Only one
gateway may use this directory at a time. Back it up with the gateway stopped.

`GATEWAY_BIND`, `GATEWAY_HOST`, and `GATEWAY_PORT` default to `127.0.0.1`,
`localhost`, and `8443`. The public hostname and port also appear in downloaded
RDP files. For remote access, replace the demo directory credentials, configure
ACME or TLS trusted by your clients, and set a reachable public hostname and bind
address. The gateway's built-in self-signed mode also works on LAN addresses;
clients must explicitly trust that certificate.

All gateway settings are passed through environment variables from `.env` or
the shell. Set `RDP_PROFILE=office` (the default) for a 30 FPS H.264 cap, or
`smooth` for 60 FPS. Bitmap fallback remains capped at 30 FPS. Restart managed
VMs to apply a profile change. Set
`H264_ENCODER`, `VM_VCPU_COUNT`, `VM_MEMORY_MIB`,
`VM_MEMORY_OVERHEAD_MIB`, `VM_CGROUP_ROOT`, and `MAX_RUNNING_VMS` to change the
defaults of automatic encoding, 2 vCPUs, 4096 MiB per guest, 512 MiB of worker
overhead, fail-closed cgroup configuration, and 4 running desktops. Allow enough
host memory for guest RAM plus the overhead described below. See the
[configuration reference](configuration.md) for every setting and the legacy Compose aliases retained for existing `.env`
files. Canonical variables take precedence over legacy aliases. The native
executable does not load `.env`; Compose passes these values into its environment.

```bash
docker compose stop         # Stop the gateway and LDAP; retain containers and data
docker compose start        # Start the existing services again
docker compose down         # Remove containers/network; retain the host bind and LDAP volumes
```

`docker compose down -v` removes the `ldap-tls` and `ldap-ca` named volumes, but
does not remove files in the host directory selected by `GATEWAY_STORAGE_PATH`.
It can therefore destroy LDAP configuration and identity material even though
gateway desktops, images, and gateway TLS/ACME state remain in the host bind.
The Compose gateway receives 45 seconds to finish shutdown. Shut down desktops
from their graphical sessions or the dashboard first when guest application
state matters.

This stack is separate from the native `make gateway` / `make ldap` workflow.
Stop whichever gateway currently uses port 8443 before starting the other, or
choose a different `GATEWAY_PORT` for Compose.

## TLS and ACME

The gateway supports three certificate modes for both HTTPS and RDP:

| Mode | Configuration | Persistence |
| --- | --- | --- |
| Automatic self-signed | Default; empty `CERT_FILE`/`KEY_FILE`, `ACME_ENABLE=false` | `gateway.crt` and `gateway.key` under `GATEWAY_STORAGE_PATH` |
| Supplied certificate | `CERT_FILE` and `KEY_FILE` together | Read from the supplied PEM paths |
| ACME | `ACME_ENABLE=true`, optionally `ACME_EMAIL` and `ACME_CA` | Account, certificates, and renewal state under `GATEWAY_STORAGE_PATH/acme` |

To enable ACME in Compose, set these values in `.env`:

```dotenv
GATEWAY_HOST=desktops.example.com
GATEWAY_BIND=0.0.0.0
GATEWAY_PORT=443
ACME_ENABLE=true
ACME_EMAIL=admin@example.com
ACME_CA=production
```

Point the public hostname's DNS records to this host and allow public TCP **443**
to reach the gateway. The container still listens on port 8443 internally;
Compose maps host 443 to that listener. ACME uses **TLS-ALPN-01** on the same
listener as HTTPS and RDP. It does not need port 80. A TLS-terminating HTTP proxy
cannot forward this challenge or the native RDP connection.

`production` (or `prod`) uses Let's Encrypt. `staging` uses its test CA, whose
certificates clients do not trust; a custom ACME directory URL is also accepted.
Enabling ACME accepts the selected CA's subscriber agreement. The gateway serves
its self-signed fallback while initial issuance is pending or retrying, then
switches to the issued certificate automatically. It renews certificates while
running and reuses cached certificates after a restart. The managed public
hostname is the only name eligible for issuance.

For supplied certificates, mount your certificate and key read-only and set
`CERT_FILE` and `KEY_FILE`, as shown in the standalone example below. Do not
combine supplied certificates with `ACME_ENABLE=true`. Supplied identities are
never modified. Keep the gateway storage directory when rebuilding or
restarting.

When upgrading from the older three-service stack, use
`docker compose up --build -d --remove-orphans` to remove the obsolete
`certificates` container. The old `gateway-tls` volume is no longer used and is
not deleted. To retain that exact frontend certificate, mount it read-only and
configure its `cert.pem` and `key.pem` as a supplied pair. Existing LDAP keys,
and directory configuration remain in their named volumes. Existing image
templates and VM disks remain only where they were stored: Compose does not
migrate the legacy `gateway-state` named volume into `GATEWAY_STORAGE_PATH`.

## Published container images

After the checks pass for a push to `main`, CI publishes the pushed head commit
as a GitHub release and two Linux amd64 container images:

- `ghcr.io/define42/devbox-gateway-kvm`: the gateway runtime with NVENC support.
- `ghcr.io/define42/devbox-gateway-kvm-ldap`: LDAP with automatic private TLS setup.

New GHCR packages default to private. After the first release, set both packages
to **Public** in their GitHub package settings for anonymous pulls, or sign in
to GHCR with package read access. See [GitHub package visibility](https://docs.github.com/en/packages/learn-github-packages/configuring-a-packages-access-control-and-visibility).

Both images use the release tag and full commit SHA as tags. Release versions
come from the commit's UTC committer date/time and its first 12 SHA characters,
for example `v2026.10.11-195952-g012345abcdef`. Month and day have no leading
zeros. Retries reuse that version and any unfinished draft; published releases
are preserved. A push containing several commits releases its head commit,
matching the reference project's workflow. Pull requests and scheduled runs
only validate changes.

The workflow promotes both images and the GitHub release to `latest` only when
the released commit is still the current `main` head. Older builds completing
later cannot replace it. Use an explicit version to deploy a specific release;
`latest` is available after a current-head release completes successfully.

To use published images with the existing Compose stack, create a local override
named `compose.published.yaml`:

```yaml
services:
  gateway:
    image: ghcr.io/define42/devbox-gateway-kvm:${RELEASE_TAG:?Set RELEASE_TAG}
  ldap:
    image: ghcr.io/define42/devbox-gateway-kvm-ldap:${RELEASE_TAG:?Set RELEASE_TAG}
```

Choose a tag from [GitHub Releases](https://github.com/define42/DevBox-Gateway-kvm/releases),
then run from a checkout of that release:

```bash
export RELEASE_TAG=v2026.10.11-195952-g012345abcdef # Replace with a published tag.
./docker/configure.sh
docker compose -f compose.yaml -f compose.published.yaml up --no-build --pull always -d
```

The override selects prebuilt images; `--no-build` keeps Compose from building
local targets. Add `-f compose.nvidia.yaml` to enable the NVIDIA runtime.
Persistent gateway storage, LDAP accounts, and admin image uploads work as
described above. The downloaded binary archive uses software encoding; the published
container retains NVENC support through the host's NVIDIA Container Toolkit.

## Build and inspect

From the repository root:

```bash
docker build --platform linux/amd64 -t devbox-gateway-kvm:local .
docker run --rm devbox-gateway-kvm:local serve -help
```

The default command starts `serve`, using environment configuration. It requires
authentication settings; the command above explicitly requests help without
starting a server. The image sets `DATA_ROOT_DIR=/var/lib/gokvm` and
`LISTEN_ADDR=0.0.0.0:8443`. The entrypoint is `gokvm`, so commands such as
`serve`, `boot`, and `probe` follow the image name directly. Check KVM
access using the same device and supplementary group used by the gateway:

```bash
docker run --rm \
  --device /dev/kvm:/dev/kvm \
  --group-add "$(stat -c '%g' /dev/kvm)" \
  devbox-gateway-kvm:local probe
```

## Run the gateway

The gateway creates an empty image catalog in its persistent state directory.
After startup, an administrator uploads QCOW2 templates through the web UI.

The standalone example below deliberately omits a cgroup mount, so it starts the
management plane but cannot start a VM. Add only a host-delegated, scoped cgroup
mount as described under
[required cgroup v2 delegation](#required-cgroup-v2-delegation-for-vm-starts).

The example below uses an external LDAP directory and a TLS certificate trusted
by the clients. Replace the example hostname, directory settings, and host paths.
Use regular PEM files, not symlinks. The private key must be owned by UID 10001
and have no group or other permissions (for example, mode `0600`). The certificate
must be readable by UID 10001, and its directories must allow traversal. A
directory owned by UID 10001 with mode `0700` satisfies these requirements.
If your existing certificate manager uses symlinks, copy the resolved certificate
and key into this mounted directory.

For this rootful, non-remapped example, create a fresh, empty gateway state
directory before starting the container:

```bash
sudo install -d -m 0700 -o 10001 -g 10001 /srv/devbox-storage

docker run -d --name devbox-gateway-kvm \
  --restart unless-stopped \
  --stop-timeout 45 \
  --device /dev/kvm:/dev/kvm \
  --group-add "$(stat -c '%g' /dev/kvm)" \
  --publish 8443:8443 \
  --mount type=bind,source=/srv/devbox-storage,target=/var/lib/gokvm \
  --mount type=bind,source=/etc/devbox-gateway-kvm/tls,target=/etc/gokvm/tls,readonly \
  --env PUBLIC_ADDRESS=desktops.example.com:8443 \
  --env CERT_FILE=/etc/gokvm/tls/fullchain.pem \
  --env KEY_FILE=/etc/gokvm/tls/key.pem \
  --env LDAP_URL=ldaps://directory.example.com \
  --env LDAP_BASE_DN=dc=example,dc=com \
  --env LDAP_USER_DOMAIN=example.com \
  --env LDAP_USERNAME_ATTRIBUTE=sAMAccountName \
  --env LDAP_USER_FILTER='(userPrincipalName=%s)' \
  --env LDAP_REQUIRED_GROUP='CN=Desktop Users,OU=Groups,DC=example,DC=com' \
  --env ADMIN_GROUP='CN=Desktop Admins,OU=Groups,DC=example,DC=com' \
  devbox-gateway-kvm:local
```

For rootless Docker or user-namespace remapping, use the mapped host UID/GID for
container UID/GID 10001 instead of copying the ownership command unchanged.

This LDAP example uses Active Directory attributes. Adjust them for your
directory as described in [LDAP configuration](gateway.md#ldap-and-remote-access).
For a private LDAP CA, also mount its PEM file read-only and pass its container
path with `LDAP_CA_FILE`. The directory address must be reachable from the
container; `127.0.0.1` refers to the container itself.

Open **https://desktops.example.com:8443**, sign in with an account in the
configured administrator group, and upload your first template under
**Admin → Base Images**. HTTPS and RDP share TCP port 8443;
both clients must reach this published port using the configured hostname. A
normal HTTP reverse proxy cannot carry the RDP connection. Browser and RDP
connections must preserve the same client source address.

Compose exposes the separate pre-authentication pool, per-source connection caps,
and TCP admission rates through the [connection admission settings](configuration.md#connection-admission).
For Internet access, also enforce source-preserving layer-4 SYN/new-connection
limits on the published port for both IPv4 and IPv6. Ensure the policy covers
Docker's port-forwarding path; the gateway does not install host firewall rules.
See [edge protection](gateway.md#connection-admission-and-edge-protection) and the
[opt-in nftables deployment guide](edge-rate-limiting.md) for a pre-DNAT example.

The host storage directory holds `vms/<id>/disk.img`, firmware variables, seeds,
uploaded templates under `images/`, metadata, and generated gateway TLS/ACME
state. It must be private and owned by the host identity mapped to container UID
10001 (`0700` for directories, normally `0600` for files). Only one gateway may
use it at a time. Back it up with the gateway stopped.

For an optional preconfigured default image, mount an immutable standalone raw
or supported uncompressed QCOW2 file read-only and set its container path in
`BASE_IMAGE`. Such a template is protected from removal through the UI. By default,
the Compose stack starts with an empty catalog and uses administrator uploads.

`VM_DISK_MAX_SIZE` defaults to `500GB` per template and managed VM disk.
Decimal `500GB` is exactly 500,000,000,000 bytes; binary `500GiB` is
536,870,912,000 bytes. This is a virtual-capacity admission guard, not an
aggregate physical-storage quota, and the gateway never grows or shrinks disks
to meet it. Sparse images, reflinks, guest writes, and QCOW2 metadata overhead
make actual volume use differ from virtual capacity.

The administrator is responsible for provisioning and monitoring aggregate
filesystem capacity, quotas, and a protected free-space reserve. The 500 GB
default does not provide those controls. H-04 remains an operator-managed
shared-storage-exhaustion risk, not a code-enforced isolation guarantee; see
[storage responsibilities](storage-isolation.md).

An oversized upload is rejected before publication, an oversized configured
`BASE_IMAGE` prevents startup, and capacity is checked again during desktop
creation and start. Lowering the limit preserves existing catalog entries and
VMs so they remain visible and removable, but an oversized template cannot be
used to create a desktop and an oversized stopped VM cannot start. See
[shared base images and administration](gateway.md#shared-base-images-and-administration)
for accepted units and the separate QCOW2 import-format ceiling.

Managed guest networking uses `user-isolated`; it needs neither a TUN device nor
`NET_ADMIN`. The container does not need `--privileged` or a Docker socket.

## Required cgroup v2 delegation for VM starts

`compose.yaml` passes `VM_MEMORY_OVERHEAD_MIB` and `VM_CGROUP_ROOT` into the
container, but it deliberately does not create a host cgroup, delegate it, or
mount it. With the default empty `VM_CGROUP_ROOT`, the gateway remains available
for management while every VM start fails closed.

The operator must provide a dedicated cgroup v2 domain subtree with:

- ownership by the container's effective UID (10001 in the supplied image) and
  no group or other write permission;
- an empty `cgroup.procs` file;
- `cgroup.events` reporting recursive `populated 0` at gateway startup, so no
  live process remains anywhere below the subtree;
- `memory` listed in both `cgroup.controllers` and
  `cgroup.subtree_control`;
- writable `cgroup.kill` controls in children created by the gateway; and
- a writable, scoped bind mount of only that delegated subtree at a private,
  clean absolute path in the container.

VM starts require Linux 5.14 or newer, cgroup v2 memory support, and clone-time
cgroup attachment. A container runtime or syscall policy can still deny the
required `clone3` cgroup attachment even when the mounted control files look
correct; that launch fails closed. Grant only the specific runtime permission
needed for this operation rather than using privileged mode or broad
capabilities.

Set `VM_CGROUP_ROOT` to that container path. Do not mount all of `/sys` or
`/sys/fs/cgroup` read-write, run the gateway as privileged, or add
`CAP_SYS_ADMIN`. How the dedicated subtree is delegated depends on the host init
system and container runtime; see the kernel's
[cgroup v2 documentation](https://docs.kernel.org/admin-guide/cgroup-v2.html).
When a non-empty root is configured, gateway startup also checks its recursive
`populated` state and refuses to start if live descendants were left by an
earlier run. The gateway validates the delegation again for every launch and
refuses to start the worker if it is missing or has drifted.

Each worker receives `memory.max` equal to `VM_MEMORY_MIB` plus
`VM_MEMORY_OVERHEAD_MIB` (default 512 MiB, accepted range 64 to 16384 MiB),
`memory.swap.max=0`, and `memory.oom.group=1`. The worker is attached during
clone, before it can run outside the limit. When setting Docker's own memory
limit, allow for every running guest and its overhead, the gateway's 1 GiB
admission headroom, and other runtime usage. Use `VM_MEMORY_BUDGET_MIB` and
`MAX_RUNNING_VMS` for additional admission limits; see
[memory limits](gateway.md#serve-options).

On worker exit, cleanup checks `cgroup.events`, writes `cgroup.kill` if residual
descendants remain, waits for recursive `populated 0`, and then removes the
launch cgroup. The reservation is not released until that succeeds. If cleanup
cannot be verified, the management plane remains available but every later VM
start is blocked. The operator must inspect and recover the dedicated subtree,
then restart the gateway; do not bypass the check with a less isolated launch.

## Enable NVIDIA NVENC

Install a compatible NVIDIA driver and the
[NVIDIA Container Toolkit](https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/latest/install-guide.html)
on the host, and configure its Docker runtime. With Compose, enable the NVIDIA
override:

```bash
docker compose -f compose.yaml -f compose.nvidia.yaml up --build -d
```

Set `H264_ENCODER=nvenc` in `.env` to require hardware encoding. Keep the same
`-f compose.yaml -f compose.nvidia.yaml` options for subsequent Compose commands.

For `docker run`, add these options before the image name:

```bash
--runtime=nvidia --gpus all
```

The image sets `NVIDIA_DRIVER_CAPABILITIES=compute,video,utility`. Keep `video`
enabled if overriding this variable; it exposes the driver libraries needed for
NVENC. See NVIDIA's
[driver capability reference](https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/latest/docker-specialized.html#driver-capabilities).

The default `H264_ENCODER=auto` falls back to software if NVENC cannot be
initialized. Set `H264_ENCODER=nvenc` to require hardware encoding, or `H264_ENCODER=software`
to select software explicitly. This accelerates the gateway's desktop video
encoding; it does not expose a GPU or 3D acceleration to the guest.

For a hardware encoding probe, build the optional Debian-based `diagnostics`
target. It includes the FFmpeg command-line tool and a shell alongside the
gateway:

```bash
docker build --platform linux/amd64 --target diagnostics \
  -t devbox-gateway-kvm:diagnostics .
docker run --rm --runtime=nvidia --gpus all \
  --entrypoint ffmpeg devbox-gateway-kvm:diagnostics \
  -hide_banner -f lavfi -i testsrc2=size=1280x720:rate=30 \
  -frames:v 3 -c:v h264_nvenc -f null -
```

This probe needs an NVENC-capable GPU and a host driver compatible with the
image's FFmpeg libraries. `nvidia-smi` alone only verifies GPU visibility. Shell
debugging is available through the `diagnostics` target; the default image has
no `/bin/sh` for `docker exec`.

## Logs and shutdown

```bash
docker logs -f devbox-gateway-kvm
docker stop --time 45 devbox-gateway-kvm
docker start devbox-gateway-kvm
```

Shut down desktops through their graphical session or the dashboard first when
guest application state matters. Gateway shutdown requests ACPI power-off and
allows `VM_STOP_TIMEOUT` (30 seconds by default) before terminating remaining VM
processes; keep Docker's stop timeout longer than this setting to allow cleanup.
Saved desktops remain in the host storage directory and load as stopped after
restart. Removing the container, including with `docker compose down -v`, does
not delete this bind-mounted data; only an explicit host-side operation can
remove it.
