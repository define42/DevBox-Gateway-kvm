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
	"sync/atomic"
	"syscall"
	"time"

	"github.com/define42/DevBox-Gateway-kvm/internal/disk"
	"github.com/define42/DevBox-Gateway-kvm/internal/rdp"
	"golang.org/x/sys/unix"
)

const defaultMaxCreations = 2

// ManagerConfig supplies operator-controlled resources for managed UEFI guests.
// BaseImage is optional; when set, it must be a standalone raw or uncompressed qcow2 image.
type ManagerConfig struct {
	DataDir, Binary, BaseImage, Firmware, VarsTemplate string
	Encoder, Keyboard                                  string
	RDPProfile                                         string // office (30 FPS) by default; smooth selects 60 FPS.
	VMProfiling                                        bool   // Opt-in private per-worker CPU profiling sockets.
	CPUs, MemoryMiB, MaxVMsPerUser, MaxRunning         int
	MemoryBudgetMiB                                    int    // Zero selects an automatic host-memory budget.
	MemoryOverheadMiB                                  int    // Host-side worker allowance above guest RAM.
	CgroupRoot                                         string // Empty disables starts, not isolation.
	MaxCreations                                       int    // Bounds provisioning I/O across all owners.
	StartTimeout, StopTimeout                          time.Duration
	MaxImageUploadBytes                                int64
	MaxDiskBytes                                       int64
}

// Manager persists each VM's disk, firmware variables, seed and TLS identity.
// Each running guest is an isolated gokvm child process. Close stops all children.
type Manager struct {
	config       ManagerConfig
	mu           sync.Mutex
	vms          map[string]*managedVM
	active       int
	memoryBudget uint64 // Capacity for guest reservations while any VM is active.
	readMemory   func() (hostMemory, error)
	readResident func(int) (uint64, error)
	cgroups      vmCgroupFactory
	isolationErr error                        // Protected by mu; failed cleanup blocks subsequent launches.
	processes    map[*managedProcess]struct{} // Started children, protected by mu.
	closed       bool
	lock         *os.File
	jobs         sync.WaitGroup
	imageMu      sync.RWMutex
	images       map[string]Image
	imageImports chan struct{}
	creations    chan struct{} // Bounds provisioning I/O across all owners.
	//nolint:containedctx // Owns the manager lifetime; never stores a caller context.
	ctx       context.Context
	cancel    context.CancelFunc
	closeOnce sync.Once
	closeErr  error
}

type managedVM struct {
	mutation chan struct{} // Serializes lifecycle changes, including persistence.
	record   VM
	run      *managedProcess
	view     atomic.Pointer[managedVMView]
}

// managedVMView is immutable once published. Readers never wait for lifecycle I/O.
type managedVMView struct {
	record VM
	run    *managedProcess
}

func newManagedVM(record VM) *managedVM {
	v := &managedVM{mutation: make(chan struct{}, 1), record: record}
	v.publish()

	return v
}

func (v *managedVM) lock(ctx context.Context) error {
	select {
	case v.mutation <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-v.mutation

			return err
		}

		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (v *managedVM) publish() {
	v.view.Store(&managedVMView{record: v.record, run: v.run})
}

func (v *managedVM) unlock() {
	v.publish()
	<-v.mutation
}

type managedProcess struct {
	cmd         *exec.Cmd
	done        chan struct{}
	err         error // Published by closing done.
	endpoint    Endpoint
	performance *processPerformance // Private to this launch; never persisted or reused.
	profiling   *processProfiling
	cgroup      workerCgroup
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
		readMemory: readHostMemory, readResident: readProcessMemory,
		cgroups:   linuxVMCgroupFactory{},
		processes: make(map[*managedProcess]struct{}), creations: make(chan struct{}, config.MaxCreations),
	}
	if config.CgroupRoot != "" {
		if err := m.cgroups.Validate(config.CgroupRoot); err != nil {
			cancel()

			return nil, errors.Join(err, releaseManagerLock(lock))
		}
	}
	if err := m.loadImages(); err != nil {
		cancel()

		return nil, errors.Join(err, releaseManagerLock(lock))
	}
	if err := m.load(); err != nil {
		cancel()

		return nil, errors.Join(err, releaseManagerLock(lock))
	}

	return m, nil
}

