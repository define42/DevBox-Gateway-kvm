package gateway

import (
	"log"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// processProfiling owns one unique directory for one child process. No profile
// samples or paths enter the public performance report or persisted VM record.
type processProfiling struct {
	directory string
	socket    string
	info      os.FileInfo
}

func (m *Manager) prepareProfiling(vmDirectory string) *processProfiling {
	if !m.config.VMProfiling {
		return nil
	}
	if err := checkPrivateDirectory(vmDirectory); err != nil {
		log.Printf("VM CPU profiling disabled: %v", err)

		return nil
	}
	directory, err := os.MkdirTemp(vmDirectory, "profile-")
	if err != nil {
		log.Printf("VM CPU profiling disabled: %v", err)

		return nil
	}
	//nolint:gosec // Generated private directory beneath the validated VM directory, not caller-selected state.
	info, err := os.Lstat(directory)
	if err != nil {
		log.Printf("VM CPU profiling directory unavailable: %v", err)

		return nil
	}
	p := &processProfiling{directory: directory, socket: filepath.Join(directory, "cpu.sock"), info: info}
	// Linux sockaddr_un reserves one byte for the pathname terminator.
	if len(p.socket) >= len(unix.RawSockaddrUnix{}.Path) {
		log.Print("VM CPU profiling disabled: state path exceeds the Unix socket path limit")
		p.close()

		return nil
	}

	return p
}

func (p *processProfiling) close() {
	//nolint:gosec // Inspect the generated launch directory; verify its identity before removing anything below it.
	current, err := os.Lstat(p.directory)
	if err != nil || !current.IsDir() || !os.SameFile(current, p.info) || !privateOwned(current) {
		return
	}
	// Called only after the child has exited (or failed to start). A killed
	// worker may leave its own socket behind. Never remove files or symlinks.
	//nolint:gosec // Fixed socket basename inside the identity-checked, private launch directory.
	if socket, err := os.Lstat(p.socket); err == nil && socket.Mode()&os.ModeSocket != 0 && privateOwned(socket) {
		_ = os.Remove(p.socket)
	}
	// Remove only the empty directory created for this launch, never recursively.
	_ = os.Remove(p.directory) //nolint:gosec // Generated directory whose ownership and identity were checked above.
}
