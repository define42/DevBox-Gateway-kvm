package cloudinit

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestValidateISOJolietNames(t *testing.T) {
	t.Parallel()

	for _, uppercase := range []bool{false, true} {
		name := "exact names"
		if uppercase {
			name = "wrong case"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			data := testISO(t, "cidata", validSeedFiles())
			rootOffset := testRootOffset(data)
			root := bytes.Clone(data[rootOffset : rootOffset+isoSectorSize])
			primaryBlock := uint32(len(data) / isoSectorSize)
			jolietBlock := primaryBlock + 1
			data = append(data, make([]byte, 2*isoSectorSize)...)
			copy(data[int(primaryBlock)*isoSectorSize:], root)
			copy(data[int(jolietBlock)*isoSectorSize:], testJolietRecords(root, uppercase))
			primary := data[16*isoSectorSize : 17*isoSectorSize]
			writeDual32(primary[158:166], primaryBlock)
			terminator := bytes.Clone(data[17*isoSectorSize : 18*isoSectorSize])
			copy(data[17*isoSectorSize:], primary)
			supplementary := data[17*isoSectorSize : 18*isoSectorSize]
			supplementary[0] = 2
			copy(supplementary[88:91], "%/E")
			writeDual32(supplementary[158:166], jolietBlock)
			copy(data[18*isoSectorSize:], terminator)
			err := ValidateISO(writeTestISO(t, data))
			if (err == nil) == uppercase {
				t.Fatalf("Joliet exact-name validation uppercase=%v: %v", uppercase, err)
			}
		})
	}
}

func testJolietRecords(root []byte, uppercase bool) []byte {
	var records []byte
	for pos := 0; pos < len(root) && root[pos] != 0; {
		length := int(root[pos])
		record := root[pos : pos+length]
		fileName := string(record[33 : 33+int(record[32])])
		pos += length
		if fileName == "\x00" || fileName == "\x01" {
			records = append(records, record...)

			continue
		}
		if uppercase && strings.HasPrefix(fileName, "user-data") {
			fileName = strings.ToUpper(fileName)
		}
		encoded := utf16.Encode([]rune(fileName))
		converted := make([]byte, 34+len(encoded)*2)
		copy(converted, record[:33])
		converted[0], converted[32] = byte(len(converted)), byte(len(encoded)*2)
		for i, unit := range encoded {
			binary.BigEndian.PutUint16(converted[33+2*i:], unit)
		}
		records = append(records, converted...)
	}

	return records
}
