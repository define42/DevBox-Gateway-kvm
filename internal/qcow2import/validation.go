// SPDX-License-Identifier: Apache-2.0
// Adapted from lima-vm/go-qcow2reader; see README.md for origin and changes.
//
//nolint:err113 // Format errors identify malformed untrusted image metadata.
package qcow2import

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
)

type metadataRange struct {
	start, end uint64
	kind       string
}

// Refcounts are accumulated in sparse pages to avoid a map entry per guest
// cluster. Cap counter storage at 64 MiB even for fragmented source files.
const (
	referencePageClusters = 4096
	maxReferencePages     = (64 << 20) / (referencePageClusters * 4)
)

type referenceCounts map[uint64]*[referencePageClusters]uint32

func (counts referenceCounts) add(offset, length, clusterSize, references uint64) error {
	first, last := offset/clusterSize, (offset+length-1)/clusterSize
	for cluster := first; cluster <= last; cluster++ {
		index := cluster / referencePageClusters
		page := counts[index]
		if page == nil {
			if len(counts) >= maxReferencePages {
				return errors.New("fragmented image exceeds 64 MiB reference validation limit")
			}
			page = new([referencePageClusters]uint32)
			counts[index] = page
		}
		counter := &page[cluster%referencePageClusters]
		if references > uint64(^uint32(0))-uint64(*counter) {
			return errors.New("reference count overflow")
		}
		*counter += uint32(references)
	}

	return nil
}

func (r *Reader) addMetadata(offset, length uint64, kind string) error {
	if offset%r.clusterSize != 0 || length == 0 || (offset == 0 && kind != "header") ||
		offset > r.fileSize || length > r.fileSize-offset {
		return fmt.Errorf("invalid %s offset or length", kind)
	}
	length = ceilDiv(length, r.clusterSize) * r.clusterSize
	if length > maxMetadataBytes-r.metadataBytes {
		return errors.New("referenced metadata exceeds 128 MiB validation limit")
	}
	r.metadataBytes += length
	r.metadata = append(r.metadata, metadataRange{offset, offset + length, kind})

	return nil
}

func (r *Reader) readExtensions(area []byte, autoclear uint64) error {
	seen := make(map[uint32]bool)
	for len(area) >= 8 {
		kind := binary.BigEndian.Uint32(area[:4])
		length := uint64(binary.BigEndian.Uint32(area[4:8]))
		if kind == 0 {
			if length != 0 {
				return errors.New("invalid extension terminator")
			}
			if (autoclear&1 != 0) != seen[0x23852875] {
				return errors.New("inconsistent bitmap extension feature")
			}

			return nil
		}
		if seen[kind] {
			return errors.New("duplicate header extension")
		}
		seen[kind] = true
		padded := ceilDiv(length, 8) * 8
		if padded > uint64(len(area)-8) {
			return errors.New("header extension exceeds first cluster")
		}
		data := area[8 : 8+length]
		switch kind {
		case 0xe2792aca, 0x0537be77, 0x44415441:
			return errors.New("backing file, encryption or external data extension is unsupported")
		case 0x6803f857:
			if len(data)%48 != 0 {
				return errors.New("invalid feature name table")
			}
			for i := 0; i < len(data); i += 48 {
				if data[i] > 2 || data[i+1] > 63 {
					return errors.New("invalid feature name entry")
				}
			}
		case 0x23852875:
			if autoclear&1 == 0 {
				return errors.New("inconsistent bitmap metadata")
			}
			if err := r.readBitmaps(data); err != nil {
				return err
			}
		}
		area = area[8+padded:]
	}

	return errors.New("missing header extension terminator")
}

