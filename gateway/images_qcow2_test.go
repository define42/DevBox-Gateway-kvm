package gateway

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestValidateUploadedQCOW2RejectsUnsafeMetadata(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		mutate func([]byte)
	}{
		{name: "backing file offset", mutate: func(b []byte) { binary.BigEndian.PutUint64(b[8:16], 512) }},
		{name: "backing file size", mutate: func(b []byte) { binary.BigEndian.PutUint32(b[16:20], 10) }},
		{name: "external data feature", mutate: func(b []byte) { binary.BigEndian.PutUint64(b[72:80], 4) }},
		{name: "external data extension", mutate: func(b []byte) { binary.BigEndian.PutUint32(b[104:108], 0x44415441) }},
		{name: "backing format extension", mutate: func(b []byte) { binary.BigEndian.PutUint32(b[104:108], 0xe2792aca) }},
		{name: "encryption extension", mutate: func(b []byte) { binary.BigEndian.PutUint32(b[104:108], 0x0537be77) }},
		{name: "encryption", mutate: func(b []byte) { binary.BigEndian.PutUint32(b[32:36], 1) }},
		{name: "dirty", mutate: func(b []byte) { binary.BigEndian.PutUint64(b[72:80], 1) }},
		{name: "corrupt", mutate: func(b []byte) { binary.BigEndian.PutUint64(b[72:80], 2) }},
		{name: "snapshots", mutate: func(b []byte) { binary.BigEndian.PutUint32(b[60:64], 1) }},
		{name: "oversized virtual disk", mutate: func(b []byte) { binary.BigEndian.PutUint64(b[24:32], 2<<40) }},
		{name: "invalid header bounds", mutate: func(b []byte) { binary.BigEndian.PutUint32(b[100:104], 2<<20) }},
		{name: "invalid extension bounds", mutate: func(b []byte) {
			binary.BigEndian.PutUint32(b[104:108], 0x6803f857)
			binary.BigEndian.PutUint32(b[108:112], 2<<20)
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "unsafe.qcow2")
			data := catalogQCOW2("unsafe")
			test.mutate(data)
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := validateUploadedQCOW2(path); !errors.Is(err, ErrInvalid) {
				t.Fatalf("unsafe QCOW2 metadata accepted: %v", err)
			}
		})
	}
}
