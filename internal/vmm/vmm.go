package vmm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/define42/DevBox-Gateway-kvm/internal/machine"
	"github.com/define42/DevBox-Gateway-kvm/internal/pvh"
	"github.com/define42/DevBox-Gateway-kvm/internal/rdp"
	"github.com/define42/DevBox-Gateway-kvm/internal/telemetry"
	"github.com/define42/DevBox-Gateway-kvm/internal/term"
	"github.com/define42/DevBox-Gateway-kvm/internal/virtio"
	"golang.org/x/sync/errgroup"
)

var errNetworkConfig = errors.New(
	"network must be 'user', 'user-isolated', or 'none' and cannot be combined with a TAP interface",
)

var errAudioConfig = errors.New("audio must be 'none' or 'rdp'; 'rdp' requires an RDP listener")

var errRDPH264ThreadsConfig = errors.New("RDP H.264 slices must be 0 (auto) or 1..16; a slice count requires H.264")

var errRDPH264EncoderConfig = errors.New("RDP H.264 encoder must be software, nvenc, or auto and requires H.264")

var errRDPProfileConfig = errors.New("RDP profile must be office or smooth and requires an RDP listener")

var errRDPStatsConfig = errors.New("RDP statistics require an RDP listener")

var errRDPWaitConfig = errors.New("RDP boot wait requires an RDP listener")

var errRDPListenerConfig = errors.New("RDP requires an inherited Unix listener and gateway process identity")

// Config selects a VM's local boot files, devices and host resources.
// Direct boot uses Kernel, optional Initrd and Params; UEFI boot instead uses
// UEFI, UEFIVars and Disk. CloudInit is an optional read-only NoCloud seed ISO,
// not a boot source. Init validates the supported combinations.
type Config struct {
	Debug          bool
	Dev            string
	Kernel         string
	Initrd         string
	UEFI           string
	UEFIVars       string
	Params         string
	ParamsSet      bool // Records an explicit Params value, including empty, for UEFI validation.
	TapIfName      string
	Network        string
	Disk           string
	CloudInit      string
	RDPProfile     string
	RDPWait        bool
	RDPCert        string
	RDPKey         string
	RDPH264        bool
	RDPH264Encoder string
	RDPH264Threads int
	RDPStats       bool
	Audio          string // Empty or "none" disables audio; "rdp" requires RDPListenerFile.
	NCPUs          int
	MemSize        int
	TraceCount     int
	// RDPListenerFile is borrowed from the caller, which keeps it open across
	// guest resets. Each display owns a duplicate; nil disables RDP.
	RDPListenerFile *os.File
	// Gateway identity is captured by the worker once and survives guest resets.
	RDPGatewayPID int
	RDPGatewayUID uint32
	// Telemetry is borrowed from the process owner and survives firmware resets.
	Telemetry *telemetry.Collector
}

// VMM owns one machine lifecycle: Init, Setup, then Boot or BootContext.
// Boot closes the machine on return; callers that stop before booting must call
// Close themselves. A closed VMM cannot be reused, including after a guest reset.
type VMM struct {
	*machine.Machine
	Config

	rdpDisplay *virtio.RDPDisplay
	closeOnce  sync.Once
	closeErr   error
}

// New stores c without validating it or acquiring host resources.
func New(c Config) *VMM {
	return &VMM{
		Machine: nil,
		Config:  c,
	}
}

func (v *VMM) validateRDPPerformanceConfig() error {
	if _, err := rdp.ProfileFrameRate(v.RDPProfile); err != nil || (v.RDPProfile != "" && v.RDPListenerFile == nil) {
		return errRDPProfileConfig
	}
	switch v.RDPH264Encoder {
	case "", "software", "nvenc", "auto":
	default:
		return errRDPH264EncoderConfig
	}
	if v.RDPH264Encoder != "" && !v.RDPH264 {
		return errRDPH264EncoderConfig
	}
	if v.RDPH264Threads < 0 || v.RDPH264Threads > 16 || (v.RDPH264Threads != 0 && !v.RDPH264) {
		return errRDPH264ThreadsConfig
	}
	if v.RDPStats && v.RDPListenerFile == nil {
		return errRDPStatsConfig
	}
	if v.RDPWait && v.RDPListenerFile == nil {
		return errRDPWaitConfig
	}
	if v.RDPListenerFile != nil && v.RDPGatewayPID <= 0 {
		return errRDPListenerConfig
	}

	return nil
}

