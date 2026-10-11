# Standalone VM runtime reference

Use `gokvm boot` to run a single VM directly. For the LDAP dashboard, image
catalog, managed desktops, and authenticated RDP downloads, see the
[gateway guide](gateway.md). The [project README](../README.md) covers installation
and the product overview.

Commands below run from the repository root. A normal server build needs Go
1.26.0 or newer and no C compiler or native codec library:

```bash
CGO_ENABLED=0 go build -o gokvm .
./gokvm probe
./gokvm boot -help
```

The host must be x86-64 Linux with hardware virtualization enabled and read/write
access to `/dev/kvm`. Nested hosts must expose KVM. Prepare a supported Linux
kernel/initrd, live ISO, UKI, or UEFI disk before starting the VM. A blank disk
image alone cannot boot an operating system.

- [Boot flags](#boot-flags)
- [UEFI disk boot and the Rocky example](#uefi-disk-boot)
- [Direct Linux, ISO, and UKI boot](#direct-linux-iso-and-uki-boot)
- [Storage and cloud-init](#storage-and-cloud-init)
- [Networking](#networking)
- [Remote display and input](#remote-display-and-input)
- [H.264 and hardware encoding](#h264-and-hardware-encoding)
- [Audio](#audio)
- [Diagnostics and compatibility checks](#diagnostics-and-compatibility-checks)

## Boot flags

VM settings are command-line flags; standalone `boot` has no VM configuration
file. `boot -help` is the complete CLI reference.

| Flag | Default | Purpose |
| --- | --- | --- |
| `-D path` | `/dev/kvm` | KVM device. |
| `-k path` | `./bzImage` | Linux kernel; PVH entry points are detected automatically. |
| `-i path` | Unset | Optional initrd for direct kernel boot. |
| `-iso path-or-url` | Unset | Supported Linux ISO; takes precedence over `-k` and `-i`. |
| `-uki path-or-url` | Unset | Single-profile x86-64 UKI; cannot be combined with `-iso`, explicit `-k`, or `-i`. |
| `-uefi path` | Unset | Local OVMF CODE image; requires `-uefi-vars` and `-d`. |
| `-uefi-vars path` | Unset | Existing writable per-VM copy of matching OVMF VARS. |
| `-p parameters` | Built-in kernel command line | Replace direct-boot arguments; unavailable with `-uefi`. RDP still applies its graphical-console policy. |
| `-c count` | `1` | Guest vCPUs, from 1 to 64. |
| `-m size` | `1G` | Guest RAM; `K`, `M`, and `G` are binary units. Unitless values mean GiB. |
| `-d path` | Unset | Existing writable raw or supported qcow2 disk, normally `/dev/vda`. |
| `-cloud-init path` | Unset | Validated local CIDATA seed, attached read-only after the primary disk; requires `-d`. |
| `-net user\|user-isolated\|none` | `none` | Built-in networking, optional host loopback isolation, or no network; mutually exclusive with `-t`. |
| `-t name` | Unset | Host TAP interface. |
| `-g path` | Unset | Save the framebuffer as PNG. |
| `-vnc address` | Unset | VNC listener, for example `127.0.0.1:5900`. |
| `-rdp address` | Unset | TLS RDP console listener, for example `127.0.0.1:3389`. |
| `-rdp-wait` | `false` | Wait until an RDP session is ready before starting guest CPUs. |
| `-rdp-cert path` | Temporary self-signed certificate | PEM certificate; requires `-rdp-key` and `-rdp`. |
| `-rdp-key path` | Unset | PEM private key; requires `-rdp-cert` and `-rdp`. |
| `-rdp-h264` | `false` | Enable AVC420 for compatible clients; requires `-rdp`. |
| `-rdp-h264-encoder software\|auto\|nvenc` | `software` | Software encoding, NVENC with fallback, or required NVENC; requires `-rdp-h264`. |
| `-rdp-h264-threads count` | `0` | Software slices: zero selects one, or request 1–16 within the host CPU budget; requires `-rdp-h264`. |
| `-rdp-stats` | `false` | Log RDP performance statistics; requires `-rdp`. |
| `-audio none\|rdp` | `none` | Virtio-snd playback through RDP; `rdp` requires `-rdp`. |
| `-T count` | `0` | Instructions to skip between trace prints; zero disables tracing. |

Any of `-g`, `-vnc`, or `-rdp` enables virtio-gpu. They can be combined; `-g` is
not required for a remote console. Networking, display, and audio are otherwise
disabled by default.

Guests larger than 3.25 GiB use separate low and high RAM regions, reserving PCI
address space below 4 GiB. The requested `-m` value is RAM; the device address
hole does not reduce it. Allow additional host memory for gokvm and other
applications.

## UEFI disk boot

`-uefi` starts x86-64 OVMF firmware and executes the EFI bootloader installed on
a raw or supported qcow2 disk. The disk's bootloader loads its own kernel and
initrd. Use a matching pair of **plain, non-SMM OVMF CODE and VARS images** from
the host's OVMF package. Create a writable VARS copy for each VM before its first
run. For the Debian/Ubuntu 4 MiB layout:

```bash
cp /usr/share/OVMF/OVMF_VARS_4M.fd ./guest_VARS.fd
./gokvm boot -uefi /usr/share/OVMF/OVMF_CODE_4M.fd \
  -uefi-vars ./guest_VARS.fd -d ./guest.qcow2 \
  -m 2G -c 2 -net user -rdp 127.0.0.1:3390 -rdp-wait
```

Connect an RDP client to `127.0.0.1:3390` to release the boot wait. The disk must
already contain an EFI System Partition and an x86-64 EFI bootloader. A fresh
VARS store can discover `EFI/BOOT/BOOTX64.EFI`; use the firmware boot manager for
other installed EFI paths. Press Escape during firmware startup for its menu.

CODE is read-only. The VARS copy is locked while the VM runs, updated by firmware
writes, and flushed on shutdown. Keep it for subsequent runs; copying the
template again resets firmware settings and boot entries. Do not use the
package's shared VARS template directly or share a writable store between VMs.

UEFI requires virtio-capable guests. Legacy BIOS/CSM boot, Secure Boot,
SMM-enabled firmware, and UEFI installation from `-iso` are unsupported.
`-uefi` cannot be combined with `-k`, `-i`, `-uki`, `-iso`, or `-p`. Firmware,
VARS, and disk paths must be local files. Change kernel arguments in the disk's
installed bootloader configuration.

### Rocky Linux XFCE example

Prepare `rocky9-xfce-v2.4.16.qcow2` in the repository root from the
[DevBox-Gateway v2.4.16 release](https://github.com/define42/DevBox-Gateway/releases/tag/v2.4.16),
following the [sample image instructions](../README.md#prepare-the-sample-desktop).
The release uses split `.img` files containing qcow2 data; the preparation step
reassembles and converts them to the filename expected by the Make target. Install the
[NVENC build dependencies](#optional-nvidia-nvenc) and `qemu-img` (`qemu-utils`
on Debian/Ubuntu), then run:

```bash
make qcow2
# In another terminal:
make rdp
```

On the first run, the target preserves the source image and creates:

| File | Purpose |
| --- | --- |
| `rocky9-xfce-v2.4.16.gokvm.qcow2` | Writable standalone working disk, converted without compressed clusters. |
| `rocky9-xfce-v2.4.16.gokvm.qcow2.vars.fd` | Persistent per-VM firmware variables. |
| `rocky9-xfce-v2.4.16.gokvm.qcow2.seed.iso` | Persistent NoCloud account seed. |
| `rocky9-xfce-v2.4.16.gokvm.qcow2.seed.iso.credentials.json` | Generated guest username and random password, mode 0600. |

Read the credentials file to log in at the graphical desktop. The example
creates a `rocky` account; the source image has no preset login. Later runs reuse
the disk, VARS, seed, and credentials. These generated files are ignored by Git.
The seed disables guest XRDP and the serial getty because the RDP connection
presents gokvm's console and the guest's normal graphical login screen.

The target uses two vCPUs, 4 GiB RAM, user networking, and H.264 RDP on
`127.0.0.1:3390`. It builds NVENC support and selects hardware encoding when
available, with software fallback. Boot waits for an RDP viewer so firmware
screens can be seen from the beginning.

| Make variable | Use |
| --- | --- |
| `QCOW2_IMAGE` | Source image. |
| `QCOW2_DISK`, `QCOW2_VARS`, `QCOW2_SEED` | Persistent output paths. |
| `QCOW2_USER`, `QCOW2_KEYBOARD` | Account and keyboard layout, default `rocky` and `us`; set before creating the seed. |
| `OVMF_CODE`, `OVMF_VARS` | Matching firmware code and VARS template. |
| `QCOW2_ARGS` | Extra boot flags, such as `-m 6G -rdp-stats` or `-rdp-wait=false`. |

Changing account variables does not reprovision an existing seed or disk. For
example, `make qcow2 QCOW2_ARGS="-m 6G -rdp-stats"` adjusts runtime settings while
preserving the existing VM files.

### Firmware, screen output, and shutdown

RDP/VNC displays firmware graphics through virtio-gpu. Firmware input uses PS/2;
once the guest initializes virtio-input, pointer input switches to an absolute
tablet to avoid acceleration drift. The keyboard continues through PS/2.

With `-rdp`, the VM has no serial UART or host terminal input. Host terminal
messages are gokvm diagnostics. OVMF's virtio-gpu graphics protocol does not
provide a directly mapped linear framebuffer: after firmware exits, its last
frame remains until a guest graphics driver publishes another frame. Early
Linux text needs a guest-supported graphical console; gokvm does not synthesize
it from serial output.

The Rocky example image's bootloader uses `console=ttyS0,115200n8`. Change its
installed arguments to `console=tty0` for screen output once Linux has a
graphical console. UEFI mode does not rewrite these arguments. RDP shows the
current screen rather than replaying missed screens; `-rdp-wait` delays guest
execution until a viewer is ready.

Shut down from the guest when possible. For UEFI guests, sending SIGUSR1 to the
`gokvm boot` process requests an ACPI power-button event; the guest decides how
to handle it. SIGINT/SIGTERM, including Ctrl+C, stop
the VM and close its disks and VARS store; they do not perform an orderly guest
OS shutdown. The serial shortcut **Ctrl-a, then x** is available without `-rdp`.
A guest-requested reset closes and recreates the VM using the same disk and VARS;
reconnect the RDP/VNC client afterward.

Large EFI applications can hit OVMF's virtio-blk limit of 1 GiB per read. A 1.6 GB
UKI returned `EFI_BAD_BUFFER_SIZE` in an independent QEMU control; firmware may
show this as `Not Found`. Use a smaller EFI application or a bootloader that
reads large payloads in chunks. Direct `-uki` loading avoids this firmware path.
See [EDK2's virtio-blk request validation](https://github.com/tianocore/edk2/blob/master/OvmfPkg/VirtioBlkDxe/VirtioBlk.c).

## Direct Linux, ISO, and UKI boot

These modes load a Linux kernel directly; they do not run BIOS/UEFI bootloader
code. Their built-in kernel arguments describe gokvm's direct-boot environment.
Use `-p` to replace those arguments when a guest needs a different root device or
init program. In RDP mode, gokvm still selects `console=tty0` and removes serial
and early-console arguments. UEFI disk boot instead preserves the installed
bootloader's configuration.

### Kernel and initrd

```bash
./gokvm boot -k ./bzImage -i ./initrd
```

This starts a serial-console VM. A kernel with a PVH entry point selects the PVH
loader automatically. Press **Ctrl-a, then x** to exit. To prepare the repository's
sample kernel and u-root initrd, use `make bzImage initrd`. The targets download
artifacts and build tools and may compile a kernel locally. See the
[Makefile](../Makefile), [kernel script](../scripts/get_kernel.bash), and
[initrd script](../scripts/get_initrd.bash) for additional host requirements.
The sample kernel configurations enable KVM guest clocks and paravirtual
spinlocks; rebuilding the images is necessary to apply configuration changes.

### Linux live ISO

```bash
./gokvm boot -iso ./TinyCore-current.iso -m 512M -vnc 127.0.0.1:5900
./gokvm boot -iso ./slax.iso -m 2G -net user -rdp 127.0.0.1:3390
```

ISO discovery reads El Torito metadata and common syslinux/isolinux or GRUB
configurations, then loads the Linux kernel and initrd. The ISO is also attached
as read-only virtio-blk media for the live system. This supports recognized Linux
ISO layouts, not arbitrary operating-system installers. An explicit `-p`
replaces the ISO arguments and added defaults, subject to the RDP console policy.

`-iso` accepts HTTP(S) URLs, fully downloading the image to a temporary file kept
for the VM's lifetime. Downloads are not cached between runs. For example:

```bash
./gokvm boot -iso http://www.tinycorelinux.net/17.x/x86/release/TinyCore-current.iso \
  -m 512M -vnc 127.0.0.1:5900
```

TinyCore remote boots receive an initrd overlay that starts the FLWM/Xvesa
desktop. Right-click its desktop for the menu; the application dock is at the
bottom. `make tinycore` runs the local ISO on VNC `127.0.0.1:5900`.

`make slax` builds without cgo and runs the local `slax.iso` with four vCPUs,
2 GiB RAM, user networking, and H.264 RDP on `127.0.0.1:3390`. It requests four
encoder slices, bounded by the host CPU budget. Connect with `make rdp`.
The Slackware-based Slax image with Linux 6.1.38 was tested through desktop
startup using its original boot parameters, without extracting a kernel or
adding a driver blacklist.

### Unified Kernel Image

```bash
./gokvm boot -uki ./netdesk.efi -m 6G -c 2 -net user \
  -rdp 127.0.0.1:3390
```

`-uki` reads `.linux`, `.initrd`, and optional `.cmdline` sections from a
single-profile x86-64 PE32+ UKI, then loads the embedded bzImage directly.
It does not execute the EFI stub, supply UEFI services, or verify Secure Boot
signatures. Arbitrary EFI applications and EFI-stub-dependent UKIs are unsupported.
The embedded command line is combined with direct-boot defaults unless `-p`
replaces it. Payloads stream into guest RAM. HTTP(S) downloads are removed once
the payloads have loaded.

[NetDesk](https://github.com/define42/NetDesk) includes its desktop in the UKI and
needs no attached disk. Six GiB matches its reference VM configuration.
`make netdesk` uses `../NetDesk/dist/netdesk.efi`, two vCPUs, 6 GiB RAM, user
networking, and required NVENC H.264 RDP on port 3390. Install the
[NVENC dependencies](#optional-nvidia-nvenc) first and connect with `make rdp`.
Override `NETDESK_UKI` for another image and append flags with `NETDESK_ARGS`,
for example `NETDESK_ARGS="-rdp-stats -rdp-h264-encoder software"`.

## Storage and cloud-init

`-d` attaches writable raw or qcow2 storage; its format is detected from its
contents. The primary disk appears as `/dev/vda`. With `-iso`, read-only live
media follows it as another virtio-blk device. Attaching a disk does not select
a disk bootloader: use `-uefi`, or provide a direct kernel, UKI, or supported ISO.

```bash
./gokvm boot -k ./bzImage -i ./initrd -d ./disk.qcow2
qemu-img create -f qcow2 blank.qcow2 4G
```

Partition and format a blank disk in the guest before using it. The virtio block
device advertises FLUSH support. A guest that negotiates it uses writeback
caching: ordinary writes can complete in the host page cache, and an explicit
guest flush synchronizes the image before reporting success. Guest filesystem
barriers and application `fsync` calls therefore determine durability boundaries.
Unflushed writes can be lost after a host crash, as with a physical writeback disk.

Guests that do not negotiate FLUSH retain writethrough behavior: pending writes
share one sync per queue batch before completion. Closing a device waits for
active I/O and synchronizes dirty data. Device reset retains that dirty state;
reset itself is not a flush. The QCOW2 refcount cache is bounded to 4 MiB of
block data and keeps metadata writes immediate; it does not defer them until
eviction.

The qcow2 backend supports standalone v2/v3 images with 16-bit refcounts.
Backing files, internal snapshots, encryption, compressed or shared data
clusters, external data files, and extended L2 entries are unsupported. Dirty
or corrupt images are rejected. Convert an unsupported image to a new standalone
file with `qemu-img` before attaching it; preserve the original:

```bash
qemu-img convert -f qcow2 -O qcow2 source.qcow2 guest.qcow2
```

### NoCloud seed

For an image that already has systemd, cloud-init, a desktop, a display manager,
and virtio drivers, create a fresh per-VM VARS copy and account seed:

```bash
cp /usr/share/OVMF/OVMF_VARS_4M.fd ./guest_VARS.fd
./gokvm seed -user rocky -hostname workstation -keyboard us -out seed.iso
# Read seed.iso.credentials.json for the generated guest login.
./gokvm boot -uefi /usr/share/OVMF/OVMF_CODE_4M.fd \
  -uefi-vars ./guest_VARS.fd -d ./guest.qcow2 -cloud-init ./seed.iso \
  -m 4G -c 2 -net user -rdp 127.0.0.1:3390
```

`-cloud-init` requires `-d`; the boot disk remains `/dev/vda`, and the CIDATA seed
is attached afterward as read-only virtio-blk storage. Invalid seeds are rejected
before KVM is opened. Seed and credential files use mode 0600 and are never
overwritten. Keep the VARS file and seed for later runs.

Provisioning creates the account and configures the existing desktop; it does
not install a desktop into a server image. Cloud-init normally applies
per-instance configuration once. See [cloud-init provisioning](gateway.md#cloud-init-provisioning)
for account behavior, password/hash input, raw user-data/meta-data/network-config,
and instance-ID requirements.

## Networking

`-net user` provides built-in IPv4 DHCP, DNS, and outbound TCP/UDP without TAP,
root privileges, host firewall changes, or a helper daemon. The first guest
address is `10.0.2.15/24`; gateway and DNS are `10.0.2.2`. DNS uses the host's
configured resolvers.

Guest TCP/UDP connections to `10.0.2.2` reach host loopback services, except the
built-in DNS/DHCP ports. `-net user-isolated` denies this alias and other host
loopback forwarding while keeping virtual DHCP/DNS and outbound IPv4 access.
The gateway uses this isolated mode for managed VMs.

IPv6, incoming port forwarding, and external ICMP/ping forwarding are not
implemented. Check outbound access with a browser or `wget https://github.com`
in the guest. `-t tap0` selects a host TAP interface and cannot be combined with
`-net`; the operator supplies its host network configuration. Networking is off
unless requested.

Allow at least 2 GiB for browser workloads. A 1 GiB Slax desktop can exhaust RAM
and compressed swap while RDP stays connected; inspect `free -m` and `vmstat 1`
in the guest. More tabs may need more memory. The tested Slax image also lacks
its default CA bundle; for HTTPS certificate errors, run inside Slax:

```bash
update-ca-certificates
ln -s certs/ca-certificates.crt /etc/ssl/cert.pem
```

## Remote display and input

### Standalone RDP console

No guest XRDP daemon is needed: RDP presents the VM's screen and injects keyboard
and pointer input. For a local TLS connection:

```bash
./gokvm boot -iso ./TinyCore-current.iso -m 512M -rdp 127.0.0.1:3389
xfreerdp /v:127.0.0.1:3389 /sec:tls /u:console /p:console /cert:ignore
```

The executable may be called `xfreerdp3`. **Standalone RDP has no account
authentication**: these credentials are dummy values. TLS encrypts the session;
it does not decide who may control the VM. Keep the listener on loopback and
use an authenticated SSH tunnel for remote access, or use the
[authenticated gateway](gateway.md). NLA/CredSSP is unsupported; choose TLS in
clients with a security selector.

`/cert:ignore` accepts the temporary self-signed certificate in this local
example. Supply persistent PEM files with `-rdp-cert console.crt -rdp-key
console.key` when needed. The server accepts one RDP client at a time, including
during handshake; another can connect after it disconnects.

RDP disables the serial port, serial text overlay, and host terminal input.
Direct Linux/PVH, ISO, and UKI boots select the graphical console; UEFI boots
leave the installed bootloader arguments unchanged. Use `-rdp-wait` to connect
before guest execution begins. RDP and VNC may view the same VM simultaneously;
viewers share keyboard and pointer control.

The default transport sends bitmap updates in 16-, 24-, or 32-bit color.
`-rdp-h264` adds AVC420. VGA/VESA and virtio-gpu output are supported. Clipboard,
drive redirection, microphone input, and multiple monitors are not implemented.
FreeRDP 3.32.1 has been tested with TinyCore and Slax desktop rendering and input;
Microsoft Remote Desktop has not been validated.

### VNC and PNG

```bash
./gokvm boot -k ./bzImage -i ./initrd -vnc 127.0.0.1:5900
./gokvm boot -k ./bzImage -i ./initrd -g ./framebuffer.png
```

VNC has no authentication or encryption; bind to loopback and tunnel remote
access over authenticated SSH. VNC and PNG include the guest cursor in their
images. They can be used alongside RDP. Virtio-gpu is a 2D framebuffer device;
there is no guest 3D acceleration.

For framebuffer console output, a custom Linux kernel needs
`CONFIG_DRM_VIRTIO_GPU`, `CONFIG_DRM_FBDEV_EMULATION`, and
`CONFIG_FRAMEBUFFER_CONSOLE` built in. The sample kernel configuration includes
them. VNC-only mode can display serial fallback text before graphics takes over.

### Resolution changes

```bash
xfreerdp /v:127.0.0.1:3390 /sec:tls /u:console /p:console \
  /cert:ignore /size:1280x720 /dynamic-resolution /gfx:AVC420
```

Omit `/gfx:AVC420` for bitmap mode. Both paths support live single-monitor
resizing without reconnecting; audio and input remain on the connection. Rapid
changes are combined over 150 ms. Dimensions must each be 200–4096 pixels, with
at most 4096×2160 total pixels; odd values round down to even values. Unsupported
layouts leave the current size active.

RDP controls the GPU's preferred mode, but the guest must apply the
virtio-gpu DRM/RandR hotplug notification to change its desktop resolution.
Otherwise its previous image is scaled to the RDP window. The fixed 1024×768
VESA fallback also scales. Linux may round mode widths to an 8-pixel boundary.
Pointer coordinates use the current framebuffer size during mode changes.

For X11 desktops without automatic preferred-mode handling, such as Fluxbox,
copy [scripts/gokvm-resize](../scripts/gokvm-resize) into the guest and run it
from its graphical session:

```bash
sh ./gokvm-resize --once  # Apply once.
sh ./gokvm-resize &       # Follow changes once per second.
```

The helper requires `xrandr` and handles one connected `Virtual-*` output. For
Slax autostart, place it at `~/.local/bin/gokvm-resize` and add
`sh "$HOME/.local/bin/gokvm-resize" &` before Fluxbox starts in
`~/.fluxbox/startup`. Desktops already handling preferred modes need no helper.

## H.264 and hardware encoding

The default software backend is the bundled [OpenH264 port](../pkg/h264).
AVC420 requires no C compiler, native codec library, or special build tag;
selected amd64 codec kernels use SIMD with scalar fallbacks.

```bash
CGO_ENABLED=0 go build -o gokvm .
./gokvm boot -iso ./TinyCore-current.iso -m 512M \
  -rdp 127.0.0.1:3389 -rdp-h264
```

Clients without AVC420 support receive bitmap updates. AVC420 uses lossy YUV
4:2:0, so small colored text can appear softer than bitmap output. AVC444 is
not implemented. Graphics encoding does not change console authentication.

### Optional NVIDIA NVENC

Build with cgo and `-tags nvenc` to encode through FFmpeg's `libavcodec`. On
Debian/Ubuntu:

```bash
sudo apt install build-essential pkg-config libavcodec-dev libavutil-dev
CGO_ENABLED=1 go build -tags nvenc -o gokvm .
./gokvm boot -iso ./slax.iso -m 2G -net user \
  -rdp 127.0.0.1:3390 -rdp-h264 -rdp-h264-encoder nvenc -rdp-stats
```

Runtime requires an NVIDIA GPU with H.264 NVENC support, a working NVIDIA
driver, and FFmpeg libraries containing `h264_nvenc`. gokvm links to these
libraries; it does not launch `ffmpeg`. If the CLI tool is installed, check the
host encoder independently:

```bash
ffmpeg -hide_banner -f lavfi -i testsrc2=size=1280x720:rate=30 \
  -frames:v 3 -c:v h264_nvenc -f null -
```

| Encoder setting | Behavior |
| --- | --- |
| `software` | Always uses the bundled codec, including in an NVENC build. |
| `nvenc` | Requires hardware initialization and encoding to succeed. |
| `auto` | Tries NVENC, falling back to software on initialization or encoding failure. |

An ordinary build supports `software` and `auto`; requesting `nvenc` explicitly
reports that cgo and the build tag are required. NVENC uses low-latency settings
without B-frames or lookahead. Framebuffer copying and RGB-to-YUV conversion
remain on the CPU. `-rdp-h264-threads` affects only software encoding; NVENC
reports zero software slices. Intel VA-API and Quick Sync backends are not
implemented.

### FreeRDP build and cursor troubleshooting

Check `xfreerdp /buildconfig` for `WITH_GFX_H264=ON` before requesting
`/gfx:AVC420`. A client built with that feature off can reject the argument even
when its help lists it. Installing OpenH264 afterward does not change the
client's build features. Rebuild the client or use bitmap mode.

The repository can build pinned FreeRDP 3.32.1 with OpenH264 decoding. Install
these Debian/Ubuntu dependencies, then run `make freerdp`:

```bash
sudo apt install build-essential cmake curl pkg-config libssl-dev libicu-dev libopenh264-dev \
  libx11-dev libxext-dev libxfixes-dev libxrandr-dev libxrender-dev libxcursor-dev \
  libxi-dev libxinerama-dev libxkbfile-dev libasound2-dev libpulse-dev libxv-dev \
  zlib1g-dev
make freerdp
```

[tools/build-freerdp.sh](../tools/build-freerdp.sh) verifies the source archive's
SHA-256 and defaults to four build jobs; override with `FREERDP_JOBS=8 make
freerdp`. The executable is `tools/freerdp/bin/xfreerdp`; bundled FreeRDP
libraries are found relative to it, without `LD_LIBRARY_PATH`. OpenH264, X11,
and audio libraries still come from the system. Downloads and build outputs
are ignored by Git.

`make rdp` prefers this local build, otherwise `xfreerdp3` or `xfreerdp` on PATH.
It checks H.264 support and connects to port 3390 with AVC420, dynamic resolution,
and the local certificate exception. Set `RDP_CLIENT=/path/to/xfreerdp` to choose
another executable. This target works with `make qcow2`, `make slax`, or
`make netdesk`; their standalone listeners use the same default port.

For X11 cursor rendering, check `WITH_XCURSOR=ON`. Virtio-gpu hardware cursor
shapes, hotspots, and positions are separate RDP updates, so pointer-only motion
does not encode desktop video. Shapes are cached, transparent padding is
trimmed, and large shapes are reduced to the supported 32×32 size. Older clients
receive a color cursor without alpha blending. Software cursors remain in the
image; clients without Xcursor support can show a duplicate local pointer.
VNC/PNG continue composing the cursor into their images.

### Performance measurements

Enable `-rdp-stats` to log five-second active-session windows and a disconnect
summary. It works with bitmap and AVC420 sessions and reports the selected
backend, source/output dimensions, frame rate, payload bytes, software slices,
and average/maximum time spent copying, converting, encoding, and writing.

`pacing` is the permitted frame interval; `ack_turnaround` samples the smoothed
send-to-acknowledgement time, including client processing. `ack_wait` measures
time waiting for a free acknowledgement slot after the pacing deadline. Zero
`ack_wait` does not rule out reduced frame rate from adaptive pacing or guest
rendering. Reported frame rate includes idle time in the reporting window.

```bash
./gokvm boot -iso ./slax.iso -m 2G -net user \
  -rdp 127.0.0.1:3390 -rdp-h264 -rdp-h264-threads 1 -rdp-stats
```

The default `-rdp-h264-threads 0` uses one slice/worker. Explicit values request
1–16 slices within a budget based on Go's CPU limit, guest vCPUs, and a CPU
reserved for host work, with a minimum of one. The codec schedules fixed slices
on at most four workers and may use fewer for small pictures. Compare 1, 2, and
4 on the same workload; more slices can reduce encoding time while adding
headers and reducing compression. NVENC ignores this setting.

Changed H.264 frames can be sent at up to 60 fps; bitmap output is capped at
30 fps. Dirty rectangles reduce pixel conversion/copying, bitmap updates use
64×64 tiles, and a slow client receives the latest frame rather than an expanding
queue. H.264 still encodes a complete picture using references to previous
frames. IDR frames are sent for initialization, resize, refresh, and recovery;
the software path disables deblocking to reduce latency. A two-frame decode
window and acknowledgement-based pacing also bound the work sent to a client.

VGA/VESA fallbacks and VNC serial fallback publish full frames. Browser rendering
can remain CPU-bound inside the guest because there is no 3D acceleration,
even when host NVENC is active. Confirm the negotiated encoder in the server log
before attributing choppy scrolling to hardware encoding.

## Audio

`-audio rdp` exposes virtio-snd playback and sends sound over the RDP connection.
The guest needs its `virtio_snd` driver; the tested Slax image includes it.

```bash
./gokvm boot -iso ./slax.iso -m 2G -net user \
  -rdp 127.0.0.1:3390 -rdp-h264 -audio rdp
xfreerdp /v:127.0.0.1:3390 /sec:tls /u:console /p:console \
  /cert:ignore /gfx:AVC420 /sound:sys:pulse,latency:40
```

Playback is stereo PCM16 at 48 kHz; guest ALSA/PulseAudio can convert formats.
Use `aplay -l` and `aplay -D plughw:0,0 example.wav` in the guest to check output.
The server logs `client using PCM audio` when audio is negotiated. Microphone
capture is unsupported; VNC carries no sound.

The client needs a working backend: `WITH_PULSE=ON` for `sys:pulse` or
`WITH_ALSA=ON` for `sys:alsa`. A `Loaded fake backend for rdpsnd` message means
it cannot produce sound. PulseAudio-compatible PipeWire works with the Pulse
backend. The 40 ms example buffer absorbs jitter; the tested Pulse backend can
briefly rebuffer after a long silence. Old samples are discarded to keep buffers
bounded, so disconnected or slow viewers do not stop the guest audio clock.

## Diagnostics and compatibility checks

`gokvm probe` reports host KVM capabilities and supported CPUID information.
For device debugging, set these environment variables to any nonempty value:

| Variable | Output |
| --- | --- |
| `GOKVM_TRACE_IO` | Guest port I/O exits. |
| `GOKVM_TRACE_MMIO` | Unhandled guest MMIO accesses. |
| `GOKVM_TRACE_ATAPI` | Commands received by the emulated ATAPI device. |

The following checks are opt-in and start test guests or exercise hardware.

For firmware boot, persistent VARS across a guest reset, graphics, and shutdown:

```bash
OVMF_CODE=/usr/share/OVMF/OVMF_CODE_4M.fd \
OVMF_VARS=/usr/share/OVMF/OVMF_VARS_4M.fd tools/test-uefi.sh
```

[tools/test-uefi.sh](../tools/test-uefi.sh) requires KVM, GNU-EFI development
files (`gnu-efi`), a C compiler/binutils, mtools, `qemu-img`, and Python 3. It
builds an EFI fixture and boots isolated raw/qcow2 disks using ephemeral RDP
ports; source firmware is untouched. Failures retain logs, disks, and screenshots.
Set `UEFI_KEEP_ARTIFACTS=1` to retain successful runs, `UEFI_CPUS=4` to change CPU
count, or `GOKVM_BINARY=/path/to/gokvm` for an existing binary. Alternate GNU-EFI
locations use `EFI_INCLUDE_DIR` and `EFI_LIB_DIR`. This checks the firmware
platform, not every guest OS.

For the NetDesk UKI desktop, input, and browser-network smoke test:

```bash
GOKVM_NETDESK_UKI=/path/to/netdesk.efi go test -tags=integration ./internal/vmm \
  -run '^TestNetDeskUKIDesktopBoot$' -timeout 6m -v
```

For actual NVIDIA encoding, with `ffmpeg` and the NVENC build dependencies:

```bash
GOKVM_TEST_NVENC=1 CGO_ENABLED=1 go test -tags 'nvenc integration' \
  ./internal/rdp/avc -run '^TestNVENCHardware' -count=1 -timeout 2m -v
```

This checks independent decoding, forced IDR refreshes, non-macroblock-aligned
sizes, full-range BT.709 metadata, and output buffer ownership.
