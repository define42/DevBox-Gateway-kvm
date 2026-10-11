//go:build linux

// diskbench runs the same queue-depth-one workload on a host file or guest disk.
// All write targets must be disposable: the workload overwrites their contents.
//
//nolint:err113 // This standalone benchmark reports contextual failures, not a public error API.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"runtime"
	"slices"
	"syscall"
	"time"
)

const blockSize = 4096

type config struct {
	Path        string `json:"path"`
	Label       string `json:"label"`
	Size        int64  `json:"size"`
	Operations  int    `json:"operations"`
	Repetitions int    `json:"repetitions"`
	Direct      bool   `json:"direct"`
	Seed        uint64 `json:"seed"`
}

//nolint:tagliatelle // Benchmark JSONL artifacts use a stable snake_case schema.
type result struct {
	Label           string  `json:"label"`
	Workload        string  `json:"workload"`
	Repetition      int     `json:"repetition"`
	Operations      int     `json:"operations"`
	BlockBytes      int     `json:"block_bytes"`
	WorkingSetBytes int64   `json:"working_set_bytes"`
	QueueDepth      int     `json:"queue_depth"`
	Direct          bool    `json:"direct"`
	SyncEvery       int     `json:"sync_every"`
	Seed            uint64  `json:"seed"`
	Seconds         float64 `json:"seconds"`
	IOPS            float64 `json:"iops"`
	MeanUS          float64 `json:"mean_us"`
	P50US           float64 `json:"p50_us"`
	P95US           float64 `json:"p95_us"`
	P99US           float64 `json:"p99_us"`
	MaxUS           float64 `json:"max_us"`
	Verified        bool    `json:"verified"`
}

func main() {
	runtime.GOMAXPROCS(1)
	var cfg config
	var err error
	if os.Getpid() == 1 {
		err = guestSetup(&cfg)
	} else {
		flag.StringVar(&cfg.Path, "path", "", "disposable file to overwrite")
		flag.StringVar(&cfg.Label, "label", "host", "result label")
		flag.Int64Var(&cfg.Size, "size", 64<<20, "working set in bytes")
		flag.IntVar(&cfg.Operations, "ops", 4096, "operations per workload")
		flag.IntVar(&cfg.Repetitions, "repetitions", 6, "repetitions")
		flag.BoolVar(&cfg.Direct, "direct", true, "open with O_DIRECT")
		flag.Uint64Var(&cfg.Seed, "seed", 42, "random offset seed")
		flag.Parse()
	}
	if err == nil {
		err = run(cfg)
	}
	if err != nil {
		fmt.Printf("DISKBENCH_ERROR %v\n", err)
	} else {
		fmt.Println("DISKBENCH_DONE")
	}
	if os.Getpid() == 1 {
		// The harness stops the VM when it receives the completion marker.
		for {
			time.Sleep(time.Hour)
		}
	}
	if err != nil {
		os.Exit(1)
	}
}

func guestSetup(cfg *config) error {
	for _, mount := range []struct{ target, kind string }{
		{target: "/dev", kind: "devtmpfs"},
		{target: "/proc", kind: "proc"},
		{target: "/sys", kind: "sysfs"},
	} {
		if err := os.MkdirAll(mount.target, 0o755); err != nil {
			return err
		}
		if err := syscall.Mount(mount.kind, mount.target, mount.kind, 0, ""); err != nil {
			return err
		}
	}
	data, err := os.ReadFile("/diskbench.json")
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		return err
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		if _, err := os.Stat(cfg.Path); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("guest disk %s did not appear", cfg.Path)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func run(cfg config) (runErr error) {
	if cfg.Path == "" || cfg.Size < blockSize || cfg.Size%blockSize != 0 {
		return errors.New("path and positive block-aligned size are required")
	}
	if cfg.Operations < 1 || cfg.Repetitions < 1 {
		return errors.New("ops and repetitions must be positive")
	}
	flags := os.O_RDWR
	if cfg.Direct {
		flags |= syscall.O_DIRECT
	}
	f, err := os.OpenFile(cfg.Path, flags, 0)
	if err != nil {
		return err
	}
	defer func() { runErr = errors.Join(runErr, f.Close()) }()
	capacity, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}
	if capacity < cfg.Size {
		return fmt.Errorf("target capacity %d is smaller than working set %d", capacity, cfg.Size)
	}
	buf, err := syscall.Mmap(-1, 0, blockSize, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_PRIVATE|syscall.MAP_ANON)
	if err != nil {
		return err
	}
	defer func() { runErr = errors.Join(runErr, syscall.Munmap(buf)) }()
	for repetition := 1; repetition <= cfg.Repetitions; repetition++ {
		for _, syncEvery := range []int{0, 1, 32} {
			if err := measure(f, buf, cfg, repetition, syncEvery); err != nil {
				return err
			}
		}
	}

	return nil
}

