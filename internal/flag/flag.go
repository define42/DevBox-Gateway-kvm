package flag

import (
	"errors"
	"flag"
	"fmt"
	"strconv"
	"strings"

	"github.com/define42/DevBox-Gateway-kvm/internal/ebda"
	"github.com/define42/DevBox-Gateway-kvm/internal/rdp"
)

var ErrorInvalidSubcommands = errors.New("expected 'boot', 'probe', 'serve', 'seed', or 'auth-user' subcommands")

var ErrRDPOptions = errors.New("RDP certificate and key must be provided together with -rdp")

var ErrRDPH264 = errors.New("-rdp-h264 requires -rdp")

var ErrRDPProfile = errors.New("-rdp-profile must be office or smooth and requires -rdp")

var ErrRDPH264Encoder = errors.New("-rdp-h264-encoder must be software, nvenc, or auto and requires -rdp-h264")

var ErrRDPH264Threads = errors.New("-rdp-h264-threads must be 0 (auto) or 1..16 and requires -rdp-h264")

var ErrRDPStats = errors.New("-rdp-stats requires -rdp")

var ErrRDPWait = errors.New("-rdp-wait requires -rdp")

var ErrRDPFD = errors.New("-rdp-fd must be 0 (disabled) or an inherited TCP listener descriptor >=3 with -rdp")

var ErrTelemetryFD = errors.New(
	"-telemetry-fd must be 0 (disabled) or an inherited descriptor >=3 distinct from -rdp-fd",
)

var ErrNetwork = errors.New("-net must be 'user', 'user-isolated', or 'none' and cannot be combined with -t")

var ErrAudio = errors.New("-audio must be 'none' or 'rdp'; 'rdp' requires -rdp")

var ErrCPUCount = fmt.Errorf("-c must be between 1 and %d", ebda.MaxVCPUs)

var ErrUKIBootSource = errors.New("-uki cannot be combined with -iso, -k, or -i")

var ErrCloudInitDisk = errors.New("-cloud-init requires a primary disk supplied with -d")

type BootArgs struct {
	Kernel         string
	MemSize        int
	NCPUs          int
	Dev            string
	Initrd         string
	ISO            string
	UKI            string
	UEFI           string
	UEFIVars       string
	Params         string
	ParamsSet      bool
	TapIfName      string
	Network        string
	Disk           string
	CloudInit      string
	GPU            string
	VNC            string
	RDP            string
	RDPProfile     string
	RDPFD          int
	TelemetryFD    int
	RDPWait        bool
	RDPCert        string
	RDPKey         string
	RDPH264        bool
	RDPH264Encoder string
	RDPH264Threads int
	RDPStats       bool
	Audio          string
	TraceCount     int
}

