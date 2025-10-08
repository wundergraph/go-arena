// SPDX-License-Identifier: Apache-2.0

package arena

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
)

// isMonotonicArenaPtr checks if a pointer is within the memory range of a monotonic arena
func isMonotonicArenaPtr(arena Arena, ptr unsafe.Pointer) bool {
	// Cast to monotonicArena to access internal structure
	ma, ok := arena.(*monotonicArena)
	if !ok {
		return false
	}

	ptrAddr := uintptr(ptr)

	// Check if the pointer is within any of the arena's buffers
	for _, buffer := range ma.buffers {
		if buffer.ptr != nil {
			bufferStart := uintptr(buffer.ptr)
			bufferEnd := bufferStart + buffer.size
			if ptrAddr >= bufferStart && ptrAddr < bufferEnd {
				return true
			}
		}
	}

	return false
}

func TestArenaBufferBasicOperations(t *testing.T) {
	arena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(1024))
	buf := NewArenaBuffer(arena)

	// Test initial state
	require.Equal(t, 0, buf.Len())
	require.Equal(t, 0, buf.Cap())
	require.Equal(t, "", buf.String())
	require.Equal(t, []byte{}, buf.Bytes())

	// Test Write
	n, err := buf.Write([]byte("hello"))
	require.NoError(t, err)
	require.Equal(t, 5, n)
	require.Equal(t, 5, buf.Len())
	require.Equal(t, "hello", buf.String())
	require.Equal(t, []byte("hello"), buf.Bytes())

	// Test WriteByte
	err = buf.WriteByte(' ')
	require.NoError(t, err)
	require.Equal(t, 6, buf.Len())
	require.Equal(t, "hello ", buf.String())

	// Test WriteString
	n, err = buf.WriteString("world")
	require.NoError(t, err)
	require.Equal(t, 5, n)
	require.Equal(t, 11, buf.Len())
	require.Equal(t, "hello world", buf.String())
}

func TestArenaBufferReadOperations(t *testing.T) {
	arena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(1024))
	buf := NewArenaBuffer(arena)

	// Write some data
	_, err := buf.Write([]byte("hello world"))
	require.NoError(t, err)

	// Test Read
	p := make([]byte, 5)
	n, err := buf.Read(p)
	require.NoError(t, err)
	require.Equal(t, 5, n)
	require.Equal(t, []byte("hello"), p)
	require.Equal(t, 6, buf.Len()) // 11 - 5 = 6
	require.Equal(t, " world", buf.String())

	// Test ReadByte
	c, err := buf.ReadByte()
	require.NoError(t, err)
	require.Equal(t, byte(' '), c)
	require.Equal(t, 5, buf.Len())
	require.Equal(t, "world", buf.String())

	// Test reading remaining data
	p = make([]byte, 10)
	n, err = buf.Read(p)
	require.Equal(t, io.EOF, err)
	require.Equal(t, 5, n)
	require.Equal(t, []byte("world"), p[:n])
	require.Equal(t, 0, buf.Len())

	// Test reading from empty buffer
	n, err = buf.Read(p)
	require.Equal(t, io.EOF, err)
	require.Equal(t, 0, n)
}

func TestArenaBufferNext(t *testing.T) {
	arena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(1024))
	buf := NewArenaBuffer(arena)

	// Write some data
	_, err := buf.Write([]byte("hello world"))
	require.NoError(t, err)

	// Test Next
	result := buf.Next(5)
	require.Equal(t, []byte("hello"), result)
	require.Equal(t, 6, buf.Len())
	require.Equal(t, " world", buf.String())

	// Test Next with more than available
	result = buf.Next(10)
	require.Equal(t, []byte(" world"), result)
	require.Equal(t, 0, buf.Len())

	// Test Next on empty buffer
	result = buf.Next(5)
	require.Equal(t, []byte{}, result)
}

