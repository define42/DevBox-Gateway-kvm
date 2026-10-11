package gateway

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/define42/gokvm/internal/disk"
	"golang.org/x/sys/unix"
)

// ManagerConfig supplies operator-controlled resources for managed UEFI guests.
// BaseImage must be a standalone raw or uncompressed qcow2 image.
type ManagerConfig struct {
	DataDir, Binary, BaseImage, Firmware, VarsTemplate string
	Encoder, Keyboard                                  string
	CPUs, MemoryMiB, MaxVMsPerUser, MaxRunning         int
	StartTimeout, StopTimeout                          time.Duration
	MaxImageUploadBytes                                int64
}

// Manager persists each VM's disk, firmware variables, seed and TLS identity.
// Each running guest is an isolated gokvm child process. Close stops all children.
type Manager struct {
	config       ManagerConfig
	mu           sync.Mutex
	vms          map[string]*managedVM
	active       int
	closed       bool
	lock         *os.File
	jobs         sync.WaitGroup
	imageMu      sync.RWMutex
	images       map[string]Image
	imageImports chan struct{}
	//nolint:containedctx // Owns the manager lifetime; never stores a caller context.
	ctx       context.Context
	cancel    context.CancelFunc
	closeOnce sync.Once
	closeErr  error
}

type managedVM struct {
	mu     sync.Mutex
	record VM
	run    *managedProcess
}

type managedProcess struct {
	cmd      *exec.Cmd
	done     chan struct{}
	err      error // Published by closing done.
	endpoint Endpoint
}

