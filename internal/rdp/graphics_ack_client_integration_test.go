//go:build integration && linux

package rdp

import (
	"bufio"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const graphicsACKTimeout = 10 * time.Second

var errGraphicsACKDecodeLog = errors.New("invalid FreeRDP decoder log")

type graphicsACKClient struct {
	session    *Session
	readErrors <-chan error
	changed    <-chan struct{}
	client     *graphicsACKProcess
	xvfb       *graphicsACKProcess
}

type graphicsACKProcess struct {
	pid  int
	done chan struct{}
	err  error // Read only after done closes.
}

func newGraphicsACKClient(tb testing.TB, width, height int) *graphicsACKClient {
	tb.Helper()
	freerdp := graphicsACKExecutable(tb, "FreeRDP", graphicsACKFreeRDPNames()...)
	xvfb := graphicsACKExecutable(tb, "Xvfb", "Xvfb")
	client := &graphicsACKClient{}
	display, server := startGraphicsACKDisplay(tb, xvfb, width, height)
	client.xvfb = server

	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { _ = listener.Close() })
	if err := listener.SetDeadline(time.Now().Add(graphicsACKTimeout)); err != nil {
		tb.Fatal(err)
	}
	serverTLS, _ := protocolTestTLS(tb)
	fingerprint := sha256.Sum256(serverTLS.Certificates[0].Certificate[0])
	command := exec.Command(freerdp,
		"/v:"+listener.Addr().String(), "/u:graphics-ack-test", "/p:unused",
		"/sec:tls", fmt.Sprintf("/cert:fingerprint:sha256:%X", fingerprint),
		"/gfx:avc420", "/frame-ack:1", "/gdi:sw", fmt.Sprintf("/size:%dx%d", width, height),
		"/log-level:WARN", "-clipboard")
	command.Env = append(os.Environ(), "DISPLAY="+display, "XDG_CONFIG_HOME="+tb.TempDir())
	client.client = startGraphicsACKProcess(tb, "FreeRDP", command)
	conn, err := listener.Accept()
	if err != nil {
		tb.Fatalf("accept FreeRDP connection: %v", err)
	}
	_ = listener.Close()

	type acceptedSession struct {
		session *Session
		err     error
	}
	accepted := make(chan acceptedSession, 1)
	acceptDone := make(chan struct{})
	go func() {
		defer close(acceptDone)
		session, err := Accept(conn, serverTLS, width, height)
		accepted <- acceptedSession{session: session, err: err}
	}()
	tb.Cleanup(func() {
		_ = conn.Close()
		<-acceptDone
	})
	timer := time.NewTimer(graphicsACKTimeout)
	defer timer.Stop()
	select {
	case result := <-accepted:
		if result.err != nil {
			tb.Fatalf("negotiate FreeRDP session: %v", result.err)
		}
		client.session = result.session
	case <-client.client.done:
		tb.Fatalf("FreeRDP exited during negotiation: %v", client.client.err)
	case <-timer.C:
		tb.Fatal("timed out negotiating FreeRDP session")
	}
	client.changed = client.session.GraphicsChanged()
	readErrors := make(chan error, 1)
	client.readErrors = readErrors
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		for {
			if _, _, err := client.session.ReadPacket(); err != nil {
				readErrors <- err

				return
			}
		}
	}()
	tb.Cleanup(func() {
		_ = client.session.Close()
		<-readDone
	})
	if started, err := client.session.BeginGraphics(); err != nil || !started {
		tb.Fatalf("begin FreeRDP graphics: started=%t error=%v", started, err)
	}
	client.wait(tb, client.session.GraphicsReady)

	return client
}

func (c *graphicsACKClient) wait(tb testing.TB, predicate func() bool) {
	tb.Helper()
	timer := time.NewTimer(graphicsACKTimeout)
	defer timer.Stop()
	for !predicate() {
		select {
		case <-c.changed:
		case err := <-c.readErrors:
			tb.Fatalf("read FreeRDP protocol: %v", err)
		case <-c.client.done:
			tb.Fatalf("FreeRDP exited: %v", c.client.err)
		case <-c.xvfb.done:
			tb.Fatalf("Xvfb exited: %v", c.xvfb.err)
		case <-timer.C:
			tb.Fatalf("timed out waiting for FreeRDP graphics: %+v", c.session.GraphicsStats())
		}
	}
}

func graphicsACKFreeRDPNames() []string {
	if override := os.Getenv("DEVBOX_KVM_FREERDP"); override != "" {
		return []string{override}
	}

	return []string{"xfreerdp-h264", "xfreerdp3", "xfreerdp", "../../tools/freerdp/bin/xfreerdp"}
}