func TestArenaBufferReset(t *testing.T) {
	arena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(1024))
	buf := NewArenaBuffer(arena)

	// Write some data
	_, err := buf.Write([]byte("hello world"))
	require.NoError(t, err)
	require.Equal(t, 11, buf.Len())

	// Reset
	buf.Reset()
	require.Equal(t, 0, buf.Len())
	require.Equal(t, "", buf.String())
	require.Equal(t, []byte{}, buf.Bytes())

	// Write new data
	_, err = buf.Write([]byte("new data"))
	require.NoError(t, err)
	require.Equal(t, 8, buf.Len())
	require.Equal(t, "new data", buf.String())
}

func TestArenaBufferTruncate(t *testing.T) {
	arena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(1024))
	buf := NewArenaBuffer(arena)

	// Write some data
	_, err := buf.Write([]byte("hello world"))
	require.NoError(t, err)
	require.Equal(t, 11, buf.Len())

	// Truncate to 5
	buf.Truncate(5)
	require.Equal(t, 5, buf.Len())
	require.Equal(t, "hello", buf.String())

	// Truncate to 0
	buf.Truncate(0)
	require.Equal(t, 0, buf.Len())
	require.Equal(t, "", buf.String())

	// Test panic cases
	require.Panics(t, func() { buf.Truncate(-1) })
	require.Panics(t, func() { buf.Truncate(10) })
}

func TestArenaBufferGrowth(t *testing.T) {
	arena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(1024))
	buf := NewArenaBuffer(arena)

	// Write data that will trigger growth
	largeData := strings.Repeat("a", 200)
	_, err := buf.Write([]byte(largeData))
	require.NoError(t, err)
	require.Equal(t, 200, buf.Len())
	require.True(t, buf.Cap() >= 200)

	// Write more data
	moreData := strings.Repeat("b", 300)
	_, err = buf.Write([]byte(moreData))
	require.NoError(t, err)
	require.Equal(t, 500, buf.Len())
	require.True(t, buf.Cap() >= 500)
}

func TestArenaBufferWithoutArena(t *testing.T) {
	// Test with nil arena (should fall back to standard allocation)
	buf := NewArenaBuffer(nil)

	// Write some data
	_, err := buf.Write([]byte("hello world"))
	require.NoError(t, err)
	require.Equal(t, 11, buf.Len())
	require.Equal(t, "hello world", buf.String())

	// Read some data
	p := make([]byte, 5)
	n, err := buf.Read(p)
	require.NoError(t, err)
	require.Equal(t, 5, n)
	require.Equal(t, []byte("hello"), p)
	require.Equal(t, " world", buf.String())
}

func TestArenaBufferArenaAllocation(t *testing.T) {
	arena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(1024))
	buf := NewArenaBuffer(arena)

	// Write data to trigger arena allocation
	_, err := buf.Write([]byte("test data"))
	require.NoError(t, err)

	// Verify that the buffer's underlying slice is allocated from the arena
	bufPtr := unsafe.Pointer(unsafe.SliceData(buf.buf))
	require.True(t, isMonotonicArenaPtr(arena, bufPtr))
}

func TestArenaBufferArenaExhaustion(t *testing.T) {
	// Create a small arena that will be exhausted
	arena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(64)) // Very small arena
	buf := NewArenaBuffer(arena)

	// Write data that exceeds arena capacity
	largeData := strings.Repeat("a", 200)
	_, err := buf.Write([]byte(largeData))
	require.NoError(t, err) // Should not error, should fall back to standard allocation

	// Verify the data was written correctly
	require.Equal(t, 200, buf.Len())
	require.Equal(t, largeData, buf.String())
}

func TestArenaBufferIoWriterCompatibility(t *testing.T) {
	arena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(1024))
	buf := NewArenaBuffer(arena)

	// Test that it implements io.Writer
	var writer io.Writer = buf
	require.NotNil(t, writer)

	// Test writing through io.Writer interface
	n, err := writer.Write([]byte("hello"))
	require.NoError(t, err)
	require.Equal(t, 5, n)
	require.Equal(t, "hello", buf.String())
}

