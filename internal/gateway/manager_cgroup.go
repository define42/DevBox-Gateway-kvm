package gateway

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const (
	maxCgroupControlBytes = 4096
	maxCgroupNameAttempts = 4
	maxVMTasks            = 512
	cgroupCleanupTimeout  = 2 * time.Second
	cgroupCleanupPoll     = 10 * time.Millisecond
)

// ErrCgroupUnavailable reports that a worker cannot be launched with its
// required kernel-enforced memory and process limits.
var (
	ErrCgroupUnavailable  = errors.New("VM resource isolation is unavailable")
	errInvalidCgroupState = errors.New("invalid or unsafe cgroup state")
	errCgroupPathChanged  = errors.New("cgroup child path changed")
)

// workerCgroup is the lifecycle surface consumed by Manager. Keeping the
// factory internal lets tests substitute a harmless implementation without a
// production configuration switch that could disable isolation.
type workerCgroup interface {
	Apply(*syscall.SysProcAttr) error
	CloseFD() error
	Cleanup() error
}

type vmCgroupFactory interface {
	Validate(string) error
	Prepare(root, vmID string, memoryMax uint64) (workerCgroup, error)
}

type linuxVMCgroupFactory struct{}

func (linuxVMCgroupFactory) Validate(root string) error {
	return validateVMCgroupRootAtStartup(linuxVMCgroupSystem{}, root)
}

func (linuxVMCgroupFactory) Prepare(root, vmID string, memoryMax uint64) (workerCgroup, error) {
	return prepareVMLaunchCgroup(root, vmID, memoryMax)
}

type vmCgroupDirectoryInfo struct {
	device, inode uint64
	mode          uint32
	uid           uint32
	filesystem    int64
}

func (i vmCgroupDirectoryInfo) sameFile(other vmCgroupDirectoryInfo) bool {
	return i.device == other.device && i.inode == other.inode
}

type vmCgroupDirectory interface {
	FD() int
	Info() (vmCgroupDirectoryInfo, error)
	ReadFile(string) (string, error)
	WriteFile(name, value string) error
	CheckWritable(string) error
	CreateChild(string) (vmCgroupDirectory, error)
	OpenChild(string, vmCgroupDirectoryInfo) (vmCgroupDirectory, error)
	RemoveChild(string, vmCgroupDirectoryInfo) error
	Close() error
}

type vmCgroupSystem interface {
	OpenRoot(string) (vmCgroupDirectory, error)
	ChildName(string) string
	EffectiveUID() uint32
}

type linuxVMCgroupSystem struct{}

func (linuxVMCgroupSystem) OpenRoot(root string) (vmCgroupDirectory, error) {
	how := &unix.OpenHow{
		Flags: uint64(unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC),
		Resolve: unix.RESOLVE_NO_MAGICLINKS |
			unix.RESOLVE_NO_SYMLINKS,
	}
	fd, err := unix.Openat2(unix.AT_FDCWD, root, how)
	if err != nil {
		return nil, err
	}

	return &linuxVMCgroupDirectory{fd: fd}, nil
}

func (linuxVMCgroupSystem) ChildName(vmID string) string {
	return "vm-" + vmID + "-" + rand.Text()
}

func (linuxVMCgroupSystem) EffectiveUID() uint32 {
	return uint32(os.Geteuid())
}

type linuxVMCgroupDirectory struct {
	fd int
}

func (d *linuxVMCgroupDirectory) FD() int {
	return d.fd
}

func (d *linuxVMCgroupDirectory) Info() (vmCgroupDirectoryInfo, error) {
	if d.fd < 0 {
		return vmCgroupDirectoryInfo{}, os.ErrClosed
	}
	var stat unix.Stat_t
	if err := unix.Fstat(d.fd, &stat); err != nil {
		return vmCgroupDirectoryInfo{}, err
	}
	var statfs unix.Statfs_t
	if err := unix.Fstatfs(d.fd, &statfs); err != nil {
		return vmCgroupDirectoryInfo{}, err
	}

	return vmCgroupDirectoryInfo{
		device: stat.Dev, inode: stat.Ino, mode: stat.Mode, uid: stat.Uid,
		filesystem: statfs.Type,
	}, nil
}

