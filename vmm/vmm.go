package vmm

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/define42/gokvm/iso9660"
	"github.com/define42/gokvm/machine"
	"github.com/define42/gokvm/pvh"
	"github.com/define42/gokvm/term"
	"github.com/define42/gokvm/virtio"
	"golang.org/x/sync/errgroup"
)

var errDownloadBootImage = errors.New("download boot image failed")

var errNetworkConfig = errors.New(
	"network must be 'user', 'user-isolated', or 'none' and cannot be combined with a TAP interface",
)

var errAudioConfig = errors.New("audio must be 'none' or 'rdp'; 'rdp' requires an RDP listener")

var errRDPH264ThreadsConfig = errors.New("RDP H.264 slices must be 0 (auto) or 1..16; a slice count requires H.264")

var errRDPH264EncoderConfig = errors.New("RDP H.264 encoder must be software, nvenc, or auto and requires H.264")

var errRDPStatsConfig = errors.New("RDP statistics require an RDP listener")

var errRDPWaitConfig = errors.New("RDP boot wait requires an RDP listener")

var errUKIBootSource = errors.New("UKI cannot be combined with ISO, kernel, or initrd")

// These parameters describe gaps in gokvm's direct Linux boot environment, not
// ISO-specific policy. The ISO's own boot config still supplies the distro
// command line; this list only adds the host/VMM plumbing that a real firmware
// boot path would normally provide or hide from the guest.
// Do not add nosmp: the guest must be able to start the vCPUs requested by -c.
const gokvmDirectLinuxBootParams = "console=tty0 console=ttyS0 earlyprintk=serial " +
	"noapic noacpi nortc nowatchdog nmi_watchdog=0 mitigations=off " +
	"lapic pci=realloc=off virtio_pci.force_legacy=1"

// Config defines the configuration of the
// virtual machine, as determined by flags.
type Config struct {
	Debug          bool
	Dev            string
	Kernel         string
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
	RDPWait        bool
	RDPCert        string
	RDPKey         string
	RDPH264        bool
	RDPH264Encoder string
	RDPH264Threads int
	RDPStats       bool
	Audio          string
	NCPUs          int
	MemSize        int
	TraceCount     int
}

type VMM struct {
	*machine.Machine
	Config

	serialOutput io.Writer
	vncDisplay   *virtio.VNCDisplay
	rdpDisplay   *virtio.RDPDisplay
	consoleInput virtio.VNCInput
	consoles     []virtio.ConsoleDisplay
	isoCleanup   func()
	closeOnce    sync.Once
	closeErr     error
}

func New(c Config) *VMM {
	return &VMM{
		Machine: nil,
		Config:  c,
	}
}

func (v *VMM) validateRDPPerformanceConfig() error {
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
	if v.RDPStats && v.RDP == "" {
		return errRDPStatsConfig
	}
	if v.RDPWait && v.RDP == "" {
		return errRDPWaitConfig
	}

	return nil
}