func TestArenaBufferLargeWrites(t *testing.T) {
	arena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(1024*1024)) // 1MB arena
	buf := NewArenaBuffer(arena)

	// Write large amounts of data
	for i := 0; i < 1000; i++ {
		data := []byte(strings.Repeat("x", 100))
		_, err := buf.Write(data)
		require.NoError(t, err)
	}

	require.Equal(t, 100000, buf.Len())
	require.True(t, buf.Cap() >= 100000)

	// Read back some data
	p := make([]byte, 1000)
	n, err := buf.Read(p)
	require.NoError(t, err)
	require.Equal(t, 1000, n)
	require.Equal(t, strings.Repeat("x", 1000), string(p))
}

func TestArenaBufferMixedOperations(t *testing.T) {
	arena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(1024))
	buf := NewArenaBuffer(arena)

	// Mix of write operations
	_, err := buf.Write([]byte("hello"))
	require.NoError(t, err)

	err = buf.WriteByte(' ')
	require.NoError(t, err)

	_, err = buf.WriteString("world")
	require.NoError(t, err)

	require.Equal(t, "hello world", buf.String())

	// Mix of read operations
	c, err := buf.ReadByte()
	require.NoError(t, err)
	require.Equal(t, byte('h'), c)

	p := make([]byte, 4)
	n, err := buf.Read(p)
	require.NoError(t, err)
	require.Equal(t, 4, n)
	require.Equal(t, []byte("ello"), p)

	require.Equal(t, " world", buf.String())
}

func TestArenaBufferEmptyOperations(t *testing.T) {
	arena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(1024))
	buf := NewArenaBuffer(arena)

	// Test operations on empty buffer
	require.Equal(t, 0, buf.Len())
	require.Equal(t, "", buf.String())
	require.Equal(t, []byte{}, buf.Bytes())

	// Read from empty buffer
	p := make([]byte, 10)
	n, err := buf.Read(p)
	require.Equal(t, io.EOF, err)
	require.Equal(t, 0, n)

	// ReadByte from empty buffer
	_, err = buf.ReadByte()
	require.Equal(t, io.EOF, err)

	// Next from empty buffer
	result := buf.Next(5)
	require.Equal(t, []byte{}, result)
}

func TestArenaBufferResetAfterOperations(t *testing.T) {
	arena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(1024))
	buf := NewArenaBuffer(arena)

	// Perform various operations
	_, err := buf.Write([]byte("hello"))
	require.NoError(t, err)

	err = buf.WriteByte(' ')
	require.NoError(t, err)

	_, err = buf.WriteString("world")
	require.NoError(t, err)

	require.Equal(t, "hello world", buf.String())

	// Reset and verify clean state
	buf.Reset()
	require.Equal(t, 0, buf.Len())
	require.Equal(t, "", buf.String())
	require.Equal(t, []byte{}, buf.Bytes())

	// Verify we can still use the buffer after reset
	_, err = buf.Write([]byte("new data"))
	require.NoError(t, err)
	require.Equal(t, "new data", buf.String())
}

// Benchmark tests
func BenchmarkArenaBufferWrite(b *testing.B) {
	arena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(1024*1024))
	buf := NewArenaBuffer(arena)
	data := []byte("hello world")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Write(data)
		buf.Reset()
	}
}

func BenchmarkArenaBufferRead(b *testing.B) {
	arena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(1024*1024))
	buf := NewArenaBuffer(arena)
	data := []byte("hello world")
	_, _ = buf.Write(data)

	p := make([]byte, len(data))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Read(p)
		buf.Reset()
		_, _ = buf.Write(data)
	}
}

func BenchmarkStandardBytesBufferWrite(b *testing.B) {
	buf := &bytes.Buffer{}
	data := []byte("hello world")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Write(data)
		buf.Reset()
	}
}

func BenchmarkStandardBytesBufferRead(b *testing.B) {
	buf := &bytes.Buffer{}
	data := []byte("hello world")
	_, _ = buf.Write(data)

	p := make([]byte, len(data))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Read(p)
		buf.Reset()
		_, _ = buf.Write(data)
	}
}

