// SPDX-License-Identifier: Apache-2.0
// Adapted from lima-vm/go-qcow2reader; see README.md for origin and changes.
//
//nolint:err113 // Decode failures identify malformed image streams.
package qcow2import

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"github.com/klauspost/compress/flate"
	"github.com/klauspost/compress/zstd"
)

func (r *Reader) decodeCluster(entry l2TableEntry) ([]byte, error) {
	if r.compressedCache != nil && r.compressedCacheEntry == entry {
		return r.compressedCache, nil
	}
	offset, length := entry.compressedRange(r.clusterBits)
	encoded, err := r.readBytes(offset, length)
	if err != nil {
		return nil, err
	}
	decoded := make([]byte, int(r.clusterSize))
	if r.compression == 0 {
		err = decodeDeflate(encoded, decoded)
	} else {
		decoded, err = r.decodeZstd(encoded, decoded)
	}
	if err != nil {
		return nil, fmt.Errorf("decode compressed cluster at %#x: %w", offset, err)
	}
	r.compressedCacheEntry = entry
	r.compressedCache = decoded

	return decoded, nil
}

func decodeDeflate(encoded, decoded []byte) error {
	decoder := flate.NewReader(bytes.NewReader(encoded))
	_, err := io.ReadFull(decoder, decoded)
	if err == nil {
		// Force stream completion, detecting truncated end markers and
		// expansion beyond the one-cluster output limit.
		var extra [1]byte
		n, tailErr := decoder.Read(extra[:])
		if n != 0 || tailErr != io.EOF {
			err = errors.New("DEFLATE stream does not end at one cluster")
		}
	}
	closeErr := decoder.Close()
	if err == nil {
		err = closeErr
	}

	return err
}

func (r *Reader) decodeZstd(encoded, decoded []byte) ([]byte, error) {
	frame, err := singleZstdFrame(encoded, r.clusterSize)
	if err != nil {
		return nil, err
	}
	if r.zstdDecoder == nil {
		r.zstdDecoder, err = zstd.NewReader(nil, zstd.WithDecoderConcurrency(1), zstd.WithDecoderLowmem(true),
			zstd.WithDecoderMaxMemory(8<<20), zstd.WithDecoderMaxWindow(8<<20), zstd.WithDecodeAllCapLimit(true))
		if err != nil {
			return nil, err
		}
	}
	decoded, err = r.zstdDecoder.DecodeAll(frame, decoded[:0])
	if err != nil {
		return nil, err
	}
	if uint64(len(decoded)) != r.clusterSize {
		return nil, errors.New("zstd output is not one cluster")
	}

	return decoded, nil
}

// QCOW2 describes compressed storage in sectors: bytes following the frame may
// be padding or another cluster's stream. Decode exactly one frame so Zstd can
// verify its checksum without interpreting those bytes as concatenated frames.
func singleZstdFrame(encoded []byte, clusterSize uint64) ([]byte, error) {
	var header zstd.Header
	if err := header.Decode(encoded); err != nil {
		return nil, err
	}
	if header.Skippable || header.DictionaryID != 0 || header.WindowSize > 8<<20 ||
		(header.HasFCS && header.FrameContentSize != clusterSize) {
		return nil, errors.New("unsupported Zstd frame or incorrect cluster size")
	}
	position := header.HeaderSize
	for {
		if position > len(encoded)-3 {
			return nil, io.ErrUnexpectedEOF
		}
		block := uint32(encoded[position]) | uint32(encoded[position+1])<<8 | uint32(encoded[position+2])<<16
		position += 3
		size := int(block >> 3)
		switch (block >> 1) & 3 {
		case 1:
			size = 1 // RLE stores just the repeated byte.
		case 3:
			return nil, errors.New("reserved Zstd block type")
		}
		if size > len(encoded)-position {
			return nil, io.ErrUnexpectedEOF
		}
		position += size
		if block&1 != 0 {
			break
		}
	}
	if header.HasCheckSum {
		if position > len(encoded)-4 {
			return nil, io.ErrUnexpectedEOF
		}
		position += 4
	}

	return encoded[:position], nil
}
