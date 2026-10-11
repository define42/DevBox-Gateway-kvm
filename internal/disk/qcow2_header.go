//nolint:err113 // Precise errors identify malformed or unsupported QCOW2 metadata.
package disk

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	qcow2MaxL1Bytes            = 32 << 20
	qcow2MaxRefcountTableBytes = 8 << 20

	qcow2ExtensionBackingFormat = 0xe2792aca
	qcow2ExtensionFeatureNames  = 0x6803f857
	qcow2ExtensionBitmaps       = 0x23852875
	qcow2ExtensionEncryption    = 0x0537be77
	qcow2ExtensionExternalData  = 0x44415441

	qcow2FeatureNameEntrySize = 48
)

func validateHeaderGeometry(h *qcow2Header) error {
	clusterSize := uint64(1) << h.clusterBits
	if uint64(h.headerLength) > clusterSize {
		return errors.New("qcow2: header exceeds cluster size")
	}
	l1Bytes := uint64(h.l1Size) * 8
	if l1Bytes > qcow2MaxL1Bytes {
		return errors.New("qcow2: l1 table exceeds supported size")
	}
	refcountBytes := uint64(h.refcountTableClusters) * clusterSize
	if refcountBytes > qcow2MaxRefcountTableBytes {
		return errors.New("qcow2: refcount table exceeds supported size")
	}
	if h.l1TableOffset > uint64(maxInt64)-l1Bytes ||
		h.refcountTableOffset > uint64(maxInt64)-refcountBytes {
		return errors.New("qcow2: metadata table offset exceeds supported file size")
	}
	l1Coverage := clusterSize * (clusterSize / 8)
	requiredL1 := h.size / l1Coverage
	if h.size%l1Coverage != 0 {
		requiredL1++
	}
	if uint64(h.l1Size) < requiredL1 {
		return errors.New("qcow2: l1 table is too small for virtual disk size")
	}

	return nil
}

func (q *qcow2Image) validateHeaderCluster(h *qcow2Header) error {
	// validateHeader bounds cluster size and header length before this allocation.
	cluster := make([]byte, uint64(1)<<h.clusterBits)
	if _, err := q.file.ReadAt(cluster, 0); err != nil {
		return fmt.Errorf("qcow2: read header cluster: %w", err)
	}
	if h.version == qcow2Version3 && h.headerLength > qcow2HeaderV3Len {
		if cluster[104] != 0 {
			return errors.New("qcow2: compression type requires unsupported incompatible feature")
		}
		for _, value := range cluster[105:112] {
			if value != 0 {
				return errors.New("qcow2: nonzero header padding")
			}
		}
	}

	return validateHeaderExtensions(cluster[h.headerLength:])
}

func validateHeaderExtensions(area []byte) error {
	seen := make(map[uint32]bool)
	for len(area) != 0 {
		if len(area) < 8 {
			return errors.New("qcow2: truncated header extension")
		}
		kind := binary.BigEndian.Uint32(area[:4])
		length := uint64(binary.BigEndian.Uint32(area[4:8]))
		if kind == 0 {
			if length != 0 {
				return errors.New("qcow2: invalid header extension terminator")
			}

			return nil
		}
		if seen[kind] {
			return fmt.Errorf("qcow2: duplicate header extension %#x", kind)
		}
		seen[kind] = true
		padded := alignUp(length, 8)
		if padded > uint64(len(area)-8) {
			return errors.New("qcow2: header extension exceeds first cluster")
		}
		if err := validateHeaderExtension(kind, area[8:8+length]); err != nil {
			return err
		}
		area = area[8+padded:]
	}

	return nil
}

func validateHeaderExtension(kind uint32, data []byte) error {
	switch kind {
	case qcow2ExtensionBackingFormat:
		return errors.New("qcow2: backing format extension is unsupported without a backing file")
	case qcow2ExtensionFeatureNames:
		if len(data)%qcow2FeatureNameEntrySize != 0 {
			return errors.New("qcow2: invalid feature name table length")
		}
		for offset := 0; offset < len(data); offset += qcow2FeatureNameEntrySize {
			if data[offset] > 2 || data[offset+1] > 63 {
				return errors.New("qcow2: invalid feature name table entry")
			}
		}
	case qcow2ExtensionBitmaps:
		return errors.New("qcow2: bitmap extensions are unsupported")
	case qcow2ExtensionEncryption:
		return errors.New("qcow2: encryption extension requires unsupported encryption method")
	case qcow2ExtensionExternalData:
		return errors.New("qcow2: external data extension requires unsupported external data feature")
	}

	// Unknown extensions are compatible metadata. Leave their contents intact.
	return nil
}