func (d *linuxVMCgroupDirectory) ReadFile(name string) (value string, readErr error) {
	fd, err := d.openControl(name, unix.O_RDONLY)
	if err != nil {
		return "", err
	}
	defer func() { readErr = errors.Join(readErr, unix.Close(fd)) }()

	data := make([]byte, 0, 128)
	buffer := make([]byte, 512)
	for {
		n, err := unix.Read(fd, buffer)
		if n > 0 {
			if len(data)+n > maxCgroupControlBytes {
				return "", fmt.Errorf(
					"%w: control file exceeds %d bytes", errInvalidCgroupState, maxCgroupControlBytes,
				)
			}
			data = append(data, buffer[:n]...)
		}
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if err != nil {
			return "", err
		}
		if n == 0 {
			return string(data), nil
		}
	}
}

func (d *linuxVMCgroupDirectory) WriteFile(name, value string) (writeErr error) {
	fd, err := d.openControl(name, unix.O_WRONLY)
	if err != nil {
		return err
	}
	defer func() { writeErr = errors.Join(writeErr, unix.Close(fd)) }()

	data := []byte(value)
	for {
		n, err := unix.Write(fd, data)
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if err != nil {
			return err
		}
		if n != len(data) {
			return io.ErrShortWrite
		}

		return nil
	}
}

func (d *linuxVMCgroupDirectory) CheckWritable(name string) (checkErr error) {
	fd, err := d.openControl(name, unix.O_WRONLY)
	if err != nil {
		return err
	}
	defer func() { checkErr = errors.Join(checkErr, unix.Close(fd)) }()

	return nil
}

func (d *linuxVMCgroupDirectory) CreateChild(name string) (vmCgroupDirectory, error) {
	if !validCgroupEntryName(name) {
		return nil, fmt.Errorf("%w: invalid cgroup child name %q", errInvalidCgroupState, name)
	}
	if err := unix.Mkdirat(d.fd, name, 0o700); err != nil {
		return nil, err
	}
	how := &unix.OpenHow{
		Flags: uint64(unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC),
		Resolve: unix.RESOLVE_BENEATH |
			unix.RESOLVE_NO_MAGICLINKS |
			unix.RESOLVE_NO_SYMLINKS,
	}
	fd, err := unix.Openat2(d.fd, name, how)
	if err != nil {
		var stat unix.Stat_t
		statErr := unix.Fstatat(d.fd, name, &stat, unix.AT_SYMLINK_NOFOLLOW)
		if statErr != nil {
			return nil, errors.Join(err, statErr)
		}
		expected := vmCgroupDirectoryInfo{device: stat.Dev, inode: stat.Ino, mode: stat.Mode}

		return nil, errors.Join(err, d.RemoveChild(name, expected))
	}

	return &linuxVMCgroupDirectory{fd: fd}, nil
}

func (d *linuxVMCgroupDirectory) OpenChild(
	name string, expected vmCgroupDirectoryInfo,
) (vmCgroupDirectory, error) {
	if !validCgroupEntryName(name) {
		return nil, fmt.Errorf("%w: invalid cgroup child name %q", errInvalidCgroupState, name)
	}
	how := &unix.OpenHow{
		Flags: uint64(unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC),
		Resolve: unix.RESOLVE_BENEATH |
			unix.RESOLVE_NO_MAGICLINKS |
			unix.RESOLVE_NO_SYMLINKS,
	}
	fd, err := unix.Openat2(d.fd, name, how)
	if err != nil {
		return nil, err
	}
	child := &linuxVMCgroupDirectory{fd: fd}
	actual, err := child.Info()
	if err != nil {
		return nil, errors.Join(err, child.Close())
	}
	if !actual.sameFile(expected) {
		return nil, errors.Join(errCgroupPathChanged, child.Close())
	}

	return child, nil
}

