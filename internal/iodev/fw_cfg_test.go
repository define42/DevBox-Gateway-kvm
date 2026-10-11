package iodev

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestFWCfgFirmwareDiscovery(t *testing.T) {
	t.Parallel()

	f, err := NewFWCfg(map[uint16][]byte{FWCfgNBCPUs: {4, 0}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if f.IOPort() != 0x510 || f.Size() != 2 {
		t.Fatalf("invalid I/O range: %#x + %d", f.IOPort(), f.Size())
	}
	for _, tc := range []struct {
		name     string
		selector uint16
		want     []byte
	}{
		{name: "signature", selector: 0, want: []byte("QEMU")},
		{name: "traditional interface only", selector: 1, want: []byte{1, 0, 0, 0}},
		{name: "CPU count", selector: 5, want: []byte{4, 0}},
		{name: "empty file directory", selector: 0x19, want: []byte{0, 0, 0, 0}},
		{name: "unknown item", selector: 0x1234, want: []byte{0, 0, 0, 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f, err := NewFWCfg(map[uint16][]byte{FWCfgNBCPUs: {4, 0}}, nil)
			if err != nil {
				t.Fatal(err)
			}
			selectFWCfg(t, f, tc.selector)
			if got := readFWCfg(t, f, len(tc.want)); !bytes.Equal(got, tc.want) {
				t.Fatalf("item %#x = %x, want %x", tc.selector, got, tc.want)
			}
			if got := readFWCfg(t, f, 4); !bytes.Equal(got, make([]byte, 4)) {
				t.Fatalf("read past end = %x, want zero", got)
			}
		})
	}
}

func TestFWCfgFilesDirectory(t *testing.T) {
	t.Parallel()

	files := map[string][]byte{
		"etc/e820":  {1, 2, 3, 4},
		"bootorder": []byte("/pci@i0cf8/disk@1\x00"),
	}
	f, err := NewFWCfg(nil, files)
	if err != nil {
		t.Fatal(err)
	}
	selectFWCfg(t, f, 0x19)
	directory := readFWCfg(t, f, 4+2*64)
	if got := binary.BigEndian.Uint32(directory); got != 2 {
		t.Fatalf("directory count = %d, want 2", got)
	}
	for i, name := range []string{"bootorder", "etc/e820"} {
		entry := directory[4+i*64 : 4+(i+1)*64]
		if got := binary.BigEndian.Uint32(entry); got != uint32(len(files[name])) {
			t.Fatalf("file %q size = %d, want %d", name, got, len(files[name]))
		}
		selector := binary.BigEndian.Uint16(entry[4:])
		if selector != 0x20+uint16(i) {
			t.Fatalf("file %q selector = %#x", name, selector)
		}
		if !bytes.Equal(entry[6:8], []byte{0, 0}) {
			t.Fatalf("file %q reserved bytes are not zero", name)
		}
		wantName := make([]byte, 56)
		copy(wantName, name)
		if !bytes.Equal(entry[8:], wantName) {
			t.Fatalf("file name = %q, want zero-padded %q", entry[8:], name)
		}
		selectFWCfg(t, f, selector)
		if got := readFWCfg(t, f, len(files[name])); !bytes.Equal(got, files[name]) {
			t.Fatalf("file %q = %x, want %x", name, got, files[name])
		}
	}
}

func TestFWCfgRewindAndImmutableData(t *testing.T) {
	t.Parallel()

	entry := []byte{4, 0}
	file := []byte{9, 8, 7}
	f, err := NewFWCfg(map[uint16][]byte{FWCfgNBCPUs: entry}, map[string][]byte{"test": file})
	if err != nil {
		t.Fatal(err)
	}
	entry[0] = 99
	file[0] = 99
	selectFWCfg(t, f, FWCfgNBCPUs)
	if got := readFWCfg(t, f, 2); !bytes.Equal(got, []byte{4, 0}) {
		t.Fatalf("entry changed with caller buffer: %x", got)
	}
	selectFWCfg(t, f, 0x20)
	if got := readFWCfg(t, f, 2); !bytes.Equal(got, []byte{9, 8}) {
		t.Fatalf("file changed with caller buffer: %x", got)
	}
	if err := f.Write(0x511, []byte{42}); err != nil {
		t.Fatal(err)
	}
	if got := readFWCfg(t, f, 1); !bytes.Equal(got, []byte{7}) {
		t.Fatalf("data write changed stream: %x", got)
	}
	selectFWCfg(t, f, 0x20)
	if got := readFWCfg(t, f, 3); !bytes.Equal(got, []byte{9, 8, 7}) {
		t.Fatalf("rewound stream = %x", got)
	}
	selectFWCfg(t, f, 0x4020)
	if got := readFWCfg(t, f, 3); !bytes.Equal(got, []byte{9, 8, 7}) {
		t.Fatalf("obsolete write-channel read alias = %x", got)
	}
}

func TestNewFWCfgRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		entries map[uint16][]byte
		files   map[string][]byte
	}{
		{name: "signature override", entries: map[uint16][]byte{0: {1}}},
		{name: "features override", entries: map[uint16][]byte{1: {3}}},
		{name: "directory override", entries: map[uint16][]byte{0x19: {1}}},
		{name: "file selector collision", entries: map[uint16][]byte{0x20: {1}}},
		{name: "write channel", entries: map[uint16][]byte{0x4003: {1}}},
		{name: "empty name", files: map[string][]byte{"": {1}}},
		{name: "unterminated name", files: map[string][]byte{strings.Repeat("a", 56): {1}}},
		{name: "embedded NUL", files: map[string][]byte{"a\x00b": {1}}},
		{name: "non ASCII", files: map[string][]byte{"etc/æ": {1}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewFWCfg(tc.entries, tc.files); err == nil {
				t.Fatal("expected invalid configuration error")
			}
		})
	}
	files := make(map[string][]byte)
	for i := range 0x4000 - 0x20 + 1 {
		files[fmt.Sprintf("file%d", i)] = []byte{}
	}
	if _, err := NewFWCfg(nil, files); err == nil {
		t.Fatal("expected error on file selector exhaustion")
	}
}

