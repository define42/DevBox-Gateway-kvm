# VM runtime and gateway RDP reference

Use `devbox-kvm boot` to run a single VM directly for development. RDP access is
available only through the gateway UI: sign in, start your desktop, and open its
downloaded RDP file. Standalone `boot -rdp <address>` listeners are not supported.
For the LDAP dashboard, image catalog, managed desktops, and RDP downloads, see the
[gateway guide](gateway.md). The [project README](../README.md) covers installation
and the product overview.

Commands below run from the repository root. A normal server build needs Go
1.26.0 or newer and no C compiler or native codec library:

```bash
CGO_ENABLED=0 go build -o devbox-kvm .
./devbox-kvm probe
./devbox-kvm boot -help
```

The host must be x86-64 Linux with hardware virtualization enabled and read/write
access to `/dev/kvm`. Nested hosts must expose KVM. Prepare a supported local Linux
kernel/initrd or UEFI disk before starting the VM. A blank disk image alone cannot
boot an operating system.

- [Boot flags](#boot-flags)
- [UEFI disk boot and the Rocky example](#uefi-disk-boot)
- [Direct Linux and PVH boot](#direct-linux-and-pvh-boot)
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
| `-uefi path` | Unset | Local OVMF CODE image; requires `-uefi-vars` and `-d`. |
| `-uefi-vars path` | Unset | Existing writable per-VM copy of matching OVMF VARS. |
| `-p parameters` | Built-in kernel command line | Replace direct-boot arguments; unavailable with `-uefi`. |
| `-c count` | `1` | Guest vCPUs, from 1 to 64. |
| `-m size` | `1G` | Guest RAM; `K`, `M`, and `G` are binary units. Unitless values mean GiB. |
| `-d path` | Unset | Existing writable raw or supported qcow2 disk, normally `/dev/vda`. |
| `-cloud-init path` | Unset | Validated local CIDATA seed, attached read-only after the primary disk; requires `-d`. |
| `-net user\|user-isolated\|none` | `none` | Built-in networking, optional host loopback isolation, or no network; mutually exclusive with `-t`. |
| `-t name` | Unset | Host TAP interface. |
| `-T count` | `0` | Instructions to skip between trace prints; zero disables tracing. |

Boot inputs must be local files; the runtime does not download boot media.
Direct loading of live/install ISOs or Unified Kernel Images (UKIs) is
unsupported. CIDATA seed ISOs remain supported through `-cloud-init` as guest
configuration data.
Standalone boots use a serial console, with networking disabled by default.
The gateway enables graphics and RDP sound playback for its managed workers
automatically.

The inherited `-rdp-fd` and `-telemetry-fd` descriptors and the worker's RDP
tuning flags are internal gateway interfaces, not standalone access modes.
Configure managed desktop graphics with the gateway's `H264_ENCODER`, `H264_QP`,
and `RDP_PROFILE` settings instead. No unsafe public-RDP override is provided.

Guests larger than 3.25 GiB use separate low and high RAM regions, reserving PCI
address space below 4 GiB. The requested `-m` value is RAM; the device address
hole does not reduce it. Allow additional host memory for devbox-kvm and other
applications.

Guest RAM uses a zero-initialized anonymous mapping outside the Go heap. Startup
does not fill untouched RAM with diagnostic bytes, so physical pages are acquired
as the loader, devices, and guest touch them. The mapping remains 2 MiB aligned
with a transparent-huge-page hint; actual huge-page use depends on the host.
Gateway-managed starts also enforce the [host-memory budget](gateway.md#serve-options).
Standalone `boot` does not perform gateway admission checks.

## UEFI disk boot

`-uefi` starts x86-64 OVMF firmware and executes the EFI bootloader installed on
a raw or supported qcow2 disk. The disk's bootloader loads its own kernel and
initrd. Use a matching pair of **plain, non-SMM OVMF CODE and VARS images** from
the host's OVMF package. Create a writable VARS copy for each VM before its first
run. For the Debian/Ubuntu 4 MiB layout:

```bash
cp /usr/share/OVMF/OVMF_VARS_4M.fd ./guest_VARS.fd
./devbox-kvm boot -uefi /usr/share/OVMF/OVMF_CODE_4M.fd \
  -uefi-vars ./guest_VARS.fd -d ./guest.qcow2 \
  -m 2G -c 2 -net user
```

This headless example exposes a host serial console; guest output depends on the
installed bootloader and OS serial configuration. For an interactive desktop,
import the disk into the gateway and connect through its UI. The disk must
already contain an EFI System Partition and an x86-64 EFI bootloader. A fresh
VARS store can discover `EFI/BOOT/BOOTX64.EFI`; other installed EFI paths may
need a configured boot entry.

CODE is read-only. The VARS copy is locked while the VM runs, updated by firmware
writes, and flushed on shutdown. Keep it for subsequent runs; copying the
template again resets firmware settings and boot entries. Do not use the
package's shared VARS template directly or share a writable store between VMs.

UEFI requires virtio-capable guests. Legacy BIOS/CSM boot, Secure Boot, and
SMM-enabled firmware are unsupported.
`-uefi` cannot be combined with `-k`, `-i`, or `-p`. Firmware,
VARS, and disk paths must be local files. Change kernel arguments in the disk's
installed bootloader configuration.

### Rocky Linux XFCE example

Prepare `rocky9-xfce-v2.4.16.qcow2` in the repository root from the
[DevBox-Gateway v2.4.16 release](https://github.com/define42/DevBox-Gateway/releases/tag/v2.4.16),
following the [sample image instructions](../README.md#prepare-the-sample-desktop).
The release uses split `.img` files containing qcow2 data; the preparation step
reassembles and converts them to the filename expected by the Make target.
Install `qemu-img` (`qemu-utils` on Debian/Ubuntu), then run this headless
developer boot helper:

```bash
make qcow2
```

On the first run, the target preserves the source image and creates:

| File | Purpose |
| --- | --- |
| `rocky9-xfce-v2.4.16.devbox-kvm.qcow2` | Writable standalone working disk, converted without compressed clusters. |
| `rocky9-xfce-v2.4.16.devbox-kvm.qcow2.vars.fd` | Persistent per-VM firmware variables. |
| `rocky9-xfce-v2.4.16.devbox-kvm.qcow2.seed.iso` | Persistent NoCloud account seed. |
| `rocky9-xfce-v2.4.16.devbox-kvm.qcow2.seed.iso.credentials.json` | Generated guest username and random password, mode 0600. |

The example creates a `rocky` account; the source image has no preset login.
Later runs reuse the disk, VARS, seed, and credentials. These generated files
are ignored by Git. The seed disables guest XRDP and the serial getty. For an
interactive desktop, upload the original image through **Admin → Base Images**,
create a managed VM, and use its downloaded RDP file.

The target uses two vCPUs, 4 GiB RAM, user networking, and the host serial
console. It starts immediately; guest serial output depends on the installed
bootloader and OS configuration.

| Make variable | Use |
| --- | --- |
| `QCOW2_IMAGE` | Source image. |
| `QCOW2_DISK`, `QCOW2_VARS`, `QCOW2_SEED` | Persistent output paths. |
| `QCOW2_USER`, `QCOW2_KEYBOARD` | Account and keyboard layout, default `rocky` and `us`; set before creating the seed. |
| `OVMF_CODE`, `OVMF_VARS` | Matching firmware code and VARS template. |
| `QCOW2_ARGS` | Extra developer boot flags, such as `-m 6G`. |

Changing account variables does not reprovision an existing seed or disk. For
example, `make qcow2 QCOW2_ARGS="-m 6G"` adjusts runtime settings while
preserving the existing VM files.

### Firmware, screen output, and shutdown

Gateway RDP displays firmware graphics through virtio-gpu. Firmware input uses
PS/2; once the guest initializes virtio-input, pointer input switches to an
absolute tablet to avoid acceleration drift. The keyboard continues through PS/2.
If the guest resets its tablet driver, pointer input returns to PS/2 until the
tablet is ready again.

Gateway-managed RDP VMs have no serial UART or host terminal input. Host
terminal messages are devbox-kvm diagnostics. OVMF's virtio-gpu graphics protocol does not
provide a directly mapped linear framebuffer: after firmware exits, its last
frame remains until a guest graphics driver publishes another frame. Early
Linux text needs a guest-supported graphical console; devbox-kvm does not synthesize
it from serial output.

The Rocky example image's bootloader uses `console=ttyS0,115200n8`. Change its
installed arguments to `console=tty0` for screen output once Linux has a
graphical console. UEFI mode does not rewrite these arguments. Gateway RDP shows
the current screen rather than replaying missed screens.

Shut down from the guest when possible. For UEFI guests, sending SIGUSR1 to the
`devbox-kvm boot` process requests an ACPI power-button event; the guest decides how
to handle it. SIGINT/SIGTERM, including Ctrl+C, stop
the VM and close its disks and VARS store; they do not perform an orderly guest
OS shutdown. The serial shortcut **Ctrl-a, then x** is available for standalone
serial-console boots.
A guest-requested reset closes and recreates the VM using the same disk and VARS;
reconnect the RDP client afterward.

Large EFI applications can hit OVMF's virtio-blk limit of 1 GiB per read, returning
`EFI_BAD_BUFFER_SIZE`; firmware may show this as `Not Found`. Use a smaller EFI
application or a bootloader that reads large payloads in chunks.
See [EDK2's virtio-blk request validation](https://github.com/tianocore/edk2/blob/master/OvmfPkg/VirtioBlkDxe/VirtioBlk.c).

## Direct Linux and PVH boot

These modes load a Linux kernel directly; they do not run BIOS/UEFI bootloader
code. Their built-in kernel arguments describe devbox-kvm's direct-boot environment.
Use `-p` to replace those arguments when a guest needs a different root device or
init program. UEFI disk boot instead preserves the installed bootloader's
configuration.

```bash
./devbox-kvm boot -k /path/to/bzImage -i /path/to/initrd
```

This starts a serial-console VM. A kernel with a PVH entry point selects the PVH
loader automatically. Press **Ctrl-a, then x** to exit. Supply your own compatible
kernel and initrd; the repository no longer downloads or builds them and does
not include a sample kernel configuration. The kernel and initrd must include
the drivers needed for the guest's root filesystem and the virtual devices it
uses, such as virtio-block for an attached disk.

Direct Linux and PVH loading remain available through the CLI. Gateway-managed
desktops use QCOW2/UEFI instead: their disk's EFI bootloader loads its own kernel
and initrd. The required CI boot check also uses QCOW2/UEFI, but with a small EFI
test application rather than a Linux guest. Neither path needs standalone
kernel/initrd files.

## Storage and cloud-init

`-d` attaches writable raw or qcow2 storage; its format is detected from its
contents. The primary disk appears as `/dev/vda`. Attaching a disk does not select
a disk bootloader: use `-uefi`, or provide a direct kernel and any required initrd.

```bash
./devbox-kvm boot -k /path/to/bzImage -i /path/to/initrd -d ./disk.qcow2
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
clusters, external data files, persistent bitmaps, and extended L2 entries are
unsupported. Images marked dirty or corrupt are rejected. Opening an image
validates header fields, table bounds, mapped-cluster refcounts, and cluster
ownership; overlapping metadata and guest data are rejected before any writes.
L1 tables are limited to 32 MiB and refcount tables to 8 MiB.

Failed metadata updates restore the previous mapping before reclaiming newly
allocated clusters. If recovery also fails, the open image rejects further
writes and reports the failure on flush; it may need offline repair. This
recovery handles reported I/O errors and does not provide a power-loss journal.

Convert an unsupported image to a new standalone file with `qemu-img` before
attaching it; preserve the original:

```bash
qemu-img convert -f qcow2 -O qcow2 source.qcow2 guest.qcow2
```

### NoCloud seed

For an image that already has systemd, cloud-init, a desktop, a display manager,
and virtio drivers, create a fresh per-VM VARS copy and account seed:

```bash
cp /usr/share/OVMF/OVMF_VARS_4M.fd ./guest_VARS.fd
./devbox-kvm seed -user rocky -hostname workstation -keyboard us -out seed.iso
# Read seed.iso.credentials.json for the generated guest login.
./devbox-kvm boot -uefi /usr/share/OVMF/OVMF_CODE_4M.fd \
  -uefi-vars ./guest_VARS.fd -d ./guest.qcow2 -cloud-init ./seed.iso \
  -m 4G -c 2 -net user
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

## Remote display and input

### Gateway RDP console

No guest XRDP daemon is needed: RDP presents the VM's screen and injects keyboard
and pointer input. Sign in to the gateway UI, start your desktop, select **RDP**,
and open the downloaded file within two minutes:

```bash
xfreerdp /path/to/desktop.rdp
```

The executable may be called `xfreerdp3`. Enter your current gateway password
when prompted, then sign in to Linux using the guest account. Download a fresh
file when reconnecting. The gateway checks the routing grant, account credentials,
and VM ownership before granting console access; see the
[gateway authentication flow](gateway.md). NLA/CredSSP is unsupported; use TLS
as configured in the downloaded file.

Each managed VM exposes only a private Unix socket, not a TCP listener. The
backend verifies the connecting gateway process using kernel peer credentials.
The inherited worker descriptor is not a public connection option, and no
standalone RDP bind address or unsafe override is supported. The VM accepts one
RDP client at a time; another can connect after it disconnects.

Managed RDP desktops disable the serial port, serial text overlay, and host
terminal input. UEFI boots leave installed bootloader arguments unchanged.

Managed VMs offer AVC420 with bitmap fallback in 16-, 24-, or 32-bit color.
Display output uses virtio-gpu. Clipboard,
drive redirection, microphone input, and multiple monitors are not implemented.
The documented client setup uses FreeRDP; Microsoft Remote Desktop has not been
validated.

### RDP graphics profiles

Managed desktops default to `RDP_PROFILE=office` for a 30 FPS AVC420 cap. Use
`smooth` for a 60 FPS cap; bitmap output remains capped at 30 FPS with either
profile. For example, with gateway prerequisites configured:

```bash
RDP_PROFILE=office make gateway
```

Configure the gateway with `RDP_PROFILE` or `serve -rdp-profile`; only `office`
and `smooth` are accepted. The selected
H.264 cap is applied before copying the framebuffer for encoding and converting
colors. H.264 still encodes a full picture for each changed output frame:
the office cap reduces how often this work runs rather than making the codec
encode individual dirty rectangles. Pending damage is combined into the next
frame; unchanged screens do not incur periodic encoding. Input, cursor updates,
and audio are not rate-limited by the graphics profile.

Use `smooth` when 60 FPS AVC420 animation is worth the higher encoding load. A profile
sets an upper bound, not a guaranteed frame rate. Changing a managed desktop's
profile requires restarting its VM.
See the [four-stream work-budget measurements](benchmarks/office-profile-2026-10-12/README.md)
for a reproducible comparison and its limits.

### Guest graphics

Virtio-gpu defaults to a 2D framebuffer. Optional virglrenderer support enables
OpenGL for Linux guests using Mesa's virgl driver. The host renders submitted
3D commands through EGL, and completed frames are read back into the existing
RDP framebuffer. This path accelerates guest rendering independently of the
selected H.264 encoder; it does not provide GPU passthrough, Vulkan, or zero-copy
video encoding.

On Debian/Ubuntu, build the optional backend with:

```bash
sudo apt install build-essential pkg-config libvirglrenderer-dev libegl-dev libgbm-dev
make devbox-kvm-virgl
# Or: CGO_ENABLED=1 go build -tags virgl -o devbox-kvm .
```

For both guest OpenGL and NVENC, also install the NVENC build dependencies below
and use `CGO_ENABLED=1 go build -tags 'nvenc virgl' -o devbox-kvm .`.
`make gateway-virgl` builds both backends before starting the gateway.
A normal `CGO_ENABLED=0` build remains supported with 3D disabled.

Enable the renderer in the gateway service environment, with the usual gateway
settings already configured:

```bash
VM_GPU_3D=true ./devbox-kvm serve
# Disable guest 3D, including in a binary compiled with virgl support:
VM_GPU_3D=false ./devbox-kvm serve
```

The same environment variable applies to direct `boot` commands, which attach
a headless 3D GPU when enabled and retain the serial console without requiring
an RDP listener. Explicit
`serve -gpu-3d=false` or `boot -gpu-3d=false` overrides the environment. The
gateway forwards its resolved boolean to every worker, so a false override
cannot accidentally be re-enabled by the worker's inherited environment.
When enabled, an unavailable build or a failed renderer initialization is an
error. Setting `false` skips renderer initialization entirely.

The host needs working EGL/OpenGL drivers and access to its DRM render node,
usually `/dev/dri/renderD128`. Grant the service user the node's group access
and restart its service/session after group membership changes. No X server or
host graphical session is needed. Containers must pass through the render node
and its group; see [Docker guest OpenGL](docker.md#guest-opengl).

The guest needs `CONFIG_DRM_VIRTIO_GPU` and Mesa with the virgl Gallium driver.
Inside the guest's graphical session, run `glxinfo -B` (usually supplied by
`mesa-utils`) and check that the OpenGL renderer reports `virgl`. Then run a
small OpenGL application and view it through RDP. A renderer containing
`llvmpipe` or `softpipe` indicates software rendering; check host drivers/device
access as well as guest Mesa. Confirm host GPU activity while rendering before
claiming hardware acceleration: a virgl renderer can itself use a host software
EGL implementation.

Native 3D textures and buffers share a 2 GiB accounting budget. This allows
GNOME wallpaper transitions to retain previous textures, the incoming image,
its mipmaps, and an upload buffer at the same time. Guest-memory backing has a
separate 512 MiB limit; these limits do not preallocate memory or change the
worker's enforced cgroup memory limit.

Apply a changed gateway setting by restarting the gateway and starting fresh
VM workers. Reconnecting RDP or rebooting only the guest keeps its existing
virtual GPU configuration.

For framebuffer console output, a custom Linux kernel needs
`CONFIG_DRM_VIRTIO_GPU`, `CONFIG_DRM_FBDEV_EMULATION`, and
`CONFIG_FRAMEBUFFER_CONSOLE` built in.

### Resolution changes

```bash
xfreerdp /path/to/desktop.rdp /size:1280x720 /dynamic-resolution /gfx:AVC420
```

Omit `/gfx:AVC420` for bitmap mode. Both paths support live single-monitor
resizing without reconnecting; input remains on the connection. Rapid
changes are combined over 150 ms. Dimensions must each be 200–4096 pixels, with
at most 4096×2160 total pixels; odd values round down to even values. Unsupported
layouts leave the current size active.

RDP controls the GPU's preferred mode, but the guest must apply the
virtio-gpu DRM/RandR hotplug notification to change its desktop resolution.
Otherwise its previous image is scaled to the RDP window. Linux may round mode
widths to an 8-pixel boundary.
Pointer coordinates use the current framebuffer size during mode changes.

For X11 desktops without automatic preferred-mode handling, such as Fluxbox,
copy [scripts/devbox-kvm-resize](../scripts/devbox-kvm-resize) into the guest and run it
from its graphical session:

```bash
sh ./devbox-kvm-resize --once  # Apply once.
sh ./devbox-kvm-resize &       # Follow changes once per second.
```

The helper requires `xrandr` and handles one connected `Virtual-*` output. For
Fluxbox autostart, place it at `~/.local/bin/devbox-kvm-resize` and add
`sh "$HOME/.local/bin/devbox-kvm-resize" &` before Fluxbox starts in
`~/.fluxbox/startup`. Desktops already handling preferred modes need no helper.

## H.264 and hardware encoding

The default software backend is the bundled [OpenH264 port](../pkg/h264).
AVC420 requires no C compiler, native codec library, or special build tag;
selected amd64 codec kernels use SIMD with scalar fallbacks.

```bash
CGO_ENABLED=0 go build -o devbox-kvm .
```

Follow the [software-only gateway setup](gateway.md#build-without-nvenc) and set
`H264_ENCODER=software`. All RDP connections still use the gateway UI's downloaded
files.

Clients without AVC420 support receive bitmap updates. AVC420 uses lossy YUV
4:2:0, so small colored text can appear softer than bitmap output. AVC444 is
not implemented. Graphics encoding does not change console authentication.

Set `H264_QP` (or `serve -h264-qp`) to control H.264 quantization. The default is
`20`, the minimum is `0`, and the maximum is `51`, inclusive. Lower QP improves
quality and increases traffic; higher QP compresses more and reduces quality.
QP `0` is the highest supported quality, but AVC420 chroma subsampling still
precludes a promise of lossless RGB. QP is not a guaranteed Mbps cap.

The gateway passes the value to each worker with `-rdp-h264-qp`. Software and
NVENC use the same value, including when `auto` falls back to software. Changing
it requires restarting the gateway and starting new VM processes; reconnecting
RDP or rebooting within the guest does not update the worker's setting.

### Optional NVIDIA NVENC

Build with cgo and `-tags nvenc` to encode through FFmpeg's `libavcodec`. On
Debian/Ubuntu:

```bash
sudo apt install build-essential pkg-config libavcodec-dev libavutil-dev
CGO_ENABLED=1 go build -tags nvenc -o devbox-kvm .
```

Select `H264_ENCODER=nvenc` to require hardware encoding, or `auto` to allow
software fallback, in the [gateway configuration](configuration.md).

Runtime requires an NVIDIA GPU with H.264 NVENC support, a working NVIDIA
driver, and FFmpeg libraries containing `h264_nvenc`. devbox-kvm links to these
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
remain on the CPU. Software slice settings do not affect NVENC, which reports
zero software slices. Intel VA-API and Quick Sync backends are not
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

Open a newly downloaded gateway file with the local client:

```bash
./tools/freerdp/bin/xfreerdp /path/to/desktop.rdp
```

The old direct `make rdp` helper is removed. Do not replace the downloaded file's
routing and authentication settings with a direct VM address or dummy credentials.

For X11 cursor rendering, check `WITH_XCURSOR=ON`. Virtio-gpu hardware cursor
shapes, hotspots, and positions are separate RDP updates, so pointer-only motion
does not encode desktop video. Shapes are cached, transparent padding is
trimmed, and large shapes are reduced to the supported 32×32 size. Older clients
receive a color cursor without alpha blending. Software cursors remain in the
image; clients without Xcursor support can show a duplicate local pointer.

### Performance measurements

Use the gateway's per-VM performance view and telemetry to compare bitmap and
AVC420 sessions. Measurements include the selected backend, source/output
dimensions, frame rate, payload bytes, software slices, and time spent copying,
converting, encoding, and writing. See the [performance guide](performance.md)
for sampling and optional CPU profiling. The worker's `-rdp-stats` flag is an
internal diagnostic option, not a standalone RDP startup mode.

`pacing` is the permitted frame interval; `ack_turnaround` samples the smoothed
send-to-acknowledgement time, including client processing. `ack_wait` measures
time waiting for a free acknowledgement slot after the pacing deadline. Zero
`ack_wait` does not rule out reduced frame rate from adaptive pacing or guest
rendering. Reported frame rate includes idle time in the reporting window.

The gateway's default software encoder uses one slice/worker. The codec can
schedule fixed slices on at most four workers and may use fewer for small
pictures. More slices can reduce encoding time while adding headers and reducing
compression; NVENC does not use software slices. Slice-count tuning is an
internal worker option, not an exposed gateway setting.

Changed H.264 frames can be sent at up to 60 fps; bitmap output is capped at
30 fps. Dirty rectangles reduce pixel conversion/copying, bitmap updates use
64×64 tiles, and a slow client receives the latest frame rather than an expanding
queue. H.264 still encodes a complete picture using references to previous
frames. IDR frames are sent for initialization, resize, refresh, and recovery;
the software path disables deblocking to reduce latency. A two-frame decode
window and acknowledgement-based pacing also bound the work sent to a client.

Browser rendering can remain CPU-bound inside the guest when `VM_GPU_3D=false`,
even when host NVENC is active. Check the active encoder in the
gateway's **Performance** view before attributing choppy scrolling to hardware
encoding. Managed worker output is discarded rather than saved in console logs.

## Audio

Managed gateway desktops enable sound playback by default through their RDP
connection. The gateway starts each VM with a virtio-snd sound card and streams
stereo, signed 16-bit PCM at 48 kHz to the connected client. No host sound card,
audio-device passthrough, or additional gateway setting is required.

After upgrading, stop and start an existing desktop from the dashboard to create
a worker with audio enabled. Reconnecting or rebooting inside an already-running
guest does not change that worker's audio configuration.

Playback requires both:

- A guest kernel with the Linux `snd_virtio` driver
  ([`CONFIG_SND_VIRTIO`](https://github.com/torvalds/linux/blob/master/sound/virtio/Kconfig))
  and a working desktop audio stack. Select the virtio sound card in the guest's
  sound settings. Images without this driver can still boot, but cannot play
  audio through the virtual sound card; provisioning does not install missing
  guest drivers or audio packages.
- An RDP client with RDPSND playback support and local sound enabled. Downloaded
  connection files request playback on the client with `audiomode:i:0` and
  explicitly disable microphone redirection with `audiocapturemode:i:0`.
  See Microsoft's [RDP audio settings](https://learn.microsoft.com/en-us/azure/virtual-desktop/rdp-properties#audiomode).

Clients that disable sound or do not support the offered PCM format can still
use the desktop. Playback buffers are bounded, and audio is discarded when no
client is listening; reconnecting negotiates a fresh audio stream instead of
replaying disconnected audio. Microphone capture is unsupported.

## Diagnostics and compatibility checks

`devbox-kvm probe` reports host KVM capabilities and supported CPUID information.
For device debugging, set these environment variables to any nonempty value:

| Variable | Output |
| --- | --- |
| `DEVBOX_KVM_TRACE_IO` | Guest port I/O exits. |
| `DEVBOX_KVM_TRACE_MMIO` | Unhandled guest MMIO accesses. |

These trace variables are intended for direct developer boots. Gateway-managed
workers discard standard output and standard error; use the gateway performance
view and diagnostics download for managed desktops.

The following checks are opt-in and start test guests or exercise hardware.

For firmware boot, persistent VARS across a guest reset, and shutdown:

```bash
OVMF_CODE=/usr/share/OVMF/OVMF_CODE_4M.fd \
OVMF_VARS=/usr/share/OVMF/OVMF_VARS_4M.fd tools/test-uefi.sh
```

[tools/test-uefi.sh](../tools/test-uefi.sh) requires KVM, GNU-EFI development
files (`gnu-efi`), a C compiler/binutils, mtools, `qemu-img`, and Python 3. It
builds an EFI fixture and boots isolated raw/qcow2 disks with a serial console;
source firmware is untouched. Failures retain logs, disks, and firmware variables.
Set `UEFI_KEEP_ARTIFACTS=1` to retain successful runs, `UEFI_CPUS=4` to change CPU
count, or `DEVBOX_KVM_BINARY=/path/to/devbox-kvm` for an existing binary. Alternate GNU-EFI
locations use `EFI_INCLUDE_DIR` and `EFI_LIB_DIR`. This checks the firmware
platform, not every guest OS.

For native virgl transfers, reset/fence completion, and readback into the RDP
framebuffer, with the virgl build dependencies and working host EGL:

```bash
DEVBOX_KVM_TEST_VIRGL=1 CGO_ENABLED=1 go test -tags 'virgl integration' \
  ./internal/virgl ./internal/virtio -run 'TestNative|TestGPUVirgl.*Integration' \
  -count=1 -timeout 2m -v
```

This exercises the native renderer without starting a guest. It can run with a
software host EGL driver and therefore does not prove hardware acceleration;
also verify the renderer and GPU activity in a real guest as described above.

For actual NVIDIA encoding, with `ffmpeg` and the NVENC build dependencies:

```bash
DEVBOX_KVM_TEST_NVENC=1 CGO_ENABLED=1 go test -tags 'nvenc integration' \
  ./internal/rdp/avc -run '^TestNVENCHardware' -count=1 -timeout 2m -v
```

This checks independent decoding, forced IDR refreshes, non-macroblock-aligned
sizes, full-range BT.709 metadata, and output buffer ownership.