func (d *linuxVMCgroupDirectory) RemoveChild(name string, expected vmCgroupDirectoryInfo) error {
	if !validCgroupEntryName(name) {
		return fmt.Errorf("%w: invalid cgroup child name %q", errInvalidCgroupState, name)
	}
	var stat unix.Stat_t
	if err := unix.Fstatat(d.fd, name, &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil
		}

		return err
	}
	actual := vmCgroupDirectoryInfo{device: stat.Dev, inode: stat.Ino, mode: stat.Mode}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR || !actual.sameFile(expected) {
		return errCgroupPathChanged
	}

	return unix.Unlinkat(d.fd, name, unix.AT_REMOVEDIR)
}

func (d *linuxVMCgroupDirectory) Close() error {
	if d.fd < 0 {
		return nil
	}
	fd := d.fd
	d.fd = -1

	return unix.Close(fd)
}

func (d *linuxVMCgroupDirectory) openControl(name string, flags int) (int, error) {
	if d.fd < 0 {
		return -1, os.ErrClosed
	}
	if !validCgroupEntryName(name) {
		return -1, fmt.Errorf("%w: invalid cgroup control name %q", errInvalidCgroupState, name)
	}
	how := &unix.OpenHow{
		Flags: uint64(flags | unix.O_CLOEXEC | unix.O_NOFOLLOW),
		Resolve: unix.RESOLVE_BENEATH |
			unix.RESOLVE_NO_MAGICLINKS |
			unix.RESOLVE_NO_SYMLINKS,
	}
	fd, err := unix.Openat2(d.fd, name, how)
	if err != nil {
		return -1, err
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		_ = unix.Close(fd)

		return -1, err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG {
		_ = unix.Close(fd)

		return -1, fmt.Errorf("%w: cgroup control is not a regular file", errInvalidCgroupState)
	}

	return fd, nil
}

func validCgroupEntryName(name string) bool {
	if name == "" || len(name) > 128 || name == "." || name == ".." {
		return false
	}
	for _, character := range name {
		if character >= 'a' && character <= 'z' ||
			character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' ||
			character == '.' || character == '_' || character == '-' {
			continue
		}

		return false
	}

	return true
}

type vmLaunchCgroup struct {
	mu       sync.Mutex
	root     vmCgroupDirectory
	child    vmCgroupDirectory
	name     string
	identity vmCgroupDirectoryInfo
	finished bool
	finalErr error
}

func prepareVMLaunchCgroup(root, vmID string, memoryMax uint64) (*vmLaunchCgroup, error) {
	return prepareVMLaunchCgroupWithSystem(linuxVMCgroupSystem{}, root, vmID, memoryMax)
}

func validateVMCgroupRootAtStartup(system vmCgroupSystem, root string) (validateErr error) {
	if err := validateCgroupRootPath(root); err != nil {
		return err
	}
	rootDirectory, err := system.OpenRoot(root)
	if err != nil {
		return cgroupFailure("open delegated root", err)
	}
	defer func() { validateErr = errors.Join(validateErr, closeCgroupRoot(rootDirectory)) }()
	if err := validateCgroupRoot(rootDirectory, system.EffectiveUID()); err != nil {
		return cgroupFailure("validate delegated root", err)
	}
	events, err := rootDirectory.ReadFile("cgroup.events")
	if err != nil {
		return cgroupFailure("read delegated root cgroup.events", err)
	}
	populated, err := parseCgroupPopulated(events)
	if err != nil {
		return cgroupFailure("validate delegated root cgroup.events", err)
	}
	if populated {
		return cgroupFailure("delegated root contains live orphan processes", nil)
	}

	return nil
}

func prepareVMLaunchCgroupWithSystem(
	system vmCgroupSystem, root, vmID string, memoryMax uint64,
) (*vmLaunchCgroup, error) {
	if err := validateCgroupRootPath(root); err != nil {
		return nil, err
	}
	if !validID(vmID) {
		return nil, cgroupFailure("invalid VM identifier", nil)
	}
	if memoryMax == 0 || memoryMax > math.MaxInt64 {
		return nil, cgroupFailure("memory limit is outside the supported range", nil)
	}

	rootDirectory, err := system.OpenRoot(root)
	if err != nil {
		return nil, cgroupFailure("open delegated root", err)
	}
	if err := validateCgroupRoot(rootDirectory, system.EffectiveUID()); err != nil {
		return nil, errors.Join(cgroupFailure("validate delegated root", err), closeCgroupRoot(rootDirectory))
	}

	child, name, err := createVMCgroup(system, rootDirectory, vmID)
	if err != nil {
		return nil, errors.Join(cgroupFailure("create launch cgroup", err), closeCgroupRoot(rootDirectory))
	}
	identity, err := child.Info()
	if err != nil {
		closeErr := errors.Join(child.Close(), rootDirectory.Close())

		return nil, errors.Join(
			cgroupFailure("identify launch cgroup", err),
			cgroupFailureIf("close cgroup descriptors", closeErr),
		)
	}
	group := &vmLaunchCgroup{
		root: rootDirectory, child: child, name: name, identity: identity,
	}
	if err := validateCgroupChild(child, system.EffectiveUID()); err != nil {
		return nil, errors.Join(cgroupFailure("validate launch cgroup", err), group.discardUnlaunched())
	}
	if err := configureVMCgroup(child, memoryMax); err != nil {
		return nil, errors.Join(cgroupFailure("configure launch cgroup", err), group.discardUnlaunched())
	}

	return group, nil
}

func validateCgroupRootPath(root string) error {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || strings.ContainsRune(root, 0) {
		return cgroupFailure("delegated root must be an absolute clean path", nil)
	}

	return nil
}

func validateCgroupRoot(directory vmCgroupDirectory, effectiveUID uint32) error {
	if err := validateCgroupDirectory(directory, effectiveUID); err != nil {
		return err
	}
	typeValue, err := directory.ReadFile("cgroup.type")
	if err != nil {
		return fmt.Errorf("read cgroup.type: %w", err)
	}
	if strings.TrimSpace(typeValue) != "domain" {
		return fmt.Errorf("%w: delegated root is not a domain cgroup", errInvalidCgroupState)
	}
	controllers, err := directory.ReadFile("cgroup.controllers")
	if err != nil {
		return fmt.Errorf("read cgroup.controllers: %w", err)
	}
	subtree, err := directory.ReadFile("cgroup.subtree_control")
	if err != nil {
		return fmt.Errorf("read cgroup.subtree_control: %w", err)
	}
	for _, controller := range []string{"memory", "pids"} {
		if !hasCgroupToken(controllers, controller) {
			return fmt.Errorf("%w: %s controller is unavailable", errInvalidCgroupState, controller)
		}
		if !hasCgroupToken(subtree, controller) {
			return fmt.Errorf("%w: %s controller is not delegated", errInvalidCgroupState, controller)
		}
	}
	processes, err := directory.ReadFile("cgroup.procs")
	if err != nil {
		return fmt.Errorf("read cgroup.procs: %w", err)
	}
	if strings.TrimSpace(processes) != "" {
		return fmt.Errorf("%w: delegated root contains processes", errInvalidCgroupState)
	}

	return nil
}

func validateCgroupChild(directory vmCgroupDirectory, effectiveUID uint32) error {
	if err := validateCgroupDirectory(directory, effectiveUID); err != nil {
		return err
	}
	typeValue, err := directory.ReadFile("cgroup.type")
	if err != nil {
		return fmt.Errorf("read cgroup.type: %w", err)
	}
	if strings.TrimSpace(typeValue) != "domain" {
		return fmt.Errorf("%w: launch cgroup is not a domain cgroup", errInvalidCgroupState)
	}
	processes, err := directory.ReadFile("cgroup.procs")
	if err != nil {
		return fmt.Errorf("read cgroup.procs: %w", err)
	}
	if strings.TrimSpace(processes) != "" {
		return fmt.Errorf("%w: new launch cgroup contains processes", errInvalidCgroupState)
	}
	if err := directory.CheckWritable("cgroup.kill"); err != nil {
		return fmt.Errorf("cgroup.kill is unavailable: %w", err)
	}
	events, err := directory.ReadFile("cgroup.events")
	if err != nil {
		return fmt.Errorf("read cgroup.events: %w", err)
	}
	populated, err := parseCgroupPopulated(events)
	if err != nil {
		return err
	}
	if populated {
		return fmt.Errorf("%w: new launch cgroup has live descendants", errInvalidCgroupState)
	}

	return nil
}

func validateCgroupDirectory(directory vmCgroupDirectory, effectiveUID uint32) error {
	info, err := directory.Info()
	if err != nil {
		return err
	}
	if info.filesystem != unix.CGROUP2_SUPER_MAGIC {
		return fmt.Errorf("%w: directory is not on cgroup v2", errInvalidCgroupState)
	}
	if info.mode&unix.S_IFMT != unix.S_IFDIR {
		return fmt.Errorf("%w: cgroup path is not a directory", errInvalidCgroupState)
	}
	if info.uid != effectiveUID || info.mode&0o022 != 0 {
		return fmt.Errorf("%w: cgroup directory is not privately controlled", errInvalidCgroupState)
	}

	return nil
}

func createVMCgroup(
	system vmCgroupSystem, root vmCgroupDirectory, vmID string,
) (vmCgroupDirectory, string, error) {
	for range maxCgroupNameAttempts {
		name := system.ChildName(vmID)
		if !validCgroupEntryName(name) {
			return nil, "", fmt.Errorf("%w: generated invalid child name", errInvalidCgroupState)
		}
		child, err := root.CreateChild(name)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return nil, "", err
		}

		return child, name, nil
	}

	return nil, "", fmt.Errorf("%w: no unique cgroup child name", errInvalidCgroupState)
}