func TestFWCfgAccessValidation(t *testing.T) {
	t.Parallel()

	f, err := NewFWCfg(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		port uint64
		size int
	}{
		{name: "short selector", port: 0x510, size: 1},
		{name: "long selector", port: 0x510, size: 4},
		{name: "empty data", port: 0x511, size: 0},
		{name: "wide data", port: 0x511, size: 2},
		{name: "invalid port", port: 0x512, size: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data := make([]byte, tc.size)
			if err := f.Read(tc.port, data); err == nil {
				t.Fatal("invalid read accepted")
			}
			if err := f.Write(tc.port, data); err == nil {
				t.Fatal("invalid write accepted")
			}
		})
	}
}

func TestFWCfgConcurrentReads(t *testing.T) {
	t.Parallel()

	payload := make([]byte, 256)
	for i := range payload {
		payload[i] = byte(i)
	}
	f, err := NewFWCfg(nil, map[string][]byte{"test": payload})
	if err != nil {
		t.Fatal(err)
	}
	selectFWCfg(t, f, 0x20)
	results := make(chan byte, len(payload))
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 32 {
				data := []byte{0}
				if err := f.Read(0x511, data); err != nil {
					t.Error(err)

					return
				}
				results <- data[0]
			}
		})
	}
	wg.Wait()
	close(results)
	counts := [256]int{}
	for value := range results {
		counts[value]++
	}
	for value, count := range counts {
		if count != 1 {
			t.Fatalf("byte %d returned %d times, want once", value, count)
		}
	}
}

func selectFWCfg(t *testing.T, f *FWCfg, selector uint16) {
	t.Helper()
	data := make([]byte, 2)
	binary.LittleEndian.PutUint16(data, selector)
	if err := f.Write(0x510, data); err != nil {
		t.Fatal(err)
	}
}

func readFWCfg(t *testing.T, f *FWCfg, size int) []byte {
	t.Helper()
	data := make([]byte, size)
	for i := range data {
		if err := f.Read(0x511, data[i:i+1]); err != nil {
			t.Fatal(err)
		}
	}

	return data
}
