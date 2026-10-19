package gateway

import (
	"errors"
	"fmt"
	"math"
	"os"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

const testCgroupVMID = "abcdefghijklmnopqrstuvwxyz"

func TestPrepareVMLaunchCgroup(t *testing.T) {
	t.Parallel()
	system := newFakeVMCgroupSystem()
	group, err := prepareVMLaunchCgroupWithSystem(system, "/delegated", testCgroupVMID, 768<<20)
	if err != nil {
		t.Fatal(err)
	}
	child := system.root.children[group.name]
	for name, want := range map[string]string{
		"memory.max":       "805306368",
		"memory.swap.max":  "0",
		"memory.oom.group": "1",
	} {
		if got := child.files[name]; got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	attributes := &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGTERM}
	if err := group.Apply(attributes); err != nil {
		t.Fatal(err)
	}
	if !attributes.UseCgroupFD || attributes.CgroupFD != group.child.FD() {
		t.Fatalf("cgroup attributes = %+v", attributes)
	}
	if !attributes.Setpgid || attributes.Pdeathsig != syscall.SIGTERM {
		t.Fatalf("Apply replaced existing process attributes: %+v", attributes)
	}
	if system.root.closeCount != 0 {
		t.Fatal("delegated root was not pinned until cleanup")
	}
	if err := group.CloseFD(); err != nil {
		t.Fatal(err)
	}
	if err := group.CloseFD(); err != nil {
		t.Fatalf("second CloseFD = %v", err)
	}
	if err := group.Cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, exists := system.root.children[group.name]; exists {
		t.Fatal("Cleanup retained the launch cgroup")
	}
	if system.root.closeCount != 1 {
		t.Fatalf("delegated root close count = %d, want 1", system.root.closeCount)
	}
	if err := group.Cleanup(); err != nil {
		t.Fatalf("second Cleanup = %v", err)
	}
}

func TestValidateVMCgroupRootAtStartup(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name      string
		events    string
		wantError bool
	}{
		{name: "empty hierarchy", events: "populated 0\nfrozen 0\n"},
		{name: "live orphan descendants", events: "populated 1\nfrozen 0\n", wantError: true},
		{name: "missing state", events: "frozen 0\n", wantError: true},
		{name: "duplicate state", events: "populated 0\npopulated 1\n", wantError: true},
		{name: "invalid state", events: "populated 2\n", wantError: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			system := newFakeVMCgroupSystem()
			system.root.children["empty-external"] = system.newChildNode()
			system.root.files["cgroup.events"] = tt.events
			err := validateVMCgroupRootAtStartup(system, "/delegated")
			if (err != nil) != tt.wantError {
				t.Fatalf("Validate = %v, wantError %t", err, tt.wantError)
			}
			if err != nil && !errors.Is(err, ErrCgroupUnavailable) {
				t.Fatalf("Validate = %v, want ErrCgroupUnavailable", err)
			}
			if system.root.closeCount != 1 {
				t.Fatal("startup validation leaked the delegated root descriptor")
			}
			if system.root.children["empty-external"] == nil {
				t.Fatal("startup validation mutated an external empty child")
			}
		})
	}
}