func BenchmarkArenaBufferReadFrom(b *testing.B) {
	arena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(1024*1024))
	buf := NewArenaBuffer(arena)
	data := []byte("hello world")
	reader := bytes.NewReader(data)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.ReadFrom(reader)
		buf.Reset()
		reader.Seek(0, 0)
	}
}

func BenchmarkStandardBytesBufferReadFrom(b *testing.B) {
	buf := &bytes.Buffer{}
	data := []byte("hello world")
	reader := bytes.NewReader(data)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.ReadFrom(reader)
		buf.Reset()
		reader.Seek(0, 0)
	}
}

func TestArenaBufferReadFrom(t *testing.T) {
	arena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(1024))
	buf := NewArenaBuffer(arena)

	// Test reading from a string reader
	reader := strings.NewReader("hello world")
	n, err := buf.ReadFrom(reader)
	require.NoError(t, err)
	require.Equal(t, int64(11), n)
	require.Equal(t, "hello world", buf.String())
	require.Equal(t, 11, buf.Len())

	// Test reading from bytes reader
	buf.Reset()
	reader2 := bytes.NewReader([]byte("test data"))
	n, err = buf.ReadFrom(reader2)
	require.NoError(t, err)
	require.Equal(t, int64(9), n)
	require.Equal(t, "test data", buf.String())
}

func TestArenaBufferReadFromLargeData(t *testing.T) {
	arena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(1024*1024)) // 1MB arena
	buf := NewArenaBuffer(arena)

	// Create large data (larger than 4KB read buffer)
	largeData := strings.Repeat("abcdefghijklmnopqrstuvwxyz", 200) // ~5.2KB
	reader := strings.NewReader(largeData)

	n, err := buf.ReadFrom(reader)
	require.NoError(t, err)
	require.Equal(t, int64(len(largeData)), n)
	require.Equal(t, largeData, buf.String())
	require.Equal(t, len(largeData), buf.Len())
}

func TestArenaBufferReadFromEmptyReader(t *testing.T) {
	arena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(1024))
	buf := NewArenaBuffer(arena)

	// Test reading from empty reader
	reader := strings.NewReader("")
	n, err := buf.ReadFrom(reader)
	require.NoError(t, err)
	require.Equal(t, int64(0), n)
	require.Equal(t, "", buf.String())
	require.Equal(t, 0, buf.Len())
}

func TestArenaBufferReadFromMultipleReads(t *testing.T) {
	arena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(1024))
	buf := NewArenaBuffer(arena)

	// Test multiple ReadFrom operations
	reader1 := strings.NewReader("hello ")
	n, err := buf.ReadFrom(reader1)
	require.NoError(t, err)
	require.Equal(t, int64(6), n)
	require.Equal(t, "hello ", buf.String())

	reader2 := strings.NewReader("world")
	n, err = buf.ReadFrom(reader2)
	require.NoError(t, err)
	require.Equal(t, int64(5), n)
	require.Equal(t, "hello world", buf.String())
	require.Equal(t, 11, buf.Len())
}

func TestArenaBufferReadFromWithError(t *testing.T) {
	arena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(1024))
	buf := NewArenaBuffer(arena)

	// Create a reader that will return an error
	errorReader := &errorReader{data: []byte("hello"), errPos: 3}
	n, err := buf.ReadFrom(errorReader)
	require.Error(t, err)
	require.Equal(t, "test error", err.Error())
	require.Equal(t, int64(3), n) // Should have read 3 bytes before error
	require.Equal(t, "hel", buf.String())
}

func TestArenaBufferReadFromArenaAllocation(t *testing.T) {
	// Use a larger arena to ensure the read buffer can be allocated from it
	arena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(8*1024)) // 8KB arena
	buf := NewArenaBuffer(arena)

	// Trigger read buffer allocation by using ReadFrom
	reader := strings.NewReader("test")
	_, err := buf.ReadFrom(reader)
	require.NoError(t, err)

	// Verify that the read buffer is allocated
	require.NotNil(t, buf.readBuf)
	require.Equal(t, 4*1024, len(buf.readBuf))

	// Check if read buffer is allocated from arena
	readBufPtr := unsafe.Pointer(unsafe.SliceData(buf.readBuf))
	require.True(t, isMonotonicArenaPtr(arena, readBufPtr))
}