// NewManager opens an exclusively owned state directory and loads saved VMs.
func NewManager(config ManagerConfig) (*Manager, error) {
	if err := normalizeManagerConfig(&config); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(config.DataDir, 0o700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(config.DataDir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || !privateOwned(info) {
		return nil, fmt.Errorf("state directory must be a private directory (0700): %w", ErrInvalid)
	}
	lock, err := openPrivateFile(filepath.Join(config.DataDir, "manager.lock"), os.O_CREATE|os.O_RDWR)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = lock.Close()

		return nil, fmt.Errorf("state directory is already in use: %w", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{
		config: config, vms: make(map[string]*managedVM), images: make(map[string]Image),
		imageImports: make(chan struct{}, 1), lock: lock, ctx: ctx, cancel: cancel,
	}
	if err := m.loadImages(); err != nil {
		cancel()
		_ = lock.Close()

		return nil, err
	}
	if err := m.load(); err != nil {
		cancel()
		_ = lock.Close()

		return nil, err
	}

	return m, nil
}

func normalizeManagerConfig(c *ManagerConfig) error {
	if c.DataDir == "" || c.BaseImage == "" || c.Firmware == "" || c.VarsTemplate == "" {
		return fmt.Errorf("state, image, firmware and variables template are required: %w", ErrInvalid)
	}
	if c.Binary == "" {
		var err error
		c.Binary, err = os.Executable()
		if err != nil {
			return err
		}
	}
	for _, path := range []*string{&c.DataDir, &c.Binary, &c.BaseImage, &c.Firmware, &c.VarsTemplate} {
		absolute, err := filepath.Abs(*path)
		if err != nil {
			return err
		}
		*path = absolute
	}
	for _, path := range []string{c.Binary, c.BaseImage, c.Firmware, c.VarsTemplate} {
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() == 0 {
			return fmt.Errorf("%s must be a nonempty regular file: %w", path, ErrInvalid)
		}
	}
	image, err := disk.OpenReadOnly(c.BaseImage)
	if err != nil {
		return fmt.Errorf("base image must be supported by gokvm (convert compressed qcow2 first): %w", err)
	}
	if err := image.Close(); err != nil {
		return err
	}

	return normalizeManagerLimits(c)
}

func normalizeManagerLimits(c *ManagerConfig) error {
	if c.MaxImageUploadBytes == 0 {
		c.MaxImageUploadBytes = DefaultMaxImageUploadBytes
	}
	if c.MaxImageUploadBytes < 1 || c.MaxImageUploadBytes > maxImageVirtualSize {
		return fmt.Errorf("invalid image upload size limit: %w", ErrInvalid)
	}
	if c.CPUs == 0 {
		c.CPUs = 2
	}
	if c.MemoryMiB == 0 {
		c.MemoryMiB = 4096
	}
	if c.MaxVMsPerUser == 0 {
		c.MaxVMsPerUser = 3
	}
	if c.MaxRunning == 0 {
		c.MaxRunning = 4
	}
	if c.CPUs < 1 || c.CPUs > 64 || c.MemoryMiB < 256 || c.MemoryMiB > 1048576 || c.MaxVMsPerUser < 1 || c.MaxRunning < 1 {
		return fmt.Errorf("invalid VM resource limits: %w", ErrInvalid)
	}
	if c.Encoder == "" {
		c.Encoder = "auto"
	}
	if c.Encoder != "auto" && c.Encoder != "nvenc" && c.Encoder != "software" {
		return fmt.Errorf("invalid H.264 encoder: %w", ErrInvalid)
	}
	if c.Keyboard == "" {
		c.Keyboard = "us"
	}
	if c.StartTimeout == 0 {
		c.StartTimeout = 30 * time.Second
	}
	if c.StopTimeout == 0 {
		c.StopTimeout = 30 * time.Second
	}
	if c.StartTimeout <= 0 || c.StopTimeout <= 0 {
		return fmt.Errorf("invalid VM timeout: %w", ErrInvalid)
	}

	return nil
}

func (m *Manager) load() error {
	root := filepath.Join(m.config.DataDir, "vms")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	if err := checkPrivateDirectory(root); err != nil {
		return err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	names := make(map[string]bool)
	for _, entry := range entries {
		if id, creating := strings.CutPrefix(entry.Name(), ".creating-"); creating && validID(id) {
			// The exclusive manager lock means these are interrupted creations.
			if err := os.RemoveAll(filepath.Join(root, entry.Name())); err != nil {
				return err
			}

			continue
		}
		if !entry.IsDir() || !validID(entry.Name()) {
			return fmt.Errorf("unexpected entry in VM state directory: %w", ErrInvalid)
		}
		if err := checkPrivateDirectory(m.directory(entry.Name())); err != nil {
			return err
		}
		data, err := readPrivateFile(filepath.Join(m.directory(entry.Name()), "vm.json"), 4096)
		if err != nil {
			return fmt.Errorf("read VM metadata: %w", err)
		}
		var record VM
		if err := json.Unmarshal(data, &record); err != nil {
			return fmt.Errorf("decode VM metadata: %w", err)
		}
		if record.ID != entry.Name() || !validOwner(record.Owner) || !validVMName(record.Name) || record.CreatedAt.IsZero() {
			return fmt.Errorf("invalid saved VM metadata: %w", ErrInvalid)
		}
		if record.ImageID == "" {
			record.ImageID, record.ImageName = DefaultImageID, m.images[DefaultImageID].Name
		}
		if record.ImageID != DefaultImageID && !validID(record.ImageID) {
			return fmt.Errorf("invalid saved VM image identity: %w", ErrInvalid)
		}
		key := record.Owner + "/" + record.Name
		if names[key] {
			return fmt.Errorf("duplicate saved desktop name: %w", ErrInvalid)
		}
		names[key] = true
		record.State, record.Error = "stopped", ""
		m.vms[record.ID] = &managedVM{record: record}
	}

	return nil
}

var (
	errProcessKillTimeout  = errors.New("desktop process did not exit after SIGKILL")
	errManagerCloseTimeout = errors.New("manager cleanup is still pending; state lock retained")
	vmNamePattern          = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	vmIDPattern            = regexp.MustCompile(`^[a-z2-7]{26}$`)
)

func validVMName(name string) bool { return vmNamePattern.MatchString(name) }
func validID(id string) bool       { return vmIDPattern.MatchString(id) }

// operation joins caller cancellation with manager shutdown and holds the state
// lock's lifetime until this operation has finished touching persistent files.
func (m *Manager) operation(ctx context.Context) (context.Context, func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()

		return nil, nil, ErrConflict
	}
	m.jobs.Add(1)
	m.mu.Unlock()
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(m.ctx, cancel)

	return ctx, func() { stop(); cancel(); m.jobs.Done() }, nil
}

func (m *Manager) directory(id string) string { return filepath.Join(m.config.DataDir, "vms", id) }

func (m *Manager) lookup(owner, id string) (*managedVM, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !validID(id) {
		return nil, ErrNotFound
	}
	v := m.vms[id]
	if v == nil || v.record.Owner != owner {
		return nil, ErrNotFound
	}
	if m.closed {
		return nil, ErrConflict
	}

	return v, nil
}

// List returns only the authenticated owner's virtual machines.
func (m *Manager) List(ctx context.Context, owner string) ([]VM, error) {
	ctx, finish, err := m.operation(ctx)
	if err != nil {
		return nil, err
	}
	defer finish()

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	list := make([]*managedVM, 0, len(m.vms))
	for _, v := range m.vms {
		if v.record.Owner == owner {
			list = append(list, v)
		}
	}
	m.mu.Unlock()
	result := make([]VM, 0, len(list))
	for _, v := range list {
		v.mu.Lock()
		result = append(result, v.record)
		v.mu.Unlock()
	}
	slices.SortFunc(result, func(a, b VM) int { return a.CreatedAt.Compare(b.CreatedAt) })

	return result, nil
}

// Create makes a fresh standalone disk and seed; the immutable base is untouched.
func (m *Manager) Create(ctx context.Context, owner, name, passwordHash string) (VM, error) {
	return m.CreateWithImage(ctx, owner, name, passwordHash, DefaultImageID)
}

// CreateWithImage clones a selected catalog image into a new standalone VM.
func (m *Manager) CreateWithImage(ctx context.Context, owner, name, passwordHash, imageID string) (VM, error) {
	return m.CreateWithProgress(ctx, owner, name, passwordHash, imageID, nil)
}

// CreateWithProgress reports actual creation stages and disk bytes synchronously.
// The callback must return promptly and must not call back into the manager.
func (m *Manager) CreateWithProgress(
	ctx context.Context, owner, name, passwordHash, imageID string, report func(CreateProgress),
) (VM, error) {
	ctx, finish, err := m.operation(ctx)
	if err != nil {
		return VM{}, err
	}
	defer finish()

	if !validOwner(owner) || !validVMName(name) || !validPasswordHash(passwordHash) {
		return VM{}, fmt.Errorf("invalid account, hostname or password hash: %w", ErrInvalid)
	}
	if err := ctx.Err(); err != nil {
		return VM{}, err
	}
	m.imageMu.RLock()
	defer m.imageMu.RUnlock()
	image, imagePath, err := m.selectedImage(imageID)
	if err != nil {
		return VM{}, err
	}
	v := &managedVM{record: VM{
		ImageID: image.ID, ImageName: image.Name,
		ID: strings.ToLower(rand.Text()), Name: name, Owner: owner,
		State: "creating", CreatedAt: time.Now().UTC(),
	}}
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := m.reserveVM(v); err != nil {
		return VM{}, err
	}
	if err := m.prepareImageWithProgress(ctx, v.record, passwordHash, imagePath, report); err != nil {
		m.mu.Lock()
		delete(m.vms, v.record.ID)
		m.mu.Unlock()

		return VM{}, err
	}
	if err := ctx.Err(); err != nil {
		m.mu.Lock()
		delete(m.vms, v.record.ID)
		m.mu.Unlock()

		return VM{}, errors.Join(err, os.RemoveAll(m.directory(v.record.ID))) //nolint:gosec // Server-generated VM ID.
	}
	v.record.State = "stopped"
	if err := m.save(v.record); err != nil {
		m.mu.Lock()
		delete(m.vms, v.record.ID)
		m.mu.Unlock()
		_ = os.RemoveAll(m.directory(v.record.ID)) //nolint:gosec // Server-generated VM ID.

		return VM{}, err
	}
	// A freshly created VM remains stopped until Start is called by the UI.
	return v.record, nil
}

func (m *Manager) reserveVM(v *managedVM) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrConflict
	}
	count := 0
	for _, current := range m.vms {
		if current.record.Owner != v.record.Owner {
			continue
		}
		if current.record.Name == v.record.Name {
			return fmt.Errorf("a desktop with this name exists: %w", ErrConflict)
		}
		count++
	}
	if count >= m.config.MaxVMsPerUser {
		return fmt.Errorf("desktop quota reached: %w", ErrConflict)
	}
	if m.vms[v.record.ID] != nil {
		return ErrConflict
	}
	m.vms[v.record.ID] = v

	return nil
}

func (m *Manager) save(record VM) error {
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}

	return writeAtomic(m.directory(record.ID), "vm.json", data)
}

