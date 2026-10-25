package flag

import (
	"errors"
	"flag"
	"fmt"
	"strconv"
	"strings"

	"github.com/define42/DevBox-Gateway-kvm/internal/config"
	"github.com/define42/DevBox-Gateway-kvm/internal/ebda"
	"github.com/define42/DevBox-Gateway-kvm/internal/rdp"
	"github.com/define42/DevBox-Gateway-kvm/internal/rdp/avc"
	"github.com/define42/DevBox-Gateway-kvm/internal/usernet"
)

var ErrorInvalidSubcommands = errors.New("expected 'boot', 'probe', 'serve', 'seed', or 'auth-user' subcommands")

var ErrRDPOptions = errors.New("RDP certificate and key must be provided together with gateway-internal -rdp-fd")

var ErrRDPH264 = errors.New("-rdp-h264 requires gateway-internal -rdp-fd")

var ErrRDPProfile = errors.New("-rdp-profile must be office or smooth and requires gateway-internal -rdp-fd")

var ErrRDPH264Encoder = errors.New("-rdp-h264-encoder must be software or auto and requires -rdp-h264")

var ErrRDPH264QP = errors.New("-rdp-h264-qp must be between 0 and 51 and requires -rdp-h264")

var ErrRDPH264Threads = errors.New("-rdp-h264-threads must be 0 (auto) or 1..16 and requires -rdp-h264")

var ErrRDPStats = errors.New("-rdp-stats requires gateway-internal -rdp-fd")

var ErrRDPWait = errors.New("-rdp-wait requires gateway-internal -rdp-fd")

var ErrRDPFD = errors.New("-rdp-fd must be 0 (disabled) or an inherited gateway Unix listener descriptor >=3")

var ErrTelemetryFD = errors.New(
	"-telemetry-fd must be 0 (disabled) or an inherited descriptor >=3 distinct from -rdp-fd",
)

var ErrNetwork = errors.New("-net must be 'user', 'user-isolated', or 'none' and cannot be combined with -t")

var ErrAudio = errors.New("-audio must be 'none' or 'rdp'; 'rdp' requires gateway-internal -rdp-fd")

var ErrCPUCount = fmt.Errorf("-c must be between 1 and %d", ebda.MaxVCPUs)

var ErrCloudInitDisk = errors.New("-cloud-init requires a primary disk supplied with -d")

type BootArgs struct {
	Kernel            string
	MemSize           int
	NCPUs             int
	Dev               string
	Initrd            string
	UEFI              string
	UEFIVars          string
	Params            string
	ParamsSet         bool
	TapIfName         string
	Network           string
	NetworkPolicy     string
	NetworkAllowCIDRs string
	Disk              string
	DiskSizeBytes     int64
	CloudInit         string
	RDPProfile        string
	RDPFD             int
	TelemetryFD       int
	ProfileSocket     string
	RDPWait           bool
	RDPCert           string
	RDPKey            string
	RDPH264           bool
	RDPH264Encoder    string
	RDPH264QP         int
	RDPH264Threads    int
	RDPStats          bool
	Audio             string
	TraceCount        int
}