func TestArenaBufferReadFromWithoutArena(t *testing.T) {
	// Test with nil arena (should fall back to standard allocation)
	buf := NewArenaBuffer(nil)

	reader := strings.NewReader("hello world")
	n, err := buf.ReadFrom(reader)
	require.NoError(t, err)
	require.Equal(t, int64(11), n)
	require.Equal(t, "hello world", buf.String())

	// Read buffer should still be allocated (from standard allocation)
	require.NotNil(t, buf.readBuf)
	require.Equal(t, 4*1024, len(buf.readBuf))
}

func TestArenaBufferReadBufferLazyAllocation(t *testing.T) {
	arena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(1024))
	buf := NewArenaBuffer(arena)

	// Initially read buffer should be nil
	require.Nil(t, buf.readBuf)

	// ReadFrom should allocate the buffer
	reader := strings.NewReader("test")
	n, err := buf.ReadFrom(reader)
	require.NoError(t, err)
	require.Equal(t, int64(4), n)
	require.Equal(t, "test", buf.String())
	require.NotNil(t, buf.readBuf) // Should be allocated
	require.Equal(t, 4*1024, len(buf.readBuf))
}

func TestArenaBufferReadFromIoReaderFromCompatibility(t *testing.T) {
	arena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(1024))
	buf := NewArenaBuffer(arena)

	// Test that it implements io.ReaderFrom
	var readerFrom io.ReaderFrom = buf
	require.NotNil(t, readerFrom)

	// Test reading through io.ReaderFrom interface
	reader := strings.NewReader("hello world")
	n, err := readerFrom.ReadFrom(reader)
	require.NoError(t, err)
	require.Equal(t, int64(11), n)
	require.Equal(t, "hello world", buf.String())
}

// errorReader is a test helper that returns an error after reading a certain number of bytes
type errorReader struct {
	data   []byte
	pos    int
	errPos int
}

func (er *errorReader) Read(p []byte) (n int, err error) {
	if er.pos >= er.errPos {
		return 0, errors.New("test error")
	}

	remaining := er.errPos - er.pos
	if len(p) > remaining {
		p = p[:remaining]
	}

	n = copy(p, er.data[er.pos:])
	er.pos += n
	return n, nil
}

func TestArenaBufferSliceAppendApproach(t *testing.T) {
	arena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(1024))
	buf := NewArenaBuffer(arena)

	// Test that Write uses SliceAppend approach
	_, err := buf.Write([]byte("hello"))
	require.NoError(t, err)
	require.Equal(t, "hello", buf.String())
	require.Equal(t, 5, buf.Len())

	// Test that WriteByte uses SliceAppend approach
	err = buf.WriteByte(' ')
	require.NoError(t, err)
	require.Equal(t, "hello ", buf.String())
	require.Equal(t, 6, buf.Len())

	// Test that WriteString uses SliceAppend approach
	_, err = buf.WriteString("world")
	require.NoError(t, err)
	require.Equal(t, "hello world", buf.String())
	require.Equal(t, 11, buf.Len())

	// Verify that all data is properly stored and accessible
	require.Equal(t, []byte("hello world"), buf.Bytes())
}