// Init instantiates a machine.
func (v *VMM) Init() (initErr error) {
	if err := v.validateBootSource(); err != nil {
		return err
	}
	if err := v.validateRDPPerformanceConfig(); err != nil {
		return err
	}
	if err := v.validateCloudInit(); err != nil {
		return err
	}
	if (v.Audio != "" && v.Audio != "none" && v.Audio != "rdp") || (v.Audio == "rdp" && v.RDP == "") {
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

	if v.RDP != "" {
		m.DisableSerial()
	}
	if v.UEFI != "" {
		if err := m.EnableUEFIPlatform(); err != nil {
			return err
		}
	}

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

	if len(v.GPU) > 0 || v.hasRemoteDisplay() {
		var input virtio.VNCInput
		if v.hasRemoteDisplay() {
			if v.UEFI != "" {
				input = m.AddUEFIInput()
			} else {
				input = m.AddVirtioInput()
			}
			v.consoleInput = input
		}

		display, err := v.display(input)
		if err != nil {
			return err
		}

		if err := m.AddGPUDisplay(display); err != nil {
			_ = display.Close()

			return err
		}

		if len(v.ISO) > 0 {
			for _, console := range v.consoles {
				m.EnableVESA(console)
				m.StartVGATextFallback(console)
			}
		}
	}
	if v.Audio == "rdp" {
		m.AddSound(v.rdpDisplay)
		log.Print("Audio: virtio-snd playback through RDP (48 kHz, stereo PCM16)")
	}

	v.Machine = m

	return nil
}

func (v *VMM) hasRemoteDisplay() bool { return v.VNC != "" || v.RDP != "" }

func (v *VMM) display(input virtio.VNCInput) (result virtio.Display, displayErr error) {
	var displays []virtio.Display
	defer func() {
		if displayErr != nil {
			for _, display := range displays {
				_ = display.Close()
			}
			v.vncDisplay = nil
			v.rdpDisplay = nil
			v.consoles = nil
		}
	}()

	if len(v.GPU) > 0 {
		displays = append(displays, virtio.NewPNGDisplay(v.GPU))
	}

	if len(v.VNC) > 0 {
		display, err := virtio.NewVNCDisplay(v.VNC)
		if err != nil {
			return nil, err
		}

		display.SetInput(input)
		v.vncDisplay = display
		v.consoles = append(v.consoles, display)
		log.Printf("VNC listening on %s", display.Addr())
		displays = append(displays, display)
	}
	if v.RDP != "" {
		var config *tls.Config
		if v.RDPCert != "" || v.RDPKey != "" {
			certificate, err := tls.LoadX509KeyPair(v.RDPCert, v.RDPKey)
			if err != nil {
				return nil, fmt.Errorf("RDP certificate: %w", err)
			}
			config = &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12}
		}
		display, err := virtio.NewRDPDisplayWithConfig(v.RDP, virtio.RDPConfig{
			TLS: config, H264: v.RDPH264, Audio: v.Audio == "rdp",
			H264Encoder: v.RDPH264Encoder,
			H264Threads: v.RDPH264Threads, Stats: v.RDPStats, GuestCPUs: v.NCPUs,
		})
		if err != nil {
			return nil, err
		}
		display.SetInput(input)
		v.rdpDisplay = display
		v.consoles = append(v.consoles, display)
		displays = append(displays, display)
		log.Printf("RDP console listening on %s (TLS, no account authentication)", display.Addr())
	}
	if v.RDP == "" && v.vncDisplay != nil {
		v.serialOutput = v.vncDisplay
	}

	if len(displays) == 1 {
		return displays[0], nil
	}

	return virtio.NewMultiDisplay(displays...), nil
}

func (v *VMM) Setup() (setupErr error) {
	defer func() {
		if setupErr != nil {
			setupErr = errors.Join(setupErr, v.Close())
		}
	}()
	if err := v.validateBootSource(); err != nil {
		return err
	}
	if v.UEFI != "" {
		return v.setupUEFI()
	}
	if v.UKI != "" {
		return v.setupUKI()
	}
	if v.ISO != "" {
		return v.setupISO()
	}

	var initrd io.ReaderAt
	// Kernel arg required to load kernel or firmware image
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

	v.attachSerialOutput()
	v.attachSerialInput()

	return nil
}

func (v *VMM) setupISO() error {
	isoFile, cleanup, err := openBootSource(v.ISO)
	if err != nil {
		return err
	}

	cleanupOnError := true
	defer func() {
		if cleanupOnError {
			cleanup()
		}
	}()

	info, err := isoFile.Stat()
	if err != nil {
		return err
	}

	isoReader, err := iso9660.NewReader(isoFile, info.Size())
	if err != nil {
		return err
	}

	// Use the El Torito boot catalog (plus the syslinux/grub config it points
	// at) to identify the kernel, initrd, and command line, then boot them
	// directly. No BIOS firmware is involved.
	files, err := iso9660.LoadBootFiles(isoReader)
	if err != nil {
		return err
	}

	params := v.bootParams(files.Cmdline)

	log.Printf("ISO El Torito boot: kernel=%s initrd=%s", files.KernelPath, files.InitrdPath)

	// Attach the raw ISO so the booted kernel can mount its live media and load
	// its extensions (e.g. TinyCore's .tcz set). virtio-blk is the proven path
	// for TinyCore's boot-time device scan to find /cde.
	if err := v.AddReadOnlyDisk(isoFile.Name()); err != nil {
		return fmt.Errorf("attach ISO media: %w", err)
	}

	log.Printf("ISO media attached as read-only virtio-blk: %s", isoFile.Name())

	if v.hasRemoteDisplay() && hasTinyCoreGUI(isoReader) {
		files.Initrd, err = addTinyCoreVNCAutostart(files.Initrd)
		if err != nil {
			return err
		}

		log.Printf("ISO boot: added TinyCore remote desktop autostart overlay")
	}

	kern := bytes.NewReader(files.Kernel)
	initrd := readerAtOrNil(files.Initrd)

	isPVH, err := pvh.CheckPVH(kern)
	if err != nil {
		return err
	}

	if isPVH {
		if err := v.LoadPVH(kern, initrd, params); err != nil {
			return err
		}
	} else if err := v.LoadLinux(kern, initrd, params); err != nil {
		return err
	}

	v.attachSerialOutput()
	v.attachSerialInput()
	v.isoCleanup = cleanup
	cleanupOnError = false

	return nil
}

