//nolint:err113 // Precise errors identify unsupported image geometry.
package disk

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
)

const qcow2CreateClusterBits = 16

// CreateQCOW2 creates a new, writable, sparse QCOW2 v3 image with the requested
// positive guest-visible size. The image uses 64 KiB clusters and 16-bit
// refcounts, and reserves enough metadata coverage to allocate the entire disk.
// It creates path exclusively with mode 0600 and removes incomplete output on
// failure. The caller owns the returned Image and must Sync and Close it.
func CreateQCOW2(path string, virtualSize int64) (Image, error) {
	h, err := qcow2CreateHeader(virtualSize)
	if err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, fmt.Errorf("qcow2: create image: %w", err)
	}
	complete := false
	defer func() {
		if !complete {
			_ = file.Close()
			_ = os.Remove(path)
		}
	}()
	if err := initializeQCOW2(file, h); err != nil {
		return nil, fmt.Errorf("qcow2: initialize image: %w", err)
	}
	image, err := openQCOW2(file)
	if err != nil {
		return nil, fmt.Errorf("qcow2: validate new image: %w", err)
	}
	complete = true

	return image, nil
}

func qcow2CreateHeader(virtualSize int64) (*qcow2Header, error) {
	if virtualSize <= 0 {
		return nil, ErrInvalidSize
	}
	const clusterSize = uint64(1) << qcow2CreateClusterBits
	const l2Entries = clusterSize / 8
	const refcountBlockEntries = clusterSize / 2
	guestClusters := (uint64(virtualSize) + clusterSize - 1) / clusterSize
	l1Entries := (guestClusters + l2Entries - 1) / l2Entries
	if l1Entries > qcow2MaxL1Bytes/8 {
		return nil, errors.New("qcow2: virtual disk requires an oversized l1 table")
	}
	l1Clusters := (l1Entries + l2Entries - 1) / l2Entries
	refcountTableClusters := uint64(1)
	for {
		// Count every possible data and L2 cluster plus the fixed metadata.
		// Each refcount block consumes one of the clusters it accounts for.
		// Iterate because the refcount table itself also needs refcounts.
		withoutBlocks := guestClusters + l1Entries + 1 + l1Clusters + refcountTableClusters
		blocks := (withoutBlocks + refcountBlockEntries - 2) / (refcountBlockEntries - 1)
		required := (blocks + l2Entries - 1) / l2Entries
		if required > qcow2MaxRefcountTableBytes/clusterSize {
			return nil, errors.New("qcow2: virtual disk requires an oversized refcount table")
		}
		if required == refcountTableClusters {
			break
		}
		refcountTableClusters = required
	}
	// Both metadata size limits above ensure the initial metadata fits within
	// one refcount block. Later blocks are created by the append allocator.
	h := &qcow2Header{
		version:               qcow2Version3,
		clusterBits:           qcow2CreateClusterBits,
		size:                  uint64(virtualSize),
		l1Size:                uint32(l1Entries),
		l1TableOffset:         (1 + refcountTableClusters) * clusterSize,
		refcountTableOffset:   clusterSize,
		refcountTableClusters: uint32(refcountTableClusters),
		refcountOrder:         qcow2RefcountOrder16,
		headerLength:          qcow2HeaderV3Len,
	}
	if err := validateHeader(h); err != nil {
		return nil, err
	}

	return h, nil
}

func initializeQCOW2(file *os.File, h *qcow2Header) error {
	clusterSize := uint64(1) << h.clusterBits
	refcountBlockOffset := h.l1TableOffset + alignUp(uint64(h.l1Size)*8, clusterSize)
	fileSize := refcountBlockOffset + clusterSize
	if fileSize/clusterSize > clusterSize/2 {
		return errors.New("qcow2: initial metadata exceeds one refcount block")
	}
	if err := file.Truncate(int64(fileSize)); err != nil {
		return err
	}
	var header [qcow2HeaderV3Len]byte
	copy(header[:], qcow2Magic)
	binary.BigEndian.PutUint32(header[4:8], h.version)
	binary.BigEndian.PutUint32(header[20:24], h.clusterBits)
	binary.BigEndian.PutUint64(header[24:32], h.size)
	binary.BigEndian.PutUint32(header[36:40], h.l1Size)
	binary.BigEndian.PutUint64(header[40:48], h.l1TableOffset)
	binary.BigEndian.PutUint64(header[48:56], h.refcountTableOffset)
	binary.BigEndian.PutUint32(header[56:60], h.refcountTableClusters)
	binary.BigEndian.PutUint32(header[96:100], h.refcountOrder)
	binary.BigEndian.PutUint32(header[100:104], h.headerLength)
	if _, err := file.WriteAt(header[:], 0); err != nil {
		return err
	}
	var entry [8]byte
	binary.BigEndian.PutUint64(entry[:], refcountBlockOffset)
	if _, err := file.WriteAt(entry[:], int64(h.refcountTableOffset)); err != nil {
		return err
	}
	block := make([]byte, clusterSize)
	for cluster := uint64(0); cluster < fileSize/clusterSize; cluster++ {
		binary.BigEndian.PutUint16(block[cluster*2:], 1)
	}
	_, err := file.WriteAt(block, int64(refcountBlockOffset))

	return err
}