func measure(f *os.File, buf []byte, cfg config, repetition, syncEvery int) error {
	offsets := make([]int64, cfg.Operations)
	rng := rand.New(rand.NewPCG(cfg.Seed, uint64(repetition))) //nolint:gosec // Reproducible benchmark offsets.
	for i := range offsets {
		offsets[i] = rng.Int64N(cfg.Size/blockSize) * blockSize
	}
	latencies := make([]int64, cfg.Operations)
	// Different payloads catch discarded writes even when phases reuse offsets.
	pattern := byte(1 + 2*((repetition-1)%127))
	if syncEvery == 32 {
		pattern++
	}
	for i := range buf {
		buf[i] = pattern
	}
	// There is no warmup write: images are already fully populated by the driver.
	// Read the entire working set before random reads so guest/host cache state is
	// explicit and the workload measures the hot buffered backend path.
	if syncEvery == 0 {
		for off := int64(0); off < cfg.Size; off += blockSize {
			if n, err := f.ReadAt(buf, off); err != nil || n != blockSize {
				return fmt.Errorf("warmup read: %d bytes: %w", n, err)
			}
		}
	}
	start := time.Now()
	for i, off := range offsets {
		opStart := time.Now()
		var n int
		var err error
		if syncEvery == 0 {
			n, err = f.ReadAt(buf, off)
		} else {
			n, err = f.WriteAt(buf, off)
		}
		if err != nil {
			return fmt.Errorf("io at %d: %w", off, err)
		}
		if n != blockSize {
			return fmt.Errorf("short io at %d: %d bytes", off, n)
		}
		if syncEvery != 0 && ((i+1)%syncEvery == 0 || i+1 == len(offsets)) {
			// The final partial group is included in timing and made durable too.
			if err := f.Sync(); err != nil {
				return fmt.Errorf("sync: %w", err)
			}
		}
		latencies[i] = time.Since(opStart).Nanoseconds()
	}
	elapsed := time.Since(start)
	if syncEvery != 0 {
		expected := bytes.Repeat([]byte{pattern}, blockSize)
		for _, off := range offsets {
			if _, err := f.ReadAt(buf, off); err != nil {
				return err
			}
			if !bytes.Equal(buf, expected) {
				return fmt.Errorf("verification failed at offset %d", off)
			}
		}
	}
	slices.Sort(latencies)
	workload := "random_read"
	if syncEvery != 0 {
		workload = fmt.Sprintf("random_write_sync%d", syncEvery)
	}
	r := result{
		Label: cfg.Label, Workload: workload, Repetition: repetition,
		Operations: cfg.Operations, BlockBytes: blockSize, WorkingSetBytes: cfg.Size,
		QueueDepth: 1, Direct: cfg.Direct, SyncEvery: syncEvery, Seed: cfg.Seed,
		Seconds: elapsed.Seconds(), IOPS: float64(cfg.Operations) / elapsed.Seconds(),
		MeanUS: float64(elapsed.Nanoseconds()) / float64(cfg.Operations) / 1e3,
		P50US:  float64(latencies[(len(latencies)-1)*50/100]) / 1e3,
		P95US:  float64(latencies[(len(latencies)-1)*95/100]) / 1e3,
		P99US:  float64(latencies[(len(latencies)-1)*99/100]) / 1e3,
		MaxUS:  float64(latencies[len(latencies)-1]) / 1e3, Verified: syncEvery != 0,
	}

	return json.NewEncoder(os.Stdout).Encode(r)
}