func readerAtOrNil(data []byte) io.ReaderAt {
	if len(data) == 0 {
		return nil
	}

	return bytes.NewReader(data)
}

func hasTinyCoreGUI(r *iso9660.Reader) bool {
	if _, err := r.ReadFile("/cde/onboot.lst"); err != nil {
		return false
	}

	if _, err := r.ReadFile("/cde/optional/Xvesa.tcz"); err != nil {
		return false
	}

	return true
}

func openBootSource(source string) (*os.File, func(), error) {
	u, err := url.Parse(source)
	if err == nil && (u.Scheme == "http" || u.Scheme == "https") {
		return downloadBootSource(source)
	}

	file, err := os.Open(source)
	if err != nil {
		return nil, nil, err
	}

	return file, func() { _ = file.Close() }, nil
}

func downloadBootSource(source string) (*os.File, func(), error) {
	resp, err := http.Get(source) //nolint:gosec // User-supplied boot media URL.
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, nil, fmt.Errorf("%w %s: %s", errDownloadBootImage, source, resp.Status)
	}

	file, err := os.CreateTemp("", "gokvm-boot-*")
	if err != nil {
		return nil, nil, err
	}

	cleanup := func() {
		name := file.Name()
		_ = file.Close()
		_ = os.Remove(name)
	}

	if _, err := io.Copy(file, resp.Body); err != nil {
		cleanup()

		return nil, nil, err
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		cleanup()

		return nil, nil, err
	}

	log.Printf("downloaded boot image %s to %s", source, file.Name())

	return file, cleanup, nil
}

func isoBootParams(cmdline string) string {
	return mergeKernelParams(cmdline, strings.Fields(gokvmDirectLinuxBootParams))
}

