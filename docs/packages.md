# Native DEB and RPM packages

[GitHub Releases](https://github.com/define42/DevBox-Gateway-kvm/releases) provide
Linux amd64 DEB and RPM packages plus `checksums.txt`. Each package contains the
static Go executable, embedded OVMF firmware and Go H.264 encoder, a systemd
service, and an environment configuration file. No host Go toolchain, Docker,
OVMF package, or native codec library is required. Container images and
standalone binary archives are no longer released.

## Host requirements

- An x86-64 Linux host with hardware virtualization and working `/dev/kvm`.
  Enable nested virtualization if the host is itself a VM.
- Linux 6.12+ with Landlock ABI 6 enabled and seccomp.
- systemd 254+ and cgroup v2 with memory and pids controllers. The packaged
  service creates a private delegated subtree for managed VM workers.
- An external LDAP directory and sufficient RAM and disk space for desktops.
  Each new desktop defaults to 2 vCPUs and 4 GiB RAM, plus 512 MiB of worker
  memory overhead.

The package installs its system dependencies and creates the `devbox-kvm`
service account with KVM group access. It does not install an LDAP directory or
a desktop image. See the [image requirements](../README.md#host-and-image-requirements).

## Install and configure

Download one `.deb` or `.rpm` and its release's `checksums.txt` into an empty
directory. Verify the downloaded package:

```bash
sha256sum --check --ignore-missing checksums.txt
```

Install the package for your distribution:

```bash
# Debian / Ubuntu
sudo apt install ./devbox-kvm_*.deb

# Fedora / RHEL-compatible
sudo dnf install ./devbox-kvm-*.rpm
```

Installation leaves the service disabled and stopped. Edit its configuration
before starting it:

```bash
sudoedit /etc/devbox-kvm/devbox-kvm.env
```

Set the LDAP search base, login attribute, and groups to match your directory.
For example, a directory that identifies accounts by email address may use:

```dotenv
LDAP_URL=ldaps://directory.example.com:636
LDAP_BASE_DN=dc=example,dc=com
LDAP_USER_DOMAIN=example.com
LDAP_USER_FILTER=(mail=%s)
LDAP_USERNAME_ATTRIBUTE=mail
LDAP_REQUIRED_GROUP=devbox-users
ADMIN_GROUP=devbox-admins
```

Configure `ADMIN_GROUP` so an administrator can upload the first image.
TLS verification uses system trust; set `LDAP_CA_FILE` to a CA PEM file readable
by `devbox-kvm` if your directory uses a private CA. The environment file uses
systemd `EnvironmentFile` syntax: `KEY=value`, without `export` or shell
substitutions. See [LDAP and remote access](gateway.md#ldap-and-remote-access)
and the [configuration reference](configuration.md) for directory-specific options.

The default listener is `127.0.0.1:8443`, with `PUBLIC_ADDRESS=localhost:8443`.
For remote access, set `LISTEN_ADDR` to the desired host interface and
`PUBLIC_ADDRESS` to the hostname and port clients use. Supply a trusted TLS
certificate/key pair with `CERT_FILE` and `KEY_FILE`, or configure
[ACME](gateway.md#gateway-certificates-and-acme). The service cannot bind
privileged ports; for ACME, forward public TCP 443 to its listener on port 8443.
Files must be readable by the service account. HTTPS and RDP share the same raw
TCP listener; an ordinary HTTP
reverse proxy cannot carry both protocols.

Start the configured service and inspect its logs:

```bash
sudo systemctl enable --now devbox-kvm
sudo systemctl status devbox-kvm
sudo journalctl -u devbox-kvm -f
```

Open the configured HTTPS address and sign in with an LDAP administrator account.
Upload a compatible QCOW2 desktop image under **Admin → Base Images**, then
follow the [desktop workflow](gateway.md#create-and-connect-to-a-desktop).
A new installation starts with an empty image catalog.

## Service and upgrades

| Installed path | Purpose |
| --- | --- |
| `/usr/bin/devbox-kvm` | Static application executable. |
| `/etc/devbox-kvm/devbox-kvm.env` | Root-owned service configuration, mode 0600. |
| `/usr/lib/systemd/system/devbox-kvm.service` | Packaged systemd unit. |
| `/usr/libexec/devbox-kvm/start-service` | Service launcher that prepares private cgroup delegation. |
| `/var/lib/devbox-kvm` | Private persistent state, owned by `devbox-kvm`, mode 0700. |

The service supplies `VM_CGROUP_ROOT` automatically; leave it out of the
configuration file. Keep the packaged state path unless you also update the
systemd filesystem permissions. See [state and shutdown](gateway.md#state-networking-and-shutdown)
for the image, disk, firmware, and credential files stored there.

For an upgrade, verify the new download, shut down desktops, stop the service,
and back up its state before installing the replacement package:

```bash
sudo systemctl stop devbox-kvm
# Back up /var/lib/devbox-kvm and /etc/devbox-kvm while stopped.
sudo apt install ./devbox-kvm_*.deb
# On RPM systems, use: sudo dnf install ./devbox-kvm-*.rpm
sudo systemctl start devbox-kvm
```

The package also stops an active gateway before replacing its executable, so an
old gateway cannot start workers from the new binary. Existing configuration,
state, and the service's enabled setting are preserved. Upgrades reload
systemd's unit configuration but leave the service stopped until an operator
starts it; this also applies to unattended upgrades. Saved desktops load as
stopped and can be started again from the dashboard.
Package removal stops the service and retains its state and service account;
remove persistent data separately only when it is no longer needed.

## Release behavior

After CI passes for a push to `main`, the pushed head commit is published as a
GitHub release containing DEB and RPM packages and SHA-256 checksums. Versions
use the commit's UTC committer date/time and its first 12 SHA characters, for
example `v2026.10.11-195952-g012345abcdef`; month and day have no leading zeros.
A push containing several commits releases its head commit. Pull requests and
scheduled checks validate changes without publishing.

Retries reuse the same version and any unfinished draft, and preserve already
published releases. The workflow marks a release as GitHub's `latest` only if
its commit is still the current `main` head, so an older build finishing later
cannot replace the latest release.

To build packages locally, install the GoReleaser version pinned in CI and run
`make packages` from the repository root. Snapshot packages and their checksums
are written to `dist/`; this target does not publish a release. Docker and
Compose remain available for [local development builds](docker.md).