func configureVMCgroup(directory vmCgroupDirectory, memoryMax uint64) error {
	controls := []struct {
		name, value string
	}{
		{name: "memory.max", value: strconv.FormatUint(memoryMax, 10)},
		{name: "memory.swap.max", value: "0"},
		{name: "memory.oom.group", value: "1"},
		{name: "pids.max", value: strconv.Itoa(maxVMTasks)},
	}
	for _, control := range controls {
		if err := directory.WriteFile(control.name, control.value); err != nil {
			return fmt.Errorf("write %s: %w", control.name, err)
		}
		actual, err := directory.ReadFile(control.name)
		if err != nil {
			return fmt.Errorf("verify %s: %w", control.name, err)
		}
		if strings.TrimSpace(actual) != control.value {
			return fmt.Errorf("%w: verify %s readback", errInvalidCgroupState, control.name)
		}
	}

	return nil
}

func hasCgroupToken(value, wanted string) bool {
	for _, token := range strings.Fields(value) {
		if token == wanted {
			return true
		}
	}

	return false
}

func parseCgroupPopulated(value string) (bool, error) {
	found, populated := false, false
	for line := range strings.SplitSeq(value, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields) != 2 {
			return false, fmt.Errorf("%w: malformed cgroup.events", errInvalidCgroupState)
		}
		if fields[0] != "populated" {
			continue
		}
		if found || fields[1] != "0" && fields[1] != "1" {
			return false, fmt.Errorf("%w: invalid populated state", errInvalidCgroupState)
		}
		found, populated = true, fields[1] == "1"
	}
	if !found {
		return false, fmt.Errorf("%w: missing populated state", errInvalidCgroupState)
	}

	return populated, nil
}