// Bitmap state is discarded by conversion, but its storage must be protected
// against guest data overlap just like the allocation tables.
func (r *Reader) readBitmaps(ext []byte) error {
	if len(ext) != 24 {
		return errors.New("invalid bitmap extension length")
	}
	count := binary.BigEndian.Uint32(ext[:4])
	if count == 0 || count > 65535 || binary.BigEndian.Uint32(ext[4:8]) != 0 {
		return errors.New("invalid bitmap count or reserved field")
	}
	length := binary.BigEndian.Uint64(ext[8:16])
	offset := binary.BigEndian.Uint64(ext[16:24])
	if err := r.addMetadata(offset, length, "bitmap directory"); err != nil {
		return err
	}
	directory, err := r.readBytes(offset, length)
	if err != nil {
		return err
	}
	names := make(map[string]bool)
	for range count {
		if len(directory) < 24 {
			return errors.New("truncated bitmap directory")
		}
		tableOffset := binary.BigEndian.Uint64(directory[:8])
		entries := uint64(binary.BigEndian.Uint32(directory[8:12]))
		flags := binary.BigEndian.Uint32(directory[12:16])
		granularity := directory[17]
		nameLength := uint64(binary.BigEndian.Uint16(directory[18:20]))
		extraLength := uint64(binary.BigEndian.Uint32(directory[20:24]))
		entryLength := ceilDiv(24+nameLength+extraLength, 8) * 8
		if flags & ^uint32(7) != 0 || directory[16] != 1 || granularity > 63 ||
			nameLength == 0 || nameLength > 1023 || entryLength > uint64(len(directory)) {
			return errors.New("invalid bitmap directory entry")
		}
		name := string(directory[24+extraLength : 24+extraLength+nameLength])
		if names[name] {
			return errors.New("duplicate bitmap name")
		}
		names[name] = true
		for _, b := range directory[24+extraLength+nameLength : entryLength] {
			if b != 0 {
				return errors.New("nonzero bitmap padding")
			}
		}
		bits := ceilDiv(r.size, uint64(1)<<granularity)
		if entries < ceilDiv(bits, r.clusterSize*8) {
			return errors.New("bitmap table too small")
		}
		if err = r.addMetadata(tableOffset, entries*8, "bitmap table"); err != nil {
			return err
		}
		table, err := r.readBytes(tableOffset, entries*8)
		if err != nil {
			return err
		}
		for i := 0; i < len(table); i += 8 {
			entry := binary.BigEndian.Uint64(table[i:])
			blockOffset := entry & offsetMask
			if entry & ^(offsetMask|1) != 0 || (blockOffset != 0 && entry&1 != 0) {
				return errors.New("invalid bitmap table entry")
			}
			if blockOffset != 0 {
				if err = r.addMetadata(blockOffset, r.clusterSize, "bitmap block"); err != nil {
					return err
				}
			}
		}
		directory = directory[entryLength:]
	}
	if len(directory) != 0 {
		return errors.New("unexpected trailing bitmap directory bytes")
	}

	return nil
}

func (r *Reader) refcount(offset uint64) (uint64, error) {
	entries := r.clusterSize * 8 / (uint64(1) << r.refcountOrder)
	cluster := offset / r.clusterSize
	index := cluster / entries
	if index >= uint64(len(r.refcounts)) || r.refcounts[index] == 0 {
		return 0, errors.New("referenced cluster has no refcount block")
	}
	blockOffset := r.refcounts[index]
	if r.refCache == nil || r.refCacheOffset != blockOffset {
		p, err := r.readBytes(blockOffset, r.clusterSize)
		if err != nil {
			return 0, err
		}
		r.refCache = p
		r.refCacheOffset = blockOffset
	}
	entry := cluster % entries
	width := uint64(1) << r.refcountOrder
	bit := entry * width
	if width < 8 {
		return uint64(r.refCache[bit/8]>>(bit%8)) & ((uint64(1) << width) - 1), nil
	}
	data := r.refCache[bit/8:]
	switch width {
	case 8:
		return uint64(data[0]), nil
	case 16:
		return uint64(binary.BigEndian.Uint16(data)), nil
	case 32:
		return uint64(binary.BigEndian.Uint32(data)), nil
	default:
		return binary.BigEndian.Uint64(data), nil
	}
}

func (r *Reader) checkReferences(offset, length uint64, copied bool) error {
	first := offset / r.clusterSize
	last := (offset + length - 1) / r.clusterSize
	for cluster := first; cluster <= last; cluster++ {
		count, err := r.refcount(cluster * r.clusterSize)
		if err != nil {
			return err
		}
		if count == 0 || (copied && count != 1) {
			return errors.New("invalid referenced cluster refcount")
		}
	}

	return nil
}

func (r *Reader) validateLayout() error {
	l2Seen, err := r.collectL2Tables()
	if err != nil {
		return err
	}
	counts := make(referenceCounts)
	if err := r.validateMetadataReferences(l2Seen, counts); err != nil {
		return err
	}
	if err := r.validateL2References(l2Seen, counts); err != nil {
		return err
	}

	return r.validateReferenceCounts(counts)
}

// Duplicate L2 pointers represent shared tables. Other metadata overlaps
// are invalid, including an L2 table aliasing a refcount or bitmap block.
func (r *Reader) collectL2Tables() (map[uint64]uint64, error) {
	l2Seen := make(map[uint64]uint64)
	for _, entry := range r.l1 {
		if err := r.ctx.Err(); err != nil {
			return nil, err
		}
		if uint64(entry) & ^(offsetMask|copiedFlag) != 0 {
			return nil, errors.New("reserved L1 entry bits")
		}
		offset := entry.l2Offset()
		if offset == 0 {
			if entry != 0 {
				return nil, errors.New("copied flag on unallocated L2 table")
			}

			continue
		}
		if l2Seen[offset] == 0 {
			if err := r.addMetadata(offset, r.clusterSize, "L2 table"); err != nil {
				return nil, err
			}
		}
		l2Seen[offset]++
		if err := r.checkReferences(offset, r.clusterSize, uint64(entry)&copiedFlag != 0); err != nil {
			return nil, err
		}
	}

	return l2Seen, nil
}