// Init validates configuration, opens KVM and attaches the configured devices.
// Call it once before Setup. On failure it closes any machine it created.
func (v *VMM) Init() (initErr error) {
	if err := v.validateUEFIBootSource(); err != nil {
		return err
	}
	if err := v.validateRDPPerformanceConfig(); err != nil {
		return err
	}
	if err := v.validateCloudInit(); err != nil {
		return err
	}
	if (v.Audio != "" && v.Audio != "none" && v.Audio != "rdp") || (v.Audio == "rdp" && v.RDPListenerFile == nil) {
		return errAudioConfig
	}
	if (v.Network != "" && v.Network != "none" && v.Network != "user" && v.Network != "user-isolated") ||
		(v.Network != "" && v.TapIfName != "") {
		return errNetworkConfig
	}

	m, err := machine.New(v.Dev, v.NCPUs, v.MemSize)
	if err != nil {
		return err
	}
	defer func() {
		if initErr != nil {
			initErr = errors.Join(initErr, m.Close())
		}
	}()

	if v.RDPListenerFile != nil {
		m.DisableSerial()
	}
	if v.UEFI != "" {
		if err := m.EnableUEFIPlatform(); err != nil {
			return err
		}
	}

	if err := v.initNetwork(m); err != nil {
		return err
	}

	if len(v.Disk) > 0 {
		if err := m.AddDisk(v.Disk); err != nil {
			return err
		}
	}
	if v.CloudInit != "" {
		if err := m.AddReadOnlyDisk(v.CloudInit); err != nil {
			return fmt.Errorf("attach cloud-init seed: %w", err)
		}
	}

	if err := v.initDisplay(m); err != nil {
		return err
	}
	if v.Audio == "rdp" {
		m.AddSound(v.rdpDisplay)
		log.Print("Audio: virtio-snd playback through RDP (48 kHz, stereo PCM16)")
	}

	v.Machine = m

	return nil
}

func (v *VMM) initNetwork(m *machine.Machine) error {
	if len(v.TapIfName) > 0 {
		if err := m.AddTapIf(v.TapIfName); err != nil {
			return err
		}
	}
	if v.Network == "user" || v.Network == "user-isolated" {
		var err error
		if v.Network == "user-isolated" {
			err = m.AddIsolatedUserNet()
		} else {
			err = m.AddUserNet()
		}
		if err != nil {
			return err
		}
		log.Print("User-mode network: DHCP 10.0.2.15, gateway/DNS 10.0.2.2")
	}

	return nil
}

func (v *VMM) initDisplay(m *machine.Machine) error {
	if v.RDPListenerFile == nil {
		return nil
	}

	var input virtio.DisplayInput
	if v.UEFI != "" {
		input = m.AddUEFIInput()
	} else {
		input = m.AddVirtioInput()
	}

	display, err := v.display(input)
	if err != nil {
		return err
	}

	if err := m.AddGPUDisplayWithTelemetry(display, v.Telemetry); err != nil {
		_ = display.Close()

		return err
	}

	return nil
}

func (v *VMM) display(input virtio.DisplayInput) (virtio.Display, error) {
	display, err := v.newRDPDisplay()
	if err != nil {
		return nil, err
	}
	display.SetInput(input)
	v.rdpDisplay = display
	log.Print("RDP console available only to the authenticated gateway process")

	return display, nil
}

// Setup loads local UEFI firmware or a Linux/PVH kernel and optional initrd into
// an initialized machine. It does not start the guest and closes the VMM on error.
func (v *VMM) Setup() (setupErr error) {
	defer func() {
		if setupErr != nil {
			setupErr = errors.Join(setupErr, v.Close())
		}
	}()
	if err := v.validateUEFIBootSource(); err != nil {
		return err
	}
	if v.UEFI != "" {
		return v.setupUEFI()
	}

	var initrd io.ReaderAt
	// Direct boot always requires a local kernel image.
	kern, err := os.Open(v.Kernel)
	if err != nil {
		return err
	}
	defer kern.Close()

	isPVH, err := pvh.CheckPVH(kern)
	if err != nil {
		return err
	}

	if v.Initrd != "" {
		initrdFile, err := os.Open(v.Initrd)
		if err != nil {
			return err
		}

		defer initrdFile.Close()
		initrd = initrdFile
	}

	if isPVH {
		if err := v.LoadPVH(kern, initrd, v.consoleBootParams(v.Params)); err != nil {
			return err
		}
	} else {
		if err := v.LoadLinux(kern, initrd, v.consoleBootParams(v.Params)); err != nil {
			return err
		}
	}

	return nil
}