// Apply attaches the child at clone time. This must run before exec.Cmd.Start;
// moving the process after Start would leave an unbounded accounting window.
func (c *vmLaunchCgroup) Apply(attributes *syscall.SysProcAttr) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if attributes == nil {
		return cgroupFailure("apply launch cgroup to nil process attributes", nil)
	}
	if c.finished || c.child == nil || c.child.FD() < 0 {
		return cgroupFailure("launch cgroup descriptor is closed", nil)
	}
	if attributes.UseCgroupFD || attributes.CgroupFD != 0 {
		return cgroupFailure("process already has cgroup attributes", nil)
	}
	attributes.UseCgroupFD = true
	attributes.CgroupFD = c.child.FD()

	return nil
}

// CloseFD releases the child descriptor after exec.Cmd.Start has cloned the
// worker. Cleanup retains the pinned root and verifies the child's identity.
func (c *vmLaunchCgroup) CloseFD() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.closeChildLocked()
}

// Cleanup kills the launch's entire process tree, verifies it is recursively
// empty, and removes only the cgroup inode created by this launch.
func (c *vmLaunchCgroup) Cleanup() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.finished {
		return c.finalErr
	}
	closeErr := c.closeChildLocked()
	child, err := c.root.OpenChild(c.name, c.identity)
	if errors.Is(err, os.ErrNotExist) {
		return c.finishCleanupLocked(closeErr)
	}
	if err != nil {
		return c.finishCleanupLocked(closeErr, cgroupFailure("reopen launch cgroup", err))
	}
	killErr := killAndWaitCgroup(child)
	childCloseErr := cgroupFailureIf("close cleanup cgroup descriptor", child.Close())
	if killErr != nil {
		return c.finishCleanupLocked(
			closeErr, cgroupFailure("terminate launch cgroup", killErr), childCloseErr,
		)
	}
	removeErr := removeCgroupWithRetry(c.root, c.name, c.identity)

	return c.finishCleanupLocked(
		closeErr, childCloseErr, cgroupFailureIf("remove launch cgroup", removeErr),
	)
}

