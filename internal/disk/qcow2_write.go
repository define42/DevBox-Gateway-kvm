package disk

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

func (q *qcow2Image) writeMetadata(data []byte, offset uint64) error {
	writer := q.metadataWriter
	if writer == nil {
		writer = q.file
	}
	n, err := writer.WriteAt(data, int64(offset))
	if err == nil && n != len(data) {
		return io.ErrShortWrite
	}

	return err
}

// Restore the previous pointer even after a short write: the on-disk entry
// must be known before its target can be reclaimed or cached metadata reused.
func (q *qcow2Image) writeMetadataEntry(offset, previous, next uint64) error {
	var data [8]byte
	binary.BigEndian.PutUint64(data[:], next)
	if err := q.writeMetadata(data[:], offset); err != nil {
		binary.BigEndian.PutUint64(data[:], previous)
		if restoreErr := q.writeMetadata(data[:], offset); restoreErr != nil {
			return q.disableWrites(errors.Join(err, restoreErr))
		}

		return err
	}

	return nil
}

func (q *qcow2Image) disableWrites(err error) error {
	q.writeFault = fmt.Errorf("qcow2: metadata recovery failed; further writes disabled: %w", err)

	return q.writeFault
}

// A newly appended cluster is reclaimed only after its pointer was restored.
// If recovery fails, keep the target and reject future writes on this handle.
func (q *qcow2Image) rollbackNewCluster(offset uint64, writeErr error) error {
	if q.writeFault != nil {
		return writeErr
	}
	if err := q.setRefcount(offset, 0); err != nil {
		return q.disableWrites(errors.Join(writeErr, err))
	}
	if err := q.file.Truncate(int64(offset)); err != nil {
		return q.disableWrites(errors.Join(writeErr, err))
	}

	return writeErr
}
