package disk

import (
	"errors"
	"io"
	"os"
	"slices"
)

// VectorReader is implemented by images that fill several buffers from
// consecutive disk bytes with fewer system calls than one ReadAt per buffer.
// ReadvAt behaves like ReadAt into the buffers' concatenation, and may run
// concurrently with ReadAt and other ReadvAt calls.
type VectorReader interface {
	ReadvAt(buffers [][]byte, off int64) (int, error)
}

var errNegativeReadOffset = errors.New("disk: negative read offset")

// readvFile reads consecutive file bytes into buffers like os.File.ReadAt: it
// returns io.EOF when the file ends first.
func readvFile(file *os.File, buffers [][]byte, off int64) (int, error) {
	if off < 0 {
		return 0, errNegativeReadOffset
	}
	done := 0
	for {
		for len(buffers) > 0 && len(buffers[0]) == 0 {
			buffers = buffers[1:]
		}
		if len(buffers) == 0 {
			return done, nil
		}
		n, err := preadv(file, buffers, off)
		done += n
		if err != nil {
			return done, err
		}
		if n == 0 {
			return done, io.EOF
		}
		off += int64(n)
		// Consuming whole buffers only changes our slice header. Clone the
		// list only when a short read stops inside a buffer, so trimming that
		// entry cannot change the caller's list.
		for len(buffers) > 0 && n >= len(buffers[0]) {
			n -= len(buffers[0])
			buffers = buffers[1:]
		}
		if n > 0 {
			buffers = slices.Clone(buffers)
			buffers[0] = buffers[0][n:]
		}
	}
}

// vectorCursor hands out consecutive byte ranges of a buffer list without
// modifying it. total is the list's length in bytes.
type vectorCursor struct {
	buffers       [][]byte
	total         uint64
	index, offset int
	iov           [][]byte
}

// take returns the next n bytes, which the buffers must hold. A range covering
// every buffer is the list itself; others reuse c.iov's storage.
func (c *vectorCursor) take(n uint64) [][]byte {
	if c.index == 0 && c.offset == 0 && n == c.total {
		c.index = len(c.buffers)

		return c.buffers
	}
	c.iov = c.iov[:0]
	for n > 0 {
		b := c.buffers[c.index][c.offset:]
		step := min(n, uint64(len(b)))
		if step > 0 {
			c.iov = append(c.iov, b[:step])
		}
		n -= step
		c.offset += int(step)
		if c.offset == len(c.buffers[c.index]) {
			c.index, c.offset = c.index+1, 0
		}
	}

	return c.iov
}
