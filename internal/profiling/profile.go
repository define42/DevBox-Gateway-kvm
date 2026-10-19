// Package profiling provides opt-in, local-only worker CPU profiles.
package profiling

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime/pprof"
	"strconv"
	"sync"
	"syscall"
	"time"
)

const (
	defaultSeconds  = 10
	maxSeconds      = 30
	maxProfileBytes = 16 << 20
)

var (
	errPrivateDirectory = errors.New("profile socket requires an absolute path in a private, owned directory")
	errProfileSize      = errors.New("CPU profile exceeded the size limit")
)

// Start listens only on a private Unix socket, never TCP or the default HTTP
// mux. The directory must already exist, be owned by this user, and have no
// group/other permissions. Existing paths are never replaced. The returned
// stop function cancels and joins active profiling and removes only this socket.
// Profiles describe host-process execution, not guest application CPU stacks.
func Start(ctx context.Context, path string) (func(), error) {
	return start(ctx, path, runtimeProfiler{})
}

func start(ctx context.Context, path string, profiler cpuProfiler) (func(), error) {
	listener, info, err := listen(path)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	handler := &profileHandler{profiler: profiler}
	server := &http.Server{
		Handler:           handler,
		BaseContext:       func(net.Listener) context.Context { return ctx },
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      (maxSeconds + 5) * time.Second,
		IdleTimeout:       5 * time.Second,
		MaxHeaderBytes:    8 << 10,
	}
	served := make(chan struct{})
	go func() {
		defer close(served)
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("VM CPU profiling stopped: %v", err)
		}
	}()
	done := make(chan struct{})
	go func() {
		defer close(done)
		select {
		case <-ctx.Done():
		case <-served:
		}
		cancel()
		_ = server.Close()
		<-served
		// Close cancels requests but does not join their handlers. This lock
		// joins the only potentially long-lived handler and StopCPUProfile.
		handler.mu.Lock()
		handler.mu.Unlock() //nolint:staticcheck // Join the active handler; no state mutation is needed at this barrier.
		removeSocket(path, info)
	}()

	return func() { cancel(); <-done }, nil
}

func listen(path string) (*net.UnixListener, os.FileInfo, error) {
	if !filepath.IsAbs(path) {
		return nil, nil, errPrivateDirectory
	}
	parent, err := os.Lstat(filepath.Dir(path))
	if err != nil {
		return nil, nil, fmt.Errorf("profile socket directory: %w", err)
	}
	owner, ok := parent.Sys().(*syscall.Stat_t)
	if !parent.IsDir() || parent.Mode().Perm()&0o077 != 0 || !ok || owner.Uid != uint32(os.Geteuid()) {
		return nil, nil, errPrivateDirectory
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, nil, fmt.Errorf("listen for CPU profiles: %w", err)
	}
	listener.SetUnlinkOnClose(false)
	info, err := os.Lstat(path)
	if err != nil {
		_ = listener.Close()

		return nil, nil, fmt.Errorf("inspect profile socket: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = listener.Close()
		removeSocket(path, info)

		return nil, nil, fmt.Errorf("protect profile socket: %w", err)
	}

	return listener, info, nil
}

func removeSocket(path string, created os.FileInfo) {
	current, err := os.Lstat(path)
	if err == nil && current.Mode()&os.ModeSocket != 0 && os.SameFile(current, created) {
		_ = os.Remove(path)
	}
}

type cpuProfiler interface {
	Start(io.Writer) error
	Stop()
}

type runtimeProfiler struct{}

func (runtimeProfiler) Start(w io.Writer) error { return pprof.StartCPUProfile(w) }
func (runtimeProfiler) Stop()                   { pprof.StopCPUProfile() }

type profileHandler struct {
	mu       sync.Mutex
	profiler cpuProfiler
}

func (h *profileHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.URL.Path != "/debug/pprof/profile" {
		http.NotFound(w, r)

		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "GET required", http.StatusMethodNotAllowed)

		return
	}
	seconds, err := profileSeconds(r.URL.RawQuery)
	if err != nil {
		http.Error(w, "seconds must be an integer from 1 through 30", http.StatusBadRequest)

		return
	}
	if !h.mu.TryLock() {
		http.Error(w, "CPU profiling already active", http.StatusConflict)

		return
	}
	defer h.mu.Unlock()
	if r.Context().Err() != nil {
		return
	}
	buffer := &profileBuffer{}
	if err := h.profiler.Start(buffer); err != nil {
		http.Error(w, "CPU profiling unavailable", http.StatusServiceUnavailable)

		return
	}
	timer := time.NewTimer(time.Duration(seconds) * time.Second)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-r.Context().Done():
	}
	h.profiler.Stop()
	if r.Context().Err() != nil {
		return
	}
	if buffer.exceeded {
		http.Error(w, "CPU profile exceeded the size limit", http.StatusServiceUnavailable)

		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="vm-cpu.pprof"`)
	w.Header().Set("Content-Length", strconv.Itoa(buffer.data.Len()))
	_, _ = w.Write(buffer.data.Bytes())
}

func profileSeconds(query string) (int, error) {
	values, err := url.ParseQuery(query)
	if err != nil {
		return 0, err
	}
	if len(values) == 0 {
		return defaultSeconds, nil
	}
	if len(values) != 1 || len(values["seconds"]) != 1 {
		return 0, strconv.ErrSyntax
	}
	seconds, err := strconv.Atoi(values.Get("seconds"))
	if err != nil || seconds < 1 || seconds > maxSeconds {
		return 0, strconv.ErrSyntax
	}

	return seconds, nil
}

// profileBuffer is written only by the runtime profile writer, and read only
// after StopCPUProfile has joined that writer. No socket writes can delay stop.
type profileBuffer struct {
	data     bytes.Buffer
	exceeded bool
}

func (b *profileBuffer) Write(p []byte) (int, error) {
	if b.exceeded || len(p) > maxProfileBytes-b.data.Len() {
		b.exceeded = true

		return 0, errProfileSize
	}

	return b.data.Write(p)
}
