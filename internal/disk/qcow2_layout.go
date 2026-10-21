//nolint:err113 // Precise format errors identify malformed image metadata.
package disk

import (
	"errors"
	"fmt"
)

const (
	qcow2OwnershipPageClusters = 4096
	// Bound temporary validation memory even for extremely fragmented sparse
	// files. A dense 1 TiB image with 64 KiB clusters needs only 2 MiB of bits.
	qcow2MaxOwnershipPages = (64 << 20) / (qcow2OwnershipPageClusters / 8)
)

type qcow2ClusterOwners map[uint64]*[qcow2OwnershipPageClusters / 64]uint64

// claim rejects every second reference to a host cluster. This backend does not
// support shared clusters, and stored refcounts alone cannot establish that two
// guest mappings or a data and metadata mapping are actually distinct.
func (owners qcow2ClusterOwners) claim(offset, length, clusterSize uint64, kind string) error {
	first := offset / clusterSize
	count := (length + clusterSize - 1) / clusterSize
	for cluster := first; cluster < first+count; cluster++ {
		pageIndex := cluster / qcow2OwnershipPageClusters
		page := owners[pageIndex]
		if page == nil {
			if len(owners) >= qcow2MaxOwnershipPages {
				return errors.New("qcow2: fragmented image exceeds validation memory limit")
			}
			page = new([qcow2OwnershipPageClusters / 64]uint64)
			owners[pageIndex] = page
		}
		bit := cluster % qcow2OwnershipPageClusters
		mask := uint64(1) << (bit % 64)
		if page[bit/64]&mask != 0 {
			return fmt.Errorf("qcow2: overlapping %s at host offset %#x", kind, cluster*clusterSize)
		}
		page[bit/64] |= mask
	}

	return nil
}

// validateLayout checks all metadata ownership before interpreting data mappings.
// Temporary ownership bits and L2 buffers are discarded after opening; malformed
// images cannot make validation retain one cached table per claimed guest region.
func (q *qcow2Image) validateLayout(h *qcow2Header) error {
	info, err := q.file.Stat()
	if err != nil {
		return err
	}
	fileSize := uint64(info.Size())
	owners := make(qcow2ClusterOwners)
	metadata := []struct {
		offset, length uint64
		name           string
	}{
		{0, q.clusterSize, "header"},
		{q.l1TableOffset, uint64(h.l1Size) * 8, "l1 table"},
		{q.refcountTableOffset, uint64(h.refcountTableClusters) * q.clusterSize, "refcount table"},
	}
	for _, region := range metadata {
		if err := qcow2FileRange(fileSize, region.offset, region.length, region.name); err != nil {
			return err
		}
		if err := owners.claim(region.offset, region.length, q.clusterSize, region.name); err != nil {
			return err
		}
	}
	if err := q.claimRefcountBlocks(owners, fileSize); err != nil {
		return err
	}
	if err := q.claimL2Tables(owners, fileSize); err != nil {
		return err
	}
	for _, region := range metadata {
		if err := q.checkRefcounts(region.offset, region.length); err != nil {
			return err
		}
	}
	for _, entry := range q.refcountTable {
		if entry != 0 {
			if err := q.checkRefcounts(entry, q.clusterSize); err != nil {
				return err
			}
		}
	}
	guestClusters := (q.size + q.clusterSize - 1) / q.clusterSize
	// Mapped data and L2 tables are overwritten in place: shared clusters are
	// rejected below. Only missing mappings need room in the append allocator.
	missing := guestClusters + (guestClusters+q.l2Entries-1)/q.l2Entries
	for index, entry := range q.l1 {
		if entry == 0 {
			continue
		}
		offset := clusterOffset(entry)
		if err := q.checkRefcounts(offset, q.clusterSize); err != nil {
			return err
		}
		l2, err := q.readUint64Table(offset, int(q.l2Entries))
		if err != nil {
			return err
		}
		if err := q.validateDataMappings(owners, fileSize, l2); err != nil {
			return err
		}
		first := uint64(index) * q.l2Entries
		if first < guestClusters {
			missing-- // This guest range already has an L2 table.
			for _, mapping := range l2[:min(q.l2Entries, guestClusters-first)] {
				if clusterOffset(mapping) != 0 {
					missing-- // Includes preallocated zero clusters.
				}
			}
		}
	}

	return q.validateGrowthCapacity(fileSize, missing)
}

func (q *qcow2Image) claimRefcountBlocks(owners qcow2ClusterOwners, fileSize uint64) error {
	for _, entry := range q.refcountTable {
		if entry == 0 {
			continue
		}
		if entry&^qcow2RefcountOffsetMask != 0 || entry%q.clusterSize != 0 {
			return errors.New("qcow2: invalid refcount block offset or reserved bits")
		}
		if err := qcow2FileRange(fileSize, entry, q.clusterSize, "refcount block"); err != nil {
			return err
		}
		if err := owners.claim(entry, q.clusterSize, q.clusterSize, "refcount block"); err != nil {
			return err
		}
	}

	return nil
}

func (q *qcow2Image) claimL2Tables(owners qcow2ClusterOwners, fileSize uint64) error {
	for _, entry := range q.l1 {
		if entry == 0 {
			continue
		}
		if entry & ^(qcow2OffsetMask|qcow2OFlagCopied) != 0 {
			return errors.New("qcow2: reserved l1 bits set")
		}
		offset := clusterOffset(entry)
		if offset == 0 || offset%q.clusterSize != 0 {
			return errors.New("qcow2: invalid l2 table offset")
		}
		if err := qcow2FileRange(fileSize, offset, q.clusterSize, "l2 table"); err != nil {
			return err
		}
		if err := owners.claim(offset, q.clusterSize, q.clusterSize, "l2 table"); err != nil {
			return err
		}
	}

	return nil
}

func (q *qcow2Image) checkRefcounts(offset, length uint64) error {
	end := offset + length
	for cluster := offset; cluster < end; cluster += q.clusterSize {
		refcount, err := q.refcount(cluster)
		if err != nil {
			return err
		}
		if refcount != 1 {
			return fmt.Errorf("qcow2: host cluster %#x has refcount %d, expected 1", cluster, refcount)
		}
	}

	return nil
}

func (q *qcow2Image) validateDataMappings(owners qcow2ClusterOwners, fileSize uint64, l2 []uint64) error {
	for _, entry := range l2 {
		if err := q.validateL2Entry(entry); err != nil {
			return err
		}
		offset := clusterOffset(entry)
		if offset == 0 {
			continue
		}
		if offset%q.clusterSize != 0 {
			return errors.New("qcow2: unaligned data cluster")
		}
		if err := qcow2FileRange(fileSize, offset, q.clusterSize, "data cluster"); err != nil {
			return err
		}
		if err := owners.claim(offset, q.clusterSize, q.clusterSize, "data cluster"); err != nil {
			return err
		}
		if err := q.checkRefcounts(offset, q.clusterSize); err != nil {
			return err
		}
	}

	return nil
}

func qcow2FileRange(fileSize, offset, length uint64, kind string) error {
	if offset > fileSize || length > fileSize-offset {
		return fmt.Errorf("qcow2: %s exceeds image file bounds", kind)
	}

	return nil
}