func TestArenaBufferSliceAppendGrowth(t *testing.T) {
	arena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(1024))
	buf := NewArenaBuffer(arena)

	// Test growth with Write
	largeData := strings.Repeat("a", 200)
	_, err := buf.Write([]byte(largeData))
	require.NoError(t, err)
	require.Equal(t, 200, buf.Len())
	require.True(t, buf.Cap() >= 200)

	// Test growth with WriteByte
	for i := 0; i < 100; i++ {
		err = buf.WriteByte('b')
		require.NoError(t, err)
	}
	require.Equal(t, 300, buf.Len())
	require.True(t, buf.Cap() >= 300)

	// Test growth with WriteString
	moreData := strings.Repeat("c", 200)
	_, err = buf.WriteString(moreData)
	require.NoError(t, err)
	require.Equal(t, 500, buf.Len())
	require.True(t, buf.Cap() >= 500)

	// Verify all data is correct
	expected := strings.Repeat("a", 200) + strings.Repeat("b", 100) + strings.Repeat("c", 200)
	require.Equal(t, expected, buf.String())
}

func TestArenaBufferSliceAppendArenaAllocation(t *testing.T) {
	arena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(1024))
	buf := NewArenaBuffer(arena)

	// Test Write with arena allocation
	_, err := buf.Write([]byte("test"))
	require.NoError(t, err)
	bufPtr := unsafe.Pointer(unsafe.SliceData(buf.buf))
	require.True(t, isMonotonicArenaPtr(arena, bufPtr))

	// Test WriteByte with arena allocation
	err = buf.WriteByte('!')
	require.NoError(t, err)
	bufPtr = unsafe.Pointer(unsafe.SliceData(buf.buf))
	require.True(t, isMonotonicArenaPtr(arena, bufPtr))

	// Test WriteString with arena allocation
	_, err = buf.WriteString("more")
	require.NoError(t, err)
	bufPtr = unsafe.Pointer(unsafe.SliceData(buf.buf))
	require.True(t, isMonotonicArenaPtr(arena, bufPtr))

	require.Equal(t, "test!more", buf.String())
}

func TestArenaBufferSliceAppendWithoutArena(t *testing.T) {
	// Test with nil arena (should fall back to standard allocation)
	buf := NewArenaBuffer(nil)

	// Test Write without arena
	_, err := buf.Write([]byte("hello"))
	require.NoError(t, err)
	require.Equal(t, "hello", buf.String())

	// Test WriteByte without arena
	err = buf.WriteByte(' ')
	require.NoError(t, err)
	require.Equal(t, "hello ", buf.String())

	// Test WriteString without arena
	_, err = buf.WriteString("world")
	require.NoError(t, err)
	require.Equal(t, "hello world", buf.String())
}

func TestArenaBufferSliceAppendReset(t *testing.T) {
	arena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(1024))
	buf := NewArenaBuffer(arena)

	// Write some data using SliceAppend approach
	_, err := buf.Write([]byte("hello"))
	require.NoError(t, err)
	err = buf.WriteByte(' ')
	require.NoError(t, err)
	_, err = buf.WriteString("world")
	require.NoError(t, err)
	require.Equal(t, "hello world", buf.String())

	// Reset should clear the slice length
	buf.Reset()
	require.Equal(t, 0, buf.Len())
	require.Equal(t, "", buf.String())
	require.Equal(t, []byte{}, buf.Bytes())

	// Should be able to write new data after reset
	_, err = buf.Write([]byte("new"))
	require.NoError(t, err)
	require.Equal(t, "new", buf.String())
}

func TestArenaBufferSliceAppendMixedOperations(t *testing.T) {
	arena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(1024))
	buf := NewArenaBuffer(arena)

	// Mix of all write operations using SliceAppend
	_, err := buf.Write([]byte("start"))
	require.NoError(t, err)

	err = buf.WriteByte('-')
	require.NoError(t, err)

	_, err = buf.WriteString("middle")
	require.NoError(t, err)

	err = buf.WriteByte('-')
	require.NoError(t, err)

	_, err = buf.Write([]byte("end"))
	require.NoError(t, err)

	require.Equal(t, "start-middle-end", buf.String())
	require.Equal(t, 16, buf.Len())

	// Test reading operations work correctly
	p := make([]byte, 5)
	n, err := buf.Read(p)
	require.NoError(t, err)
	require.Equal(t, 5, n)
	require.Equal(t, []byte("start"), p)
	require.Equal(t, "-middle-end", buf.String())
}
