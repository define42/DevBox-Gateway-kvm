package console

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestReadConfig(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "encoder.cfg")
	if err := os.WriteFile(path, []byte("  Width\t640 # comment\n# comment only\n\nHeight 480"), 0o600); err != nil {
		t.Fatal(err)
	}
	reader := NewCReadConfig(path)
	t.Cleanup(reader.Close)
	if !reader.ExistFile() || reader.EndOfFile() || reader.GetFileName() != path {
		t.Fatal("new reader did not open the requested configuration")
	}
	// This sequence exercises one reader's progression, not independent cases.
	for i, tt := range []struct {
		want  []string
		count int32
	}{
		{[]string{"Width", "640", "", ""}, 3},
		{[]string{"", "", "", ""}, 1},
		{[]string{"", "", "", ""}, 1},
		{[]string{"Height", "480", "", ""}, 2},
	} {
		values := []string{"stale", "stale", "stale", "stale"}
		if got := reader.ReadLine(values, len(values)); got != tt.count || !reflect.DeepEqual(values, tt.want) {
			t.Fatalf("line %d: count=%d values=%q, want count=%d values=%q", i+1, got, values, tt.count, tt.want)
		}
		if got := reader.GetLines(); got != int32(i+1) {
			t.Fatalf("line count = %d, want %d", got, i+1)
		}
	}
	if !reader.EndOfFile() {
		t.Fatal("unterminated final line must set EOF")
	}
	reader.Close()
	reader.Close()
	if reader.ExistFile() || !reader.EndOfFile() || reader.ReadLine(make([]string, 4), 4) != 0 {
		t.Fatal("closed reader remains readable")
	}
	reader.Openf("")
	if reader.ExistFile() || reader.GetFileName() != path {
		t.Fatal("empty Openf must preserve the filename and closed state")
	}
	reader.Openf(path)
	if !reader.ExistFile() || reader.EndOfFile() {
		t.Fatal("reopened reader must clear EOF")
	}
	if reader.ReadLine(nil, 4) != 0 || reader.ReadLine(make([]string, 1), 1) != 0 {
		t.Fatal("invalid output buffers must not be read")
	}
}

func TestReadConfigUnavailable(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ name, path string }{{"empty path", ""}, {"missing file", filepath.Join(t.TempDir(), "missing.cfg")}} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			reader := NewCReadConfig(tt.path)
			if reader.ExistFile() || !reader.EndOfFile() || reader.GetFileName() != tt.path {
				t.Fatalf("unavailable configuration %q has incorrect state", tt.path)
			}
			reader.Close()
		})
	}
	reader := NewCReadConfig(t.TempDir())
	t.Cleanup(reader.Close)
	if got := reader.ReadLine(make([]string, 4), 4); got != 1 || !reader.EndOfFile() {
		t.Fatalf("directory read = %d, EOF=%v, want 1 and true", got, reader.EndOfFile())
	}
}

func TestReadConfigTagLimit(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "tags.cfg")
	if err := os.WriteFile(path, []byte("one two three four\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	reader := NewCReadConfig(path)
	t.Cleanup(reader.Close)
	values := make([]string, 3)
	if got := reader.ReadLine(values, len(values)); got != 3 || !reflect.DeepEqual(values, []string{"one", "two", "three"}) {
		t.Fatalf("bounded tags = %q (count %d)", values, got)
	}
	if got := reader.ReadLine(values, len(values)); got != 1 || values[0] != "four" {
		t.Fatalf("remaining tags = %q (count %d)", values, got)
	}
}
