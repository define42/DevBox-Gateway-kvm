//nolint:err113 // The qcow2 parser returns precise validation errors for malformed image metadata.
package disk

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
)

const (
	qcow2Version2 = 2
	qcow2Version3 = 3

	qcow2HeaderV2Len = 72
	qcow2HeaderV3Len = 104

	qcow2CryptNone = 0

	qcow2IncompatibleDirty       = 1 << 0
	qcow2IncompatibleCorrupt     = 1 << 1
	qcow2IncompatibleExternal    = 1 << 2
	qcow2IncompatibleCompression = 1 << 3
	qcow2IncompatibleExtendedL2  = 1 << 4

	qcow2RefcountOrder16 = 4

	// Bound resident refcount block data independently of virtual disk size.
	qcow2RefcountCacheBytes = 4 * 1024 * 1024

	qcow2OFlagZero       = uint64(1)
	qcow2OFlagCompressed = uint64(1) << 62
	qcow2OFlagCopied     = uint64(1) << 63

	qcow2OffsetMask         = uint64(0x00fffffffffffe00)
	qcow2RefcountOffsetMask = ^uint64(0x1ff)
	qcow2ReservedL2Mask     = uint64(0x3f000000000001fe)

	maxInt64 = int64(1<<63 - 1)
)

type qcow2Header struct {
	version               uint32
	backingFileOffset     uint64
	backingFileSize       uint32
	clusterBits           uint32
	size                  uint64
	cryptMethod           uint32
	l1Size                uint32
	l1TableOffset         uint64
	refcountTableOffset   uint64
	refcountTableClusters uint32
	nbSnapshots           uint32
	snapshotsOffset       uint64
	incompatibleFeatures  uint64
	autoclearFeatures     uint64
	refcountOrder         uint32
	headerLength          uint32
}

type qcow2RefcountBlock struct {
	offset uint64
	data   []byte
}

type qcow2Image struct {
	file *os.File

	size        uint64
	clusterSize uint64

	l1TableOffset uint64
	l1            []uint64

	refcountTableOffset  uint64
	refcountTable        []uint64
	refcountBlockEntries uint64

	refcountCache     map[uint64]int
	refcountBlocks    []qcow2RefcountBlock
	refcountCacheNext int

	l2Entries uint64
	l2Cache   map[uint64][]uint64

	mu sync.Mutex
}

func openQCOW2(file *os.File) (Image, error) {
	q := &qcow2Image{
		file:    file,
		l2Cache: make(map[uint64][]uint64),
	}

	if err := q.open(); err != nil {
		_ = file.Close()

		return nil, err
	}

	return q, nil
}

func (q *qcow2Image) open() error {
	h, err := q.readHeader()
	if err != nil {
		return err
	}

	q.size = h.size
	q.clusterSize = uint64(1) << h.clusterBits
	q.l1TableOffset = h.l1TableOffset
	q.refcountTableOffset = h.refcountTableOffset
	q.l2Entries = q.clusterSize / 8
	q.refcountBlockEntries = q.clusterSize / 2

	l1Size, err := checkedInt(uint64(h.l1Size))
	if err != nil {
		return err
	}

	q.l1, err = q.readUint64Table(h.l1TableOffset, l1Size)
	if err != nil {
		return fmt.Errorf("qcow2: read l1 table: %w", err)
	}

	refcountEntries := uint64(h.refcountTableClusters) * q.clusterSize / 8
	refcountLen, err := checkedInt(refcountEntries)
	if err != nil {
		return err
	}

	q.refcountTable, err = q.readUint64Table(h.refcountTableOffset, refcountLen)
	if err != nil {
		return fmt.Errorf("qcow2: read refcount table: %w", err)
	}

	return q.validateMappings()
}

