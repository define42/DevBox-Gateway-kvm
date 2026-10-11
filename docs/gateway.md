# Desktop gateway operations

The gateway serves the web dashboard, authenticates users, provisions persistent
Linux desktops, and routes downloaded RDP files to the correct VM. Each desktop
runs in a separate gokvm process using KVM, UEFI firmware, and gokvm's own RDP
console. The RDP connection shows the guest's shared screen, including firmware
and its graphical login. It does not create a separate guest XRDP session.

Run the commands below from the repository root. See the [README](../README.md)
for host requirements and the project overview.

## Local development setup

The example uses matching OVMF CODE/VARS files, Docker with the Compose plugin,
OpenSSL, and `qemu-img` from `qemu-utils` for image uploads that need conversion.
No desktop image is required at startup. The `make gateway` target builds with
cgo and the `nvenc` tag, so it also requires a C compiler, `pkg-config`, and
FFmpeg development libraries
(`libavcodec-dev` and `libavutil-dev` on Debian/Ubuntu). Its default encoder is
`auto`: NVIDIA encoding is used when available, with software fallback.

```bash
make gateway
```

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

A new gateway starts with an empty image catalog in `.gokvm-gateway/images`.
Sign in as `administrator`, open **Admin → Base Images**, and upload a compatible
QCOW2 desktop image. Users can then select it when creating a desktop. Templates
and desktop disks persist across restarts. `qemu-img` performs image conversion
only; gokvm runs the VMs.

| Make variable | Default | Purpose |
| --- | --- | --- |
| `QCOW2_IMAGE` | `./rocky9-xfce-v2.4.16.qcow2` | Source used only when an explicit `GATEWAY_IMAGE` needs creating. |
| `GATEWAY_STATE` | `./.gokvm-gateway` | Private persistent state. |
| `GATEWAY_IMAGE` | Empty | Optional protected default image; if set and absent, converted from `QCOW2_IMAGE`. |
| `GATEWAY_ARGS` | Empty | Additional `serve` flags. |
| `OVMF_CODE` | `/usr/share/OVMF/OVMF_CODE_4M.fd` | Firmware code. |
| `OVMF_VARS` | `/usr/share/OVMF/OVMF_VARS_4M.fd` | Matching pristine variables template. |
| `GATEWAY_LDAP_LOCAL` | `1` | Set to `0` to skip the bundled directory. |
| `LDAP_PORT` | `1636` | Bundled directory's host port. |

The executable reads [environment configuration](configuration.md). These Make
variables remain helper aliases for the local setup; explicit `serve` flags
override environment values. To opt into a preconfigured protected default, for example:

```bash
make gateway GATEWAY_IMAGE=./.gokvm-gateway/base.qcow2 QCOW2_IMAGE=/path/to/desktop.qcow2
```

This prepares the destination once if absent, then reuses it. The original
source and the working disk used by `make qcow2` are preserved. Without
`GATEWAY_IMAGE`, the gateway uses only its uploaded catalog.

### Build without NVENC

To use the bundled software encoder without a C compiler or native codec
libraries, build directly and start LDAP separately:

```bash
CGO_ENABLED=0 go build -o gokvm .
make ldap
H264_ENCODER=software \
LDAP_URL=ldaps://127.0.0.1:1636 LDAP_BASE_DN=dc=glauth,dc=com \
LDAP_USER_DOMAIN=example.com LDAP_USER_FILTER='(mail=%s)' \
LDAP_USERNAME_ATTRIBUTE=mail \
LDAP_REQUIRED_GROUP=ou=team10_r,ou=groups,dc=glauth,dc=com \
ADMIN_GROUP=devbox-admins LDAP_CA_FILE=./testldap/.tls/cert.pem \
./gokvm serve
```

Open the same **https://localhost:8443** page and use the demo accounts above.
Sign in as `administrator` to upload the first template if the catalog is empty.
The default OVMF paths must exist; set `UEFI_CODE` and `UEFI_VARS_TEMPLATE` if
needed. `qemu-img` is still needed to prepare compressed images and for catalog
imports that require conversion. `H264_ENCODER=auto` also uses software encoding
when the binary has no NVENC support. `make gateway` always rebuilds the NVENC
target, so use the direct `serve` command to keep this build.

## Create and connect to a desktop

Once an administrator has uploaded at least one template:

1. Sign in, select **Create DevBox**, enter a name, and choose a **Base Image**.
   Names use lowercase letters, digits, and interior hyphens, up to 63 characters.
   Names must be unique within your account.
2. Wait for creation to finish. The dialog reports disk-copy progress, then
   configuration and finalization. A filesystem clone can complete immediately;
   reaching 100% copy progress does not by itself mean finalization has finished.
3. Select **Start**. A `running` state means the private RDP console responds;
   firmware, cloud-init, and the desktop may still be booting.
4. Select **RDP** and open the downloaded file within two minutes, while keeping
   your browser session signed in. For a locally built FreeRDP client:

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

### RDP identity and routing

The downloaded file contains two random values: a routing token in
`loadbalanceinfo` and a separate proof in `username`. **Keep the downloaded
username unchanged.** It is not the Linux account name. The gateway verifies the
proof inside TLS before activating the RDP session; the routing token alone is
insufficient. The file contains no account password.

A grant is single use, expires after two minutes or when its browser session
expires, and is bound to the session, client IP, VM, and owner. Downloading a new
file for the same VM in that session invalidates the previous unused grant.
Download again after a failed or closed connection. Browser and RDP connections
must reach the gateway from the same source IP.

Browser sessions last eight hours. Logging out revokes that session's unused
grants and active RDP connections; session expiry also ends its RDP connections.
The gateway checks ownership again when connecting and pins the private VM
server's certificate. Even administrators can download RDP files only for their
own VMs.

Use TLS in the RDP client; NLA/CredSSP is not supported. The guest console accepts
one RDP connection at a time. Managed VMs enable H.264, with bitmap fallback for
clients without AVC420 support. Clipboard redirection is not implemented, and
managed VMs currently do not enable audio, even though the downloaded file
contains client preferences for these features.

`make rdp` connects to the standalone console on port 3390, such as one started
with `make qcow2`. It does not use the gateway's downloaded routing credentials.

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
drivers. Upload validation checks storage format, not
whether the installed guest can boot or provision successfully.

- Uploads must contain standalone QCOW2 version 2 or 3 data. The file picker
  accepts `.qcow2`, `.img`, and `.raw` names, but raw-format uploads are rejected.
- Backing files, encryption, internal snapshots, external data files, dirty or
  corrupt flags, and unsupported incompatible features are rejected.
- Compressed images and some other unsupported layouts are checked and converted
  with `qemu-img` when necessary. Install `qemu-utils` on the gateway host for
  this import path. Images already supported by gokvm need no conversion tool.
- Virtual disk capacity is limited to 1 TiB. The upload file and the normalized
  file each have a default limit of 20 GiB; this is separate from virtual capacity.
- Only one upload/import runs at a time. Image names must be unique, ignoring
  case. An incomplete import is not published as a selectable template.

Upload and import requests default to a 30-minute timeout. To permit a larger
file and a longer overall request:

```bash
IMAGE_UPLOAD_MAX_BYTES=42949672960 IMAGE_UPLOAD_TIMEOUT=1h make gateway
```

Normalization by `qemu-img` has its own 30-minute cap, even when the overall
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
browsers and RDP clients. The host must have matching OVMF firmware. An administrator uploads
image templates after startup:

```bash
DATA_ROOT_DIR=/var/lib/gokvm \
LISTEN_ADDR=0.0.0.0:8443 PUBLIC_ADDRESS=desktops.example.com:8443 \
CERT_FILE=/etc/gokvm/fullchain.pem KEY_FILE=/etc/gokvm/key.pem \
LDAP_URL=ldaps://directory.example.com LDAP_BASE_DN=dc=example,dc=com \
LDAP_USER_DOMAIN=example.com LDAP_USERNAME_ATTRIBUTE=sAMAccountName \
LDAP_USER_FILTER='(userPrincipalName=%s)' \
LDAP_REQUIRED_GROUP='CN=Desktop Users,OU=Groups,DC=example,DC=com' \
ADMIN_GROUP='CN=Desktop Admins,OU=Groups,DC=example,DC=com' \
./gokvm serve
```

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
and corresponding flag, or run `./gokvm serve -help`.

CPU, RAM, and encoder settings apply when a VM process starts; they are
gateway-wide settings, not per-image sizing. Quotas limit VM counts, not total allocated disk bytes.