// Start starts a saved VM with a private RDP endpoint and its own TLS identity.
func (m *Manager) Start(ctx context.Context, owner, id string) error {
	ctx, finish, err := m.operation(ctx)
	if err != nil {
		return err
	}
	defer finish()

	v, err := m.lookup(owner, id)
	if err != nil {
		return err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if v.run != nil || v.record.State != "stopped" {
		return ErrConflict
	}
	m.mu.Lock()
	if m.closed || m.active >= m.config.MaxRunning {
		m.mu.Unlock()

		return fmt.Errorf("running desktop limit reached: %w", ErrConflict)
	}
	m.active++
	m.jobs.Add(1)
	m.mu.Unlock()
	run, err := m.startProcess(v)
	if err != nil {
		m.mu.Lock()
		m.active--
		m.mu.Unlock()
		m.jobs.Done()

		return err
	}
	v.run = run
	v.record.State, v.record.Error = "starting", ""
	go m.watch(v, run)
	if err := m.waitStarted(ctx, run); err != nil {
		return m.failedStart(v, run, err)
	}
	v.record.State = "running"
	if err := m.save(v.record); err != nil {
		return m.failedStart(v, run, err)
	}

	return nil
}

func (m *Manager) startProcess(v *managedVM) (*managedProcess, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		return nil, err
	}
	dir := m.directory(v.record.ID)
	if err := checkPrivateDirectory(dir); err != nil {
		return nil, err
	}
	for _, name := range []string{"disk.img", "vars.fd", "seed.iso", "rdp.crt", "rdp.key"} {
		file, err := openPrivateFile(filepath.Join(dir, name), os.O_RDONLY)
		if err != nil {
			return nil, err
		}
		if err := file.Close(); err != nil {
			return nil, err
		}
	}
	cert, err := readPrivateFile(filepath.Join(dir, "rdp.crt"), 64<<10)
	if err != nil {
		return nil, err
	}
	logFile, err := openPrivateFile(filepath.Join(dir, "console.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY)
	if err != nil {
		return nil, err
	}
	defer logFile.Close()
	args := []string{
		"boot", "-uefi", m.config.Firmware, "-uefi-vars", filepath.Join(dir, "vars.fd"),
		"-d", filepath.Join(dir, "disk.img"), "-cloud-init", filepath.Join(dir, "seed.iso"),
		"-c", fmt.Sprint(m.config.CPUs), "-m", fmt.Sprintf("%dM", m.config.MemoryMiB), "-net", "user-isolated",
		"-rdp", address, "-rdp-cert", filepath.Join(dir, "rdp.crt"), "-rdp-key", filepath.Join(dir, "rdp.key"),
		"-rdp-h264", "-rdp-h264-encoder", m.config.Encoder,
	}
	cmd := exec.Command(m.config.Binary, args...) //nolint:gosec // Operator binary; validated VM paths, no shell.
	cmd.Stdout, cmd.Stderr = logFile, logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGTERM}
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	return &managedProcess{
		cmd: cmd, done: make(chan struct{}),
		endpoint: Endpoint{Address: address, ServerName: "vm-" + v.record.ID, CertificatePEM: cert},
	}, nil
}

func (m *Manager) waitStarted(ctx context.Context, run *managedProcess) error {
	ctx, cancel := context.WithTimeout(ctx, m.config.StartTimeout)
	defer cancel()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-run.done:
			return fmt.Errorf("desktop process exited during startup: %w", ErrConflict)
		case <-tick.C:
			probeCtx, cancelProbe := context.WithTimeout(ctx, time.Second)
			conn, err := dialRDPBackend(probeCtx, run.endpoint)
			cancelProbe()
			if err == nil {
				_ = conn.NetConn().Close()
				if err := ctx.Err(); err != nil {
					return err
				}
				select {
				case <-run.done:
					return ErrConflict
				default:
					return nil
				}
			}
		}
	}
}

