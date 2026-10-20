//go:build linux

package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunValidatesDisposableTarget(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		cfg  config
		want string
	}{
		{name: "missing_path", cfg: config{Size: blockSize, Operations: 1, Repetitions: 1}, want: "path"},
		{name: "unaligned_size", cfg: config{Path: "unused", Size: blockSize + 1}, want: "block-aligned"},
		{name: "missing_operations", cfg: config{Path: "unused", Size: blockSize, Repetitions: 1}, want: "ops"},
		{name: "missing_repetitions", cfg: config{Path: "unused", Size: blockSize, Operations: 1}, want: "repetitions"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := run(tc.cfg); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("run = %v, want %q", err, tc.want)
			}
		})
	}
	path := filepath.Join(t.TempDir(), "disk")
	if err := os.WriteFile(path, make([]byte, blockSize), 0o600); err != nil {
		t.Fatal(err)
	}
	err := run(config{Path: path, Size: 2 * blockSize, Operations: 1, Repetitions: 1})
	if err == nil || !strings.Contains(err.Error(), "capacity") {
		t.Fatalf("undersized target = %v", err)
	}
	if err := run(config{Path: path + ".missing", Size: blockSize, Operations: 1, Repetitions: 1}); !os.IsNotExist(err) {
		t.Fatalf("missing target = %v", err)
	}
}

//nolint:paralleltest // The command writes JSON to process-wide stdout, which this test captures.
func TestRunMeasuresAndVerifiesTemporaryDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "disposable-disk")
	if err := os.WriteFile(path, bytes.Repeat([]byte{0xa5}, blockSize), 0o600); err != nil {
		t.Fatal(err)
	}
	output, err := os.Create(filepath.Join(dir, "results"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = output.Close() })
	original := os.Stdout
	os.Stdout = output
	t.Cleanup(func() { os.Stdout = original })
	const operations = 33 // Includes a final partial batch in sync32 mode.
	err = run(config{Path: path, Label: "test", Size: blockSize, Operations: operations, Repetitions: 2, Seed: 42})
	os.Stdout = original
	if err != nil {
		t.Fatal(err)
	}
	if _, err := output.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(output)
	for repetition := 1; repetition <= 2; repetition++ {
		for _, syncEvery := range []int{0, 1, 32} {
			var got result
			if err := decoder.Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got.Label != "test" || got.Repetition != repetition || got.SyncEvery != syncEvery {
				t.Fatalf("wrong workload identity: %+v", got)
			}
			if got.Operations != operations || got.WorkingSetBytes != blockSize || got.BlockBytes != blockSize {
				t.Fatalf("wrong workload size: %+v", got)
			}
			if got.Verified != (syncEvery != 0) || got.Direct || got.QueueDepth != 1 {
				t.Fatalf("wrong durability/direct-IO metadata: %+v", got)
			}
			if got.Seconds <= 0 || got.IOPS <= 0 || got.MeanUS <= 0 {
				t.Fatalf("invalid measured rates: %+v", got)
			}
			if got.P50US > got.P95US || got.P95US > got.P99US || got.P99US > got.MaxUS {
				t.Fatalf("latency percentiles are unordered: %+v", got)
			}
		}
	}
	if err := decoder.Decode(&result{}); err != io.EOF {
		t.Fatalf("unexpected extra result: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, bytes.Repeat([]byte{4}, blockSize)) {
		t.Fatal("reopened disk does not contain the last verified write pattern")
	}
}

func TestMeasureReportsIOFailure(t *testing.T) {
	t.Parallel()
	file, err := os.CreateTemp(t.TempDir(), "short")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	cfg := config{Size: blockSize, Operations: 1, Repetitions: 1}
	if err := measure(file, make([]byte, blockSize), cfg, 1, 0); err == nil {
		t.Fatal("warmup on empty file succeeded")
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := measure(file, make([]byte, blockSize), cfg, 1, 1); err == nil {
		t.Fatal("write to closed file succeeded")
	}
}
