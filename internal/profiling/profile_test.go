package profiling

import (
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func profileTestDirectory(t *testing.T) string {
	t.Helper()
	// Keep paths shorter than sockaddr_un even with long test names.
	directory, err := os.MkdirTemp("", "devbox-kvm-prof-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(directory); err != nil {
			t.Error(err)
		}
	})

	return directory
}

func unixClient(path string) *http.Client {
	return &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", path)
			},
		},
	}
}

func TestStartCPUProfile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(profileTestDirectory(t), "cpu.sock")
	stop, err := Start(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
	info, err := os.Lstat(path)
	if err != nil || info.Mode().Perm() != 0o600 || info.Mode()&os.ModeSocket == 0 {
		t.Fatalf("socket is not private: info=%v err=%v", info, err)
	}
	client := unixClient(path)
	defer client.CloseIdleConnections()
	response, err := client.Get("http://local/debug/pprof/profile?seconds=1")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "application/octet-stream" {
		t.Fatalf("profile response: %s, %v", response.Status, response.Header)
	}
	reader, err := gzip.NewReader(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, maxProfileBytes))
	if err != nil || len(data) == 0 {
		t.Fatalf("invalid compressed CPU profile: %d bytes, %v", len(data), err)
	}
	stop()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("socket not removed after stop: %v", err)
	}
}

func TestStartShutdownStopsProfile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(profileTestDirectory(t), "cpu.sock")
	profiler := newFakeProfiler()
	stop, err := start(t.Context(), path, profiler)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
	client := unixClient(path)
	defer client.CloseIdleConnections()
	done := make(chan struct{})
	go func() {
		defer close(done)
		response, err := client.Get("http://local/debug/pprof/profile?seconds=30")
		if err == nil {
			_ = response.Body.Close()
		}
	}()
	select {
	case <-profiler.started:
	case <-time.After(5 * time.Second):
		t.Fatal("profile did not start")
	}
	stop()
	select {
	case <-profiler.stopped:
	default:
		t.Fatal("shutdown did not join CPU profiling")
	}
	<-done
}

func TestStartPreservesReplacementPath(t *testing.T) {
	t.Parallel()
	directory := profileTestDirectory(t)
	path := filepath.Join(directory, "cpu.sock")
	stop, err := Start(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
	if err := os.Rename(path, filepath.Join(directory, "original.sock")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("preserve me"), 0o600); err != nil {
		t.Fatal(err)
	}
	stop()
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "preserve me" {
		t.Fatalf("replacement path changed: %q, %v", data, err)
	}
}

func TestListenRejectsUnsafePaths(t *testing.T) {
	t.Parallel()
	for _, name := range []string{
		"relative", "public directory", "symlink directory", "existing file", "existing socket",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			directory := profileTestDirectory(t)
			path := filepath.Join(directory, "cpu.sock")
			switch name {
			case "relative":
				path = "cpu.sock"
			case "public directory":
				if err := os.Chmod(directory, 0o755); err != nil {
					t.Fatal(err)
				}
			case "symlink directory":
				link := filepath.Join(directory, "alias")
				if err := os.Symlink(directory, link); err != nil {
					t.Fatal(err)
				}
				path = filepath.Join(link, "cpu.sock")
			case "existing file":
				if err := os.WriteFile(path, []byte("existing"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "existing socket":
				listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
				if err != nil {
					t.Fatal(err)
				}
				defer listener.Close()
			}
			if stop, err := Start(t.Context(), path); err == nil {
				stop()
				t.Fatal("unsafe profiling socket was accepted")
			}
			if name == "existing file" {
				data, err := os.ReadFile(path)
				if err != nil || string(data) != "existing" {
					t.Fatalf("existing file changed: %q, %v", data, err)
				}
			}
		})
	}
}

type fakeProfiler struct {
	started chan struct{}
	stopped chan struct{}
	writer  io.Writer
	err     error
}

func newFakeProfiler() *fakeProfiler {
	return &fakeProfiler{started: make(chan struct{}), stopped: make(chan struct{})}
}

func (p *fakeProfiler) Start(w io.Writer) error {
	p.writer = w
	close(p.started)

	return p.err
}

func (p *fakeProfiler) Stop() {
	_, _ = io.WriteString(p.writer, "profile data")
	close(p.stopped)
}

func TestProfileHandlerValidation(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, method, target string
		status               int
	}{
		{name: "index hidden", method: "GET", target: "/debug/pprof/", status: 404},
		{name: "heap hidden", method: "GET", target: "/debug/pprof/heap", status: 404},
		{name: "no post", method: "POST", target: "/debug/pprof/profile", status: 405},
		{name: "no head", method: "HEAD", target: "/debug/pprof/profile", status: 405},
		{name: "zero", method: "GET", target: "/debug/pprof/profile?seconds=0", status: 400},
		{name: "negative", method: "GET", target: "/debug/pprof/profile?seconds=-1", status: 400},
		{name: "too long", method: "GET", target: "/debug/pprof/profile?seconds=31", status: 400},
		{name: "bad duration", method: "GET", target: "/debug/pprof/profile?seconds=abc", status: 400},
		{name: "duplicate", method: "GET", target: "/debug/pprof/profile?seconds=1&seconds=2", status: 400},
		{name: "unknown option", method: "GET", target: "/debug/pprof/profile?debug=1", status: 400},
		{name: "invalid query", method: "GET", target: "/debug/pprof/profile?seconds=%zz", status: 400},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			profiler := newFakeProfiler()
			handler := &profileHandler{profiler: profiler}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(test.method, test.target, nil))
			if response.Code != test.status || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("status=%d headers=%v", response.Code, response.Header())
			}
			select {
			case <-profiler.started:
				t.Fatal("invalid request started profiling")
			default:
			}
		})
	}
}