func (m *Manager) watch(v *managedVM, run *managedProcess) {
	defer m.jobs.Done()
	run.err = run.cmd.Wait()
	m.mu.Lock()
	m.active--
	m.mu.Unlock()
	close(run.done)
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.run != run {
		return
	}
	v.run = nil
	v.record.State = "stopped"
	if run.err != nil && v.record.Error == "" {
		v.record.Error = "Desktop stopped unexpectedly; inspect its host log."
	}
	if err := m.save(v.record); err != nil {
		v.record.Error = "Could not save desktop state."
	}
}

// Stop requests the guest's ACPI power button and waits for a clean shutdown.
// A timeout leaves the VM running so callers can use the graphical shutdown UI.
func (m *Manager) Stop(ctx context.Context, owner, id string) error {
	ctx, finish, err := m.operation(ctx)
	if err != nil {
		return err
	}
	defer finish()

	v, err := m.lookup(owner, id)
	if err != nil {
		return err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.run == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	run := v.run
	if err := run.cmd.Process.Signal(syscall.SIGUSR1); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, m.config.StopTimeout)
	defer cancel()
	select {
	case <-ctx.Done():
		return fmt.Errorf("guest has not shut down; use its desktop power menu: %w", ctx.Err())
	case <-run.done:
		v.run = nil
		v.record.State, v.record.Error = "stopped", ""
		if run.err != nil {
			v.record.Error = "Desktop stopped unexpectedly; inspect its host log."

			return errors.Join(fmt.Errorf("desktop shutdown failed: %w", run.err), m.save(v.record))
		}

		return m.save(v.record)
	}
}

