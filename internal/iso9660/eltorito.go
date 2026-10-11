package iso9660

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
)

const elToritoSystemID = "EL TORITO SPECIFICATION"

var (
	errNoElToritoBootCatalog = errors.New("iso9660: el torito boot catalog not found")
	errInvalidBootCatalog    = errors.New("iso9660: invalid el torito boot catalog")
	errNoElToritoBootImage   = errors.New("iso9660: el torito boot image not found")
)

type elToritoBootEntry struct {
	bootable    bool
	mediaType   uint8
	loadSegment uint16
	systemType  uint8
	sectorCount uint16
	imageLBA    uint32
}

func (r *Reader) elToritoBootImagePath() (string, error) {
	entry, err := r.elToritoDefaultEntry()
	if err != nil {
		return "", err
	}

	if !entry.bootable || entry.imageLBA == 0 {
		return "", errNoElToritoBootImage
	}

	name, err := r.findPathByExtent(entry.imageLBA)
	if err != nil {
		return "", fmt.Errorf("el torito image lba %d: %w", entry.imageLBA, err)
	}

	return name, nil
}

func (r *Reader) elToritoDefaultEntry() (elToritoBootEntry, error) {
	catalogLBA, err := r.elToritoCatalogLBA()
	if err != nil {
		return elToritoBootEntry{}, err
	}

	catalog, err := r.readSector(catalogLBA)
	if err != nil {
		return elToritoBootEntry{}, fmt.Errorf("el torito catalog: %w", err)
	}

	if !validElToritoValidationEntry(catalog[:32]) {
		return elToritoBootEntry{}, errInvalidBootCatalog
	}

	return parseElToritoInitialEntry(catalog[32:64])
}

func (r *Reader) elToritoCatalogLBA() (uint32, error) {
	var sector [sectorSize]byte

	for lba := int64(16); (lba+1)*sectorSize <= r.size || r.size == 0; lba++ {
		if _, err := r.r.ReadAt(sector[:], lba*sectorSize); err != nil {
			if errors.Is(err, io.EOF) {
				return 0, errNoElToritoBootCatalog
			}

			return 0, err
		}

		if string(sector[1:6]) != "CD001" {
			continue
		}

		if sector[0] == 255 {
			return 0, errNoElToritoBootCatalog
		}

		if sector[0] != 0 || sector[6] != 1 {
			continue
		}

		systemID := strings.TrimRight(string(sector[7:39]), "\x00 ")
		if systemID != elToritoSystemID {
			continue
		}

		catalogLBA := binary.LittleEndian.Uint32(sector[71:75])
		if catalogLBA == 0 {
			return 0, errInvalidBootCatalog
		}

		return catalogLBA, nil
	}

	return 0, errNoElToritoBootCatalog
}

func (r *Reader) readSector(lba uint32) ([]byte, error) {
	sector := make([]byte, sectorSize)
	if _, err := r.r.ReadAt(sector, int64(lba)*sectorSize); err != nil {
		return nil, err
	}

	return sector, nil
}

func validElToritoValidationEntry(entry []byte) bool {
	if len(entry) < 32 || entry[0] != 0x01 || entry[30] != 0x55 || entry[31] != 0xaa {
		return false
	}

	var sum uint16
	for i := 0; i < 32; i += 2 {
		sum += binary.LittleEndian.Uint16(entry[i : i+2])
	}

	return sum == 0
}

func parseElToritoInitialEntry(entry []byte) (elToritoBootEntry, error) {
	if len(entry) < 32 {
		return elToritoBootEntry{}, errInvalidBootCatalog
	}

	bootIndicator := entry[0]
	if bootIndicator != 0x88 && bootIndicator != 0x00 {
		return elToritoBootEntry{}, errInvalidBootCatalog
	}

	return elToritoBootEntry{
		bootable:    bootIndicator == 0x88,
		mediaType:   entry[1],
		loadSegment: binary.LittleEndian.Uint16(entry[2:4]),
		systemType:  entry[4],
		sectorCount: binary.LittleEndian.Uint16(entry[6:8]),
		imageLBA:    binary.LittleEndian.Uint32(entry[8:12]),
	}, nil
}

func (r *Reader) findPathByExtent(extent uint32) (string, error) {
	seen := map[uint32]bool{}

	if name, ok, err := r.findPathByExtentInDir("/", r.root, extent, seen); ok || err != nil {
		return name, err
	}

	return "", ErrNotFound
}

func (r *Reader) findPathByExtentInDir(
	base string,
	dir dirEntry,
	extent uint32,
	seen map[uint32]bool,
) (string, bool, error) {
	if seen[dir.extent] {
		return "", false, nil
	}
	seen[dir.extent] = true

	entries, err := r.readDir(dir)
	if err != nil {
		return "", false, err
	}

	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		entry := entries[name]
		entryPath := path.Join(base, name)
		if !entry.isDir && entry.extent == extent {
			return entryPath, true, nil
		}

		if !entry.isDir {
			continue
		}

		found, ok, err := r.findPathByExtentInDir(entryPath, entry, extent, seen)
		if ok || err != nil {
			return found, ok, err
		}
	}

	return "", false, nil
}