func TestProfileHandlerDuration(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, query string
		seconds     int
	}{
		{name: "default", seconds: 10},
		{name: "minimum", query: "?seconds=1", seconds: 1},
		{name: "maximum", query: "?seconds=30", seconds: 30},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				handler := &profileHandler{profiler: newFakeProfiler()}
				response := httptest.NewRecorder()
				before := time.Now()
				handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/debug/pprof/profile"+test.query, nil))
				if elapsed := time.Since(before); elapsed != time.Duration(test.seconds)*time.Second {
					t.Fatalf("profile duration = %s", elapsed)
				}
				if response.Code != http.StatusOK || response.Body.String() != "profile data" {
					t.Fatalf("profile response: %d %s", response.Code, response.Body)
				}
				if response.Header().Get("Content-Disposition") != `attachment; filename="vm-cpu.pprof"` {
					t.Fatal("missing fixed profile filename")
				}
			})
		})
	}
}

func TestProfileHandlerConcurrentAndCancellation(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		profiler := newFakeProfiler()
		handler := &profileHandler{profiler: profiler}
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		done := make(chan struct{})
		go func() {
			defer close(done)
			handler.ServeHTTP(httptest.NewRecorder(),
				httptest.NewRequestWithContext(ctx, http.MethodGet, "/debug/pprof/profile?seconds=30", nil))
		}()
		<-profiler.started
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/debug/pprof/profile", nil))
		if response.Code != http.StatusConflict {
			t.Fatalf("concurrent request status = %d", response.Code)
		}
		cancel()
		<-done
		select {
		case <-profiler.stopped:
		default:
			t.Fatal("cancelled request left profiling active")
		}
		handler.profiler = newFakeProfiler()
		response = httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/debug/pprof/profile?seconds=1", nil))
		if response.Code != http.StatusOK {
			t.Fatal("cancelled profile did not release the slot")
		}
	})
}

func TestProfileHandlerUnavailable(t *testing.T) {
	t.Parallel()
	profiler := newFakeProfiler()
	profiler.err = io.ErrClosedPipe
	response := httptest.NewRecorder()
	(&profileHandler{profiler: profiler}).ServeHTTP(response,
		httptest.NewRequest(http.MethodGet, "/debug/pprof/profile", nil))
	if response.Code != http.StatusServiceUnavailable || strings.Contains(response.Body.String(), profiler.err.Error()) {
		t.Fatalf("unexpected profile error response: %d %s", response.Code, response.Body)
	}
}

func TestProfileBufferBounded(t *testing.T) {
	t.Parallel()
	buffer := &profileBuffer{}
	if _, err := buffer.Write(make([]byte, maxProfileBytes)); err != nil {
		t.Fatal(err)
	}
	if _, err := buffer.Write([]byte{1}); !errors.Is(err, errProfileSize) || !buffer.exceeded {
		t.Fatalf("profile exceeded its memory bound without rejection: %v", err)
	}
	if buffer.data.Len() != maxProfileBytes {
		t.Fatalf("profile buffer grew to %d", buffer.data.Len())
	}
}