// Delete removes an owned, stopped desktop and its persistent state.
func (m *Manager) Delete(ctx context.Context, owner, id string) error {
	ctx, finish, err := m.operation(ctx)
	if err != nil {
		return err
	}
	defer finish()

	if err := ctx.Err(); err != nil {
		return err
	}
	v, err := m.lookup(owner, id)
	if err != nil {
		return err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if v.run != nil || v.record.State != "stopped" {
		return ErrConflict
	}
	if err := os.RemoveAll(m.directory(id)); err != nil { //nolint:gosec // Owned VM ID validated by lookup.
		return err
	}
	m.mu.Lock()
	delete(m.vms, id)
	m.mu.Unlock()
	v.record.State = "deleted"

	return nil
}

// RDP resolves only a running VM owned by the caller; identity is pinned by the proxy.
func (m *Manager) RDP(ctx context.Context, owner, id string) (Endpoint, error) {
	ctx, finish, err := m.operation(ctx)
	if err != nil {
		return Endpoint{}, err
	}
	defer finish()

	if err := ctx.Err(); err != nil {
		return Endpoint{}, err
	}
	v, err := m.lookup(owner, id)
	if err != nil {
		return Endpoint{}, err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Endpoint{}, err
	}
	if v.run == nil || v.record.State != "running" {
		return Endpoint{}, ErrConflict
	}
	select {
	case <-v.run.done:
		return Endpoint{}, ErrConflict
	default:
	}
	endpoint := v.run.endpoint
	endpoint.CertificatePEM = slices.Clone(endpoint.CertificatePEM)

	return endpoint, nil
}

// failedStart terminates an unusable child before releasing its startup call.
// A child ignoring SIGTERM is killed; normal Stop never uses this path.
func (m *Manager) failedStart(v *managedVM, run *managedProcess, startErr error) error {
	cleanupErr := m.terminate(run)
	select {
	case <-run.done:
		v.run = nil
		v.record.State = "stopped"
	default:
	}
	v.record.Error = "Desktop startup failed; inspect its host log."

	return errors.Join(startErr, cleanupErr, m.save(v.record))
}

func waitProcess(run *managedProcess, timeout time.Duration) bool {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-run.done:
		return true
	case <-timer.C:
		return false
	}
}

func signalProcess(run *managedProcess, signal syscall.Signal) error {
	if err := run.cmd.Process.Signal(signal); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}

	return nil
}

func (m *Manager) terminate(run *managedProcess) error {
	select {
	case <-run.done:
		return nil
	default:
	}
	wait := min(m.config.StopTimeout, 2*time.Second)
	termErr := signalProcess(run, syscall.SIGTERM)
	if waitProcess(run, wait) {
		return termErr
	}
	killErr := signalProcess(run, syscall.SIGKILL)
	if !waitProcess(run, wait) {
		return errors.Join(termErr, killErr, errProcessKillTimeout)
	}

	return errors.Join(termErr, killErr)
}