func (q *qcow2Image) readHeader() (*qcow2Header, error) {
	var b [qcow2HeaderV3Len]byte

	if _, err := q.file.ReadAt(b[:qcow2HeaderV2Len], 0); err != nil {
		return nil, fmt.Errorf("qcow2: read header: %w", err)
	}

	if string(b[:4]) != qcow2Magic {
		return nil, errors.New("qcow2: invalid magic")
	}

	h := &qcow2Header{
		version:               binary.BigEndian.Uint32(b[4:8]),
		backingFileOffset:     binary.BigEndian.Uint64(b[8:16]),
		backingFileSize:       binary.BigEndian.Uint32(b[16:20]),
		clusterBits:           binary.BigEndian.Uint32(b[20:24]),
		size:                  binary.BigEndian.Uint64(b[24:32]),
		cryptMethod:           binary.BigEndian.Uint32(b[32:36]),
		l1Size:                binary.BigEndian.Uint32(b[36:40]),
		l1TableOffset:         binary.BigEndian.Uint64(b[40:48]),
		refcountTableOffset:   binary.BigEndian.Uint64(b[48:56]),
		refcountTableClusters: binary.BigEndian.Uint32(b[56:60]),
		nbSnapshots:           binary.BigEndian.Uint32(b[60:64]),
		snapshotsOffset:       binary.BigEndian.Uint64(b[64:72]),
		refcountOrder:         qcow2RefcountOrder16,
		headerLength:          qcow2HeaderV2Len,
	}

	switch h.version {
	case qcow2Version2:
	case qcow2Version3:
		if _, err := q.file.ReadAt(b[qcow2HeaderV2Len:qcow2HeaderV3Len], qcow2HeaderV2Len); err != nil {
			return nil, fmt.Errorf("qcow2: read v3 header: %w", err)
		}

		h.incompatibleFeatures = binary.BigEndian.Uint64(b[72:80])
		h.autoclearFeatures = binary.BigEndian.Uint64(b[88:96])
		h.refcountOrder = binary.BigEndian.Uint32(b[96:100])
		h.headerLength = binary.BigEndian.Uint32(b[100:104])
	default:
		return nil, fmt.Errorf("qcow2: unsupported version %d", h.version)
	}

	return h, validateHeader(h)
}

func validateHeader(h *qcow2Header) error {
	if h.backingFileOffset != 0 || h.backingFileSize != 0 {
		return errors.New("qcow2: backing files are unsupported")
	}

	if h.clusterBits < 9 || h.clusterBits > 21 {
		return fmt.Errorf("qcow2: unsupported cluster_bits %d", h.clusterBits)
	}

	if h.size > uint64(maxInt64) {
		return errors.New("qcow2: virtual disk is too large")
	}

	if h.cryptMethod != qcow2CryptNone {
		return errors.New("qcow2: encryption is unsupported")
	}

	if h.l1Size == 0 {
		return errors.New("qcow2: empty l1 table")
	}

	if h.l1TableOffset == 0 || h.refcountTableOffset == 0 {
		return errors.New("qcow2: missing metadata table")
	}

	if h.refcountTableClusters == 0 {
		return errors.New("qcow2: missing refcount table")
	}

	if h.nbSnapshots != 0 || h.snapshotsOffset != 0 {
		return errors.New("qcow2: internal snapshots are unsupported")
	}

	if h.incompatibleFeatures&qcow2IncompatibleDirty != 0 {
		return errors.New("qcow2: dirty images are unsupported")
	}

	if h.incompatibleFeatures&qcow2IncompatibleCorrupt != 0 {
		return errors.New("qcow2: corrupt images are unsupported")
	}

	if h.incompatibleFeatures&qcow2IncompatibleExternal != 0 {
		return errors.New("qcow2: external data files are unsupported")
	}

	if h.incompatibleFeatures&qcow2IncompatibleCompression != 0 {
		return errors.New("qcow2: non-default compression is unsupported")
	}

	if h.incompatibleFeatures&qcow2IncompatibleExtendedL2 != 0 {
		return errors.New("qcow2: extended l2 entries are unsupported")
	}

	knownIncompatible := uint64(qcow2IncompatibleDirty |
		qcow2IncompatibleCorrupt |
		qcow2IncompatibleExternal |
		qcow2IncompatibleCompression |
		qcow2IncompatibleExtendedL2)
	if h.incompatibleFeatures&^knownIncompatible != 0 {
		return fmt.Errorf("qcow2: unknown incompatible features %#x", h.incompatibleFeatures&^knownIncompatible)
	}

	if h.autoclearFeatures != 0 {
		return fmt.Errorf("qcow2: autoclear features are unsupported: %#x", h.autoclearFeatures)
	}

	if h.refcountOrder != qcow2RefcountOrder16 {
		return fmt.Errorf("qcow2: unsupported refcount_order %d", h.refcountOrder)
	}

	if h.version == qcow2Version3 &&
		(h.headerLength < qcow2HeaderV3Len || h.headerLength%8 != 0) {
		return fmt.Errorf("qcow2: invalid header_length %d", h.headerLength)
	}

	clusterSize := uint64(1) << h.clusterBits
	if h.l1TableOffset%clusterSize != 0 ||
		h.refcountTableOffset%clusterSize != 0 {
		return errors.New("qcow2: unaligned metadata table")
	}

	return nil
}

