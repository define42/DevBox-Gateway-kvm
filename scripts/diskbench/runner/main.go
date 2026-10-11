//go:build linux && amd64

// diskbench-runner boots only the disposable benchmark guest. Its serial
// plumbing is deliberately local to this harness, outside product defaults.
//
//nolint:err113 // This standalone benchmark reports contextual failures, not a public error API.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/define42/gokvm/internal/machine"
)

type serialOutput struct {
	file     *os.File
	line     []byte
	done     chan error
	finished bool
}

func (s *serialOutput) Write(p []byte) (int, error) {
	if _, err := s.file.Write(p); err != nil {
		return 0, err
	}
	for _, b := range p {
		if b != '\n' {
			s.line = append(s.line, b)

			continue
		}
		line := strings.TrimSpace(string(s.line))
		s.line = s.line[:0]
		if strings.HasPrefix(line, "{\"label\":") {
			fmt.Println(line)
		}
		if !s.finished && line == "DISKBENCH_DONE" {
			s.finished = true
			s.done <- nil
		}
		if !s.finished && strings.HasPrefix(line, "DISKBENCH_ERROR ") {
			s.finished = true
			s.done <- errors.New(line)
		}
	}

	return len(p), nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() (runErr error) {
	kernel := flag.String("kernel", "bzImage", "guest Linux bzImage")
	initrd := flag.String("initrd", "", "benchmark initrd")
	disk := flag.String("disk", "", "disposable raw or qcow2 disk")
	serialLog := flag.String("serial-log", "", "complete guest serial log")
	timeout := flag.Duration("timeout", 5*time.Minute, "boot and workload deadline")
	flag.Parse()
	if *initrd == "" || *disk == "" || *serialLog == "" {
		return errors.New("initrd, disk, and serial-log are required")
	}
	kern, err := os.Open(*kernel)
	if err != nil {
		return err
	}
	defer kern.Close()
	rd, err := os.ReadFile(*initrd)
	if err != nil {
		return err
	}
	logFile, err := os.Create(*serialLog)
	if err != nil {
		return err
	}
	defer logFile.Close()
	m, err := machine.New("/dev/kvm", 1, 256<<20)
	if err != nil {
		return err
	}
	// Close stops and joins the vCPU and device workers before closing the disk.
	defer func() { runErr = errors.Join(runErr, m.Close()) }()
	if err := m.AddDisk(*disk); err != nil {
		return err
	}
	const params = "console=ttyS0 earlyprintk=serial noapic noacpi nortc nowatchdog " +
		"nmi_watchdog=0 mitigations=off lapic pci=realloc=off virtio_pci.force_legacy=1 " +
		"rdinit=/init quiet"
	if err := m.LoadLinux(kern, bytes.NewReader(rd), params); err != nil {
		return err
	}
	output := &serialOutput{file: logFile, done: make(chan error, 1), line: make([]byte, 0, 4096)}
	m.GetSerial().SetOutput(output)
	stopped := make(chan error, 1)
	go func() { stopped <- m.RunInfiniteLoop(0) }()
	timer := time.NewTimer(*timeout)
	defer timer.Stop()
	select {
	case err := <-output.done:
		return err
	case err := <-stopped:
		if err == nil {
			err = errors.New("unexpected clean exit")
		}

		return fmt.Errorf("guest stopped before completion (see %s): %w", *serialLog, err)
	case <-timer.C:
		return fmt.Errorf("guest timed out (see %s)", *serialLog)
	}
}