// Close requests ACPI shutdown, then bounds cleanup with SIGTERM and SIGKILL.
// Concurrent callers wait for the same result. The exclusive state lock remains
// held until every child and in-flight operation has finished.
func (m *Manager) Close() error {
	m.closeOnce.Do(func() {
		m.mu.Lock()
		m.closed = true
		m.cancel()
		list := make([]*managedVM, 0, len(m.vms))
		for _, v := range m.vms {
			list = append(list, v)
		}
		m.mu.Unlock()
		var closeWG sync.WaitGroup
		errorsByVM := make([]error, len(list))
		for i, v := range list {
			closeWG.Go(func() {
				v.mu.Lock()
				run := v.run
				v.mu.Unlock()
				if run == nil {
					return
				}
				_ = signalProcess(run, syscall.SIGUSR1)
				if !waitProcess(run, m.config.StopTimeout) {
					errorsByVM[i] = m.terminate(run)
				}
			})
		}
		closeWG.Wait()
		finished := make(chan error, 1)
		go func() { m.jobs.Wait(); finished <- m.lock.Close() }()
		select {
		case err := <-finished:
			m.closeErr = errors.Join(errors.Join(errorsByVM...), err)
		case <-time.After(min(m.config.StopTimeout, 2*time.Second)):
			m.closeErr = errors.Join(errors.Join(errorsByVM...), errManagerCloseTimeout)
		}
	})

	return m.closeErr
}

func privateOwned(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)

	return ok && stat.Uid == uint32(os.Geteuid()) && info.Mode().Perm()&0o077 == 0
}

func checkPrivateDirectory(path string) error {
	info, err := os.Lstat(path) //nolint:gosec // Operator state directory or a validated VM ID below it.
	if err != nil {
		return err
	}
	if !info.IsDir() || !privateOwned(info) {
		return fmt.Errorf("state directory must be private and owned by the current user: %w", ErrInvalid)
	}

	return nil
}

func openPrivateFile(path string, flags int) (*os.File, error) {
	//nolint:gosec // Operator state path or a validated VM ID; no symlinks, FIFOs or public files are accepted.
	file, err := os.OpenFile(path, flags|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0o600)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || !privateOwned(info) {
		_ = file.Close()
		if err != nil {
			return nil, err
		}

		return nil, fmt.Errorf("state file must be private, regular and owned by the current user: %w", ErrInvalid)
	}

	return file, nil
}

func readPrivateFile(path string, limit int64) ([]byte, error) {
	file, err := openPrivateFile(path, os.O_RDONLY)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("state file exceeds its size limit: %w", ErrInvalid)
	}

	return data, nil
}

func writeAtomic(dir, name string, data []byte) (writeErr error) {
	f, err := os.CreateTemp(dir, ".write-*")
	if err != nil {
		return err
	}
	defer func() {
		_ = f.Close()
		_ = os.Remove(f.Name()) //nolint:gosec // Remove only the temporary file created above in the private directory.
	}()
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	//nolint:gosec // Private staging/VM directory and a fixed internal filename.
	if err := os.Rename(f.Name(), filepath.Join(dir, name)); err != nil {
		return err
	}
	directory, err := os.Open(dir) //nolint:gosec // Sync the same private directory used for the atomic write.
	if err != nil {
		return err
	}
	defer directory.Close()

	return directory.Sync()
}

func cloneFile(ctx context.Context, source, target string) error {
	return cloneFileWithProgress(ctx, source, target, nil)
}

func cloneFileWithProgress(
	ctx context.Context, source, target string, report func(CreateProgress),
) (cloneErr error) {
	input, err := os.Open(source) //nolint:gosec // Validated operator-configured base image or firmware template.
	if err != nil {
		return err
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return err
	}
	if err := reportCreationProgress(ctx, report, CreateProgress{Phase: "copying", TotalBytes: info.Size()}); err != nil {
		return err
	}
	//nolint:gosec // Fixed filename in a generated private staging directory; O_EXCL prevents replacement.
	output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer func() { cloneErr = errors.Join(cloneErr, output.Close()) }()
	if err := unix.IoctlFileClone(int(output.Fd()), int(input.Fd())); err == nil {
		if err := output.Sync(); err != nil {
			return err
		}

		return reportCreationProgress(ctx, report, CreateProgress{
			Phase: "copying", CopiedBytes: info.Size(), TotalBytes: info.Size(),
		})
	}
	if err := copyFileWithProgress(ctx, input, output, info.Size(), report); err != nil {
		return err
	}

	return output.Sync()
}