func (q *qcow2Image) ReadAt(b []byte, off int64) (int, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(b) == 0 {
		return 0, nil
	}

	if off < 0 {
		return 0, errors.New("qcow2: negative read offset")
	}

	if uint64(off) >= q.size {
		return 0, io.EOF
	}

	remaining := uint64(len(b))
	if remaining > q.size-uint64(off) {
		remaining = q.size - uint64(off)
	}

	done := uint64(0)
	for done < remaining {
		guestOff := uint64(off) + done
		inCluster := guestOff & (q.clusterSize - 1)
		chunk := minUint64(remaining-done, q.clusterSize-inCluster)

		hostOff, zero, err := q.hostOffset(guestOff)
		if err != nil {
			return int(done), err
		}

		buf := b[done : done+chunk]
		if zero {
			zeroBytes(buf)
		} else if _, err := q.file.ReadAt(buf, int64(hostOff)); err != nil {
			return int(done), err
		}

		done += chunk
	}

	if done < uint64(len(b)) {
		return int(done), io.EOF
	}

	return int(done), nil
}

func (q *qcow2Image) WriteAt(b []byte, off int64) (int, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(b) == 0 {
		return 0, nil
	}

	if off < 0 {
		return 0, errors.New("qcow2: negative write offset")
	}

	if uint64(off) >= q.size || uint64(len(b)) > q.size-uint64(off) {
		return 0, errors.New("qcow2: write exceeds virtual disk size")
	}

	done := uint64(0)
	for done < uint64(len(b)) {
		guestOff := uint64(off) + done
		inCluster := guestOff & (q.clusterSize - 1)
		chunk := minUint64(uint64(len(b))-done, q.clusterSize-inCluster)

		hostOff, err := q.ensureWritableCluster(guestOff)
		if err != nil {
			return int(done), err
		}

		buf := b[done : done+chunk]
		if _, err := q.file.WriteAt(buf, int64(hostOff)); err != nil {
			return int(done), err
		}

		done += chunk
	}

	return int(done), nil
}

func (q *qcow2Image) Size() int64 {
	return int64(q.size)
}

func (q *qcow2Image) Sync() error {
	return q.file.Sync()
}

func (q *qcow2Image) Close() error {
	return q.file.Close()
}

func (q *qcow2Image) hostOffset(guestOff uint64) (uint64, bool, error) {
	l2, l2Index, err := q.l2Table(guestOff)
	if err != nil {
		return 0, false, err
	}

	if l2 == nil {
		return 0, true, nil
	}

	entry := l2[l2Index]
	if entry == 0 {
		return 0, true, nil
	}

	if err := validateL2Entry(entry); err != nil {
		return 0, false, err
	}

	if entry&qcow2OFlagZero != 0 {
		return 0, true, nil
	}

	clusterOff := clusterOffset(entry)
	if clusterOff == 0 {
		return 0, true, nil
	}

	return clusterOff + (guestOff & (q.clusterSize - 1)), false, nil
}

