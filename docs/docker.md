# Docker

The default Docker image starts from `scratch` and includes DevBox-Gateway-kvm,
OVMF, `qemu-img`, CA certificates, and the shared libraries needed for NVENC and
image conversion. The executable uses dynamic linking; NVENC also loads NVIDIA
driver libraries supplied by the host. The runtime has no shell, package manager,
or FFmpeg command-line tool.

It runs as UID/GID **10001**. The host needs native **x86-64 Linux**, Docker, and
working `/dev/kvm` access. If Linux itself runs in a VM, nested virtualization
must be enabled.

## Start the gateway and LDAP with Compose

The root Compose stack builds the gateway and starts the bundled development
LDAP directory. No desktop image is needed to start it. Run from the repository
root:

```bash
./docker/configure.sh
```

The setup helper detects `/dev/kvm`'s group and writes `KVM_GID` to `.env`,
which Compose loads automatically. It creates `.env` from `.env.example` when
needed and preserves your other settings on later runs. Run it again if the
host KVM group changes; the gateway continues to run as UID 10001.

```bash
docker compose up --build -d
docker compose logs -f gateway ldap
```

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

The `certificates` service runs once at startup to prepare private TLS keys in
named volumes. It creates a gateway certificate for `localhost`, `127.0.0.1`,
and `GATEWAY_HOST`, plus an LDAP certificate for the internal name `ldap`.
The gateway explicitly trusts the LDAP certificate, so LDAP certificate
verification stays enabled. Valid certificates are reused across restarts;
expired certificates or a changed hostname that no longer matches cause
initialization to fail rather than replace existing keys.

The stack uses these named volumes:

| Volume | Contents |
| --- | --- |
| `gateway-state` | Desktop disks, firmware variables, seeds, uploaded images, and metadata |
| `gateway-tls` | Gateway certificate/key and the trusted LDAP certificate |
| `ldap-tls` | LDAP certificate/key and directory configuration |

Compose prefixes these names with its project name, `devbox-gateway-kvm`.
Uploaded QCOW2 templates and their metadata live in `/var/lib/gokvm/images`
inside `gateway-state`. Each VM receives its own independent disk. Both the
catalog and desktop state survive container rebuilds and restarts. Removing a
template, including the last one, does not remove existing desktops.
Only one gateway may use the state volume at a time. Back it up with the gateway
stopped.

`GATEWAY_BIND`, `GATEWAY_HOST`, and `GATEWAY_PORT` default to `127.0.0.1`,
`localhost`, and `8443`. The public hostname and port also appear in downloaded
RDP files. For remote access, replace the demo directory credentials, configure
TLS trusted by your clients, and set a reachable public hostname and bind
address. Changing `GATEWAY_HOST` requires a matching gateway certificate;
existing private keys are never overwritten automatically. Use a local Compose
override to mount your own certificate/key and trusted LDAP CA when configuring
external services; see the standalone example below for the gateway flags.

The `.env` settings also control the encoder, vCPUs, guest memory, and maximum
running desktops. Defaults are automatic encoding, 2 vCPUs, 4096 MiB per guest,
and 4 running desktops. Allow enough host memory for guest RAM plus the overhead
described below.

```bash
docker compose stop         # Stop the gateway and LDAP; retain containers and data
docker compose start        # Start the existing services again
docker compose down         # Remove containers and network; retain named volumes
```

`docker compose down -v` also deletes saved desktops and generated certificates.
The Compose gateway receives 45 seconds to finish shutdown. Shut down desktops
from their graphical sessions or the dashboard first when guest application
state matters.

This stack is separate from the native `make gateway` / `make ldap` workflow.
Stop whichever gateway currently uses port 8443 before starting the other, or
choose a different `GATEWAY_PORT` for Compose.

## Published container images

After the checks pass for a push to `main`, CI publishes the pushed head commit
as a GitHub release and two Linux amd64 container images:

- `ghcr.io/define42/devbox-gateway-kvm`: the gateway runtime with NVENC support.
- `ghcr.io/define42/devbox-gateway-kvm-certificates`: the Compose TLS initializer.

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
  certificates:
    image: ghcr.io/define42/devbox-gateway-kvm-certificates:${RELEASE_TAG:?Set RELEASE_TAG}
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
Persistent volumes, LDAP accounts, and admin image uploads work as described
above. The downloaded binary archive uses software encoding; the published
container retains NVENC support through the host's NVIDIA Container Toolkit.

## Build and inspect

From the repository root:

```bash
docker build --platform linux/amd64 -t devbox-gateway-kvm:local .
docker run --rm devbox-gateway-kvm:local
```

The default command prints `serve` help. The entrypoint is `gokvm`, so commands
such as `serve`, `boot`, and `probe` follow the image name directly. Check KVM
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