func parseBootArgs(args []string) (*BootArgs, error) {
	bootCmd := flag.NewFlagSet("boot subcommand", flag.ExitOnError)
	c := &BootArgs{}

	bootCmd.StringVar(&c.Dev, "D", "/dev/kvm", "path of kvm device")
	bootCmd.StringVar(&c.Kernel, "k", "./bzImage", "kernel image path")
	bootCmd.StringVar(&c.Initrd, "i", "", "initrd path")
	bootCmd.StringVar(&c.ISO, "iso", "", "ISO image path or http(s) URL to boot")
	bootCmd.StringVar(&c.UKI, "uki", "", "x86-64 Unified Kernel Image path or http(s) URL to boot directly")
	bootCmd.StringVar(&c.UEFI, "uefi", "", "OVMF firmware code file for UEFI disk boot (requires -uefi-vars and -d)")
	bootCmd.StringVar(&c.UEFIVars, "uefi-vars", "", "existing writable per-VM copy of the matching OVMF variables file")
	//  refs: commit 1621292e73770aabbc146e72036de5e26f901e86 in kvmtool
	bootCmd.StringVar(&c.Params, "p", `console=tty0 console=ttyS0 earlyprintk=serial `+
		`noapic noacpi nowatchdog nmi_watchdog=0 mitigations=off lapic `+
		`pci=realloc=off `+
		`virtio_pci.force_legacy=1 rdinit=/init init=/init `+
		`gokvm.ipv4_addr=192.168.20.1/24`,
		"kernel command-line parameters")
	bootCmd.StringVar(&c.TapIfName, "t", "", `name of tap interface. `+
		`If the string is an empty, no tap intarface is created. (default"")`)
	bootCmd.StringVar(&c.Network, "net", "", "network backend: user (DHCP/DNS and outbound access), "+
		"user-isolated (also denies host loopback), or none (default); mutually exclusive with -t")
	bootCmd.StringVar(&c.Disk, "d", "", "path of disk file (for /dev/vda)")
	bootCmd.StringVar(&c.CloudInit, "cloud-init", "",
		"path to a NoCloud/CIDATA seed ISO attached as a read-only secondary disk (requires -d)")
	bootCmd.StringVar(&c.GPU, "g", "", `path to write the virtio-gpu framebuffer as PNG. `+
		`If empty, no virtio-gpu device is created. (default "")`)
	bootCmd.StringVar(&c.VNC, "vnc", "", `VNC listen address for virtio-gpu, for example ":5900". `+
		`If empty, no VNC server is created. (default "")`)
	bootCmd.StringVar(&c.RDP, "rdp", "", "RDP console listen address, for example 127.0.0.1:3389 "+
		"(one connection at a time, TLS, no authentication)")
	bootCmd.StringVar(&c.RDPProfile, "rdp-profile", rdp.ProfileSmooth,
		"RDP display profile: office (30 FPS) or smooth (60 FPS); requires -rdp")
	bootCmd.IntVar(&c.RDPFD, "rdp-fd", 0,
		"inherited TCP listener descriptor (>=3) matching -rdp; 0 opens a new listener")
	bootCmd.IntVar(&c.TelemetryFD, "telemetry-fd", 0,
		"private inherited Unix datagram descriptor (>=3); 0 disables managed performance telemetry")
	bootCmd.BoolVar(&c.RDPWait, "rdp-wait", false, "wait for an RDP client before starting the guest (requires -rdp)")
	bootCmd.StringVar(&c.RDPCert, "rdp-cert", "",
		"RDP TLS certificate PEM file (default: temporary self-signed certificate)")
	bootCmd.StringVar(&c.RDPKey, "rdp-key", "", "RDP TLS private key PEM file")
	bootCmd.BoolVar(&c.RDPH264, "rdp-h264", false,
		"enable H.264 AVC420 graphics for compatible RDP clients")
	bootCmd.StringVar(&c.RDPH264Encoder, "rdp-h264-encoder", "software",
		"H.264 encoder: software, nvenc, or auto (NVENC with software fallback); requires -rdp-h264")
	bootCmd.IntVar(&c.RDPH264Threads, "rdp-h264-threads", 0,
		"software H.264 slices: 0 selects one automatically, "+
			"1..16 requests a count within the host budget (requires -rdp-h264)")
	bootCmd.BoolVar(&c.RDPStats, "rdp-stats", false, "log RDP performance statistics every 5 seconds")
	bootCmd.StringVar(&c.Audio, "audio", "", "audio output: rdp (virtio-snd playback through RDP) or none (default)")

	bootCmd.IntVar(&c.NCPUs, "c", 1, fmt.Sprintf("number of cpus (1..%d)", ebda.MaxVCPUs))

	msize := bootCmd.String("m", "1G",
		"memory size: as number[gGmM], optional units, defaults to G")
	tc := bootCmd.String("T", "0",
		"how many instructions to skip between trace prints -- 0 means tracing disabled")

	var err error

	if err = bootCmd.Parse(args); err != nil {
		return nil, err
	}
	if c.NCPUs < 1 || c.NCPUs > ebda.MaxVCPUs {
		return nil, ErrCPUCount
	}
	var threadsSet, encoderSet, profileSet bool
	bootCmd.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "p":
			c.ParamsSet = true
		case "rdp-h264-threads":
			threadsSet = true
		case "rdp-h264-encoder":
			encoderSet = true
		case "rdp-profile":
			profileSet = true
		}
	})
	if c.CloudInit != "" && c.Disk == "" {
		return nil, ErrCloudInitDisk
	}
	if err := selectUEFIBootSource(bootCmd, c); err != nil {
		return nil, err
	}
	if err := selectUKIBootSource(bootCmd, c); err != nil {
		return nil, err
	}
	if err := validateRDPOptions(c, threadsSet, encoderSet, profileSet); err != nil {
		return nil, err
	}
	if err := validateTelemetryOptions(c); err != nil {
		return nil, err
	}
	if (c.Network != "" && c.Network != "none" && c.Network != "user" && c.Network != "user-isolated") ||
		(c.Network != "" && c.TapIfName != "") {
		return nil, ErrNetwork
	}
	if c.MemSize, err = ParseSize(*msize, "g"); err != nil {
		return nil, err
	}

	if c.TraceCount, err = ParseSize(*tc, ""); err != nil {
		return nil, err
	}

	return c, nil
}