func (q *qcow2Image) ensureWritableCluster(guestOff uint64) (uint64, error) {
	l1Index, l2Index, err := q.tableIndexes(guestOff)
	if err != nil {
		return 0, err
	}

	l2, err := q.ensureL2Table(l1Index)
	if err != nil {
		return 0, err
	}

	entry := l2[l2Index]
	if err := validateL2Entry(entry); err != nil {
		return 0, err
	}

	clusterOff := clusterOffset(entry)
	if entry != 0 && clusterOff != 0 {
		if err := q.prepareMappedCluster(l1Index, l2Index, entry, clusterOff); err != nil {
			return 0, err
		}

		return clusterOff + (guestOff & (q.clusterSize - 1)), nil
	}

	clusterOff, err = q.allocateCluster()
	if err != nil {
		return 0, err
	}

	if err := q.updateL2Entry(l1Index, l2Index, clusterOff|qcow2OFlagCopied); err != nil {
		return 0, err
	}

	return clusterOff + (guestOff & (q.clusterSize - 1)), nil
}

func (q *qcow2Image) prepareMappedCluster(l1Index, l2Index, entry, clusterOff uint64) error {
	refcount, err := q.refcount(clusterOff)
	if err != nil {
		return err
	}

	if refcount != 1 {
		return fmt.Errorf("qcow2: copy-on-write refcount %d is unsupported", refcount)
	}

	if entry&qcow2OFlagZero == 0 {
		return nil
	}

	if err := q.zeroCluster(clusterOff); err != nil {
		return err
	}

	return q.updateL2Entry(l1Index, l2Index, clusterOff|qcow2OFlagCopied)
}

func (q *qcow2Image) ensureL2Table(l1Index uint64) ([]uint64, error) {
	if l2, ok := q.l2Cache[l1Index]; ok {
		return l2, nil
	}

	if l1Index >= uint64(len(q.l1)) {
		return nil, errors.New("qcow2: l1 table is too small")
	}

	entry := q.l1[l1Index]
	if entry != 0 {
		l2Off := clusterOffset(entry)
		if l2Off == 0 {
			return nil, errors.New("qcow2: invalid l1 entry")
		}

		refcount, err := q.refcount(l2Off)
		if err != nil {
			return nil, err
		}

		if refcount != 1 {
			return nil, fmt.Errorf("qcow2: l2 copy-on-write refcount %d is unsupported", refcount)
		}

		return q.loadL2(l1Index)
	}

	l2Off, err := q.allocateCluster()
	if err != nil {
		return nil, err
	}

	l2Len, err := checkedInt(q.l2Entries)
	if err != nil {
		return nil, err
	}

	l2 := make([]uint64, l2Len)
	q.l2Cache[l1Index] = l2

	if err := q.updateL1Entry(l1Index, l2Off|qcow2OFlagCopied); err != nil {
		return nil, err
	}

	return l2, nil
}

func (q *qcow2Image) l2Table(guestOff uint64) ([]uint64, uint64, error) {
	l1Index, l2Index, err := q.tableIndexes(guestOff)
	if err != nil {
		return nil, 0, err
	}

	if l1Index >= uint64(len(q.l1)) {
		return nil, 0, errors.New("qcow2: l1 table is too small")
	}

	if clusterOffset(q.l1[l1Index]) == 0 {
		return nil, l2Index, nil
	}

	l2, err := q.loadL2(l1Index)
	if err != nil {
		return nil, 0, err
	}

	return l2, l2Index, nil
}

func (q *qcow2Image) tableIndexes(guestOff uint64) (uint64, uint64, error) {
	guestCluster := guestOff / q.clusterSize
	l1Index := guestCluster / q.l2Entries
	l2Index := guestCluster % q.l2Entries

	if l1Index >= uint64(len(q.l1)) {
		return 0, 0, errors.New("qcow2: l1 table is too small")
	}

	return l1Index, l2Index, nil
}