func TestPrepareVMLaunchCgroupRejectsUnsafeOrUnavailableHierarchy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		root      string
		vmID      string
		memoryMax uint64
		setup     func(*fakeVMCgroupSystem)
	}{
		{name: "relative root", root: "delegated"},
		{name: "unclean root", root: "/delegated/../delegated"},
		{name: "invalid VM ID", vmID: "../desktop"},
		{name: "zero memory", memoryMax: 0},
		{name: "memory over kernel range", memoryMax: math.MaxUint64},
		{name: "root open failure", setup: func(s *fakeVMCgroupSystem) { s.openErr = os.ErrPermission }},
		{name: "not cgroup v2", setup: func(s *fakeVMCgroupSystem) {
			s.root.info.filesystem = unix.TMPFS_MAGIC
		}},
		{name: "wrong root owner", setup: func(s *fakeVMCgroupSystem) { s.root.info.uid++ }},
		{name: "group writable root", setup: func(s *fakeVMCgroupSystem) { s.root.info.mode |= 0o020 }},
		{name: "threaded root", setup: func(s *fakeVMCgroupSystem) { s.root.files["cgroup.type"] = "threaded" }},
		{name: "memory unavailable", setup: func(s *fakeVMCgroupSystem) {
			s.root.files["cgroup.controllers"] = "cpu memoryish pids"
		}},
		{name: "memory not delegated", setup: func(s *fakeVMCgroupSystem) {
			s.root.files["cgroup.subtree_control"] = "cpu pids"
		}},
		{name: "gateway in delegated root", setup: func(s *fakeVMCgroupSystem) {
			s.root.files["cgroup.procs"] = "42\n"
		}},
		{name: "child not cgroup v2", setup: func(s *fakeVMCgroupSystem) {
			s.mutateChild = func(child *fakeVMCgroupNode) { child.info.filesystem = unix.TMPFS_MAGIC }
		}},
		{name: "child not domain", setup: func(s *fakeVMCgroupSystem) {
			s.mutateChild = func(child *fakeVMCgroupNode) { child.files["cgroup.type"] = "threaded" }
		}},
		{name: "child unexpectedly populated", setup: func(s *fakeVMCgroupSystem) {
			s.mutateChild = func(child *fakeVMCgroupNode) { child.files["cgroup.procs"] = "99\n" }
		}},
		{name: "cgroup kill unavailable", setup: func(s *fakeVMCgroupSystem) {
			s.mutateChild = func(child *fakeVMCgroupNode) { delete(child.files, "cgroup.kill") }
		}},
		{name: "memory max unavailable", setup: func(s *fakeVMCgroupSystem) {
			s.mutateChild = func(child *fakeVMCgroupNode) { delete(child.files, "memory.max") }
		}},
		{name: "swap write denied", setup: func(s *fakeVMCgroupSystem) {
			s.writeErrors["memory.swap.max"] = os.ErrPermission
		}},
		{name: "kernel readback mismatch", setup: func(s *fakeVMCgroupSystem) {
			s.writeValues["memory.oom.group"] = "0"
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			system := newFakeVMCgroupSystem()
			if tt.setup != nil {
				tt.setup(system)
			}
			root, vmID, memoryMax := tt.root, tt.vmID, tt.memoryMax
			if root == "" {
				root = "/delegated"
			}
			if vmID == "" {
				vmID = testCgroupVMID
			}
			if memoryMax == 0 && tt.name != "zero memory" {
				memoryMax = 768 << 20
			}
			group, err := prepareVMLaunchCgroupWithSystem(system, root, vmID, memoryMax)
			if group != nil || !errors.Is(err, ErrCgroupUnavailable) {
				t.Fatalf("prepare = (%v, %v), want nil ErrCgroupUnavailable", group, err)
			}
			if len(system.root.children) != 0 {
				t.Fatalf("failed preparation leaked children: %v", system.root.children)
			}
		})
	}
}

func TestPrepareVMLaunchCgroupDoesNotReuseExistingChild(t *testing.T) {
	t.Parallel()
	system := newFakeVMCgroupSystem()
	system.names = []string{"vm-existing", "vm-new"}
	existing := system.newChildNode()
	system.root.children[system.names[0]] = existing
	group, err := prepareVMLaunchCgroupWithSystem(system, "/delegated", testCgroupVMID, 768<<20)
	if err != nil {
		t.Fatal(err)
	}
	if group.name != "vm-new" || system.root.children["vm-existing"] != existing {
		t.Fatalf("existing child was reused or replaced: %q", group.name)
	}
	if err := group.Cleanup(); err != nil {
		t.Fatal(err)
	}
	if system.root.children["vm-existing"] != existing {
		t.Fatal("cleanup removed an external child")
	}
}