// kernelParamFields follows Linux next_arg: double quotes group whitespace,
// while single quotes and backslashes are ordinary characters. Only the
// parsed names and values are normalized; the original command line is kept.
func kernelParamFields(cmdline string) []string {
	var fields []string
	for pos := 0; pos < len(cmdline); {
		for pos < len(cmdline) && strings.IndexByte(" \t\n\r\v\f\xa0", cmdline[pos]) >= 0 {
			pos++
		}
		start := pos
		inQuote := false
		for pos < len(cmdline) {
			ch := cmdline[pos]
			if strings.IndexByte(" \t\n\r\v\f\xa0", ch) >= 0 && !inQuote {
				break
			}
			if ch == '"' {
				inQuote = !inQuote
			}
			pos++
		}
		if start == pos {
			break
		}

		field := cmdline[start:pos]
		quoted := strings.HasPrefix(field, `"`)
		if quoted {
			field = field[1:]
		}
		name, value, hasValue := strings.Cut(field, "=")
		valueQuoted := hasValue && strings.HasPrefix(value, `"`)
		if (quoted || valueQuoted) && strings.HasSuffix(field, `"`) {
			if hasValue {
				value = strings.TrimSuffix(value, `"`)
			} else {
				name = strings.TrimSuffix(name, `"`)
			}
		}
		if valueQuoted {
			value = strings.TrimPrefix(value, `"`)
		}
		if !hasValue && name == "--" {
			// Linux passes the remaining arguments to init, not its parser.
			break
		}
		if hasValue {
			fields = append(fields, name+"="+value)
		} else {
			fields = append(fields, name)
		}
	}

	return fields
}

func kernelParamName(field string) string {
	name, _, _ := strings.Cut(field, "=")

	return name
}

// Boot runs the prepared VM with a background context and closes it on return.
func (v *VMM) Boot() error {
	return v.BootContext(context.Background())
}

// BootContext runs the prepared VM, optionally waiting for an RDP client first.
// Cancellation wakes CPUs even when KVM_RUN is blocked in HLT. Before returning,
// it joins execution workers and closes the machine. Execution, cancellation and
// cleanup errors are joined in the returned error.
func (v *VMM) BootContext(ctx context.Context) (bootErr error) {
	defer func() { bootErr = errors.Join(bootErr, v.Close()) }()
	if err := ctx.Err(); err != nil {
		return err
	}
	if v.RDPWait {
		if v.rdpDisplay == nil {
			return errRDPWaitConfig
		}
		log.Print("Waiting for an RDP client before starting the guest")
		if err := v.rdpDisplay.WaitReady(ctx); err != nil {
			return fmt.Errorf("wait for RDP client: %w", err)
		}
	}
	trace := v.TraceCount > 0
	if err := v.SingleStep(trace); err != nil {
		return fmt.Errorf("setting trace to %v: %w", trace, err)
	}
	terminal := v.GetSerial() != nil && term.IsTerminal()
	if terminal {
		restore, err := term.SetRawMode()
		if err != nil {
			return err
		}
		defer restore()
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	// The stop worker is joined before Close can unmap any KVM state.
	stopped := make(chan error, 1)
	go func() { <-runCtx.Done(); stopped <- v.Stop() }()
	var group errgroup.Group
	for cpu := range v.NCPUs {
		group.Go(func() error {
			defer cancel()
			err := v.VCPU(os.Stderr, cpu, v.TraceCount)
			if errors.Is(err, machine.ErrMachineStopped) {
				return nil
			}

			return err
		})
	}
	if terminal {
		group.Go(func() error {
			defer cancel()
			err := v.GetSerial().StartContext(runCtx, os.Stdin, v.InjectSerialIRQ)
			if errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) {
				return nil
			}

			return err
		})
	}
	err := group.Wait()
	cancel()
	stopErr := <-stopped

	return errors.Join(err, stopErr, ctx.Err())
}

// Close stops the machine and persists attached state. Init and Setup must not
// overlap Close. Repeated or concurrent calls return the same result. Borrowed
// resources such as the original RDPListenerFile remain owned by the caller.
func (v *VMM) Close() error {
	v.closeOnce.Do(func() {
		if v.Machine != nil {
			v.closeErr = v.Machine.Close()
		}
	})

	return v.closeErr
}