func parseBootArgs(args []string) (*BootArgs, error) {
	bootCmd := flag.NewFlagSet("boot subcommand", flag.ContinueOnError)
	c := &BootArgs{}
	settings := config.NewSettings()

	bootCmd.StringVar(&c.Dev, "D", "/dev/kvm", "path of kvm device")
	bootCmd.StringVar(&c.Kernel, "k", "./bzImage", "kernel image path")
	bootCmd.StringVar(&c.Initrd, "i", "", "initrd path")
	bootCmd.StringVar(&c.UEFI, "uefi", "", "OVMF firmware code file for UEFI disk boot (requires -uefi-vars and -d)")
	bootCmd.StringVar(&c.UEFIVars, "uefi-vars", "", "existing writable per-VM copy of the matching OVMF variables file")
	//  refs: commit 1621292e73770aabbc146e72036de5e26f901e86 in kvmtool
	bootCmd.StringVar(&c.Params, "p", `console=tty0 console=ttyS0 earlyprintk=serial `+
		`noapic noacpi nowatchdog nmi_watchdog=0 mitigations=off lapic `+
		`pci=realloc=off `+
		`virtio_pci.force_legacy=1 rdinit=/init init=/init `+
		`devbox-kvm.ipv4_addr=192.168.20.1/24`,
		"kernel command-line parameters")
	bootCmd.StringVar(&c.TapIfName, "t", "", `name of tap interface. `+
		`If the string is an empty, no tap intarface is created. (default"")`)
	bootCmd.StringVar(&c.Network, "net", "", "network backend: user (DHCP/DNS and outbound access), "+
		"user-isolated (public IPv4 by default), or none (default); mutually exclusive with -t")
	bootCmd.StringVar(&c.NetworkPolicy, "net-policy", settings.Get(config.VM_NETWORK_POLICY),
		"user-isolated destination policy: internet or allowlist ["+config.VM_NETWORK_POLICY+"]")
	bootCmd.StringVar(&c.NetworkAllowCIDRs, "net-allow-cidrs", settings.Get(config.VM_NETWORK_ALLOW_CIDRS),
		"user-isolated allowed IPv4 CIDRs, comma-separated; host alias requires 127.0.0.1/32 ["+
			config.VM_NETWORK_ALLOW_CIDRS+"]")
	bootCmd.StringVar(&c.Disk, "d", "", "path of QCOW2 disk file (for /dev/vda)")
	bootCmd.Int64Var(&c.DiskSizeBytes, "disk-size-bytes", 0,
		"fixed primary disk capacity in bytes; standalone resolves once if omitted")
	bootCmd.StringVar(&c.CloudInit, "cloud-init", "",
		"path to a NoCloud/CIDATA seed ISO attached as a read-only secondary disk (requires -d)")
	bootCmd.StringVar(&c.RDPProfile, "rdp-profile", rdp.ProfileSmooth,
		"gateway-internal RDP display profile: office or smooth (both 30 FPS); requires -rdp-fd")
	bootCmd.IntVar(&c.RDPFD, "rdp-fd", 0,
		"gateway-internal inherited Unix listener descriptor (>=3); 0 disables RDP")
	bootCmd.IntVar(&c.TelemetryFD, "telemetry-fd", 0,
		"private inherited Unix seqpacket descriptor (>=3); 0 disables managed performance telemetry")
	bootCmd.StringVar(&c.ProfileSocket, "profile-socket", "",
		"opt-in CPU profiling Unix socket in a private directory; empty disables profiling")
	bootCmd.BoolVar(&c.RDPWait, "rdp-wait", false,
		"gateway-internal wait for an RDP client before starting the guest (requires -rdp-fd)")
	bootCmd.StringVar(&c.RDPCert, "rdp-cert", "",
		"RDP TLS certificate PEM file (default: temporary self-signed certificate)")
	bootCmd.StringVar(&c.RDPKey, "rdp-key", "", "RDP TLS private key PEM file")
	bootCmd.BoolVar(&c.RDPH264, "rdp-h264", false,
		"enable H.264 AVC420 graphics for compatible RDP clients")
	bootCmd.StringVar(&c.RDPH264Encoder, "rdp-h264-encoder", "software",
		"H.264 encoder: software or auto (alias for the bundled pure Go encoder); requires -rdp-h264")
	bootCmd.IntVar(&c.RDPH264QP, "rdp-h264-qp", avc.DefaultQP,
		"H.264 quantization: min 0, max 51; lower means higher quality and more bandwidth (requires -rdp-h264)")
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
	if err := validateDiskOptions(c); err != nil {
		return nil, err
	}
	if c.NCPUs < 1 || c.NCPUs > ebda.MaxVCPUs {
		return nil, ErrCPUCount
	}
	var threadsSet, encoderSet, profileSet, qpSet bool
	bootCmd.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "p":
			c.ParamsSet = true
		case "rdp-h264-threads":
			threadsSet = true
		case "rdp-h264-encoder":
			encoderSet = true
		case "rdp-h264-qp":
			qpSet = true
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
	if err := validateRDPOptions(c, threadsSet, encoderSet, profileSet, qpSet); err != nil {
		return nil, err
	}
	if err := validateTelemetryOptions(c); err != nil {
		return nil, err
	}
	if (c.Network != "" && c.Network != "none" && c.Network != "user" && c.Network != "user-isolated") ||
		(c.Network != "" && c.TapIfName != "") {
		return nil, ErrNetwork
	}
	if err := usernet.ValidateDestinationPolicy(c.NetworkPolicy, c.NetworkAllowCIDRs); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNetwork, err)
	}
	if c.Network != "user-isolated" && (c.NetworkAllowCIDRs != "" ||
		(c.NetworkPolicy != "" && c.NetworkPolicy != "internet")) {
		return nil, fmt.Errorf("%w: destination policy options require user-isolated", ErrNetwork)
	}
	if c.MemSize, err = ParseSize(*msize, "g"); err != nil {
		return nil, err
	}

	if c.TraceCount, err = ParseSize(*tc, ""); err != nil {
		return nil, err
	}

	return c, nil
}

func validateRDPOptions(c *BootArgs, threadsSet, encoderSet, profileSet, qpSet bool) error {
	if c.RDPFD != 0 && c.RDPFD < 3 {
		return ErrRDPFD
	}
	if _, err := rdp.ProfileFrameRate(c.RDPProfile); err != nil || c.RDPProfile == "" || (profileSet && c.RDPFD == 0) {
		return ErrRDPProfile
	}
	if (c.RDPCert == "") != (c.RDPKey == "") || (c.RDPCert != "" && c.RDPFD == 0) {
		return ErrRDPOptions
	}
	if c.RDPWait && c.RDPFD == 0 {
		return ErrRDPWait
	}
	if err := validateRDPH264Options(c, threadsSet, encoderSet, qpSet); err != nil {
		return err
	}
	if c.RDPStats && c.RDPFD == 0 {
		return ErrRDPStats
	}
	if (c.Audio != "" && c.Audio != "none" && c.Audio != "rdp") || (c.Audio == "rdp" && c.RDPFD == 0) {
		return ErrAudio
	}

	return nil
}

func validateRDPH264Options(c *BootArgs, threadsSet, encoderSet, qpSet bool) error {
	if c.RDPH264 && c.RDPFD == 0 {
		return ErrRDPH264
	}
	switch c.RDPH264Encoder {
	case "software", "auto":
	default:
		return ErrRDPH264Encoder
	}
	if encoderSet && !c.RDPH264 {
		return ErrRDPH264Encoder
	}
	c.RDPH264Encoder = "software"
	if c.RDPH264Threads < 0 || c.RDPH264Threads > 16 || (threadsSet && !c.RDPH264) {
		return ErrRDPH264Threads
	}
	if _, err := avc.ResolveQP(&c.RDPH264QP); err != nil || (qpSet && !c.RDPH264) {
		return ErrRDPH264QP
	}

	return nil
}

func validateTelemetryOptions(c *BootArgs) error {
	if c.TelemetryFD != 0 && (c.TelemetryFD < 3 || c.TelemetryFD == c.RDPFD) {
		return ErrTelemetryFD
	}

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
