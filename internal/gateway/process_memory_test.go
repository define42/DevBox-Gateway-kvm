package gateway

import (
	"errors"
	"os"
	"strconv"
	"testing"
)

func TestReadProcessMemory(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		readErr error
	}{
		{name: "anonymous PSS"},
		{name: "permission denied", readErr: os.ErrPermission},
		{name: "process exited", readErr: os.ErrNotExist},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := readProcessMemoryFrom(12345, func(path string) ([]byte, error) {
				if path != "/proc/12345/smaps_rollup" {
					t.Fatalf("read path = %q", path)
				}

				return []byte("Pss_Anon: 64 kB\n"), tc.readErr
			})
			if !errors.Is(err, tc.readErr) {
				t.Fatalf("readProcessMemoryFrom error = %v, want %v", err, tc.readErr)
			}
			want := uint64(64 << 10)
			if tc.readErr != nil {
				want = 0
			}
			if got != want {
				t.Fatalf("resident anonymous bytes = %d, want %d", got, want)
			}
		})
	}
}

func TestReadProcessMemoryInvalidPID(t *testing.T) {
	t.Parallel()
	for _, pid := range []int{0, -1} {
		t.Run(strconv.Itoa(pid), func(t *testing.T) {
			t.Parallel()
			got, err := readProcessMemoryFrom(pid, func(string) ([]byte, error) {
				t.Fatal("invalid PID reached filesystem access")

				return nil, nil
			})
			if got != 0 || !errors.Is(err, ErrInvalid) {
				t.Fatalf("invalid PID: got %d, %v", got, err)
			}
		})
	}
}

func TestParseProcessMemory(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, input string
		want        uint64
		invalid     bool
	}{
		{
			name: "only resident anonymous PSS counts",
			input: "00400000-00800000 ---p 00000000 00:00 0 [rollup]\n" +
				"Rss: 8192 kB\nPss: 4096 kB\nPss_Anon: 1024 kB\n" +
				"Pss_File: 2048 kB\nPss_Shmem: 1024 kB\nSwapPss: 512 kB\n",
			want: 1 << 20,
		},
		{name: "zero anonymous pages", input: "Pss_Anon: 0 kB", want: 0},
		{name: "kernel spacing", input: "Pss_Anon:\t    123 kB\n", want: 123 * 1024},
		{name: "largest byte count", input: "Pss_Anon: 18014398509481983 kB", want: ^uint64(0) - 1023},
		{name: "empty", invalid: true},
		{name: "missing field", input: "Rss: 1024 kB\nPss: 1024 kB", invalid: true},
		{name: "similar field", input: "Pss_Anonymous: 1024 kB", invalid: true},
		{name: "missing value", input: "Pss_Anon: kB", invalid: true},
		{name: "missing unit", input: "Pss_Anon: 1", invalid: true},
		{name: "wrong unit", input: "Pss_Anon: 1 MB", invalid: true},
		{name: "extra field", input: "Pss_Anon: 1 kB extra", invalid: true},
		{name: "fractional", input: "Pss_Anon: 1.5 kB", invalid: true},
		{name: "negative", input: "Pss_Anon: -1 kB", invalid: true},
		{name: "plus sign", input: "Pss_Anon: +1 kB", invalid: true},
		{name: "nonnumeric", input: "Pss_Anon: unknown kB", invalid: true},
		{name: "byte overflow", input: "Pss_Anon: 18014398509481984 kB", invalid: true},
		{name: "number overflow", input: "Pss_Anon: 18446744073709551616 kB", invalid: true},
		{name: "duplicate", input: "Pss_Anon: 1 kB\nPss_Anon: 2 kB", invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseProcessMemory(tc.input)
			if tc.invalid {
				if got != 0 || !errors.Is(err, ErrInvalid) {
					t.Fatalf("invalid accounting accepted: %d, %v", got, err)
				}

				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("parseProcessMemory = %d, %v; want %d, nil", got, err, tc.want)
			}
		})
	}
}
