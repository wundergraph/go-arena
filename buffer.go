// SPDX-License-Identifier: Apache-2.0

package arena

import (
	"io"
)

// Buffer is a bytes.Buffer-like struct backed by an arena.
// It implements io.Writer, io.ReaderFrom and provides similar methods to bytes.Buffer.
// All memory allocation is done through the provided arena.
type Buffer struct {
	arena   Arena
	buf     []byte
	off     int    // read offset
	readBuf []byte // intermediate buffer for ReadFrom
}

// NewArenaBuffer creates a new Buffer backed by the given arena.
// If arena is nil, it will fall back to standard Go allocation.
func NewArenaBuffer(arena Arena) *Buffer {
	return &Buffer{
		arena:   arena,
		buf:     nil,
		off:     0,
		readBuf: nil,
	}
}

// Write implements io.Writer interface.
// It writes len(p) bytes from p to the buffer.
func (b *Buffer) Write(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}

	b.buf = SliceAppend(b.arena, b.buf, p...)
	b.off = len(b.buf)

	return len(p), nil
}

// WriteByte writes a single byte to the buffer.
func (b *Buffer) WriteByte(c byte) error {
	b.buf = SliceAppend(b.arena, b.buf, c)
	b.off = len(b.buf)
	return nil
}

// WriteString writes a string to the buffer.
func (b *Buffer) WriteString(s string) (n int, err error) {
	if len(s) == 0 {
		return 0, nil
	}

	b.buf = SliceAppend(b.arena, b.buf, []byte(s)...)
	b.off = len(b.buf)

	return len(s), nil
}

func (b *Buffer) WriteTo(w io.Writer) (n int64, err error) {
	if b.off == 0 {
		return 0, nil
	}

	m, err := w.Write(b.buf[:b.off])
	if m > 0 {
		n += int64(m)
		// Remove written bytes by shifting remaining data
		copy(b.buf, b.buf[m:b.off])
		b.off -= m
	}

	return n, err
}

// Read reads up to len(p) bytes from the buffer into p.
// It returns the number of bytes read and any error encountered.
func (b *Buffer) Read(p []byte) (n int, err error) {
	if b.off == 0 {
		return 0, io.EOF
	}

	n = copy(p, b.buf[:b.off])
	if n < len(p) {
		err = io.EOF
	}

	// Remove read bytes by shifting remaining data
	copy(b.buf, b.buf[n:b.off])
	b.off -= n

	return n, err
}

// ReadByte reads and returns the next byte from the buffer.
// If no byte is available, it returns an error.
func (b *Buffer) ReadByte() (byte, error) {
	if b.off == 0 {
		return 0, io.EOF
	}

	c := b.buf[0]
	copy(b.buf, b.buf[1:b.off])
	b.off--

	return c, nil
}

// Bytes returns a slice of length b.Len() holding the unread portion of the buffer.
// The slice is valid for use only until the next buffer modification.
func (b *Buffer) Bytes() []byte {
	if b.off == 0 {
		return []byte{}
	}
	return b.buf[:b.off]
}

// String returns the contents of the unread portion of the buffer as a string.
func (b *Buffer) String() string {
	return string(b.buf[:b.off])
}

// Len returns the number of bytes of the unread portion of the buffer.
func (b *Buffer) Len() int {
	return b.off
}

// Cap returns the capacity of the buffer's underlying byte slice.
func (b *Buffer) Cap() int {
	return cap(b.buf)
}

// Reset resets the buffer to be empty.
func (b *Buffer) Reset() {
	b.off = 0
	if b.buf != nil {
		b.buf = b.buf[:0]
	}
}

// Truncate discards all but the first n unread bytes from the buffer.
// It panics if n is negative or greater than the length of the buffer.
func (b *Buffer) Truncate(n int) {
	if n < 0 || n > b.off {
		panic("arena: truncation out of range")
	}
	b.off = n
}

// Next returns a slice containing the next n bytes from the buffer,
// advancing the buffer as if the bytes had been returned by Read.
func (b *Buffer) Next(n int) []byte {
	if n <= 0 {
		return []byte{}
	}

	if n > b.off {
		n = b.off
	}

	if n == 0 {
		return []byte{}
	}

	result := make([]byte, n)
	copy(result, b.buf[:n])
	copy(b.buf, b.buf[n:b.off])
	b.off -= n

	return result
}

// ReadFrom implements io.ReaderFrom interface.
// It reads data from r until EOF or error, writing it to the buffer.
// The intermediate read buffer is allocated from the arena.
func (b *Buffer) ReadFrom(r io.Reader) (n int64, err error) {
	// Ensure read buffer is allocated
	if b.readBuf == nil {
		b.allocateReadBuffer()
	}

	for {
		// Read into the intermediate buffer
		nr, er := r.Read(b.readBuf)
		if nr > 0 {
			// Write the read data to our buffer
			_, ew := b.Write(b.readBuf[:nr])
			if ew != nil {
				return n, ew
			}
			n += int64(nr)
		}
		if er != nil {
			if er == io.EOF {
				break
			}
			return n, er
		}
	}
	return n, nil
}

// allocateReadBuffer allocates the intermediate read buffer from the arena.
// If arena allocation fails, it falls back to standard allocation.
func (b *Buffer) allocateReadBuffer() {
	const readBufferSize = 4 * 1024 // 4KB read buffer
	b.readBuf = AllocateSlice[byte](b.arena, readBufferSize, readBufferSize)
}
