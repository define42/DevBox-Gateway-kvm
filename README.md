# gokvm

[![Go Version](https://img.shields.io/github/go-mod/go-version/define42/gokvm)](go.mod) [![License](https://img.shields.io/github/license/define42/gokvm)](LICENSE) [![CI](https://github.com/define42/gokvm/actions/workflows/ci.yml/badge.svg)](https://github.com/define42/gokvm/actions/workflows/ci.yml) [![Coverage Status](https://coveralls.io/repos/github/define42/gokvm/badge.svg?branch=main)](https://coveralls.io/github/define42/gokvm?branch=main) [![Go Reference](https://pkg.go.dev/badge/github.com/define42/gokvm.svg)](https://pkg.go.dev/github.com/define42/gokvm) [![Go Report Card](https://goreportcard.com/badge/github.com/define42/gokvm)](https://goreportcard.com/report/github.com/define42/gokvm)

gokvm is an experimental virtual machine monitor written in Go that boots Linux
guests using KVM on x86-64 Linux hosts. It provides emulated devices, a serial
console, and VNC/RDP desktops for exploring virtualization from user space.

**This is an experimental project, so please do not use it in production.**

![gokvm booting a Linux guest](demo.gif)

## Getting Started

### Requirements

- An x86-64 Linux host with hardware virtualization enabled and read/write access
  to `/dev/kvm`. Nested hosts must expose KVM to the environment running gokvm.
- Go 1.26.0 or newer to build from source, plus Git to clone the repository.
  The default build needs no C compiler or native codec libraries. Optional
  [NVIDIA NVENC encoding](#optional-nvidia-nvenc-encoding) requires cgo and FFmpeg
  development libraries.
- A Linux kernel and optional initrd, a supported Linux live ISO, or an x86-64
  Unified Kernel Image (UKI). VNC and RDP
  also require a client on the machine where you want to view the guest.

### Installation

Build the current checkout with cgo disabled:

```bash
git clone https://github.com/define42/gokvm.git
cd gokvm
CGO_ENABLED=0 go build -o gokvm .
./gokvm boot -help
```

For packaged versions, see [GitHub Releases](https://github.com/define42/gokvm/releases).
Extract the Linux amd64 archive and run its `gokvm` executable. The examples below
describe the current source tree.

### Boot a guest

With `TinyCore-current.iso` saved in the working directory, start a desktop:

```bash
./gokvm boot -iso ./TinyCore-current.iso -m 512M -vnc 127.0.0.1:5900
```

Connect a VNC client to `127.0.0.1:5900`. The TinyCore desktop starts automatically.
From a source checkout, `make tinycore` builds gokvm and runs the same example;
it expects the ISO to exist locally. VNC has no authentication or encryption,
so keep it on loopback and use an authenticated SSH tunnel for remote access.

To boot an existing kernel and initrd with a serial console:

```bash
./gokvm boot -k ./bzImage -i ./initrd
```

Press **Ctrl-a, then x** in the host terminal to exit. The defaults are one guest
CPU and 1 GiB of RAM; use `-c` and `-m` to change them. Networking, graphics, and
audio are disabled until requested. Run `./gokvm probe` to inspect the host's KVM
capabilities and supported CPUID information.

The repository also provides `make bzImage initrd` to prepare its sample kernel
and u-root initrd. These targets download build tools and guest artifacts and may
compile a kernel locally. See the [Makefile](Makefile),
[kernel script](scripts/get_kernel.bash), and [initrd script](scripts/get_initrd.bash)
for their additional host dependencies. Both sample kernel configurations enable
KVM guest clocks and paravirtual spinlocks. Rebuild existing kernel images to
apply configuration changes; changing the config files alone does not update an
already built kernel.

## Features

| Area | Support |
| --- | --- |
| CPU and boot | KVM acceleration, multiple vCPUs, direct Linux and PVH boot, x86-64 UKI loading, Linux ISO discovery through El Torito and boot configs |
| Storage | Modern virtio-blk, writable raw/qcow2 disks, read-only ISO media |
| Networking | Modern virtio-net, built-in IPv4 DHCP/DNS and outbound TCP/UDP, or a host-managed TAP interface |
| Display and input | Modern virtio-gpu 2D framebuffer, PNG output, VNC/RDP keyboard and mouse input |
| RDP graphics | TLS, bitmap updates or H.264 AVC420 with bundled software encoding or optional NVIDIA NVENC, hardware cursor updates, live single-monitor resizing |
| Audio | Virtio-snd stereo playback through RDP |
| Console and API | Serial console and Go wrappers for KVM ioctls |

### CLI reference

Use `gokvm boot [flags]` to start a VM and `gokvm probe` to inspect the host.
`gokvm boot -help` prints every boot option. VM configuration is supplied through
flags; there is no VM configuration file.

| Flag | Default | Purpose |
| --- | --- | --- |
| `-D path` | `/dev/kvm` | KVM device used by `boot`. |
| `-k path` | `./bzImage` | Linux kernel image; PVH is detected automatically. |
| `-i path` | Unset | Optional initrd for direct kernel boot. |
| `-iso path-or-url` | Unset | Linux ISO path or HTTP(S) URL; takes precedence over `-k` and `-i`. |
| `-uki path-or-url` | Unset | x86-64 Unified Kernel Image; cannot be combined with `-iso`, `-k`, or `-i`. |
| `-p parameters` | Built-in kernel command line | Replace the kernel command line; ISO/UKI boot otherwise uses the image's arguments plus gokvm defaults. |
| `-c count` | `1` | Number of guest vCPUs, from 1 to 64. |
| `-m size` | `1G` | Guest RAM; `K`, `M`, and `G` use binary units. Unitless values mean GiB. |
| `-d path` | Unset | Existing writable raw or qcow2 disk, exposed as `/dev/vda`. |
| `-net user\|none` | `none` | Built-in networking or no network; mutually exclusive with `-t`. |
| `-t name` | Unset | Host TAP interface. |
| `-g path` | Unset | Write the framebuffer to a PNG file. |
| `-vnc address` | Unset | VNC listener, for example `127.0.0.1:5900`. |
| `-rdp address` | Unset | TLS RDP listener, for example `127.0.0.1:3389`. |
| `-rdp-cert path` | Temporary self-signed certificate | PEM certificate; requires `-rdp-key` and `-rdp`. |
| `-rdp-key path` | Unset | PEM private key; requires `-rdp-cert` and `-rdp`. |
| `-rdp-h264` | `false` | Enable AVC420 for compatible clients; requires `-rdp`. |
| `-rdp-h264-encoder software\|auto\|nvenc` | `software` | H.264 backend; `auto` tries NVENC with software fallback, and `nvenc` requires working hardware encoding; requires `-rdp-h264`. |
| `-rdp-h264-threads count` | `0` | Software encoder: one automatic slice, or request 1–16 within the host CPU budget; ignored by NVENC; requires `-rdp-h264`. |
| `-rdp-stats` | `false` | Log RDP performance statistics; requires `-rdp`. |
| `-audio none\|rdp` | `none` | Virtio-snd playback through RDP; `rdp` requires `-rdp`. |
| `-T count` | `0` | Instructions to skip between trace prints; zero disables tracing. |

Any of `-g`, `-vnc`, or `-rdp` enables the virtio-gpu device. They can be combined;
`-g` is not required for a remote console. See [RDP console](#rdp-console) for
connection, security, graphics, and audio details.

### Boot and storage

Direct boot accepts a Linux kernel with `-k` and an optional initrd with `-i`.
A kernel with a PVH entry point uses the PVH loader automatically. Use `-p` when
the guest requires a different root device, init program, or console setup;
`boot -help` shows the full default command line.

ISO boot support reads the El Torito boot catalog when present, uses the boot
image location to find common syslinux/isolinux or GRUB configs, then loads the
Linux kernel/initrd through gokvm's direct Linux loader. It does not emulate the
BIOS/UEFI bootloader code, so no SeaBIOS firmware is required. The raw ISO is
attached to the guest as a read-only virtio-blk device so the booted kernel can
mount its live media. This supports recognized Linux ISO layouts, not arbitrary
operating-system installers. An explicit `-p` replaces the ISO command line
entirely, including gokvm's added defaults.

`-iso` also accepts HTTP(S) URLs. The image is fully downloaded to a temporary
file for the lifetime of the VM; it is not cached between runs. For example:

```bash
./gokvm boot -iso http://www.tinycorelinux.net/17.x/x86/release/TinyCore-current.iso \
  -m 512M -vnc 127.0.0.1:5900
```

Boot a [NetDesk](https://github.com/define42/NetDesk) Unified Kernel Image:

```bash
./gokvm boot -uki ./netdesk.efi -m 6G -c 2 -net user \
  -rdp 127.0.0.1:3390
```

Connect an RDP client to `127.0.0.1:3390`, or replace `-rdp` with
`-vnc 127.0.0.1:5900` for VNC. The image contains the complete desktop and
needs no attached disk. Six GiB matches NetDesk's reference VM configuration;
allow additional host memory for gokvm and other applications.

For a local NetDesk checkout, `make netdesk` builds gokvm with cgo and the
`nvenc` tag, then boots `../NetDesk/dist/netdesk.efi` with 6 GiB RAM, two vCPUs,
user networking, and NVIDIA NVENC H.264 RDP on `127.0.0.1:3390`. Install the
[NVENC build dependencies and NVIDIA driver](#optional-nvidia-nvenc-encoding)
first. Connect with `make rdp` in another terminal.
Use `make netdesk NETDESK_UKI=/path/to/netdesk.efi` to select another image.

`-uki` reads a single-profile x86-64 PE32+ UKI's `.linux`, `.initrd`, and optional
`.cmdline` sections, then boots the embedded Linux bzImage directly. It does not
execute the EFI stub, provide UEFI services, or verify Secure Boot signatures.
Arbitrary EFI programs and UKIs that depend on EFI-stub features are unsupported.
The embedded command line is combined with gokvm's direct-boot defaults; an
explicit `-p` replaces it entirely. Kernel and initramfs payloads stream directly
into guest RAM. HTTP(S) URLs are also accepted; downloaded UKIs are removed once
their payloads have been loaded.

Guests larger than 3.25 GiB use separate low and high RAM regions, with the PCI
device address space reserved below 4 GiB. The requested `-m` size is RAM, so the
device address hole does not reduce it.

To run the optional NetDesk desktop smoke test with a local image and `/dev/kvm`:

```bash
GOKVM_NETDESK_UKI=/path/to/netdesk.efi go test -tags=integration ./vmm \
  -run '^TestNetDeskUKIDesktopBoot$' -timeout 6m -v
```

The test checks NetDesk's desktop readiness, graphics, VNC pointer and keyboard
input, and Chromium access to a local HTTP server through the guest network.

Attach an existing raw or qcow2 disk with `-d`:

```bash
./gokvm boot -k ./bzImage -i ./initrd -d ./disk.qcow2
```

The format is detected from the file contents. The disk is writable and appears
as `/dev/vda`; when combined with `-iso`, the read-only ISO follows it as another
virtio-blk device. `-d` attaches storage; a kernel or ISO is still needed to boot.
Block writes are synchronized before successful completion is reported to the
guest. Requests already pending in one queue batch share a sync; writeback caching
and the optional FLUSH command are not advertised.

The qcow2 backend supports standalone v2/v3 images with 16-bit refcounts.
Backing files, internal snapshots, encryption, compressed or shared data
clusters, external data files, and extended L2 entries are unsupported.
Dirty or corrupt images are rejected. To create a blank disk with `qemu-img`
(an optional host tool):

```bash
qemu-img create -f qcow2 disk.qcow2 4G
```

Create a filesystem in the guest before storing files on a new disk; partition
the disk first if needed.

### Desktop guests

When booting a TinyCore ISO with `-vnc` or `-rdp`, gokvm injects an autostart overlay into
the initrd so the guest brings up the FLWM desktop (Xvesa) on the remote display
instead of a text login. Both servers forward keyboard and mouse input back
to the guest. Try it with `make tinycore`, which builds gokvm and boots
the local `TinyCore-current.iso` on `127.0.0.1:5900`.

Slax can boot directly from its ISO with its normal graphical startup:

```bash
./gokvm boot -iso ./slax.iso -m 2G -rdp 127.0.0.1:3390
```

Run `make slax` to build gokvm with the software-only pure-Go H.264 encoder and
boot the local `slax.iso` with four guest CPUs, 2 GiB of memory, user-mode
networking, and H.264 RDP listening on `127.0.0.1:3390`. It requests four
encoder slices, bounded by the existing host-budget calculation. The bundled
codec encodes those fixed slices concurrently with bounded Go workers and needs
no native H.264 library.

With `make slax` running, use `make rdp` in another terminal to open FreeRDP and
connect to `127.0.0.1:3390` with automatic resizing. Run `make freerdp` first to
build the local H.264 client using the [dependencies below](#h264-graphics).
`make rdp` prefers `tools/freerdp/bin/xfreerdp` and requires H.264 AVC420 support.
Without the local build, it finds `xfreerdp3` or `xfreerdp` on your PATH and exits
with an error if that client lacks H.264. To use a custom client, run
`make rdp RDP_CLIENT=/path/to/xfreerdp`. The local connection accepts the
console's self-signed TLS certificate.

The Slackware-based Slax image with Linux 6.1.38 has been tested through to the
desktop. Keep the ISO's default boot parameters; no extracted kernel, custom
initrd, or driver blacklist is needed. For AVC420, add `-rdp-h264` as described
below; no special server build tag is required.

### Networking

Add `-net user` for networking inside the gokvm process. It needs no TAP device,
root privileges, host firewall rules, or helper daemon:

```bash
./gokvm boot -iso ./slax.iso -m 2G -net user \
  -rdp 127.0.0.1:3390 -rdp-h264
```

Use at least 2 GB for browser workloads. A 1 GB Slax guest can reach the desktop,
but complex websites can fill both RAM and Slax's compressed swap, making the
whole desktop unresponsive while RDP remains connected. Check `free -m` and
`vmstat 1` inside the guest when this happens; additional tabs may need more RAM.

Omit `-rdp-h264` to use bitmap updates. Slax requests an address
automatically: the first guest receives `10.0.2.15/24`, with gateway and DNS
server `10.0.2.2`. DNS uses the host's configured resolvers. Guest TCP/UDP
connections to `10.0.2.2` reach host loopback services, except the built-in DNS
and DHCP ports.

This backend supports outbound IPv4 TCP/UDP. Incoming port forwarding and IPv6
are not implemented. External ICMP/ping is not forwarded; check connectivity
with a web browser or `wget https://github.com` in the guest. The network stack
and default H.264 encoder are built in Go and work with `CGO_ENABLED=0`.

The tested Slax image lacks its default CA certificate bundle. If its HTTPS
tools report certificate errors, run these commands inside Slax, then retry:

```bash
update-ca-certificates
ln -s certs/ca-certificates.crt /etc/ssl/cert.pem
```

Networking remains disabled by default (`-net none`). The existing `-t tap0`
option attaches a host-managed TAP interface; it cannot be combined with `-net`.

### VNC and PNG output

Use `-vnc` for a remote framebuffer with keyboard and mouse input, or `-g` to
write the current framebuffer to a PNG file:

```bash
./gokvm boot -k ./bzImage -i ./initrd -vnc 127.0.0.1:5900
./gokvm boot -k ./bzImage -i ./initrd -g ./framebuffer.png
```

The bundled kernel config enables virtio-gpu and framebuffer console support, so
VNC shows the Linux framebuffer console once the guest probes the GPU. If you
use a different kernel, make sure it has `CONFIG_DRM_VIRTIO_GPU`,
`CONFIG_DRM_FBDEV_EMULATION`, and `CONFIG_FRAMEBUFFER_CONSOLE` built in.

VNC has no authentication or encryption. Bind to loopback and use an
authenticated SSH tunnel for access from another machine. Both VNC and PNG
outputs include the guest cursor. Virtio-gpu provides 2D display only; guest 3D
acceleration is not implemented.

### RDP console

RDP is implemented in Go inside gokvm; no xrdp daemon or guest RDP server is
needed. Build and start the bundled TinyCore desktop:

```bash
go build -o gokvm .
./gokvm boot -iso ./TinyCore-current.iso -rdp 127.0.0.1:3389 -m 512M
```

Connect using FreeRDP (the executable may be named `xfreerdp3` on your system):

```bash
xfreerdp /v:127.0.0.1:3389 /sec:tls /u:console /p:console /cert:ignore
```

The example accepts the temporary self-signed certificate for local testing.
To supply a persistent certificate, add `-rdp-cert console.crt -rdp-key console.key`
to the gokvm command; both files must be PEM encoded. In clients with a security
selector, choose TLS. NLA/CredSSP is not supported in this initial implementation.

**The console has no user authentication.** The example credentials are dummy
values and are not checked. TLS encrypts the connection; it does not restrict who
can control the VM. Keep the listener on loopback and use an authenticated SSH
tunnel for access from another machine.

The server accepts one RDP connection at a time, including during the handshake.
Additional connections are rejected; a new client can connect after the current
connection closes.

The RDP session uses the client's requested desktop size and supports live
single-monitor resizing. By default it sends uncompressed bitmap updates with
16-, 24-, or 32-bit color. The `-rdp-h264` option adds compressed AVC420 graphics.
It includes the same serial/VGA/VESA fallbacks as VNC. Both `-rdp` and `-vnc` can
be supplied to view the same guest at once; connected viewers share its keyboard
and mouse. Optional audio playback uses `-audio rdp`. Clipboard, microphone input,
drive redirection, and multiple monitors are not implemented. FreeRDP 3.32.1 has
been tested with TinyCore and Slax desktop rendering, pointer positioning, menu
interaction, and application launch; Microsoft Remote Desktop has not yet been validated.

In TinyCore, right-click the desktop to open its menu. The application dock
appears along the bottom of the screen.

#### Screen resolution

Set an initial size with `/size` and enable window-driven resizing with
`/dynamic-resolution` in FreeRDP:

```bash
xfreerdp /v:127.0.0.1:3390 /sec:tls /u:console /p \
  /cert:ignore /size:1280x720 /dynamic-resolution /gfx:AVC420
```

Omit `/gfx:AVC420` for bitmap clients. Both paths resize without reconnecting;
audio and input remain on the same connection. Rapid window changes are combined
over 150 ms. Each dimension must be 200–4096 pixels, with at most 4096×2160 total
pixels; odd dimensions round down to even pixels. Unsupported layouts leave the
current size active. Only one monitor is supported.

The active RDP session controls the GPU's preferred mode. Mouse coordinates
follow the current framebuffer size throughout a mode change.

The guest must use `virtio_gpu` and apply its DRM/RandR hotplug notification to
change the actual desktop resolution. A desktop that does not apply the mode
continues rendering at its previous size, scaled into the RDP window. The fixed
1024×768 VESA fallback used by older guests also scales; it cannot change modes.
Linux may round a preferred mode's width to an 8-pixel boundary.

For X11 desktops without automatic RandR mode handling, such as Fluxbox, copy
[`scripts/gokvm-resize`](scripts/gokvm-resize) into the guest and run it from a
terminal in the graphical session:

```bash
sh ./gokvm-resize --once  # Apply the current preferred mode once.
sh ./gokvm-resize &       # Follow future changes (checks once per second).
```

The helper requires `xrandr` and handles one connected `Virtual-*` output. To
start it with Slax's desktop, put the script at `~/.local/bin/gokvm-resize` and
add `sh "$HOME/.local/bin/gokvm-resize" &` before the line that starts Fluxbox in
`~/.fluxbox/startup`. Desktops that already apply preferred modes need no helper.

#### H.264 graphics

By default, the server uses the bundled pure-Go OpenH264 port in
[`pkg/h264`](pkg/h264) for H.264/AVC420 over the RDP graphics pipeline. This backend
needs no C compiler, `pkg-config`, native codec library, or special build tag.
A normal build includes
AVC420 support. On amd64, selected codec kernels use SIMD when supported by the
host CPU, with scalar fallbacks:

```bash
CGO_ENABLED=0 go build -o gokvm .
./gokvm boot -iso ./TinyCore-current.iso -rdp 127.0.0.1:3389 -rdp-h264 -m 512M
```

##### Optional NVIDIA NVENC encoding

Build with the `nvenc` tag to enable hardware H.264 encoding through FFmpeg's
`libavcodec`. On Debian or Ubuntu, install the build dependencies, then compile:

```bash
sudo apt install build-essential pkg-config libavcodec-dev libavutil-dev
CGO_ENABLED=1 go build -tags nvenc -o gokvm .
./gokvm boot -iso ./slax.iso -m 2G -net user \
  -rdp 127.0.0.1:3390 -rdp-h264 -rdp-h264-encoder nvenc -rdp-stats
```

The runtime needs an NVIDIA GPU with H.264 NVENC support, a working NVIDIA driver,
and FFmpeg libraries built with the `h264_nvenc` encoder. gokvm links to those
libraries; it does not start an `ffmpeg` subprocess. The FFmpeg command-line tool,
if installed, can check driver and encoder availability before booting a guest:

```bash
ffmpeg -hide_banner -f lavfi -i testsrc2=size=1280x720:rate=30 \
  -frames:v 3 -c:v h264_nvenc -f null -
```

`-rdp-h264-encoder nvenc` requires NVENC to initialize and encode successfully.
Use `-rdp-h264-encoder auto` to try NVENC and fall back to the bundled software
encoder if hardware initialization or encoding fails. The default `software`
always uses the bundled encoder, including in an NVENC-enabled build. A normal
build without the `nvenc` tag supports `software` and `auto`; explicitly selecting
`nvenc` reports that the backend is unavailable.

NVENC uses low-latency settings without B-frames or lookahead and preserves RDP
keyframes for resize and refresh. Framebuffer copying and RGB-to-YUV conversion
still run on the CPU. `-rdp-h264-threads` controls only the software encoder; NVENC
reports zero software slices in the statistics. Intel VA-API and Quick Sync
backends are not implemented.

##### Client support and performance

Use a FreeRDP build with H.264 decoding enabled:

```bash
xfreerdp /v:127.0.0.1:3389 /sec:tls /u:console /p:console /cert:ignore /gfx:AVC420
```

Check `xfreerdp /buildconfig` for `WITH_GFX_H264=ON`. A build with
`WITH_GFX_H264=OFF` rejects `/gfx:AVC420` during command-line parsing, even if
its help lists that option. Installing the OpenH264 library alone does not
enable codec support in an already-built FreeRDP client. Use a client built
with H.264 decoding, or omit `/gfx:AVC420` to connect using bitmap updates.
The interoperability tests used a separate H.264-enabled FreeRDP build.

To build FreeRDP 3.32.1 locally with OpenH264 decoding, install the following
build dependencies on Debian or Ubuntu:

```bash
sudo apt install build-essential cmake curl pkg-config libssl-dev libicu-dev libopenh264-dev \
  libx11-dev libxext-dev libxfixes-dev libxrandr-dev libxrender-dev libxcursor-dev \
  libxi-dev libxinerama-dev libxkbfile-dev libasound2-dev libpulse-dev libxv-dev \
  zlib1g-dev
make freerdp
```

The build downloads the pinned 3.32.1 release and verifies its SHA-256 checksum.
It uses four parallel build jobs by default; set `FREERDP_JOBS` to change this
(for example, `FREERDP_JOBS=8 make freerdp`).
Downloaded sources, build files, and the local installation are ignored by Git.
The client is installed under `tools/freerdp`; its FreeRDP libraries are in
`tools/freerdp/lib` and are found relative to the executable without setting
`LD_LIBRARY_PATH`. It uses system libraries for OpenH264, X11, and audio.
`make rdp` selects this build automatically and connects to the Slax console on
port 3390 with AVC420 enabled.

For the X11 client, also check for `WITH_XCURSOR=ON`. Virtio-gpu hardware cursors
are sent as separate RDP pointer updates, so the client draws them immediately
without waiting for a video frame. Shapes and hotspots are cached; moving the
hardware cursor alone does not encode desktop video. Transparent padding is
trimmed, and shapes larger than the supported 32×32 RDP pointer size are reduced.
Older clients receive a compatible color cursor without alpha blending.

Guest software cursors, including the VESA fallback, remain in the desktop
image and require the client's local cursor to be hidden. FreeRDP builds without
Xcursor support can show a second pointer in this case. VNC and PNG outputs
continue receiving an image with the guest cursor composed into it, including
when used alongside RDP.

The server logs the selected H.264 backend after negotiation. Clients without
AVC420 support receive bitmap updates automatically. Software AVC420 is available
in normal builds without shared codec libraries.

The bundled OpenH264 port can divide an RDP frame into fixed slices and encode
them concurrently. The default `-rdp-h264-threads 0` requests one slice and one
codec worker. Explicit values from 1 to 16 request a slice count; the bundled
codec schedules those slices on at most four workers. The slice count is fixed
for the session and bounded by the existing calculation using the Go runtime
CPU limit, guest vCPU count, and one CPU for host work, with a minimum of one.
The codec may use fewer slices and workers for small pictures. Frames remain
ordered, and a slow client receives the latest frame without building a queue.

Use `-rdp-stats` to log performance measurements every five seconds while frames
are active, plus a summary when the client disconnects. Statistics include frame
and compressed payload byte counts, actual slice count, average/maximum time
for framebuffer copying, color conversion, H.264 encoding and network writes,
and time spent waiting for client acknowledgements. Statistics are disabled by
default and also work with bitmap RDP. For example:

```bash
./gokvm boot -iso ./slax.iso -m 2G -net user \
  -rdp 127.0.0.1:3390 -rdp-h264 -rdp-h264-threads 1 -rdp-stats
```

Try slice counts of 1, 2, and 4 while scrolling the same page, then compare
encoding time, payload size, and responsiveness. Multiple slices reduce
software-encoding latency when CPU cores are available, while adding slice
headers and sometimes reducing compression. Guest rendering, color conversion,
and client acknowledgement waits can also limit frame rate. These options
require no changes to the RDP client command. `-rdp-h264-threads` requires
`-rdp-h264`; `-rdp-stats` requires `-rdp`.

AVC420 uses lossy YUV 4:2:0 compression, so small colored text can be softer than
bitmap output with either encoder. AVC444 is not implemented. TLS and
authentication behavior are the same as for bitmap RDP.

The H.264 path sends changed frames at up to 60 fps. The ordered RDP transport
uses IDR frames for stream initialization, resize, refresh, and codec recovery
instead of inserting periodic IDRs. The software encoder disables the H.264
deblocking filter to reduce encoding latency. Virtio-gpu dirty rectangles limit
pixel conversion and framebuffer copying to changed regions; the session retains
its image and catches up safely after skipping intermediate frames.
Bitmap output updates changed 64×64 tiles, while H.264 updates a persistent YUV
image and sends repaint rectangles with its compressed video. H.264 still
encodes a complete video picture, using references to earlier frames.

Frame notifications avoid waiting for a separate polling cycle, and a slow client
receives the latest available image when ready. The server keeps a two-frame
decode window and adapts H.264 pacing from normal frame-acknowledgment turnaround
and the client's reported queued bytes, backing off quickly and recovering
gradually. Resizing and refresh requests repaint the whole desktop.
Serial/VGA/VESA fallbacks still publish full images. Actual frame rate depends
on guest rendering, host CPU/GPU, and client decoding;
bitmap RDP remains capped at 30 fps.

Virtio-gpu currently provides a 2D framebuffer without guest 3D acceleration.
Complex browser pages can therefore be limited by software rendering inside
the guest even when AVC420 is active. For choppy scrolling, first check the
server's H.264 backend message to confirm the session is using AVC420.

#### Audio playback

Add `-audio rdp` to expose a virtio-snd card and play guest audio through the
connected RDP client. The tested Slax ISO already includes the Linux
`virtio_snd` driver. Audio and software H.264 need no native library or special
build tag in gokvm.

```bash
./gokvm boot -iso ./slax.iso -m 2G -net user \
  -rdp 127.0.0.1:3390 -rdp-h264 -audio rdp

xfreerdp /v:127.0.0.1:3390 /sec:tls /u:console /p \
  /cert:ignore /gfx:AVC420 /sound:sys:pulse,latency:40
```

The client must be built with a working audio output backend. Check
`xfreerdp /buildconfig` for `WITH_PULSE=ON` when using `sys:pulse`, or
`WITH_ALSA=ON` for `sys:alsa`. A client that logs `Loaded fake backend for rdpsnd`
cannot produce sound. PulseAudio-compatible PipeWire servers work with the
Pulse backend. Audio negotiation logs `client using PCM audio` on the server.
The 40 ms client buffer helps absorb scheduling jitter during playback.
The tested Pulse backend can briefly rebuffer when playback resumes after a
long silence.

Playback is stereo, signed 16-bit PCM at 48 kHz; guest ALSA/PulseAudio can convert
application formats. Use `aplay -l` in the guest to check that the card is present,
and `aplay -D plughw:0,0 example.wav` to test it directly. Microphone capture is
not implemented. Audio is off by default, and `-audio rdp` requires `-rdp`.
Disconnected or slow clients cannot stop the guest audio clock; old queued
samples are discarded to keep buffering bounded. VNC carries no audio.

### Go packages

The [`kvm`](kvm) package provides wrappers for KVM ioctls; [`vmm`](vmm) combines
them with guest boot and device setup. The bundled codec has a separate
[H.264 API and usage guide](pkg/h264/README.md). Browse the
[Go package reference](https://pkg.go.dev/github.com/define42/gokvm/kvm)
for the published KVM API.

## Contributing

Build from source using the [installation steps](#installation). Include a
reproduction or focused test for behavior changes, and update examples when
changing CLI options or guest requirements.

Run the RDP, graphics, input, and flag tests without booting a VM or enabling cgo:

```bash
CGO_ENABLED=0 go test -short ./internal/rdp/... ./virtio ./vmm ./flag
```

With the NVENC build dependencies installed, check the tagged build and unit tests
without requiring an NVIDIA GPU:

```bash
CGO_ENABLED=1 go build -tags nvenc ./...
CGO_ENABLED=1 go test -short -tags nvenc ./internal/rdp/... ./virtio ./vmm ./flag
```

To test actual NVIDIA encoding, install `ffmpeg` and run the opt-in hardware test:

```bash
GOKVM_TEST_NVENC=1 CGO_ENABLED=1 go test -tags 'nvenc integration' \
  ./internal/rdp/avc -run '^TestNVENCHardware' -count=1 -timeout 2m -v
```

This verifies independent decoding, forced IDR refreshes, non-macroblock-aligned
sizes, full-range BT.709 metadata, and output buffer ownership.

Check audio, networking, storage, ISO parsing, and the resize helper separately:

```bash
go test ./internal/audio ./internal/usernet ./disk ./iso9660 ./scripts
```

The disk tests use `qemu-img` and `qemu-io` when available; the AVC interoperability
test uses `ffmpeg` when available. Set `GOKVM_TEST_ISO` to a local ISO path to opt into the
ISO parser's real-image test.

For concurrency changes, run the relevant race checks (these require cgo and a
C compiler):

```bash
go test -race ./internal/audio ./internal/rdp/...
go test -race ./virtio ./vmm -run 'Test(RDP|GPU|MultiDisplay|Audio|Sound|Modern|Framebuffer|SerialMirror)'
```

`make golangci` runs code generation and the repository's pinned linter.
`make test` prepares guest images, runs generation and linting, then executes the
full test suite with coverage in a network namespace and checks module tidiness.
It needs the additional host tools in the [Makefile](Makefile) and
[CI workflow](.github/workflows/ci.yml), KVM access, and permission to create user
and network namespaces. Some VM integration tests also require root and guest
artifacts; they skip when their prerequisites are missing. Short mode alone does
not remove the KVM requirements of every package.

For device debugging, set these variables to any nonempty value:

| Variable | Output |
| --- | --- |
| `GOKVM_TRACE_IO` | Guest port I/O exits. |
| `GOKVM_TRACE_MMIO` | Unhandled guest MMIO accesses. |
| `GOKVM_TRACE_ATAPI` | Commands received by the emulated ATAPI device. |

## Contributors

gokvm builds on [Nobuhiro MIKI's original project](https://github.com/bobuhiro11/gokvm).
Thanks to everyone who contributes code, tests, bug reports, and documentation;
see the [contributors](https://github.com/define42/gokvm/graphs/contributors).

### References

Thanks to the many useful resources on KVM, this project was able to boot Linux on a virtual machine.

- [The Definitive KVM API Documentation](https://docs.kernel.org/virt/kvm/api.html#)
- [Using the KVM API, lwn.net](https://lwn.net/Articles/658511/)
- [kvmtest.c, lwn.net](https://lwn.net/Articles/658512/)
- [KVM tool](https://git.kernel.org/pub/scm/linux/kernel/git/will/kvmtool.git/about/)
- [kvm-hello-world](https://github.com/dpw/kvm-hello-world)
- [linux kvm-api: types,structures, consts](https://github.com/torvalds/linux/blob/master/include/uapi/linux/kvm.h)
- [aghosn/kvm.go](https://gist.github.com/aghosn/f72c8e8f53bf99c3c4117f49677ab0b9)
- [KVM HOST IN A FEW LINES OF CODE](https://zserge.com/posts/kvm/)
- [zserge/kvm-host.c](https://gist.github.com/zserge/ae9098a75b2b83a1299d19b79b5fe488)
- [CS 695: Virtualization and Cloud Computing, cse.iitb.ac.in](https://www.cse.iitb.ac.in/~cs695/)
- [The Linux/x86 Boot Protocol, kernel.org](https://www.kernel.org/doc/html/latest/x86/boot.html)
- [Build and run minimal Linux / Busybox systems in Qemu](https://gist.github.com/chrisdone/02e165a0004be33734ac2334f215380e)
- [kvm_cost.go, google/gvisor](https://github.com/google/gvisor/blob/master/pkg/sentry/platform/kvm/kvm_const.go)
- [Serial UART information, www.lammertbies.nl](https://www.lammertbies.nl/comm/info/serial-uart)
- [Virtual I/O Device (VIRTIO) Version 1.1](https://docs.oasis-open.org/virtio/virtio/v1.1/csprd01/virtio-v1.1-csprd01.html)
- [rust-vmm/vm-virtio](https://github.com/rust-vmm/vm-virtio/tree/main/crates/virtio-queue)
- [ハイパーバイザの作り方～ちゃんと理解する仮想化技術～ 第１１回 virtioによる準仮想化デバイス その１「virtioの概要とVirtio PCI」](https://syuu1228.github.io/howto_implement_hypervisor/part11.html)
- [ハイパーバイザの作り方～ちゃんと理解する仮想化技術～ 第１２回 virtioによる準仮想化デバイス その２「Virtqueueとvirtio-netの実現」](https://syuu1228.github.io/howto_implement_hypervisor/part12.html)
- [Xen PVH boot protocol](https://github.com/mirage/xen/blob/master/docs/misc/hvmlite.markdown)
- [Cloud Hypervisor](https://github.com/cloud-hypervisor/cloud-hypervisor)

## License

gokvm is licensed under the [MIT License](LICENSE). The bundled OpenH264 port is
covered by the [BSD 2-Clause license](LICENSE-OpenH264). The repository also
retains the [go.264 license notice](LICENSE-go.264).
