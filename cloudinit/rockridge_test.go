package cloudinit

import (
	"bytes"
	"testing"
)

func TestValidateISORockRidgeNames(t *testing.T) {
	t.Parallel()

	files := map[string][]byte{
		"userdata": []byte("#cloud-config\n"), "metadata": []byte("instance-id: rr-vm\n"),
	}
	data := testISO(t, "cidata", files)
	rewriteRootRecords(t, data, func(record []byte) []byte {
		name := string(record[33 : 33+int(record[32])])
		switch name {
		case "\x00":
			return appendSystemUse(record, rockRidgeRootFields())
		case "userdata;1":
			return appendSystemUse(record, alternateName("user-data"))
		case "metadata;1":
			return appendSystemUse(record, alternateName("meta-data"))
		}

		return record
	})
	if err := ValidateISO(writeTestISO(t, data)); err != nil {
		t.Fatalf("Rock Ridge NoCloud seed failed validation: %v", err)
	}
}

func TestValidateISORejectsRockRidgeSymlink(t *testing.T) {
	t.Parallel()

	data := testISO(t, "cidata", validSeedFiles())
	rewriteRootRecords(t, data, func(record []byte) []byte {
		name := string(record[33 : 33+int(record[32])])
		switch name {
		case "\x00":
			return appendSystemUse(record, rockRidgeRootFields())
		case "user-data;1":
			return appendSystemUse(record, []byte{'S', 'L', 5, 1, 0})
		}

		return record
	})
	if err := ValidateISO(writeTestISO(t, data)); err == nil {
		t.Fatal("seed document symlink was accepted")
	}
}

func TestValidateISORejectsRockRidgeContinuationCycle(t *testing.T) {
	t.Parallel()

	data := testISO(t, "cidata", validSeedFiles())
	// Append a continuation area referring to itself. A general recursive ISO
	// reader can loop or exhaust its stack; validation must reject it promptly.
	block := uint32(len(data) / isoSectorSize)
	continuation := make([]byte, isoSectorSize)
	copy(continuation, []byte{'C', 'E', 28, 1})
	writeDual32(continuation[4:12], block)
	writeDual32(continuation[12:20], 0)
	writeDual32(continuation[20:28], 28)
	data = append(data, continuation...)
	rewriteRootRecords(t, data, func(record []byte) []byte {
		if record[32] == 1 && record[33] == 0 {
			fields := append(rockRidgeRootFields(), continuation[:28]...)

			return appendSystemUse(record, fields)
		}

		return record
	})
	if err := ValidateISO(writeTestISO(t, data)); err == nil {
		t.Fatal("cyclic Rock Ridge continuation was accepted")
	}
}

func TestValidateISORejectsDuplicateDocuments(t *testing.T) {
	t.Parallel()

	files := validSeedFiles()
	files["otherdata"] = []byte("#cloud-config\n")
	data := testISO(t, "cidata", files)
	rewriteRootRecords(t, data, func(record []byte) []byte {
		if string(record[33:33+int(record[32])]) == "otherdata;1" {
			copy(record[33:], "user-data;1")
		}

		return record
	})
	if err := ValidateISO(writeTestISO(t, data)); err == nil {
		t.Fatal("duplicate seed document was accepted")
	}
}

func rockRidgeRootFields() []byte {
	fields := []byte{'S', 'P', 7, 1, 0xbe, 0xef, 0}
	identifier := []byte("RRIP_1991A")
	fields = append(fields, 'E', 'R', byte(8+len(identifier)), 1, byte(len(identifier)), 0, 0, 1)

	return append(fields, identifier...)
}

func alternateName(name string) []byte {
	return append([]byte{'N', 'M', byte(5 + len(name)), 1, 0}, []byte(name)...)
}

func appendSystemUse(record, fields []byte) []byte {
	record = append(bytes.Clone(record), fields...)
	record[0] = byte(len(record))

	return record
}

func rewriteRootRecords(t *testing.T, data []byte, rewrite func([]byte) []byte) {
	t.Helper()
	offset := testRootOffset(data)
	original := bytes.Clone(data[offset : offset+isoSectorSize])
	var records []byte
	for pos := 0; pos < len(original) && original[pos] != 0; {
		length := int(original[pos])
		records = append(records, rewrite(original[pos:pos+length])...)
		pos += length
	}
	if len(records) > isoSectorSize {
		t.Fatal("test root directory exceeds a sector")
	}
	clear(data[offset : offset+isoSectorSize])
	copy(data[offset:], records)
}

func FuzzValidateISOData(f *testing.F) {
	data, err := CreateSeedISO(Seed{
		UserData: []byte("#cloud-config\n"), MetaData: []byte("instance-id: fuzz-vm\n"),
	})
	if err != nil {
		f.Fatal(err)
	}
	f.Add(data)
	f.Add([]byte("not an ISO"))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > MaxISOSize {
			t.Skip("input exceeds the public validator's size bound")
		}
		_ = validateISOData(data)
	})
}
