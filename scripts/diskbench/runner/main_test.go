//go:build linux && amd64

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSerialOutputCompletion(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		chunks []string
		want   string
	}{
		{name: "success_split_across_writes", chunks: []string{"booting\nDISK", "BENCH_DONE\r", "\n", "DISKBENCH_DONE\n"}},
		{name: "failure", chunks: []string{"DISKBENCH_ERROR disk missing\n", "DISKBENCH_DONE\n"}, want: "disk missing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "serial")
			file, err := os.Create(path)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = file.Close() })
			output := &serialOutput{file: file, done: make(chan error, 2)}
			for _, chunk := range tc.chunks {
				if n, err := output.Write([]byte(chunk)); err != nil || n != len(chunk) {
					t.Fatalf("Write = %d, %v", n, err)
				}
			}
			select {
			case err := <-output.done:
				if tc.want == "" && err != nil {
					t.Fatal(err)
				}
				if tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
					t.Fatalf("completion = %v, want %q", err, tc.want)
				}
			default:
				t.Fatal("completion marker was not delivered")
			}
			select {
			case err := <-output.done:
				t.Fatalf("duplicate completion: %v", err)
			default:
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != strings.Join(tc.chunks, "") {
				t.Fatalf("serial log = %q", data)
			}
		})
	}
}

func TestSerialOutputWriteFailure(t *testing.T) {
	t.Parallel()
	file, err := os.CreateTemp(t.TempDir(), "serial")
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	output := &serialOutput{file: file, done: make(chan error, 1)}
	if n, err := output.Write([]byte("DISKBENCH_DONE\n")); n != 0 || err == nil {
		t.Fatalf("write to closed log = %d, %v", n, err)
	}
	select {
	case <-output.done:
		t.Fatal("failed write must not report guest completion")
	default:
	}
}
