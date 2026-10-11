package iso9660

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
)

const sectorSize = 2048

var (
	ErrNoPrimaryVolumeDescriptor = errors.New("iso9660: primary volume descriptor not found")
	ErrInvalidRootDirectory      = errors.New("iso9660: invalid root directory record")
	ErrNotFound                  = errors.New("iso9660: file not found")
	ErrNotDirectory              = errors.New("iso9660: not a directory")
)

type Reader struct {
	r    io.ReaderAt
	size int64
	root dirEntry
}

type dirEntry struct {
	name   string
	extent uint32
	size   uint32
	isDir  bool
}

func NewReader(r io.ReaderAt, size int64) (*Reader, error) {
	root, err := readRoot(r, size)
	if err != nil {
		return nil, err
	}

	return &Reader{
		r:    r,
		size: size,
		root: root,
	}, nil
}

func (r *Reader) ReadFile(name string) ([]byte, error) {
	entry, err := r.lookup(name)
	if err != nil {
		return nil, err
	}

	if entry.isDir {
		return nil, ErrNotDirectory
	}

	data := make([]byte, entry.size)
	if len(data) == 0 {
		return data, nil
	}

	n, err := r.r.ReadAt(data, int64(entry.extent)*sectorSize)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}

	return data[:n], nil
}

func (r *Reader) lookup(name string) (dirEntry, error) {
	parts := cleanPathParts(name)
	if len(parts) == 0 {
		return r.root, nil
	}

	cur := r.root
	for _, part := range parts {
		if !cur.isDir {
			return dirEntry{}, ErrNotDirectory
		}

		entries, err := r.readDir(cur)
		if err != nil {
			return dirEntry{}, err
		}

		next, ok := entries[part]
		if !ok {
			return dirEntry{}, fmt.Errorf("%s: %w", name, ErrNotFound)
		}

		cur = next
	}

	return cur, nil
}

func (r *Reader) readDir(dir dirEntry) (map[string]dirEntry, error) {
	if !dir.isDir {
		return nil, ErrNotDirectory
	}

	data := make([]byte, dir.size)
	if len(data) == 0 {
		return map[string]dirEntry{}, nil
	}

	n, err := r.r.ReadAt(data, int64(dir.extent)*sectorSize)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}

	data = data[:n]
	res := make(map[string]dirEntry)

	for off := 0; off < len(data); {
		recLen := int(data[off])
		if recLen == 0 {
			off = nextSectorOffset(off)

			continue
		}

		if off+recLen > len(data) {
			break
		}

		entry, ok := parseDirEntry(data[off : off+recLen])
		if ok && entry.name != "." && entry.name != ".." {
			res[entry.name] = entry
		}

		off += recLen
	}

	return res, nil
}

func readRoot(r io.ReaderAt, size int64) (dirEntry, error) {
	var sector [sectorSize]byte

	for lba := int64(16); (lba+1)*sectorSize <= size || size == 0; lba++ {
		if _, err := r.ReadAt(sector[:], lba*sectorSize); err != nil {
			if errors.Is(err, io.EOF) {
				return dirEntry{}, ErrNoPrimaryVolumeDescriptor
			}

			return dirEntry{}, err
		}

		if string(sector[1:6]) != "CD001" {
			continue
		}

		switch sector[0] {
		case 1:
			root, ok := parseDirEntry(sector[156:])
			if !ok {
				return dirEntry{}, ErrInvalidRootDirectory
			}

			root.name = "."
			root.isDir = true

			return root, nil
		case 255:
			return dirEntry{}, ErrNoPrimaryVolumeDescriptor
		}
	}

	return dirEntry{}, ErrNoPrimaryVolumeDescriptor
}

func parseDirEntry(b []byte) (dirEntry, bool) {
	if len(b) < 34 {
		return dirEntry{}, false
	}

	recLen := int(b[0])
	if recLen == 0 {
		return dirEntry{}, false
	}

	if recLen > len(b) {
		return dirEntry{}, false
	}

	nameLen := int(b[32])
	if 33+nameLen > recLen {
		return dirEntry{}, false
	}

	return dirEntry{
		name:   normalizeISOName(b[33 : 33+nameLen]),
		extent: binary.LittleEndian.Uint32(b[2:6]),
		size:   binary.LittleEndian.Uint32(b[10:14]),
		isDir:  b[25]&0x02 != 0,
	}, true
}

func normalizeISOName(raw []byte) string {
	if len(raw) == 1 {
		switch raw[0] {
		case 0:
			return "."
		case 1:
			return ".."
		}
	}

	name := string(raw)
	if i := strings.IndexByte(name, ';'); i >= 0 {
		name = name[:i]
	}

	name = strings.TrimRight(name, ".")
	name = strings.ToLower(name)

	return name
}

func cleanPathParts(name string) []string {
	name = strings.TrimSpace(name)
	name = strings.Trim(name, "\"'")
	name = strings.TrimPrefix(name, "cdrom:")
	name = path.Clean("/" + strings.TrimPrefix(name, "/"))
	if name == "/" || name == "." {
		return nil
	}

	parts := strings.Split(strings.TrimPrefix(name, "/"), "/")
	for i := range parts {
		parts[i] = strings.ToLower(parts[i])
	}

	return parts
}

func nextSectorOffset(off int) int {
	return ((off / sectorSize) + 1) * sectorSize
}