func (q *qcow2Image) loadL2(l1Index uint64) ([]uint64, error) {
	if l2, ok := q.l2Cache[l1Index]; ok {
		return l2, nil
	}

	l2Off := clusterOffset(q.l1[l1Index])
	if l2Off == 0 {
		return nil, errors.New("qcow2: missing l2 table")
	}

	if l2Off%q.clusterSize != 0 {
		return nil, errors.New("qcow2: unaligned l2 table")
	}

	l2Len, err := checkedInt(q.l2Entries)
	if err != nil {
		return nil, err
	}

	l2, err := q.readUint64Table(l2Off, l2Len)
	if err != nil {
		return nil, fmt.Errorf("qcow2: read l2 table: %w", err)
	}

	q.l2Cache[l1Index] = l2

	return l2, nil
}

func (q *qcow2Image) updateL1Entry(l1Index, entry uint64) error {
	var b [8]byte

	binary.BigEndian.PutUint64(b[:], entry)
	if _, err := q.file.WriteAt(b[:], int64(q.l1TableOffset+l1Index*8)); err != nil {
		return err
	}

	q.l1[l1Index] = entry

	return nil
}

func (q *qcow2Image) updateL2Entry(l1Index, l2Index, entry uint64) error {
	l2, err := q.loadL2(l1Index)
	if err != nil {
		return err
	}

	l2Off := clusterOffset(q.l1[l1Index])
	var b [8]byte

	binary.BigEndian.PutUint64(b[:], entry)
	if _, err := q.file.WriteAt(b[:], int64(l2Off+l2Index*8)); err != nil {
		return err
	}

	l2[l2Index] = entry

	return nil
}

// allocateCluster appends a zeroed cluster with its refcount already recorded.
// Missing refcount blocks are appended first and account for themselves.
func (q *qcow2Image) allocateCluster() (uint64, error) {
	info, err := q.file.Stat()
	if err != nil {
		return 0, err
	}
	if info.Size() < 0 {
		return 0, errors.New("qcow2: invalid file size")
	}
	oldSize := info.Size()
	offset := alignUp(uint64(oldSize), q.clusterSize)
	if err := q.validateAppendCapacity(offset); err != nil {
		return 0, err
	}
	for {
		_, _, exists, err := q.refcountLocation(offset)
		if err != nil {
			return 0, err
		}
		if exists {
			break
		}
		if err := q.appendRefcountBlock(offset, oldSize); err != nil {
			return 0, err
		}
		offset += q.clusterSize
		oldSize = int64(offset)
	}
	if err := q.file.Truncate(int64(offset + q.clusterSize)); err != nil {
		return 0, err
	}
	if err := q.setRefcount(offset, 1); err != nil {
		// A failed refcount write must not leave an untracked tail cluster.
		return 0, errors.Join(err, q.setRefcount(offset, 0), q.file.Truncate(oldSize))
	}

	return offset, nil
}

// validateAppendCapacity accounts for new refcount blocks as well as the data
// cluster before extending the file. Refcount-table expansion is unsupported.
func (q *qcow2Image) validateAppendCapacity(offset uint64) error {
	for {
		if offset > uint64(maxInt64)-q.clusterSize {
			return errors.New("qcow2: host image is too large")
		}
		_, _, exists, err := q.refcountLocation(offset)
		if err != nil {
			return err
		}
		if exists {
			return nil
		}
		if offset > uint64(maxInt64)-2*q.clusterSize {
			return errors.New("qcow2: host image is too large")
		}
		next := offset + q.clusterSize
		if next/q.clusterSize/q.refcountBlockEntries == offset/q.clusterSize/q.refcountBlockEntries {
			return nil
		}
		offset = next
	}
}