func validateRDPOptions(c *BootArgs, threadsSet, encoderSet, profileSet bool) error {
	if _, err := rdp.ProfileFrameRate(c.RDPProfile); err != nil || c.RDPProfile == "" || (profileSet && c.RDP == "") {
		return ErrRDPProfile
	}
	if c.RDPFD != 0 && (c.RDPFD < 3 || c.RDP == "") {
		return ErrRDPFD
	}
	if (c.RDPCert == "") != (c.RDPKey == "") || (c.RDPCert != "" && c.RDP == "") {
		return ErrRDPOptions
	}
	if c.RDPWait && c.RDP == "" {
		return ErrRDPWait
	}
	if c.RDPH264 && c.RDP == "" {
		return ErrRDPH264
	}
	switch c.RDPH264Encoder {
	case "software", "nvenc", "auto":
	default:
		return ErrRDPH264Encoder
	}
	if encoderSet && !c.RDPH264 {
		return ErrRDPH264Encoder
	}
	if c.RDPH264Threads < 0 || c.RDPH264Threads > 16 || (threadsSet && !c.RDPH264) {
		return ErrRDPH264Threads
	}
	if c.RDPStats && c.RDP == "" {
		return ErrRDPStats
	}
	if (c.Audio != "" && c.Audio != "none" && c.Audio != "rdp") || (c.Audio == "rdp" && c.RDP == "") {
		return ErrAudio
	}

	return nil
}

func validateTelemetryOptions(c *BootArgs) error {
	if c.TelemetryFD != 0 && (c.TelemetryFD < 3 || c.TelemetryFD == c.RDPFD) {
		return ErrTelemetryFD
	}

	return nil
}

func selectUKIBootSource(cmd *flag.FlagSet, c *BootArgs) error {
	if c.UKI == "" {
		return nil
	}
	var explicitPayload bool
	cmd.Visit(func(f *flag.Flag) {
		if f.Name == "k" || f.Name == "i" {
			explicitPayload = true
		}
	})
	if c.ISO != "" || explicitPayload {
		return ErrUKIBootSource
	}
	// The CLI's default kernel does not apply to an embedded UKI payload.
	c.Kernel = ""

	return nil
}

type ProbeArgs struct{}

func parseProbeArgs(args []string) (*ProbeArgs, error) {
	probeCmd := flag.NewFlagSet("probe subcommand", flag.ExitOnError)
	c := &ProbeArgs{}

	if err := probeCmd.Parse(args); err != nil {
		return nil, err
	}

	return c, nil
}

func ParseArgs(args []string) (*BootArgs, *ProbeArgs, error) {
	if len(args) < 2 {
		return nil, nil, ErrorInvalidSubcommands
	}

	switch args[1] {
	case "boot":
		conf, err := parseBootArgs(args[2:])

		return conf, nil, err

	case "probe":
		conf, err := parseProbeArgs(args[2:])

		return nil, conf, err
	}

	return nil, nil, ErrorInvalidSubcommands
}

// ParseSize parses a size string as number[gGmMkK]. The multiplier is optional,
// and if not set, the unit passed in is used. The number can be any base and
// size.
func ParseSize(s, unit string) (int, error) {
	sz := strings.TrimRight(s, "gGmMkK")
	if len(sz) == 0 {
		return -1, fmt.Errorf("%q:can't parse as num[gGmMkK]:%w", s, strconv.ErrSyntax)
	}

	amt, err := strconv.ParseUint(sz, 0, 0)
	if err != nil {
		return -1, err
	}

	if len(s) > len(sz) {
		unit = s[len(sz):]
	}

	switch unit {
	case "G", "g":
		return int(amt) << 30, nil
	case "M", "m":
		return int(amt) << 20, nil
	case "K", "k":
		return int(amt) << 10, nil
	case "":
		return int(amt), nil
	}

	return -1, fmt.Errorf("can not parse %q as num[gGmMkK]:%w", s, strconv.ErrSyntax)
}
