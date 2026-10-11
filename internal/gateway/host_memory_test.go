package gateway

import (
	"errors"
	"os"
	"testing"
)

func TestParseHostMeminfo(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, input string
		want        hostMemory
		invalid     bool
	}{
		{
			name: "bytes", input: "MemTotal: 16384 kB\nMemAvailable: 12288 kB\nMemFree: 1 kB",
			want: hostMemory{16 << 20, 12 << 20},
		},
		{name: "zero available", input: "MemTotal: 1 kB\nMemAvailable: 0 kB", want: hostMemory{1024, 0}},
		{name: "missing available", input: "MemTotal: 1 kB", invalid: true},
		{name: "missing total", input: "MemAvailable: 1 kB", invalid: true},
		{name: "zero total", input: "MemTotal: 0 kB\nMemAvailable: 0 kB", invalid: true},
		{name: "excess available", input: "MemTotal: 1 kB\nMemAvailable: 2 kB", invalid: true},
		{name: "unit", input: "MemTotal: 1 MB\nMemAvailable: 0 kB", invalid: true},
		{name: "negative", input: "MemTotal: -1 kB\nMemAvailable: 0 kB", invalid: true},
		{name: "byte overflow", input: "MemTotal: 18014398509481984 kB\nMemAvailable: 0 kB", invalid: true},
		{name: "number overflow", input: "MemTotal: 18446744073709551616 kB\nMemAvailable: 0 kB", invalid: true},
		{name: "duplicate", input: "MemTotal: 1 kB\nMemTotal: 1 kB\nMemAvailable: 0 kB", invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseHostMeminfo(tc.input)
			if (err != nil) != tc.invalid || (!tc.invalid && got != tc.want) {
				t.Fatalf("parseHostMeminfo = %+v, %v; want %+v, invalid=%t", got, err, tc.want, tc.invalid)
			}
		})
	}
}