func (q *qcow2Image) appendRefcountBlock(offset uint64, oldSize int64) error {
	clusterIndex := offset / q.clusterSize
	tableIndex := clusterIndex / q.refcountBlockEntries
	blockIndex := clusterIndex % q.refcountBlockEntries
	block := make([]byte, int(q.clusterSize))
	binary.BigEndian.PutUint16(block[blockIndex*2:], 1)
	if _, err := q.file.WriteAt(block, int64(offset)); err != nil {
		return errors.Join(fmt.Errorf("qcow2: append refcount block: %w", err), q.file.Truncate(oldSize))
	}
	var entry [8]byte
	binary.BigEndian.PutUint64(entry[:], offset)
	tableOffset := int64(q.refcountTableOffset + tableIndex*8)
	if _, err := q.file.WriteAt(entry[:], tableOffset); err != nil {
		// Restore the unused table entry before removing its new block.
		clear(entry[:])
		_, restoreErr := q.file.WriteAt(entry[:], tableOffset)
		if restoreErr != nil {
			return errors.Join(err, restoreErr)
		}

		return errors.Join(err, q.file.Truncate(oldSize))
	}
	q.refcountTable[tableIndex] = offset
	q.cacheRefcountBlock(offset, block)

	return nil
}

func (q *qcow2Image) refcount(clusterOff uint64) (uint16, error) {
	refBlockOff, blockIndex, ok, err := q.refcountLocation(clusterOff)
	if err != nil || !ok {
		return 0, err
	}

	if index, ok := q.refcountCache[refBlockOff]; ok {
		return binary.BigEndian.Uint16(q.refcountBlocks[index].data[blockIndex*2:]), nil
	}

	block := make([]byte, int(q.clusterSize))
	if _, err := q.file.ReadAt(block, int64(refBlockOff)); err != nil {
		return 0, err
	}
	q.cacheRefcountBlock(refBlockOff, block)

	return binary.BigEndian.Uint16(block[blockIndex*2:]), nil
}

// cacheRefcountBlock uses FIFO replacement: refcounts are clustered, so one
// block read serves many validation checks and allocated-cluster overwrites.
// The image mutex protects the cache after the initial single-threaded open.
func (q *qcow2Image) cacheRefcountBlock(offset uint64, block []byte) {
	if q.refcountCache == nil {
		q.refcountCache = make(map[uint64]int)
	}
	if index, ok := q.refcountCache[offset]; ok {
		q.refcountBlocks[index].data = block

		return
	}

	limit := int(qcow2RefcountCacheBytes / q.clusterSize)
	index := len(q.refcountBlocks)
	entry := qcow2RefcountBlock{offset: offset, data: block}
	if index < limit {
		q.refcountBlocks = append(q.refcountBlocks, entry)
	} else {
		index = q.refcountCacheNext
		old := q.refcountBlocks[index]
		if old.data != nil {
			delete(q.refcountCache, old.offset)
		}
		q.refcountBlocks[index] = entry
		q.refcountCacheNext = (index + 1) % limit
	}
	q.refcountCache[offset] = index
}

func (q *qcow2Image) invalidateRefcountBlock(offset uint64) {
	if index, ok := q.refcountCache[offset]; ok {
		q.refcountBlocks[index].data = nil
		delete(q.refcountCache, offset)
	}
}

func (q *qcow2Image) setRefcount(clusterOff uint64, refcount uint16) error {
	refBlockOff, blockIndex, ok, err := q.refcountLocation(clusterOff)
	if err != nil {
		return err
	}

	if !ok {
		return errors.New("qcow2: missing refcount block")
	}

	var b [2]byte

	binary.BigEndian.PutUint16(b[:], refcount)
	n, err := q.file.WriteAt(b[:], int64(refBlockOff+blockIndex*2))
	if err == nil && n != len(b) {
		err = io.ErrShortWrite
	}
	if err != nil {
		// A partial metadata write may have changed the on-disk value. Do not
		// retain the old value while allocation rollback attempts to repair it.
		q.invalidateRefcountBlock(refBlockOff)

		return err
	}
	if index, ok := q.refcountCache[refBlockOff]; ok {
		binary.BigEndian.PutUint16(q.refcountBlocks[index].data[blockIndex*2:], refcount)
	}

	return nil
}