func TestPrepareVMLaunchCgroupBoundsNameCollisions(t *testing.T) {
	t.Parallel()
	system := newFakeVMCgroupSystem()
	for index := range maxCgroupNameAttempts {
		name := fmt.Sprintf("vm-existing-%d", index)
		system.names = append(system.names, name)
		system.root.children[name] = system.newChildNode()
	}
	group, err := prepareVMLaunchCgroupWithSystem(system, "/delegated", testCgroupVMID, 768<<20)
	if group != nil || !errors.Is(err, ErrCgroupUnavailable) {
		t.Fatalf("prepare = (%v, %v), want collision failure", group, err)
	}
	if len(system.root.children) != maxCgroupNameAttempts {
		t.Fatal("collision handling changed an existing child")
	}
	if system.root.closeCount != 1 {
		t.Fatal("collision failure leaked the delegated root descriptor")
	}
}

func TestVMLaunchCgroupApplyFailsClosed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		attributes *syscall.SysProcAttr
		closeFirst bool
	}{
		{name: "nil attributes"},
		{name: "existing cgroup flag", attributes: &syscall.SysProcAttr{UseCgroupFD: true}},
		{name: "existing cgroup descriptor", attributes: &syscall.SysProcAttr{CgroupFD: 9}},
		{name: "closed descriptor", attributes: &syscall.SysProcAttr{}, closeFirst: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			system := newFakeVMCgroupSystem()
			group, err := prepareVMLaunchCgroupWithSystem(system, "/delegated", testCgroupVMID, 768<<20)
			if err != nil {
				t.Fatal(err)
			}
			if tt.closeFirst {
				if err := group.CloseFD(); err != nil {
					t.Fatal(err)
				}
			}
			if err := group.Apply(tt.attributes); !errors.Is(err, ErrCgroupUnavailable) {
				t.Fatalf("Apply = %v, want ErrCgroupUnavailable", err)
			}
			if err := group.Cleanup(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestVMLaunchCgroupCleanupRefusesReplacement(t *testing.T) {
	t.Parallel()
	system := newFakeVMCgroupSystem()
	group, err := prepareVMLaunchCgroupWithSystem(system, "/delegated", testCgroupVMID, 768<<20)
	if err != nil {
		t.Fatal(err)
	}
	replacement := system.newChildNode()
	system.root.children[group.name] = replacement
	if err := group.Cleanup(); !errors.Is(err, ErrCgroupUnavailable) {
		t.Fatalf("Cleanup = %v, want ErrCgroupUnavailable", err)
	}
	if system.root.children[group.name] != replacement {
		t.Fatal("Cleanup deleted a replacement directory")
	}
	if err := group.Cleanup(); !errors.Is(err, ErrCgroupUnavailable) {
		t.Fatalf("second Cleanup = %v, want original failure", err)
	}
}

func TestVMLaunchCgroupCleanupRetriesTransientBusy(t *testing.T) {
	t.Parallel()
	system := newFakeVMCgroupSystem()
	system.removeErrors = []error{unix.EBUSY, nil}
	group, err := prepareVMLaunchCgroupWithSystem(system, "/delegated", testCgroupVMID, 768<<20)
	if err != nil {
		t.Fatal(err)
	}
	if err := group.Cleanup(); err != nil {
		t.Fatalf("Cleanup = %v", err)
	}
	if system.root.closeCount != 1 {
		t.Fatal("successful cleanup did not close the delegated root")
	}
}

func TestVMLaunchCgroupCleanupKillsDescendants(t *testing.T) {
	t.Parallel()
	system := newFakeVMCgroupSystem()
	group, err := prepareVMLaunchCgroupWithSystem(system, "/delegated", testCgroupVMID, 768<<20)
	if err != nil {
		t.Fatal(err)
	}
	child := system.root.children[group.name]
	child.files["cgroup.events"] = "populated 1\nfrozen 0\n"
	if err := group.Cleanup(); err != nil {
		t.Fatal(err)
	}
	if child.killWrites != 1 {
		t.Fatalf("cgroup.kill writes = %d, want 1", child.killWrites)
	}
}

func TestVMLaunchCgroupCleanupFailureClosesDescriptors(t *testing.T) {
	t.Parallel()
	system := newFakeVMCgroupSystem()
	group, err := prepareVMLaunchCgroupWithSystem(system, "/delegated", testCgroupVMID, 768<<20)
	if err != nil {
		t.Fatal(err)
	}
	system.root.children[group.name].files["cgroup.events"] = "populated 1\nfrozen 0\n"
	system.writeErrors["cgroup.kill"] = os.ErrPermission
	if err := group.Cleanup(); !errors.Is(err, ErrCgroupUnavailable) {
		t.Fatalf("Cleanup = %v, want ErrCgroupUnavailable", err)
	}
	if system.root.closeCount != 1 {
		t.Fatal("terminal cleanup failure leaked the delegated root descriptor")
	}
	if err := group.Cleanup(); !errors.Is(err, ErrCgroupUnavailable) {
		t.Fatalf("second Cleanup = %v, want stable terminal failure", err)
	}
}

func TestValidCgroupEntryName(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		valid bool
	}{
		{name: "vm-safe_2.A", valid: true},
		{name: ""},
		{name: "."},
		{name: ".."},
		{name: "../escape"},
		{name: "nested/child"},
		{name: "line\nbreak"},
		{name: string(make([]byte, 129))},
	} {
		t.Run(fmt.Sprintf("%q", tt.name), func(t *testing.T) {
			t.Parallel()
			if got := validCgroupEntryName(tt.name); got != tt.valid {
				t.Fatalf("validCgroupEntryName(%q) = %t, want %t", tt.name, got, tt.valid)
			}
		})
	}
}

type fakeVMCgroupSystem struct {
	uid          uint32
	root         *fakeVMCgroupNode
	names        []string
	nameIndex    int
	nextFD       int
	nextInode    uint64
	openErr      error
	mutateChild  func(*fakeVMCgroupNode)
	writeErrors  map[string]error
	writeValues  map[string]string
	removeErrors []error
}

type fakeVMCgroupNode struct {
	system     *fakeVMCgroupSystem
	info       vmCgroupDirectoryInfo
	files      map[string]string
	children   map[string]*fakeVMCgroupNode
	closeCount int
	killWrites int
}

type fakeVMCgroupDirectory struct {
	node   *fakeVMCgroupNode
	fd     int
	closed bool
}

func newFakeVMCgroupSystem() *fakeVMCgroupSystem {
	system := &fakeVMCgroupSystem{
		uid: 1000, nextFD: 40, nextInode: 100,
		writeErrors: make(map[string]error), writeValues: make(map[string]string),
	}
	system.root = system.newNode(map[string]string{
		"cgroup.type":            "domain\n",
		"cgroup.controllers":     "cpu memory pids\n",
		"cgroup.subtree_control": "memory pids\n",
		"cgroup.procs":           "",
		"cgroup.events":          "populated 0\nfrozen 0\n",
	})

	return system
}

func (s *fakeVMCgroupSystem) OpenRoot(string) (vmCgroupDirectory, error) {
	if s.openErr != nil {
		return nil, s.openErr
	}

	return s.open(s.root), nil
}

func (s *fakeVMCgroupSystem) ChildName(vmID string) string {
	if s.nameIndex < len(s.names) {
		name := s.names[s.nameIndex]
		s.nameIndex++

		return name
	}
	name := fmt.Sprintf("vm-%s-%d", vmID, s.nameIndex)
	s.nameIndex++

	return name
}

func (s *fakeVMCgroupSystem) EffectiveUID() uint32 {
	return s.uid
}

func (s *fakeVMCgroupSystem) newNode(files map[string]string) *fakeVMCgroupNode {
	s.nextInode++

	return &fakeVMCgroupNode{
		system: s,
		info: vmCgroupDirectoryInfo{
			device: 1, inode: s.nextInode, mode: unix.S_IFDIR | 0o755,
			uid: s.uid, filesystem: unix.CGROUP2_SUPER_MAGIC,
		},
		files: files, children: make(map[string]*fakeVMCgroupNode),
	}
}

func (s *fakeVMCgroupSystem) newChildNode() *fakeVMCgroupNode {
	return s.newNode(map[string]string{
		"cgroup.type":        "domain\n",
		"cgroup.procs":       "",
		"cgroup.events":      "populated 0\nfrozen 0\n",
		"cgroup.kill":        "",
		"memory.max":         "max\n",
		"memory.swap.max":    "max\n",
		"memory.oom.group":   "0\n",
		"cgroup.controllers": "",
	})
}

func (s *fakeVMCgroupSystem) open(node *fakeVMCgroupNode) *fakeVMCgroupDirectory {
	s.nextFD++

	return &fakeVMCgroupDirectory{node: node, fd: s.nextFD}
}

func (d *fakeVMCgroupDirectory) FD() int {
	if d.closed {
		return -1
	}

	return d.fd
}

func (d *fakeVMCgroupDirectory) Info() (vmCgroupDirectoryInfo, error) {
	if d.closed {
		return vmCgroupDirectoryInfo{}, os.ErrClosed
	}

	return d.node.info, nil
}

func (d *fakeVMCgroupDirectory) ReadFile(name string) (string, error) {
	if d.closed {
		return "", os.ErrClosed
	}
	value, exists := d.node.files[name]
	if !exists {
		return "", os.ErrNotExist
	}

	return value, nil
}

func (d *fakeVMCgroupDirectory) WriteFile(name, value string) error {
	if d.closed {
		return os.ErrClosed
	}
	if err := d.node.system.writeErrors[name]; err != nil {
		return err
	}
	if _, exists := d.node.files[name]; !exists {
		return os.ErrNotExist
	}
	if replacement, exists := d.node.system.writeValues[name]; exists {
		value = replacement
	}
	d.node.files[name] = value
	if name == "cgroup.kill" && value == "1" {
		d.node.killWrites++
		d.node.files["cgroup.events"] = "populated 0\nfrozen 0\n"
	}

	return nil
}

func (d *fakeVMCgroupDirectory) CheckWritable(name string) error {
	if d.closed {
		return os.ErrClosed
	}
	if _, exists := d.node.files[name]; !exists {
		return os.ErrNotExist
	}

	return nil
}

func (d *fakeVMCgroupDirectory) CreateChild(name string) (vmCgroupDirectory, error) {
	if d.closed {
		return nil, os.ErrClosed
	}
	if _, exists := d.node.children[name]; exists {
		return nil, os.ErrExist
	}
	child := d.node.system.newChildNode()
	if d.node.system.mutateChild != nil {
		d.node.system.mutateChild(child)
	}
	d.node.children[name] = child

	return d.node.system.open(child), nil
}

func (d *fakeVMCgroupDirectory) OpenChild(
	name string, expected vmCgroupDirectoryInfo,
) (vmCgroupDirectory, error) {
	if d.closed {
		return nil, os.ErrClosed
	}
	child := d.node.children[name]
	if child == nil {
		return nil, os.ErrNotExist
	}
	if !child.info.sameFile(expected) {
		return nil, errCgroupPathChanged
	}

	return d.node.system.open(child), nil
}

func (d *fakeVMCgroupDirectory) RemoveChild(name string, expected vmCgroupDirectoryInfo) error {
	if d.closed {
		return os.ErrClosed
	}
	if len(d.node.system.removeErrors) > 0 {
		err := d.node.system.removeErrors[0]
		d.node.system.removeErrors = d.node.system.removeErrors[1:]
		if err != nil {
			return err
		}
	}
	child := d.node.children[name]
	if child == nil {
		return nil
	}
	if child.info.mode&unix.S_IFMT != unix.S_IFDIR || !child.info.sameFile(expected) {
		return errCgroupPathChanged
	}
	delete(d.node.children, name)

	return nil
}

func (d *fakeVMCgroupDirectory) Close() error {
	if d.closed {
		return nil
	}
	d.closed = true
	d.node.closeCount++

	return nil
}

var _ vmCgroupSystem = (*fakeVMCgroupSystem)(nil)