func normalizeManagerConfig(c *ManagerConfig) error {
	if c.DataDir == "" || c.Firmware == "" || c.VarsTemplate == "" {
		return fmt.Errorf("state, firmware and variables template are required: %w", ErrInvalid)
	}
	if c.Binary == "" {
		var err error
		c.Binary, err = os.Executable()
		if err != nil {
			return err
		}
	}
	if err := normalizeManagerLimits(c); err != nil {
		return err
	}
	for _, path := range []*string{&c.DataDir, &c.Binary, &c.BaseImage, &c.Firmware, &c.VarsTemplate} {
		if *path == "" {
			continue
		}
		absolute, err := filepath.Abs(*path)
		if err != nil {
			return err
		}
		*path = absolute
	}
	for _, path := range []string{c.Binary, c.BaseImage, c.Firmware, c.VarsTemplate} {
		if path == "" {
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() == 0 {
			return fmt.Errorf("%s must be a nonempty regular file: %w", path, ErrInvalid)
		}
	}
	if c.BaseImage != "" {
		image, err := disk.OpenReadOnly(c.BaseImage)
		if err != nil {
			return fmt.Errorf("base image must be supported by gokvm (convert compressed qcow2 first): %w", err)
		}
		if err := validateAndCloseDiskImage(image, c.MaxDiskBytes); err != nil {
			return fmt.Errorf("configured base image: %w", err)
		}
	}

	return nil
}

func normalizeManagerLimits(c *ManagerConfig) error {
	if err := normalizeWorkerMemoryConfig(c); err != nil {
		return err
	}
	if c.RDPProfile == "" {
		c.RDPProfile = rdp.ProfileOffice
	}
	if _, err := rdp.ProfileFrameRate(c.RDPProfile); err != nil {
		return fmt.Errorf("invalid RDP profile %q: %w", c.RDPProfile, ErrInvalid)
	}
	if err := normalizeManagerStorageLimits(c); err != nil {
		return err
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
	if c.MaxCreations == 0 {
		c.MaxCreations = defaultMaxCreations
	}
	if c.MaxCreations < 1 || c.MaxCreations > 64 {
		return fmt.Errorf("concurrent creation limit must be between 1 and 64: %w", ErrInvalid)
	}
	if c.CPUs < 1 || c.CPUs > 64 || c.MemoryMiB < 256 || c.MemoryMiB > 1048576 || c.MaxVMsPerUser < 1 || c.MaxRunning < 1 {
		return fmt.Errorf("invalid VM resource limits: %w", ErrInvalid)
	}
	if c.MemoryBudgetMiB < 0 || c.MemoryBudgetMiB > 1<<30 {
		return fmt.Errorf("memory budget must be between 0 and 1073741824 MiB: %w", ErrInvalid)
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
		// Older versions appended unbounded child output here. Managed guests
		// no longer persist process output, so reclaim any legacy log on load.
		logPath := filepath.Join(m.directory(entry.Name()), "console.log")
		if err := os.Remove(logPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove legacy VM console log: %w", err)
		}
		record.State, record.Error = "stopped", ""
		m.vms[record.ID] = newManagedVM(record)
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
	if v == nil || v.view.Load().record.Owner != owner {
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
	result := make([]VM, 0)
	for _, v := range m.vms {
		record := v.view.Load().record
		if record.Owner == owner {
			result = append(result, record)
		}
	}
	m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	slices.SortFunc(result, func(a, b VM) int { return a.CreatedAt.Compare(b.CreatedAt) })

	return result, nil
}

// Create clones the operator-configured default image into a standalone VM.
// Without a default image, callers must select an uploaded template with CreateWithImage.
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
	image, imageFile, err := m.selectedImage(imageID)
	if err != nil {
		return VM{}, err
	}
	defer imageFile.Close()
	v := newManagedVM(VM{
		ImageID: image.ID, ImageName: image.Name,
		ID: strings.ToLower(rand.Text()), Name: name, Owner: owner,
		State: "creating", CreatedAt: time.Now().UTC(),
	})
	if err := v.lock(ctx); err != nil {
		return VM{}, err
	}
	defer v.unlock()
	if err := m.reserveVM(v); err != nil {
		return VM{}, err
	}
	if err := m.prepareWithSlot(ctx, v.record, passwordHash, imageFile, report); err != nil {
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

// prepareWithSlot queues provisioning without holding a catalog or readable state lock.
func (m *Manager) prepareWithSlot(
	ctx context.Context, record VM, passwordHash string, imageFile *os.File, report func(CreateProgress),
) error {
	select {
	case m.creations <- struct{}{}:
		defer func() { <-m.creations }()
	case <-ctx.Done():
		return ctx.Err()
	}

	return m.prepareOpenImageWithProgress(ctx, record, passwordHash, imageFile, report)
}

func (m *Manager) reserveVM(v *managedVM) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrConflict
	}
	count := 0
	for _, current := range m.vms {
		record := current.view.Load().record
		if record.Owner != v.record.Owner {
			continue
		}
		if record.Name == v.record.Name {
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
	if err := v.lock(ctx); err != nil {
		return err
	}
	defer v.unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if v.run != nil || v.record.State != "stopped" {
		return ErrConflict
	}
	if _, err := m.validatePrivateDisk(filepath.Join(m.directory(v.record.ID), "disk.img")); err != nil {
		return fmt.Errorf("validate VM disk before start: %w", err)
	}
	m.mu.Lock()
	if m.isolationErr != nil {
		err := m.isolationErr
		m.mu.Unlock()

		return err
	}
	if m.closed || m.active >= m.config.MaxRunning {
		m.mu.Unlock()

		return fmt.Errorf("running desktop limit reached: %w", ErrConflict)
	}
	if err := m.checkMemory(); err != nil {
		m.mu.Unlock()

		return err
	}
	m.active++
	m.jobs.Add(1)
	m.mu.Unlock()
	run, err := m.startProcess(v)
	if err != nil {
		m.mu.Lock()
		if !errors.Is(err, errWorkerCleanup) {
			m.active--
		}
		m.mu.Unlock()
		m.jobs.Done()

		return err
	}
	m.mu.Lock()
	m.processes[run] = struct{}{}
	m.mu.Unlock()
	v.run = run
	v.record.State, v.record.Error = "starting", ""
	v.publish()
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
	address := "@gokvm-rdp-" + rand.Text()
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: address, Net: "unix"})
	if err != nil {
		return nil, fmt.Errorf("create private RDP listener: %w", err)
	}
	defer listener.Close()
	// Keep the abstract socket bound until exec inherits it. Its address stays
	// in gateway state rather than worker arguments; the worker enforces access
	// using kernel peer credentials.
	listenerFile, err := listener.File()
	if err != nil {
		return nil, fmt.Errorf("duplicate RDP listener: %w", err)
	}
	defer listenerFile.Close()
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
	if _, err := m.validatePrivateDisk(filepath.Join(dir, "disk.img")); err != nil {
		return nil, fmt.Errorf("validate VM disk before worker exec: %w", err)
	}
	cert, err := readPrivateFile(filepath.Join(dir, "rdp.crt"), 64<<10)
	if err != nil {
		return nil, err
	}
	args := []string{
		"boot", "-uefi", m.config.Firmware, "-uefi-vars", filepath.Join(dir, "vars.fd"),
		"-d", filepath.Join(dir, "disk.img"), "-cloud-init", filepath.Join(dir, "seed.iso"),
		"-c", fmt.Sprint(m.config.CPUs), "-m", fmt.Sprintf("%dM", m.config.MemoryMiB), "-net", "user-isolated",
		"-rdp-fd", "3",
		"-rdp-cert", filepath.Join(dir, "rdp.crt"), "-rdp-key", filepath.Join(dir, "rdp.key"),
		"-rdp-h264", "-rdp-h264-encoder", m.config.Encoder,
		"-rdp-profile", m.config.RDPProfile,
	}
	cmd := exec.Command(m.config.Binary, args...) //nolint:gosec // Operator binary; validated VM paths, no shell.
	profiling := m.prepareProfiling(dir)
	defer func() {
		if profiling != nil {
			profiling.close()
		}
	}()
	if profiling != nil {
		cmd.Args = append(cmd.Args, "-profile-socket", profiling.socket)
	}
	// Leave stdout and stderr unset so os/exec connects them to the null
	// device. Guest-controlled output must not consume persistent host storage.
	cmd.ExtraFiles = []*os.File{listenerFile} // ExtraFiles[0] is descriptor 3 in the child.
	performanceConn, performanceFile, _ := newPerformancePipe()
	if performanceFile != nil {
		defer performanceFile.Close()
		defer func() {
			if performanceConn != nil {
				_ = performanceConn.Close()
			}
		}()
		cmd.ExtraFiles = append(cmd.ExtraFiles, performanceFile)
		cmd.Args = append(cmd.Args, "-telemetry-fd", "4")
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGTERM}
	cgroup, err := m.startConfinedWorker(cmd, v.record.ID)
	if err != nil {
		return nil, err
	}

	run := &managedProcess{
		cmd: cmd, done: make(chan struct{}),
		endpoint:  Endpoint{Address: address, ServerName: "vm-" + v.record.ID, CertificatePEM: cert},
		profiling: profiling,
		cgroup:    cgroup,
	}
	profiling = nil // The running process now owns this launch's private directory.
	if performanceConn != nil {
		run.performance = newProcessPerformance(performanceConn)
		performanceConn = nil // The reader now owns the connection.
	}

	return run, nil
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
			return fmt.Errorf("desktop process exited during startup: %w", errors.Join(run.err, ErrConflict))
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
					return fmt.Errorf("desktop process exited during startup: %w", errors.Join(run.err, ErrConflict))
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
	var cleanupErr error
	if run.cgroup != nil {
		cleanupErr = m.cleanupWorker(run.cgroup)
		run.err = errors.Join(run.err, cleanupErr)
	}
	if run.profiling != nil {
		run.profiling.close()
	}
	if run.performance != nil {
		run.performance.close()
	}
	m.mu.Lock()
	delete(m.processes, run)
	if cleanupErr == nil {
		m.active--
	}
	m.mu.Unlock()
	close(run.done)
	// Cleanup must publish the final state even after manager cancellation.
	v.mutation <- struct{}{}
	defer v.unlock()
	if v.run != run {
		return
	}
	v.run = nil
	v.record.State = "stopped"
	if run.err != nil && v.record.Error == "" {
		v.record.Error = "Desktop stopped unexpectedly."
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
	if err := v.lock(ctx); err != nil {
		return err
	}
	defer v.unlock()
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
			v.record.Error = "Desktop stopped unexpectedly."

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
	if err := v.lock(ctx); err != nil {
		return err
	}
	defer v.unlock()
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
	view := v.view.Load()
	if err := ctx.Err(); err != nil {
		return Endpoint{}, err
	}
	if view.run == nil || view.record.State != "running" {
		return Endpoint{}, ErrConflict
	}
	select {
	case <-view.run.done:
		return Endpoint{}, ErrConflict
	default:
	}
	endpoint := view.run.endpoint
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
	v.record.Error = "Desktop startup failed."

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
				run := v.view.Load().run
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
		go func() {
			m.jobs.Wait()
			m.mu.Lock()
			isolationErr := m.isolationErr
			m.mu.Unlock()
			finished <- errors.Join(isolationErr, releaseManagerLock(m.lock))
		}()
		select {
		case err := <-finished:
			m.closeErr = errors.Join(errors.Join(errorsByVM...), err)
		case <-time.After(min(m.config.StopTimeout, 2*time.Second)):
			m.closeErr = errors.Join(errors.Join(errorsByVM...), errManagerCloseTimeout)
		}
	})

	return m.closeErr
}

func releaseManagerLock(lock *os.File) error {
	// A concurrently forked child retains this open file description until exec.
	// Explicitly unlock so those inherited references cannot delay release.
	unlockErr := unix.Flock(int(lock.Fd()), unix.LOCK_UN)

	return errors.Join(unlockErr, lock.Close())
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

	return cloneOpenFileWithProgress(ctx, input, target, report)
}

func cloneOpenFileWithProgress(
	ctx context.Context, input *os.File, target string, report func(CreateProgress),
) (cloneErr error) {
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