func (q *qcow2Image) refcountLocation(clusterOff uint64) (uint64, uint64, bool, error) {
	if clusterOff%q.clusterSize != 0 {
		return 0, 0, false, errors.New("qcow2: unaligned cluster")
	}

	clusterIndex := clusterOff / q.clusterSize
	tableIndex := clusterIndex / q.refcountBlockEntries
	blockIndex := clusterIndex % q.refcountBlockEntries

	if tableIndex >= uint64(len(q.refcountTable)) {
		return 0, 0, false, errors.New("qcow2: refcount table is too small")
	}

	entry := q.refcountTable[tableIndex]
	if entry == 0 {
		return 0, blockIndex, false, nil
	}

	refBlockOff := entry & qcow2RefcountOffsetMask
	if refBlockOff%q.clusterSize != 0 {
		return 0, 0, false, errors.New("qcow2: unaligned refcount block")
	}

	return refBlockOff, blockIndex, true, nil
}

func (q *qcow2Image) zeroCluster(clusterOff uint64) error {
	zeroLen, err := checkedInt(q.clusterSize)
	if err != nil {
		return err
	}

	_, err = q.file.WriteAt(make([]byte, zeroLen), int64(clusterOff))

	return err
}

func (q *qcow2Image) validateMappings() error {
	for l1Index, entry := range q.l1 {
		if entry == 0 {
			continue
		}

		l2Off := clusterOffset(entry)
		if l2Off == 0 {
			return errors.New("qcow2: invalid l1 entry")
		}

		if l2Off%q.clusterSize != 0 {
			return errors.New("qcow2: unaligned l2 table")
		}

		refcount, err := q.refcount(l2Off)
		if err != nil {
			return err
		}

		if refcount != 1 {
			return fmt.Errorf("qcow2: l2 copy-on-write refcount %d is unsupported", refcount)
		}

		l2, err := q.loadL2(uint64(l1Index))
		if err != nil {
			return err
		}

		for _, l2Entry := range l2 {
			if l2Entry == 0 {
				continue
			}

			if err := validateL2Entry(l2Entry); err != nil {
				return err
			}

			dataOff := clusterOffset(l2Entry)
			if dataOff == 0 {
				continue
			}

			if dataOff%q.clusterSize != 0 {
				return errors.New("qcow2: unaligned data cluster")
			}

			refcount, err := q.refcount(dataOff)
			if err != nil {
				return err
			}

			if refcount != 1 {
				return fmt.Errorf("qcow2: copy-on-write refcount %d is unsupported", refcount)
			}
		}
	}

	return nil
}

func validateL2Entry(entry uint64) error {
	if entry&qcow2OFlagCompressed != 0 {
		return errors.New("qcow2: compressed clusters are unsupported")
	}

	if entry&qcow2ReservedL2Mask != 0 {
		return fmt.Errorf("qcow2: reserved l2 bits set: %#x", entry&qcow2ReservedL2Mask)
	}

	return nil
}

func (q *qcow2Image) readUint64Table(off uint64, entries int) ([]uint64, error) {
	if entries < 0 || uint64(entries) > uint64(maxInt64)/8 {
		return nil, errors.New("qcow2: table is too large")
	}

	buf := make([]byte, entries*8)
	if _, err := q.file.ReadAt(buf, int64(off)); err != nil {
		return nil, err
	}

	table := make([]uint64, entries)
	for i := range table {
		table[i] = binary.BigEndian.Uint64(buf[i*8 : (i+1)*8])
	}

	return table, nil
}

func clusterOffset(entry uint64) uint64 {
	return entry & qcow2OffsetMask
}

func checkedInt(v uint64) (int, error) {
	if v > uint64(int(^uint(0)>>1)) {
		return 0, errors.New("qcow2: value is too large")
	}

	return int(v), nil
}

func zeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

func minUint64(a, b uint64) uint64 {
	if a < b {
		return a
	}

	return b
}

func alignUp(v, align uint64) uint64 {
	if v%align == 0 {
		return v
	}

	return v + align - v%align
}