func (c *vmLaunchCgroup) discardUnlaunched() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.finished {
		return c.finalErr
	}
	closeErr := c.closeChildLocked()
	removeErr := removeCgroupWithRetry(c.root, c.name, c.identity)

	return c.finishCleanupLocked(
		closeErr, cgroupFailureIf("remove unlaunched cgroup", removeErr),
	)
}

func (c *vmLaunchCgroup) finishCleanupLocked(errs ...error) error {
	rootErr := c.root.Close()
	c.root = nil
	c.finished = true
	c.finalErr = errors.Join(
		errors.Join(errs...),
		cgroupFailureIf("close delegated cgroup root", rootErr),
	)

	return c.finalErr
}

func killAndWaitCgroup(directory vmCgroupDirectory) error {
	events, err := directory.ReadFile("cgroup.events")
	if err != nil {
		return fmt.Errorf("read cgroup.events: %w", err)
	}
	populated, err := parseCgroupPopulated(events)
	if err != nil {
		return err
	}
	if !populated {
		return nil
	}
	if err := directory.WriteFile("cgroup.kill", "1"); err != nil {
		return fmt.Errorf("write cgroup.kill: %w", err)
	}
	timeout := time.NewTimer(cgroupCleanupTimeout)
	ticker := time.NewTicker(cgroupCleanupPoll)
	defer timeout.Stop()
	defer ticker.Stop()
	for {
		events, err = directory.ReadFile("cgroup.events")
		if err != nil {
			return fmt.Errorf("read cgroup.events: %w", err)
		}
		populated, err = parseCgroupPopulated(events)
		if err != nil {
			return err
		}
		if !populated {
			return nil
		}
		select {
		case <-ticker.C:
		case <-timeout.C:
			return fmt.Errorf("%w: cgroup remained populated", errInvalidCgroupState)
		}
	}
}

func removeCgroupWithRetry(
	root vmCgroupDirectory, name string, expected vmCgroupDirectoryInfo,
) error {
	deadline := time.Now().Add(cgroupCleanupTimeout)
	for {
		err := root.RemoveChild(name, expected)
		if !errors.Is(err, unix.EBUSY) || time.Now().After(deadline) {
			return err
		}
		time.Sleep(cgroupCleanupPoll)
	}
}

func (c *vmLaunchCgroup) closeChildLocked() error {
	if c.child == nil {
		return nil
	}
	err := c.child.Close()
	c.child = nil

	return cgroupFailureIf("close launch cgroup descriptor", err)
}

func closeCgroupRoot(root vmCgroupDirectory) error {
	return cgroupFailureIf("close delegated cgroup root", root.Close())
}

func cgroupFailureIf(operation string, err error) error {
	if err == nil {
		return nil
	}

	return cgroupFailure(operation, err)
}

func cgroupFailure(operation string, err error) error {
	if err == nil {
		return fmt.Errorf("%w: %s", ErrCgroupUnavailable, operation)
	}

	return fmt.Errorf("%w: %s: %w", ErrCgroupUnavailable, operation, err)
}

var (
	_ vmCgroupFactory = linuxVMCgroupFactory{}
	_ workerCgroup    = (*vmLaunchCgroup)(nil)
)