The example below uses an external LDAP directory and a TLS certificate trusted
by the clients. Replace the example hostname, directory settings, and host paths.
The mounted certificate directory must be readable by UID 10001;
directories also need search permission. Keep the TLS private key private, for
example owned by UID 10001 with mode `0600` in a directory owned by UID 10001
with mode `0700`.

```bash
docker volume create devbox-gateway-kvm-state

docker run -d --name devbox-gateway-kvm \
  --restart unless-stopped \
  --stop-timeout 45 \
  --device /dev/kvm:/dev/kvm \
  --group-add "$(stat -c '%g' /dev/kvm)" \
  --publish 8443:8443 \
  --mount type=volume,source=devbox-gateway-kvm-state,target=/var/lib/gokvm \
  --mount type=bind,source=/etc/devbox-gateway-kvm/tls,target=/etc/gokvm/tls,readonly \
  devbox-gateway-kvm:local serve \
    -state /var/lib/gokvm \
    -listen 0.0.0.0:8443 \
    -public-address desktops.example.com:8443 \
    -tls-cert /etc/gokvm/tls/fullchain.pem \
    -tls-key /etc/gokvm/tls/key.pem \
    -ldap-url ldaps://directory.example.com \
    -ldap-base-dn dc=example,dc=com \
    -ldap-domain example.com \
    -ldap-username-attribute sAMAccountName \
    -ldap-filter '(userPrincipalName=%s)' \
    -ldap-required-group 'CN=Desktop Users,OU=Groups,DC=example,DC=com' \
    -ldap-admin-group 'CN=Desktop Admins,OU=Groups,DC=example,DC=com'
```

This LDAP example uses Active Directory attributes. Adjust them for your
directory as described in [LDAP configuration](gateway.md#ldap-and-remote-access).
For a private LDAP CA, also mount its PEM file read-only and pass its container
path with `-ldap-ca`. The directory address must be reachable from the container;
`127.0.0.1` refers to the container itself.

Open **https://desktops.example.com:8443**, sign in with an account in the
configured administrator group, and upload your first template under
**Admin → Base Images**. HTTPS and RDP share TCP port 8443;
both clients must reach this published port using the configured hostname. A
normal HTTP reverse proxy cannot carry the RDP connection. Browser and RDP
connections must preserve the same client source address.

The state volume holds VM disks, firmware variables, seeds, uploaded templates,
and metadata. A fresh named volume inherits the image's private directory
permissions. Existing state must be owned by UID 10001 and private (`0700` for
directories, normally `0600` for files). Only one gateway may use it at a time.
If using a bind mount instead, create and assign ownership to the state directory
before starting the container. Back up the volume with the gateway stopped.

For an optional preconfigured default image, mount an immutable standalone raw
or supported uncompressed QCOW2 file read-only and pass its container path with
`-image`. Such a template is protected from removal through the UI. This is an
opt-in alternative; the Compose stack uses administrator uploads exclusively.

Managed guest networking uses `user-isolated`; it needs neither a TUN device nor
`NET_ADMIN`. The container does not need `--privileged` or a Docker socket.

Defaults are 2 vCPUs and 4096 MiB per desktop. When setting Docker's `--memory`
limit, allow for all running guest RAM, 256 MiB per guest, the gateway's 1 GiB
headroom, and additional runtime usage. The gateway considers visible cgroup
memory limits and can reject a VM start when capacity is insufficient. Use
`-memory`, `-memory-budget`, and `-max-running` to control admission; see
[memory limits](gateway.md#serve-options).

## Enable NVIDIA NVENC

Install a compatible NVIDIA driver and the
[NVIDIA Container Toolkit](https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/latest/install-guide.html)
on the host, and configure its Docker runtime. With Compose, enable the NVIDIA
override:

```bash
docker compose -f compose.yaml -f compose.nvidia.yaml up --build -d
```

Set `GATEWAY_ENCODER=nvenc` in `.env` to require hardware encoding. Keep the same
`-f compose.yaml -f compose.nvidia.yaml` options for subsequent Compose commands.

For `docker run`, add these options before the image name:

```bash
--runtime=nvidia --gpus all
```

The image sets `NVIDIA_DRIVER_CAPABILITIES=compute,video,utility`. Keep `video`
enabled if overriding this variable; it exposes the driver libraries needed for
NVENC. See NVIDIA's
[driver capability reference](https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/latest/docker-specialized.html#driver-capabilities).

The default `-encoder auto` falls back to software if NVENC cannot be initialized.
Pass `-encoder nvenc` to require hardware encoding, or `-encoder software` to
select software explicitly. This accelerates the gateway's desktop video
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
allows 30 seconds before terminating remaining VM processes; the longer Docker
timeout gives that cleanup time to finish. Saved desktops remain in the volume
and load as stopped after restart. Removing the container does not delete the
named volume; deleting the volume removes its saved desktops.