func mergeKernelParams(cmdline string, defaults []string) string {
	fields := kernelParamFields(cmdline)
	missing := make([]string, 0, len(defaults))
	for _, param := range defaults {
		if !hasKernelParam(fields, param) {
			missing = append(missing, param)
		}
	}
	if len(missing) == 0 {
		return cmdline
	}
	prefix := strings.Join(missing, " ")
	if cmdline == "" {
		return prefix
	}

	// Keep quoted values and whitespace in the embedded command line intact.
	return prefix + " " + cmdline
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

func hasKernelParam(fields []string, param string) bool {
	paramName := kernelParamName(param)
	for _, field := range fields {
		if paramName == "console" {
			device, _, _ := strings.Cut(field, ",")
			defaultDevice, _, _ := strings.Cut(param, ",")
			if device == defaultDevice {
				return true
			}

			continue
		}

		if kernelParamName(field) == paramName {
			return true
		}
	}

	return false
}

func kernelParamName(field string) string {
	name, _, _ := strings.Cut(field, "=")

	return name
}

func (v *VMM) attachSerialOutput() {
	if v.RDP != "" || v.serialOutput == nil || v.GetSerial() == nil {
		return
	}

	v.GetSerial().SetOutput(io.MultiWriter(os.Stdout, v.serialOutput))
}

func (v *VMM) attachSerialInput() {
	if v.RDP != "" || v.ISO == "" || len(v.consoles) == 0 || v.GetSerial() == nil {
		return
	}

	input := &serialMirrorInput{
		primary: v.consoleInput,
		serial:  v.GetSerial().GetInputChan(),
		inject:  v.InjectSerialIRQ,
	}
	for _, console := range v.consoles {
		console.SetInput(input)
	}
}

type serialMirrorInput struct {
	primary virtio.VNCInput
	serial  chan<- byte
	inject  func() error
}

func (s *serialMirrorInput) KeyEvent(down bool, keysym uint32) {
	if s.primary != nil {
		s.primary.KeyEvent(down, keysym)
	}

	if !down || s.serial == nil {
		return
	}

	for _, b := range serialBytesForKeysym(keysym) {
		// A graphical guest may never read its serial console. Keep that
		// best-effort mirror from blocking remote input or display shutdown.
		select {
		case s.serial <- b:
			if s.inject != nil {
				_ = s.inject()
			}
		default:
		}
	}
}

func (s *serialMirrorInput) PointerEvent(buttonMask uint8, x, y uint16) {
	if s.primary != nil {
		s.primary.PointerEvent(buttonMask, x, y)
	}
}

func (s *serialMirrorInput) PointerEventInBounds(buttonMask uint8, x, y uint16, width, height int) {
	if bounded, ok := s.primary.(interface {
		PointerEventInBounds(uint8, uint16, uint16, int, int)
	}); ok {
		bounded.PointerEventInBounds(buttonMask, x, y, width, height)

		return
	}
	s.PointerEvent(buttonMask, x, y)
}

func serialBytesForKeysym(keysym uint32) []byte {
	if keysym >= 0x20 && keysym <= 0x7e {
		return []byte{byte(keysym)}
	}

	switch keysym {
	case 0xff08: // BackSpace
		return []byte{0x7f}
	case 0xff09: // Tab
		return []byte{'\t'}
	case 0xff0d: // Return
		return []byte{'\r'}
	case 0xff1b: // Escape
		return []byte{0x1b}
	case 0xff51: // Left
		return []byte{0x1b, '[', 'D'}
	case 0xff52: // Up
		return []byte{0x1b, '[', 'A'}
	case 0xff53: // Right
		return []byte{0x1b, '[', 'C'}
	case 0xff54: // Down
		return []byte{0x1b, '[', 'B'}
	default:
		return nil
	}
}

func addTinyCoreVNCAutostart(initrd []byte) ([]byte, error) {
	const autologin = `#!/bin/sh
if [ -f /var/log/autologin ]; then
	exec /sbin/getty 38400 tty1
fi
touch /var/log/autologin
TCUSER="$(cat /etc/sysconfig/tcuser 2>/dev/null || echo tc)"
if command -v Xvesa >/dev/null 2>&1 && \
	command -v flwm >/dev/null 2>&1 && \
	[ ! -f /etc/sysconfig/text ]; then
	[ -s /etc/sysconfig/Xserver ] || echo Xvesa > /etc/sysconfig/Xserver
	[ -s /etc/sysconfig/desktop ] || echo flwm > /etc/sysconfig/desktop
	[ -s /etc/sysconfig/icons ] || echo wbar > /etc/sysconfig/icons
	for file in .xsession .setbackground .Xdefaults; do
		if [ ! -e /home/"$TCUSER"/"$file" ] && [ -e /etc/skel/"$file" ]; then
			cp /etc/skel/"$file" /home/"$TCUSER"/
			chown "$TCUSER":staff /home/"$TCUSER"/"$file" 2>/dev/null || chown "$TCUSER" /home/"$TCUSER"/"$file"
		fi
	done
	cat > /tmp/gokvm-vnc-session <<'EOF'
#!/bin/sh
export DISPLAY=:0.0
export DESKTOP=flwm
export ICONS=wbar
# TinyCore's startx normally builds the FLWM menu and the icon configuration.
# This session starts Xvesa directly, so perform that setup before the desktop.
setupdesktop >/tmp/setupdesktop.log 2>&1
Xvesa -br -screen 1024x768x32 -mouse /dev/input/mice,5 -a 1 -t 0 -nolisten tcp -I >/tmp/Xvesa.log 2>&1 &
XPID=$!
for _ in 1 2 3 4 5 6 7 8 9 10; do
	waitforX >/dev/null 2>&1 && break
	sleep 0.2
done
# Xvesa discards five small mouse packets while identifying the PS/2 protocol.
# Prime it through the tablet's evdev node before showing the desktop. These
# five balanced steps return to the initial center, so the discarded motion
# leaves Xvesa and the kernel's absolute pointer at the same position.
for event in /sys/class/input/event*; do
	[ "$(cat "$event/device/name")" = "gokvm tablet" ] || continue
	(
		# TinyCore x86 uses 16-byte input_event records: timeval, type, code, value.
		for x in '\000\100' '\040\100' '\100\100' '\040\100' '\340\077' '\000\100'; do
			printf '\000\000\000\000\000\000\000\000\003\000\000\000%b\000\000' "$x"
			printf '\000\000\000\000\000\000\000\000\003\000\001\000\000\100\000\000'
			printf '\000\000\000\000\000\000\000\000\000\000\000\000\000\000\000\000'
			sleep 0.05
		done
	) > "/dev/input/${event##*/}"
done
flwm >/tmp/flwm.log 2>&1 &
[ -x "$HOME/.setbackground" ] && "$HOME/.setbackground" >/tmp/background.log 2>&1
wbar.sh >/tmp/wbar.log 2>&1 &
wait "$XPID"
EOF
	chmod 755 /tmp/gokvm-vnc-session
	chown "$TCUSER":staff /tmp/gokvm-vnc-session 2>/dev/null || chown "$TCUSER" /tmp/gokvm-vnc-session
	exec su "$TCUSER" -c /tmp/gokvm-vnc-session
fi
exec login -f "$TCUSER"
`

	return appendInitramfsFile(initrd, "sbin/autologin", []byte(autologin), 0o100755)
}

func appendInitramfsFile(initrd []byte, name string, data []byte, mode uint32) ([]byte, error) {
	raw := initrd
	compressed := len(initrd) >= 2 && initrd[0] == 0x1f && initrd[1] == 0x8b
	if compressed {
		zr, err := gzip.NewReader(bytes.NewReader(initrd))
		if err != nil {
			return nil, fmt.Errorf("initramfs gzip: %w", err)
		}

		raw, err = io.ReadAll(zr)
		closeErr := zr.Close()
		if err != nil {
			return nil, fmt.Errorf("initramfs decompress: %w", err)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("initramfs gzip close: %w", closeErr)
		}
	}

	if trimmed, ok := trimNewcTrailer(raw); ok {
		raw = trimmed
	}

	buf := bytes.NewBuffer(make([]byte, 0, len(raw)+len(data)+512))
	buf.Write(raw)
	writeNewcEntry(buf, name, mode, data)
	writeNewcEntry(buf, "TRAILER!!!", 0, nil)

	if !compressed {
		return buf.Bytes(), nil
	}

	var out bytes.Buffer
	zw := gzip.NewWriter(&out)
	if _, err := zw.Write(buf.Bytes()); err != nil {
		return nil, fmt.Errorf("initramfs recompress: %w", err)
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("initramfs gzip finish: %w", err)
	}

	return out.Bytes(), nil
}

func trimNewcTrailer(data []byte) ([]byte, bool) {
	for off := 0; off+110 <= len(data); {
		if string(data[off:off+6]) != "070701" {
			return nil, false
		}

		filesize, ok := parseNewcHex(data[off+54 : off+62])
		if !ok {
			return nil, false
		}

		namesize, ok := parseNewcHex(data[off+94 : off+102])
		if !ok || namesize == 0 {
			return nil, false
		}

		nameStart := off + 110
		nameEnd := nameStart + int(namesize)
		if nameEnd > len(data) {
			return nil, false
		}

		name := string(bytes.TrimRight(data[nameStart:nameEnd], "\x00"))
		next := align4(nameEnd) + align4(int(filesize))
		if next > len(data) {
			return nil, false
		}

		if name == "TRAILER!!!" {
			return data[:off], true
		}

		off = next
	}

	return nil, false
}

func writeNewcEntry(buf *bytes.Buffer, name string, mode uint32, data []byte) {
	namesize := len(name) + 1
	fmt.Fprintf(
		buf,
		"070701%08x%08x%08x%08x%08x%08x%08x%08x%08x%08x%08x%08x%08x",
		1, mode, 0, 0, 1, 0, len(data), 0, 0, 0, 0, namesize, 0,
	)
	buf.WriteString(name)
	buf.WriteByte(0)
	padBuffer(buf)
	buf.Write(data)
	padBuffer(buf)
}

func parseNewcHex(data []byte) (uint64, bool) {
	v, err := strconv.ParseUint(string(data), 16, 64)

	return v, err == nil
}

func align4(n int) int {
	return (n + 3) &^ 3
}

func padBuffer(buf *bytes.Buffer) {
	for buf.Len()%4 != 0 {
		buf.WriteByte(0)
	}
}

// Boot runs the VM until a CPU stops or terminal input requests shutdown.
func (v *VMM) Boot() error {
	return v.BootContext(context.Background())
}

// BootContext coordinates CPU and terminal workers and returns the first real
// execution error. Cancellation wakes CPUs even when KVM_RUN is blocked in HLT.
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

// Close stops the machine, persists attached state, and removes temporary boot
// media. Setup must finish before Close begins. Repeated calls are safe.
func (v *VMM) Close() error {
	v.closeOnce.Do(func() {
		if v.Machine != nil {
			v.closeErr = v.Machine.Close()
		}
		v.cleanupISO()
	})

	return v.closeErr
}

func (v *VMM) cleanupISO() {
	if v.isoCleanup == nil {
		return
	}

	v.isoCleanup()
	v.isoCleanup = nil
}
