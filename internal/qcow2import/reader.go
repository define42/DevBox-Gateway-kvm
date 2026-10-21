// SPDX-License-Identifier: Apache-2.0
// Adapted from lima-vm/go-qcow2reader; see README.md for origin and changes.
// Package qcow2import reads standalone QCOW2 images for one-time conversion.
//
//nolint:err113 // Format errors identify malformed untrusted image metadata.
package qcow2import

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/klauspost/compress/zstd"
)

const (
	offsetMask       = uint64(0x00fffffffffffe00)
	copiedFlag       = uint64(1) << 63
	compressedFlag   = uint64(1) << 62
	maxVirtualSize   = uint64(1) << 40
	maxL1Bytes       = uint64(32) << 20
	maxRefcountBytes = uint64(8) << 20
	maxMetadataBytes = uint64(128) << 20
)

// Extent describes a contiguous guest byte range with the same allocation state.
type Extent struct {
	Start      int64
	Length     int64
	Allocated  bool
	Zero       bool
	Compressed bool
}

// These descriptor types and bit extraction methods are adapted from Lima's
// image/qcow2/qcow2.go. Compressed sizes include the partial first sector.
type l1TableEntry uint64

func (x l1TableEntry) l2Offset() uint64 { return uint64(x) & offsetMask }

type l2TableEntry uint64

func (x l2TableEntry) compressed() bool          { return uint64(x)&compressedFlag != 0 }
func (x l2TableEntry) hostClusterOffset() uint64 { return uint64(x) & offsetMask }
func (x l2TableEntry) compressedRange(clusterBits uint32) (uint64, uint64) {
	shift := 62 - (clusterBits - 8)
	offset := uint64(x) & ((uint64(1) << shift) - 1)
	sectors := ((uint64(x) &^ compressedFlag) >> shift) + 1

	return offset, sectors*512 - offset%512
}

// Reader supports concurrent ReadAt and Extent calls. The input must remain
// unchanged while the reader is in use. The caller owns and closes the file.
type Reader struct {
	file                 *os.File
	ctx                  context.Context //nolint:containedctx // Reader and context share one bounded import lifetime.
	fileSize             uint64
	size                 uint64
	version              uint32
	clusterBits          uint32
	clusterSize          uint64
	l2Entries            uint64
	extended             bool
	compression          byte
	refcountOrder        uint32
	l1                   []l1TableEntry
	refcounts            []uint64
	metadata             []metadataRange
	metadataBytes        uint64
	mu                   sync.Mutex
	l2CacheOffset        uint64
	l2Cache              []byte
	refCacheOffset       uint64
	refCache             []byte
	compressedCacheEntry l2TableEntry
	compressedCache      []byte
	zstdDecoder          *zstd.Decoder
}

// Open validates the image and opens a reader without taking file ownership.
func Open(file *os.File) (*Reader, error) { return OpenContext(context.Background(), file) }

