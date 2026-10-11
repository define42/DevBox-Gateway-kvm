package cloudinit

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

const (
	isoSectorSize = 2048
	maxRootSize   = 128 << 10
	maxRRDepth    = 8
)

var (
	errInvalidISO      = errors.New("invalid cloud-init ISO filesystem")
	errISOFile         = errors.New("cloud-init ISO must be a regular file")
	errISOSize         = errors.New("cloud-init ISO exceeds the 16 MiB size limit")
	errSeedRegular     = errors.New("cloud-init seed documents must be regular files")
	errSeedDuplicate   = errors.New("cloud-init ISO contains duplicate seed documents")
	errMissingMetaData = errors.New("cloud-init ISO requires meta-data at its root")
	errISOLabel        = errors.New("cloud-init ISO label must be cidata or CIDATA")
)

// ValidateISO checks a regular, read-only-opened seed file before VM startup.
// The image must be at most MaxISOSize bytes, labelled cidata or CIDATA, and
// contain nonempty user-data and valid meta-data files at its root. Optional
// network-config counts towards the same document limit as CreateSeedISO.
// ISO9660, Joliet, and Rock Ridge alternate names are supported. File contents
// are never included in errors and the image is never mounted or modified.
func ValidateISO(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open cloud-init ISO: %w", err)
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat cloud-init ISO: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errISOFile
	}
	if info.Size() > MaxISOSize {
		return errISOSize
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxISOSize+1))
	if err != nil {
		return fmt.Errorf("read cloud-init ISO: %w", err)
	}
	if len(data) > MaxISOSize {
		return errISOSize
	}

	return validateISOData(data)
}

// Seed validation uses a bounded root-only parser. The ISO writer's general
// reader follows Rock Ridge continuations without bounding allocation/depth.
type seedISO struct {
	data     []byte
	rrBudget int
}

type seedEntry struct {
	name      string
	data      []byte
	systemUse []byte
	flags     byte
}

func validateISOData(data []byte) error {
	image := seedISO{data: data, rrBudget: MaxISOSize}
	root, jolietRoot, err := image.rootDirectory()
	if err != nil {
		return err
	}
	entries, err := image.directory(root)
	if err != nil {
		return err
	}
	rrSkip, rockRidge, err := image.rockRidge(entries)
	if err != nil {
		return err
	}
	joliet := !rockRidge && jolietRoot != nil
	if joliet {
		entries, err = image.directory(jolietRoot)
		if err != nil {
			return err
		}
	}
	documents := make(map[string][]byte, 3)
	for _, entry := range entries {
		name, regular, err := image.entryName(entry, rrSkip, rockRidge, joliet)
		if err != nil {
			return err
		}
		switch name {
		case "user-data", "meta-data", "network-config":
			if !regular {
				return errSeedRegular
			}
			if _, found := documents[name]; found {
				return errSeedDuplicate
			}
			documents[name] = entry.data
		}
	}
	if _, found := documents["meta-data"]; !found {
		return errMissingMetaData
	}

	return validateSeed(Seed{
		UserData: documents["user-data"], MetaData: documents["meta-data"], NetworkConfig: documents["network-config"],
	})
}

func (image *seedISO) rootDirectory() ([]byte, []byte, error) {
	var root, joliet []byte
	for offset := 16 * isoSectorSize; offset+isoSectorSize <= len(image.data); offset += isoSectorSize {
		descriptor := image.data[offset : offset+isoSectorSize]
		if string(descriptor[1:6]) != "CD001" || descriptor[6] != 1 {
			return nil, nil, errInvalidISO
		}
		switch descriptor[0] {
		case 1:
			if root != nil {
				return nil, nil, errInvalidISO
			}
			label := strings.TrimRight(string(descriptor[40:72]), " ")
			if label != "cidata" && label != "CIDATA" {
				return nil, nil, errISOLabel
			}
			var err error
			root, err = image.descriptorRoot(descriptor)
			if err != nil {
				return nil, nil, err
			}
		case 2:
			escape := string(descriptor[88:91])
			if escape == "%/@" || escape == "%/C" || escape == "%/E" {
				var err error
				joliet, err = image.descriptorRoot(descriptor)
				if err != nil {
					return nil, nil, err
				}
			}
		case 255:
			if root == nil {
				return nil, nil, errInvalidISO
			}

			return root, joliet, nil
		}
	}

	return nil, nil, errInvalidISO
}

func (image *seedISO) descriptorRoot(descriptor []byte) ([]byte, error) {
	if binary.LittleEndian.Uint16(descriptor[128:130]) != isoSectorSize ||
		binary.BigEndian.Uint16(descriptor[130:132]) != isoSectorSize {
		return nil, errInvalidISO
	}
	entry, err := image.entry(descriptor[156:190])
	if err != nil || entry.flags&2 == 0 || len(entry.data) == 0 || len(entry.data) > maxRootSize {
		return nil, errInvalidISO
	}

	return entry.data, nil
}

func (image *seedISO) directory(data []byte) ([]seedEntry, error) {
	var entries []seedEntry
	for pos := 0; pos < len(data); {
		length := int(data[pos])
		if length == 0 {
			pos = (pos/isoSectorSize + 1) * isoSectorSize

			continue
		}
		if length > len(data)-pos || length > isoSectorSize-pos%isoSectorSize {
			return nil, errInvalidISO
		}
		entry, err := image.entry(data[pos : pos+length])
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
		pos += length
	}

	return entries, nil
}

func (image *seedISO) entry(record []byte) (seedEntry, error) {
	if len(record) < 34 || int(record[0]) != len(record) {
		return seedEntry{}, errInvalidISO
	}
	nameLength := int(record[32])
	useOffset := 33 + nameLength + (nameLength+1)%2
	if nameLength == 0 || useOffset > len(record) || record[1] != 0 || record[26] != 0 || record[27] != 0 {
		return seedEntry{}, errInvalidISO
	}
	extent, ok := isoUint32(record[2:10])
	if !ok {
		return seedEntry{}, errInvalidISO
	}
	length, ok := isoUint32(record[10:18])
	if !ok {
		return seedEntry{}, errInvalidISO
	}
	data, err := image.region(uint64(extent)*isoSectorSize, uint64(length))
	if err != nil {
		return seedEntry{}, err
	}

	return seedEntry{
		name: string(record[33 : 33+nameLength]), data: data,
		systemUse: record[useOffset:], flags: record[25],
	}, nil
}

func (image *seedISO) region(offset, length uint64) ([]byte, error) {
	if offset > uint64(len(image.data)) || length > uint64(len(image.data))-offset {
		return nil, errInvalidISO
	}

	return image.data[offset : offset+length], nil
}

func isoUint32(data []byte) (uint32, bool) {
	value := binary.LittleEndian.Uint32(data[:4])

	return value, value == binary.BigEndian.Uint32(data[4:8])
}