func TestReadHostMemoryCgroups(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		files   map[string]string
		want    hostMemory
		invalid bool
	}{
		{name: "no cgroups", want: hostMemory{16 << 20, 12 << 20}},
		{
			name: "no memory membership",
			files: map[string]string{
				"/proc/self/cgroup": "2:cpu:/app\n",
			},
			want: hostMemory{16 << 20, 12 << 20},
		},
		{
			name: "v2 parent remaining memory is tighter",
			files: map[string]string{
				"/proc/self/cgroup":             "0::/tenant/app\n",
				"/proc/self/mountinfo":          "1 0 0:1 / /cg rw - cgroup2 cgroup rw\n",
				"/cg/cgroup.controllers":        "cpu memory\n",
				"/cg/tenant/memory.max":         "8388608\n",
				"/cg/tenant/memory.current":     "6291456\n",
				"/cg/tenant/app/memory.max":     "16777216\n",
				"/cg/tenant/app/memory.current": "1048576\n",
			}, want: hostMemory{8 << 20, 2 << 20},
		},
		{
			name: "v2 limited namespace root",
			files: map[string]string{
				"/proc/self/cgroup":    "0::/\n",
				"/proc/self/mountinfo": "1 0 0:1 / /cg rw - cgroup2 cgroup rw\n",
				"/cg/memory.max":       "4194304",
				"/cg/memory.current":   "1048576",
			}, want: hostMemory{4 << 20, 3 << 20},
		},
		{
			name: "v2 bind root and escaped mount point",
			files: map[string]string{
				"/proc/self/cgroup":             "0::/tenant/app\n",
				"/proc/self/mountinfo":          "1 0 0:1 /tenant /cg\\040memory rw - cgroup2 cgroup rw\n",
				"/cg memory/memory.max":         "8388608",
				"/cg memory/memory.current":     "4194304",
				"/cg memory/app/memory.max":     "max",
				"/cg memory/app/memory.current": "1048576",
			}, want: hostMemory{8 << 20, 4 << 20},
		},
		{
			name: "v2 broad mount exposes ancestor",
			files: map[string]string{
				"/proc/self/cgroup": "0::/tenant/app\n",
				"/proc/self/mountinfo": "1 0 0:1 /tenant/app /leaf rw - cgroup2 cgroup rw\n" +
					"2 0 0:1 / /cg rw - cgroup2 cgroup rw\n",
				"/cg/cgroup.controllers":        "memory",
				"/cg/tenant/memory.max":         "8388608",
				"/cg/tenant/memory.current":     "6291456",
				"/cg/tenant/app/memory.max":     "max",
				"/cg/tenant/app/memory.current": "1048576",
			}, want: hostMemory{8 << 20, 2 << 20},
		},
		{
			name: "v2 absent memory controller",
			files: map[string]string{
				"/proc/self/cgroup":          "0::/app\n",
				"/proc/self/mountinfo":       "1 0 0:1 / /cg rw - cgroup2 cgroup rw\n",
				"/cg/cgroup.controllers":     "cpu",
				"/cg/app/cgroup.controllers": "cpu",
			}, want: hostMemory{16 << 20, 12 << 20},
		},
		{
			name: "v2 usage exceeds limit",
			files: map[string]string{
				"/proc/self/cgroup":    "0::/\n",
				"/proc/self/mountinfo": "1 0 0:1 / /cg rw - cgroup2 cgroup rw\n",
				"/cg/memory.max":       "4194304",
				"/cg/memory.current":   "5242880",
			}, want: hostMemory{4 << 20, 0},
		},
		{
			name: "v1 hierarchy on hybrid host",
			files: map[string]string{
				"/proc/self/cgroup":                    "0::/unified\n5:cpu,memory:/tenant/app\n",
				"/proc/self/mountinfo":                 "1 0 0:1 / /cg rw - cgroup none rw,cpu,memory\n",
				"/cg/memory.limit_in_bytes":            "9223372036854771712",
				"/cg/memory.usage_in_bytes":            "1048576",
				"/cg/tenant/memory.limit_in_bytes":     "8388608",
				"/cg/tenant/memory.usage_in_bytes":     "6291456",
				"/cg/tenant/app/memory.limit_in_bytes": "16777216",
				"/cg/tenant/app/memory.usage_in_bytes": "1048576",
			}, want: hostMemory{8 << 20, 2 << 20},
		},
		{
			name: "raw membership with literal backslash",
			files: map[string]string{
				"/proc/self/cgroup":      "0::/tenant\\040/app\n",
				"/proc/self/mountinfo":   "1 0 0:1 /tenant\\134040 /cg rw - cgroup2 cgroup rw\n",
				"/cg/memory.max":         "8388608",
				"/cg/memory.current":     "4194304",
				"/cg/app/memory.max":     "max",
				"/cg/app/memory.current": "1048576",
			}, want: hostMemory{8 << 20, 4 << 20},
		},
		{
			name: "limited namespace root missing controls",
			files: map[string]string{
				"/proc/self/cgroup":      "0::/\n",
				"/proc/self/mountinfo":   "1 0 0:1 / /cg rw - cgroup2 cgroup rw\n",
				"/cg/cgroup.controllers": "memory",
				"/cg/cgroup.type":        "domain",
			}, invalid: true,
		},
		{
			name: "malformed membership hierarchy",
			files: map[string]string{
				"/proc/self/cgroup": "x::/app\n",
			}, invalid: true,
		},
		{
			name: "malformed broad mount is not ignored",
			files: map[string]string{
				"/proc/self/cgroup":    "0::/tenant\n",
				"/proc/self/mountinfo": "1 0 0:1 /tenant /cg rw - cgroup2 cgroup rw\nmalformed record\n",
			}, invalid: true,
		},
		{
			name: "missing mount",
			files: map[string]string{
				"/proc/self/cgroup": "0::/app\n",
			}, invalid: true,
		},
		{
			name: "component mismatch",
			files: map[string]string{
				"/proc/self/cgroup":    "0::/tenant2/app\n",
				"/proc/self/mountinfo": "1 0 0:1 /tenant /cg rw - cgroup2 cgroup rw\n",
			}, invalid: true,
		},
		{
			name: "unresolvable inherited namespace mount",
			files: map[string]string{
				"/proc/self/cgroup":    "0::/\n",
				"/proc/self/mountinfo": "1 0 0:1 /.. /cg rw - cgroup2 cgroup rw\n",
			}, invalid: true,
		},
		{
			name: "missing one control",
			files: map[string]string{
				"/proc/self/cgroup":    "0::/\n",
				"/proc/self/mountinfo": "1 0 0:1 / /cg rw - cgroup2 cgroup rw\n",
				"/cg/memory.max":       "max",
			}, invalid: true,
		},
		{
			name: "missing controls with enabled memory",
			files: map[string]string{
				"/proc/self/cgroup":          "0::/app\n",
				"/proc/self/mountinfo":       "1 0 0:1 / /cg rw - cgroup2 cgroup rw\n",
				"/cg/app/cgroup.controllers": "memory",
			}, invalid: true,
		},
		{
			name: "invalid usage even when unlimited",
			files: map[string]string{
				"/proc/self/cgroup":    "0::/\n",
				"/proc/self/mountinfo": "1 0 0:1 / /cg rw - cgroup2 cgroup rw\n",
				"/cg/memory.max":       "max",
				"/cg/memory.current":   "-1",
			}, invalid: true,
		},
		{
			name: "overflow cgroup limit",
			files: map[string]string{
				"/proc/self/cgroup":    "0::/\n",
				"/proc/self/mountinfo": "1 0 0:1 / /cg rw - cgroup2 cgroup rw\n",
				"/cg/memory.max":       "18446744073709551616",
				"/cg/memory.current":   "1",
			}, invalid: true,
		},
		{
			name: "v1 missing root controls",
			files: map[string]string{
				"/proc/self/cgroup":    "5:memory:/\n",
				"/proc/self/mountinfo": "1 0 0:1 / /cg rw - cgroup none rw,memory\n",
			}, invalid: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			readFile := func(name string) ([]byte, error) {
				if name == "/proc/meminfo" {
					return []byte("MemTotal: 16384 kB\nMemAvailable: 12288 kB\n"), nil
				}
				if content, ok := tc.files[name]; ok {
					return []byte(content), nil
				}

				return nil, os.ErrNotExist
			}
			got, err := readHostMemoryFrom(readFile)
			if (err != nil) != tc.invalid || (!tc.invalid && got != tc.want) {
				t.Fatalf("readHostMemoryFrom = %+v, %v; want %+v, invalid=%t", got, err, tc.want, tc.invalid)
			}
		})
	}
}

func TestReadHostMemoryPermissionFailure(t *testing.T) {
	t.Parallel()
	for _, denied := range []string{
		"/proc/meminfo", "/proc/self/cgroup", "/proc/self/mountinfo", "/cg/memory.max", "/cg/memory.current",
	} {
		t.Run(denied, func(t *testing.T) {
			t.Parallel()
			files := map[string]string{
				"/proc/meminfo":        "MemTotal: 16384 kB\nMemAvailable: 12288 kB\n",
				"/proc/self/cgroup":    "0::/\n",
				"/proc/self/mountinfo": "1 0 0:1 / /cg rw - cgroup2 cgroup rw\n",
				"/cg/memory.max":       "max",
				"/cg/memory.current":   "0",
			}
			_, err := readHostMemoryFrom(func(name string) ([]byte, error) {
				if name == denied {
					return nil, os.ErrPermission
				}
				if content, ok := files[name]; ok {
					return []byte(content), nil
				}

				return nil, os.ErrNotExist
			})
			if !errors.Is(err, os.ErrPermission) {
				t.Fatalf("error = %v, want permission error for %s", err, denied)
			}
		})
	}
}