func (r *Reader) validateMetadataReferences(l2Seen map[uint64]uint64, counts referenceCounts) error {
	sort.Slice(r.metadata, func(i, j int) bool { return r.metadata[i].start < r.metadata[j].start })
	for i, area := range r.metadata {
		if err := r.ctx.Err(); err != nil {
			return err
		}
		if i > 0 && area.start < r.metadata[i-1].end {
			return fmt.Errorf("overlapping %s and %s", r.metadata[i-1].kind, area.kind)
		}
		if err := r.checkReferences(area.start, area.end-area.start, false); err != nil {
			return err
		}
		references := uint64(1)
		if area.kind == "L2 table" {
			references = l2Seen[area.start]
		}
		if err := counts.add(area.start, area.end-area.start, r.clusterSize, references); err != nil {
			return err
		}
	}

	return nil
}

func (r *Reader) validateL2References(l2Seen map[uint64]uint64, counts referenceCounts) error {
	for offset, references := range l2Seen {
		if err := r.ctx.Err(); err != nil {
			return err
		}
		count, err := r.refcount(offset)
		if err != nil {
			return err
		}
		if count < references {
			return errors.New("insufficient shared L2 table refcount")
		}
		table, err := r.readBytes(offset, r.clusterSize)
		if err != nil {
			return err
		}
		stride := 8
		if r.extended {
			stride = 16
		}
		for i := 0; i < len(table); i += stride {
			if i%4096 == 0 {
				if err := r.ctx.Err(); err != nil {
					return err
				}
			}
			entry := l2TableEntry(binary.BigEndian.Uint64(table[i:]))
			var bitmap uint64
			if r.extended {
				bitmap = binary.BigEndian.Uint64(table[i+8:])
			}
			if err := r.validateDataEntry(entry, bitmap); err != nil {
				return err
			}
			dataOffset, dataLength := entry.hostClusterOffset(), r.clusterSize
			if entry.compressed() {
				dataOffset, dataLength = entry.compressedRange(r.clusterBits)
			}
			if dataOffset != 0 {
				if err := counts.add(dataOffset, dataLength, r.clusterSize, references); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func (r *Reader) validateReferenceCounts(counts referenceCounts) error {
	for pageIndex, page := range counts {
		if err := r.ctx.Err(); err != nil {
			return err
		}
		for index, expected := range page {
			if expected == 0 {
				continue
			}
			actual, err := r.refcount((pageIndex*referencePageClusters + uint64(index)) * r.clusterSize)
			if err != nil {
				return err
			}
			if actual < uint64(expected) {
				return errors.New("insufficient refcount for shared cluster")
			}
		}
	}

	return nil
}

func (r *Reader) validateDataEntry(entry l2TableEntry, bitmap uint64) error {
	var offset, length uint64
	if entry.compressed() {
		var err error
		offset, length, err = r.validateCompressedEntry(entry, bitmap)
		if err != nil {
			return err
		}
	} else {
		if err := r.validateStandardEntry(entry, bitmap); err != nil {
			return err
		}
		offset, length = entry.hostClusterOffset(), r.clusterSize
		if offset == 0 {
			return nil
		}
	}
	if offset == 0 || offset > r.fileSize || length > r.fileSize-offset {
		return errors.New("data cluster exceeds source file")
	}
	first := offset / r.clusterSize * r.clusterSize
	end := ceilDiv(offset+length, r.clusterSize) * r.clusterSize
	index := sort.Search(len(r.metadata), func(i int) bool { return r.metadata[i].end > first })
	if index < len(r.metadata) && r.metadata[index].start < end {
		return errors.New("guest data overlaps image metadata")
	}

	return r.checkReferences(offset, length, uint64(entry)&copiedFlag != 0)
}

func (r *Reader) validateStandardEntry(entry l2TableEntry, bitmap uint64) error {
	if uint64(entry) & ^(offsetMask|copiedFlag|1) != 0 {
		return errors.New("reserved L2 entry bits")
	}
	if uint64(entry)&1 != 0 && (r.version == 2 || r.extended) {
		return errors.New("zero cluster flag unsupported in this layout")
	}
	offset := entry.hostClusterOffset()
	if offset%r.clusterSize != 0 {
		return errors.New("unaligned data cluster")
	}
	if r.extended && (uint32(bitmap)&uint32(bitmap>>32) != 0 || (uint32(bitmap) != 0 && offset == 0)) {
		return errors.New("invalid extended L2 allocation bitmap")
	}
	if offset == 0 && uint64(entry)&copiedFlag != 0 {
		return errors.New("copied flag on unallocated cluster")
	}

	return nil
}

func (r *Reader) validateCompressedEntry(entry l2TableEntry, bitmap uint64) (uint64, uint64, error) {
	if uint64(entry)&copiedFlag != 0 || bitmap != 0 {
		return 0, 0, errors.New("invalid compressed entry flags or bitmap")
	}
	offset, length := entry.compressedRange(r.clusterBits)
	if offset>>56 != 0 {
		return 0, 0, errors.New("reserved compressed offset bits")
	}

	return offset, length, nil
}