Before starting the first desktop, the gateway sets its automatic memory budget
from Linux `MemAvailable`, leaves 1 GiB for the host, and applies any smaller
`VM_MEMORY_BUDGET_MIB` limit. Visible cgroup v1/v2 memory limits and usage also
constrain available capacity, including ancestor limits. Each starting or running desktop
reserves its full configured RAM plus a 256 MiB allowance for runtime/device
overhead, even when the guest has not touched all of its RAM yet. Concurrent
starts share this budget; launch failure or child exit releases the reservation.
The automatic budget is refreshed once all desktops have stopped and another
start is requested.

Every start also checks current available memory and effective memory limits,
including existing reservations that are not yet backed by resident anonymous
pages. Anonymous proportional resident memory from each child's Linux
`smaps_rollup` is credited so committed RAM is not counted twice; unreadable
child accounting receives no credit.
Insufficient capacity leaves the desktop stopped and displays a memory-specific
error. If host memory cannot be measured, the start is refused. For example:

```bash
VM_MEMORY_MIB=4096 VM_MEMORY_BUDGET_MIB=10240 make gateway
```

This caps desktop reservations at 10 GiB, allowing at most two 4 GiB desktops
(each charged 4352 MiB), subject to available host memory and `MAX_RUNNING_VMS`.
These checks are admission control, not a hard memory limit or an OS reservation:
other applications can allocate memory afterward, and actual process overhead
can exceed the allowance. Guest RAM is fixed while running; there is no memory
ballooning. Standalone `boot` commands are outside the gateway budget.

For an explicit local-auth development setup:

```bash
./gokvm auth-user -user developer -out auth.json
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
| `images/<image-id>/image.json` | Uploaded template metadata. |
| `images/<image-id>/disk.qcow2` | Validated, normalized template. |
| `vms/<vm-id>/vm.json` | Persistent name, owner, state, and source-image identity. |
| `vms/<vm-id>/disk.img` | Writable guest disk; the format follows the source despite the filename. |
| `vms/<vm-id>/vars.fd` | Writable firmware settings for this VM. |
| `vms/<vm-id>/seed.iso` | NoCloud provisioning, including the password hash. |
| `vms/<vm-id>/rdp.crt`, `rdp.key` | Private VM-console TLS identity. |
| `vms/<vm-id>/console.log` | Appended child-process diagnostics. |

The `images` directory is created automatically and starts empty for a new
state directory without `BASE_IMAGE`. The Compose stack stores it at
`/var/lib/gokvm/images` in its persistent `gateway-state` volume.

Back up the state with the gateway and VMs stopped; include any optional base
image configured outside the state directory. Keep seeds and private keys
protected. The host account and other local host users must be trusted: the VM's loopback RDP
listener is a private transport, not a separate local-user authentication
boundary.

Managed VMs use `-net user-isolated`. They retain IPv4 DHCP/DNS and outbound
TCP/UDP, while host loopback destinations are denied. This restriction does not
make the guest network an offline environment or isolate all reachable host/LAN
services.

**Stop** requests the guest's ACPI power button and waits up to `VM_STOP_TIMEOUT`
(30 seconds by default). If the guest needs confirmation or does not shut down, it remains running; use its
graphical power menu and refresh status. **Delete** requires a stopped VM and
permanently removes its disk, firmware variables, seed, metadata, and log.

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

For standalone disk boot, generate a seed explicitly. Start with an existing
supported UEFI disk and create a separate writable variables copy:

```bash
cp /usr/share/OVMF/OVMF_VARS_4M.fd ./guest_VARS.fd
./gokvm seed -user rocky -hostname workstation -keyboard us -out seed.iso
# Read seed.iso.credentials.json for the generated guest login.
./gokvm boot -uefi /usr/share/OVMF/OVMF_CODE_4M.fd \
  -uefi-vars ./guest_VARS.fd -d ./guest.qcow2 -cloud-init ./seed.iso \
  -m 4G -c 2 -net user -rdp 127.0.0.1:3390
```

Account mode supports `-password-stdin` or `-password-hash-file` instead of a
generated password. Standard-input passwords must be 12–72 bytes with no embedded
line breaks. The hash-file mode expects SHA-512 crypt and does not write a
plaintext credentials file. `-credentials` selects another credentials output
path when a plaintext password is available. The default hostname is `desktop`,
keyboard is `us`, and `-instance-id` defaults to a generated identity.

To supply your own documents instead of the generated account configuration:

```bash
./gokvm seed -user-data ./user-data -meta-data ./meta-data \
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