func graphicsACKExecutable(tb testing.TB, name string, candidates ...string) string {
	tb.Helper()
	for _, candidate := range candidates {
		if path, err := exec.LookPath(candidate); err == nil {
			return path
		}
	}
	message := fmt.Sprintf("%s required for graphics ACK integration test; tried %v", name, candidates)
	if os.Getenv("DEVBOX_KVM_REQUIRE_ACK_TEST") == "1" {
		tb.Fatal(message)
	} else {
		tb.Skip(message)
	}

	return ""
}

func startGraphicsACKDisplay(tb testing.TB, executable string, width, height int) (string, *graphicsACKProcess) {
	tb.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { _ = reader.Close() })
	defer writer.Close()
	//nolint:gosec // Xvfb executable is discovered by exec.LookPath; arguments are fixed test settings.
	command := exec.Command(executable, "-displayfd", "3", "-screen", "0",
		fmt.Sprintf("%dx%dx24", width, height), "-nolisten", "tcp", "-noreset")
	command.ExtraFiles = []*os.File{writer}
	process := startGraphicsACKProcess(tb, "Xvfb", command)
	_ = writer.Close()
	type displayResult struct {
		number string
		err    error
	}
	ready := make(chan displayResult, 1)
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		number, err := bufio.NewReader(reader).ReadString('\n')
		ready <- displayResult{number: strings.TrimSpace(number), err: err}
	}()
	tb.Cleanup(func() {
		_ = reader.Close()
		<-readDone
	})
	timer := time.NewTimer(graphicsACKTimeout)
	defer timer.Stop()
	select {
	case result := <-ready:
		if result.err != nil {
			tb.Fatalf("read Xvfb display number: %v", result.err)
		}
		display, err := strconv.Atoi(result.number)
		if err != nil || display < 0 {
			tb.Fatalf("invalid Xvfb display number %q", result.number)
		}

		return ":" + result.number, process
	case <-process.done:
		tb.Fatalf("Xvfb exited during startup: %v", process.err)
	case <-timer.C:
		tb.Fatal("timed out starting Xvfb")
	}

	return "", nil
}

func startGraphicsACKProcess(tb testing.TB, name string, command *exec.Cmd) *graphicsACKProcess {
	tb.Helper()
	logPath := filepath.Join(tb.TempDir(), name+".log")
	log, err := os.Create(logPath)
	if err != nil {
		tb.Fatal(err)
	}
	command.Stdout, command.Stderr = log, log
	process := &graphicsACKProcess{done: make(chan struct{})}
	if err := command.Start(); err != nil {
		_ = log.Close()
		tb.Fatalf("start %s: %v", name, err)
	}
	process.pid = command.Process.Pid
	go func() {
		process.err = command.Wait()
		close(process.done)
	}()
	tb.Cleanup(func() {
		select {
		case <-process.done:
		default:
			// Xvfb removes its display lock and socket on graceful shutdown.
			_ = command.Process.Signal(syscall.SIGTERM)
			timer := time.NewTimer(time.Second)
			select {
			case <-process.done:
			case <-timer.C:
				_ = command.Process.Kill()
				<-process.done
			}
			timer.Stop()
		}
		_ = log.Close()
		if name == "FreeRDP" {
			if err := graphicsACKCheckDecodeLog(logPath); err != nil {
				tb.Errorf("FreeRDP decoder: %v", err)
			}
		}
		if tb.Failed() {
			tb.Logf("%s output (tail):\n%s", name, graphicsACKLogTail(logPath))
		}
	})

	return process
}

// FreeRDP may acknowledge a frame even after logging a decoder failure. Check
// the entire bounded log, including warnings, before accepting timing samples.
// Disconnect errors from the deliberate connection close are not decode errors.
func graphicsACKCheckDecodeLog(path string) error {
	log, err := os.Open(path)
	if err != nil {
		return err
	}
	defer log.Close()
	const limit = 4 << 20
	data, err := io.ReadAll(io.LimitReader(log, limit+1))
	if err != nil {
		return err
	}
	if len(data) > limit {
		return fmt.Errorf("%w: client log exceeds %d bytes; cannot validate decoder output", errGraphicsACKDecodeLog, limit)
	}
	for line := range strings.SplitSeq(strings.ToLower(string(data)), "\n") {
		decoder := strings.Contains(line, "decompress") || strings.Contains(line, "decode") ||
			strings.Contains(line, "video frame")
		failed := strings.Contains(line, "fail") || strings.Contains(line, "error")
		if decoder && failed {
			return fmt.Errorf("%w: client reported a decode failure: %s", errGraphicsACKDecodeLog, line)
		}
	}

	return nil
}

func graphicsACKLogTail(path string) string {
	log, err := os.Open(path)
	if err != nil {
		return err.Error()
	}
	defer log.Close()
	info, err := log.Stat()
	if err != nil {
		return err.Error()
	}
	if _, err := log.Seek(max(0, info.Size()-8192), io.SeekStart); err != nil {
		return err.Error()
	}
	data, err := io.ReadAll(io.LimitReader(log, 8192))
	if err != nil {
		return err.Error()
	}

	return string(data)
}