// OpenContext also permits cancellation of metadata validation and reads.
func OpenContext(ctx context.Context, file *os.File) (*Reader, error) {
	if file == nil {
		return nil, errors.New("qcow2: nil source file")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() < 72 {
		return nil, errors.New("qcow2: source must be a regular image file")
	}
	r := &Reader{file: file, ctx: ctx, fileSize: uint64(info.Size())}
	if err := r.open(); err != nil {
		return nil, fmt.Errorf("qcow2 import: %w", err)
	}

	return r, nil
}

// Size returns the virtual disk size in bytes.
func (r *Reader) Size() int64 { return int64(r.size) }

// Close releases cached buffers. It never closes the caller-owned source file.
func (r *Reader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.l2Cache = nil
	r.refCache = nil
	r.compressedCache = nil
	if r.zstdDecoder != nil {
		r.zstdDecoder.Close()
		r.zstdDecoder = nil
	}

	return nil
}

func (r *Reader) readBytes(offset, length uint64) ([]byte, error) {
	if err := r.ctx.Err(); err != nil {
		return nil, err
	}
	if offset > r.fileSize || length > r.fileSize-offset || length > maxMetadataBytes {
		return nil, errors.New("image range exceeds file or allocation bounds")
	}
	p := make([]byte, int(length))
	_, err := r.file.ReadAt(p, int64(offset))

	return p, err
}

func (r *Reader) open() error {
	h, err := r.readBytes(0, 72)
	if err != nil {
		return err
	}
	if err := r.parseFixedHeader(h); err != nil {
		return err
	}
	h, err = r.readBytes(0, r.clusterSize)
	if err != nil {
		return err
	}
	headerLength, autoclear, err := r.parseVersionHeader(h)
	if err != nil {
		return err
	}
	r.l2Entries = r.clusterSize / 8
	if r.extended {
		r.l2Entries /= 2
	}
	l1Bytes := uint64(binary.BigEndian.Uint32(h[36:40])) * 8
	if l1Bytes == 0 || l1Bytes > maxL1Bytes || l1Bytes/8 < ceilDiv(r.size, r.clusterSize*r.l2Entries) {
		return errors.New("invalid or oversized L1 table")
	}
	l1Offset := binary.BigEndian.Uint64(h[40:48])
	refOffset := binary.BigEndian.Uint64(h[48:56])
	refBytes := uint64(binary.BigEndian.Uint32(h[56:60])) * r.clusterSize
	if refBytes == 0 || refBytes > maxRefcountBytes {
		return errors.New("invalid or oversized refcount table")
	}
	if err = r.addMetadata(0, r.clusterSize, "header"); err != nil {
		return err
	}
	if err = r.addMetadata(l1Offset, l1Bytes, "L1 table"); err != nil {
		return err
	}
	if err = r.addMetadata(refOffset, refBytes, "refcount table"); err != nil {
		return err
	}
	if err = r.readExtensions(h[headerLength:], autoclear); err != nil {
		return err
	}
	l1Data, err := r.readBytes(l1Offset, l1Bytes)
	if err != nil {
		return err
	}
	r.l1 = make([]l1TableEntry, len(l1Data)/8)
	for i := range r.l1 {
		r.l1[i] = l1TableEntry(binary.BigEndian.Uint64(l1Data[i*8:]))
	}
	refs, err := r.readBytes(refOffset, refBytes)
	if err != nil {
		return err
	}
	r.refcounts = make([]uint64, len(refs)/8)
	for i := range r.refcounts {
		offset := binary.BigEndian.Uint64(refs[i*8:])
		r.refcounts[i] = offset
		if offset != 0 {
			if err = r.addMetadata(offset, r.clusterSize, "refcount block"); err != nil {
				return err
			}
		}
	}

	return r.validateLayout()
}

func (r *Reader) parseFixedHeader(h []byte) error {
	if string(h[:4]) != "QFI\xfb" {
		return errors.New("not a QCOW2 image")
	}
	r.version = binary.BigEndian.Uint32(h[4:8])
	if r.version != 2 && r.version != 3 {
		return errors.New("unsupported QCOW2 version")
	}
	if binary.BigEndian.Uint64(h[8:16]) != 0 || binary.BigEndian.Uint32(h[16:20]) != 0 {
		return errors.New("backing files are unsupported")
	}
	r.clusterBits = binary.BigEndian.Uint32(h[20:24])
	if r.clusterBits < 9 || r.clusterBits > 21 {
		return errors.New("invalid cluster size")
	}
	r.clusterSize = uint64(1) << r.clusterBits
	r.size = binary.BigEndian.Uint64(h[24:32])
	if r.size == 0 || r.size > maxVirtualSize {
		return errors.New("virtual size must be between 1 byte and 1 TiB")
	}
	if binary.BigEndian.Uint32(h[32:36]) != 0 {
		return errors.New("encryption is unsupported")
	}
	if binary.BigEndian.Uint32(h[60:64]) != 0 || binary.BigEndian.Uint64(h[64:72]) != 0 {
		return errors.New("snapshots are unsupported")
	}

	return nil
}

func (r *Reader) parseVersionHeader(h []byte) (uint64, uint64, error) {
	headerLength := uint64(72)
	r.refcountOrder = 4
	var autoclear uint64
	if r.version == 2 {
		return headerLength, autoclear, nil
	}
	incompatible := binary.BigEndian.Uint64(h[72:80])
	if incompatible & ^uint64((1<<3)|(1<<4)) != 0 {
		return 0, 0, errors.New("dirty, corrupt, external data or unknown incompatible feature")
	}
	autoclear = binary.BigEndian.Uint64(h[88:96])
	if autoclear & ^uint64(1) != 0 {
		return 0, 0, errors.New("external data or unknown autoclear feature")
	}
	r.refcountOrder = binary.BigEndian.Uint32(h[96:100])
	if r.refcountOrder > 6 {
		return 0, 0, errors.New("invalid refcount width")
	}
	headerLength = uint64(binary.BigEndian.Uint32(h[100:104]))
	if headerLength < 104 || headerLength%8 != 0 || headerLength > r.clusterSize-8 {
		return 0, 0, errors.New("invalid header length")
	}
	if headerLength > 104 {
		r.compression = h[104]
		for _, v := range h[105:112] {
			if v != 0 {
				return 0, 0, errors.New("nonzero header padding")
			}
		}
	}
	if (incompatible&(1<<3) != 0) != (r.compression != 0) || r.compression > 1 {
		return 0, 0, errors.New("unsupported or inconsistent compression type")
	}
	r.extended = incompatible&(1<<4) != 0
	if r.extended && r.clusterBits < 14 {
		return 0, 0, errors.New("extended L2 requires clusters of at least 16 KiB")
	}

	return headerLength, autoclear, nil
}

func ceilDiv(n, d uint64) uint64 { return n/d + boolUint(n%d != 0) }
func boolUint(b bool) uint64 {
	if b {
		return 1
	}

	return 0
}

func (r *Reader) l2Entry(off uint64) (l2TableEntry, uint64, error) {
	cluster := off / r.clusterSize
	l1 := r.l1[cluster/r.l2Entries]
	if l1.l2Offset() == 0 {
		return 0, 0, nil
	}
	if r.l2Cache == nil || r.l2CacheOffset != l1.l2Offset() {
		p, err := r.readBytes(l1.l2Offset(), r.clusterSize)
		if err != nil {
			return 0, 0, err
		}
		r.l2Cache = p
		r.l2CacheOffset = l1.l2Offset()
	}
	index := (cluster % r.l2Entries) * 8
	if r.extended {
		index *= 2
	}
	entry := l2TableEntry(binary.BigEndian.Uint64(r.l2Cache[index:]))
	var bitmap uint64
	if r.extended {
		bitmap = binary.BigEndian.Uint64(r.l2Cache[index+8:])
	}

	return entry, bitmap, nil
}

func (r *Reader) status(off uint64) (Extent, l2TableEntry, error) {
	coverage := r.clusterSize * r.l2Entries
	if r.l1[off/coverage].l2Offset() == 0 {
		// Skip whole absent L2 tables so a sparse TiB image does not require
		// billions of individual cluster lookups.
		return Extent{Start: int64(off), Length: int64(min(coverage-off%coverage, r.size-off)), Zero: true}, 0, nil
	}
	entry, bitmap, err := r.l2Entry(off)
	if err != nil {
		return Extent{}, 0, err
	}
	length := r.clusterSize - off%r.clusterSize
	ext := Extent{
		Start: int64(off), Length: int64(length),
		Allocated:  entry.hostClusterOffset() != 0,
		Zero:       entry.hostClusterOffset() == 0 || uint64(entry)&1 != 0,
		Compressed: entry.compressed(),
	}
	if entry.compressed() {
		ext.Allocated = true
		ext.Zero = false
	} else if r.extended {
		subSize := r.clusterSize / 32
		bit := (off % r.clusterSize) / subSize
		ext.Length = int64(subSize - off%subSize)
		ext.Allocated = uint32(bitmap)&(uint32(1)<<bit) != 0
		ext.Zero = !ext.Allocated
	}
	ext.Length = min(ext.Length, int64(r.size-off))

	return ext, entry, nil
}

// Extent returns the next range with a uniform allocation state, clipped to
// length. Unallocated standalone-image bytes always have Zero set.
func (r *Reader) Extent(start, length int64) (Extent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if start < 0 || length < 0 || uint64(start) > r.size || uint64(length) > r.size-uint64(start) {
		return Extent{}, errors.New("qcow2: extent out of bounds")
	}
	current := Extent{Start: start}
	for current.Length < length {
		if err := r.ctx.Err(); err != nil {
			return Extent{}, err
		}
		next, _, err := r.status(uint64(start + current.Length))
		if err != nil {
			return Extent{}, err
		}
		next.Length = min(next.Length, length-current.Length)
		if current.Length == 0 {
			current = next

			continue
		}
		if current.Allocated != next.Allocated || current.Zero != next.Zero || current.Compressed != next.Compressed {
			break
		}
		current.Length += next.Length
	}

	return current, nil
}

// ReadAt implements io.ReaderAt, including partial reads at the virtual EOF.
func (r *Reader) ReadAt(p []byte, off int64) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if off < 0 {
		return 0, errors.New("qcow2: negative read offset")
	}
	if len(p) == 0 {
		return 0, nil
	}
	if uint64(off) >= r.size {
		return 0, io.EOF
	}
	wanted := len(p)
	if uint64(wanted) > r.size-uint64(off) {
		p = p[:int(r.size-uint64(off))]
	}
	n := 0
	for n < len(p) {
		if err := r.ctx.Err(); err != nil {
			return n, err
		}
		position := uint64(off) + uint64(n)
		ext, entry, err := r.status(position)
		if err != nil {
			return n, err
		}
		count := min(len(p)-n, int(ext.Length))
		dst := p[n : n+count]
		switch {
		case ext.Zero:
			clear(dst)
		case ext.Compressed:
			decoded, err := r.decodeCluster(entry)
			if err != nil {
				return n, err
			}
			copy(dst, decoded[position%r.clusterSize:])
		default:
			got, err := r.file.ReadAt(dst, int64(entry.hostClusterOffset()+position%r.clusterSize))
			if err != nil {
				return n + got, err
			}
		}
		n += count
	}
	if n < wanted {
		return n, io.EOF
	}

	return n, nil
}
