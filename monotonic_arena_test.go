// SPDX-License-Identifier: Apache-2.0

package arena

import (
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
)

func TestMonotonicArenaLen(t *testing.T) {
	// Test with single buffer
	arena := NewMonotonicArena()
	require.Equal(t, 0, arena.Len())

	// Allocate some memory
	ptr1 := arena.Alloc(100, 1)
	require.NotNil(t, ptr1)
	require.Equal(t, 100, arena.Len())

	// Allocate more memory
	ptr2 := arena.Alloc(200, 1)
	require.NotNil(t, ptr2)
	require.Equal(t, 300, arena.Len())

	// Allocate with alignment
	ptr3 := arena.Alloc(50, 8)
	require.NotNil(t, ptr3)
	// Should be more than 350 due to alignment padding
	require.True(t, arena.Len() >= 350)
}

func TestMonotonicArenaCap(t *testing.T) {
	// Test with single buffer
	arena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(1024))
	require.Equal(t, 1024, arena.Cap())

	// Test with multiple buffers
	arena = NewMonotonicArena(WithInitialBufferCount(3), WithMinBufferSize(512))
	require.Equal(t, 1536, arena.Cap()) // 512 * 3

	// Test with different buffer sizes
	arena = NewMonotonicArena(WithInitialBufferCount(4), WithMinBufferSize(256))
	require.Equal(t, 1024, arena.Cap()) // 256 * 4
}

func TestMonotonicArenaLenCapAfterReset(t *testing.T) {
	arena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(1024))

	// Allocate some memory
	ptr1 := arena.Alloc(100, 1)
	require.NotNil(t, ptr1)
	require.Equal(t, 100, arena.Len())
	require.Equal(t, 1024, arena.Cap())

	// Reset without release
	arena.Reset()
	require.Equal(t, 0, arena.Len())
	require.Equal(t, 1024, arena.Cap())

	// Allocate again
	ptr2 := arena.Alloc(50, 1)
	require.NotNil(t, ptr2)
	require.Equal(t, 50, arena.Len())
	require.Equal(t, 1024, arena.Cap())

	// Reset with release
	arena.Release()
	require.Equal(t, 0, arena.Len())
	require.Equal(t, 1024, arena.Cap())
}

func TestMonotonicArenaMultipleBuffers(t *testing.T) {
	// Create arena with multiple buffers
	arena := NewMonotonicArena(WithInitialBufferCount(3), WithMinBufferSize(100)) // 3 buffers of 100 bytes each
	require.Equal(t, 300, arena.Cap())
	require.Equal(t, 0, arena.Len())

	// Fill first buffer
	ptr1 := arena.Alloc(100, 1)
	require.NotNil(t, ptr1)
	require.Equal(t, 100, arena.Len())

	// Fill second buffer
	ptr2 := arena.Alloc(100, 1)
	require.NotNil(t, ptr2)
	require.Equal(t, 200, arena.Len())

	// Fill third buffer
	ptr3 := arena.Alloc(100, 1)
	require.NotNil(t, ptr3)
	require.Equal(t, 300, arena.Len())

	// Try to allocate more (should succeed by creating a new buffer)
	ptr4 := arena.Alloc(1, 1)
	require.NotNil(t, ptr4)
	require.Equal(t, 301, arena.Len())
}

func TestMonotonicArenaAlignment(t *testing.T) {
	arena := NewMonotonicArena()

	// Allocate with different alignments
	ptr1 := arena.Alloc(1, 1) // No alignment needed
	require.NotNil(t, ptr1)
	len1 := arena.Len()
	require.Equal(t, 1, len1)

	ptr2 := arena.Alloc(1, 8) // 8-byte alignment
	require.NotNil(t, ptr2)
	len2 := arena.Len()
	require.True(t, len2 > len1) // Should be more due to alignment padding

	ptr3 := arena.Alloc(1, 16) // 16-byte alignment
	require.NotNil(t, ptr3)
	len3 := arena.Len()
	require.True(t, len3 > len2) // Should be more due to alignment padding
}

func TestMonotonicArenaWithTypes(t *testing.T) {
	arena := NewMonotonicArena()

	// Test with different types
	type TestStruct struct {
		a int64
		b int32
		c int16
	}

	// Allocate using New function
	ptr1 := Allocate[TestStruct](arena)
	require.NotNil(t, ptr1)
	expectedSize := unsafe.Sizeof(TestStruct{})
	require.Equal(t, int(expectedSize), arena.Len())

	// Allocate using MakeSlice
	slice := AllocateSlice[int](arena, 10, 20)
	require.NotNil(t, slice)
	require.Len(t, slice, 10)
	require.Equal(t, 20, cap(slice))

	// Len should include the slice allocation
	expectedSize += unsafe.Sizeof(int(0)) * 20
	require.Equal(t, int(expectedSize), arena.Len())
}

func TestMonotonicArenaEdgeCases(t *testing.T) {
	// Test with zero-sized arena
	arena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(0))
	require.Equal(t, 0, arena.Cap())
	require.Equal(t, 0, arena.Len())

	ptr := arena.Alloc(1, 1)
	require.NotNil(t, ptr)

	// Test with zero buffers
	arena = NewMonotonicArena(WithInitialBufferCount(0), WithMinBufferSize(1024))
	require.Equal(t, 0, arena.Cap())
	require.Equal(t, 0, arena.Len())

	ptr = arena.Alloc(1, 1)
	require.NotNil(t, ptr)
}

func BenchmarkMonotonicArenaLen(b *testing.B) {
	arena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(1024*1024))

	// Pre-allocate some memory
	for i := 0; i < 1000; i++ {
		arena.Alloc(100, 1)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = arena.Len()
	}
}

func BenchmarkMonotonicArenaCap(b *testing.B) {
	arena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(1024*1024))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = arena.Cap()
	}
}

func TestMonotonicArenaPeak(t *testing.T) {
	// Test with single buffer
	arena := NewMonotonicArena()
	require.Equal(t, 0, arena.Peak())

	// Allocate some memory
	ptr1 := arena.Alloc(100, 1)
	require.NotNil(t, ptr1)
	require.Equal(t, 100, arena.Peak())

	// Allocate more memory
	ptr2 := arena.Alloc(200, 1)
	require.NotNil(t, ptr2)
	require.Equal(t, 300, arena.Peak())

	// Allocate with alignment
	ptr3 := arena.Alloc(50, 8)
	require.NotNil(t, ptr3)
	// Peak should be more than 350 due to alignment padding
	require.True(t, arena.Peak() >= 350)
}

func TestMonotonicArenaPeakAfterReset(t *testing.T) {
	arena := NewMonotonicArena()

	// Allocate some memory
	ptr1 := arena.Alloc(100, 1)
	require.NotNil(t, ptr1)
	require.Equal(t, 100, arena.Peak())

	// Allocate more memory
	ptr2 := arena.Alloc(200, 1)
	require.NotNil(t, ptr2)
	require.Equal(t, 300, arena.Peak())

	// Reset without release - peak should remain
	arena.Reset()
	require.Equal(t, 0, arena.Len())
	require.Equal(t, 300, arena.Peak()) // Peak should not be reset

	// Allocate again
	ptr3 := arena.Alloc(50, 1)
	require.NotNil(t, ptr3)
	require.Equal(t, 50, arena.Len())
	require.Equal(t, 300, arena.Peak()) // Peak should still be 300

	// Allocate more than previous peak
	ptr4 := arena.Alloc(400, 1)
	require.NotNil(t, ptr4)
	require.Equal(t, 450, arena.Len())
	require.Equal(t, 450, arena.Peak()) // Peak should be updated to 450

	// Reset with release - peak should still remain
	arena.Release()
	require.Equal(t, 0, arena.Len())
	require.Equal(t, 450, arena.Peak()) // Peak should not be reset
}

func TestMonotonicArenaPeakMultipleBuffers(t *testing.T) {
	// Create arena with multiple buffers
	arena := NewMonotonicArena(WithInitialBufferCount(3), WithMinBufferSize(100)) // 3 buffers of 100 bytes each
	require.Equal(t, 0, arena.Peak())

	// Fill first buffer
	ptr1 := arena.Alloc(100, 1)
	require.NotNil(t, ptr1)
	require.Equal(t, 100, arena.Peak())

	// Fill second buffer
	ptr2 := arena.Alloc(100, 1)
	require.NotNil(t, ptr2)
	require.Equal(t, 200, arena.Peak())

	// Fill third buffer
	ptr3 := arena.Alloc(100, 1)
	require.NotNil(t, ptr3)
	require.Equal(t, 300, arena.Peak())

	// Try to allocate more (should succeed by creating a new buffer)
	ptr4 := arena.Alloc(1, 1)
	require.NotNil(t, ptr4)
	require.Equal(t, 301, arena.Peak()) // Peak should be updated to reflect new allocation

	// Reset and verify peak is preserved
	arena.Reset()
	require.Equal(t, 0, arena.Len())
	require.Equal(t, 301, arena.Peak())
}

func TestMonotonicArenaPeakAlignment(t *testing.T) {
	arena := NewMonotonicArena()
	require.Equal(t, 0, arena.Peak())

	// Allocate with different alignments
	ptr1 := arena.Alloc(1, 1) // No alignment needed
	require.NotNil(t, ptr1)
	peak1 := arena.Peak()
	require.Equal(t, 1, peak1)

	ptr2 := arena.Alloc(1, 8) // 8-byte alignment
	require.NotNil(t, ptr2)
	peak2 := arena.Peak()
	require.True(t, peak2 > peak1) // Should be more due to alignment padding

	ptr3 := arena.Alloc(1, 16) // 16-byte alignment
	require.NotNil(t, ptr3)
	peak3 := arena.Peak()
	require.True(t, peak3 > peak2) // Should be more due to alignment padding

	// Reset and verify peak is preserved
	arena.Reset()
	require.Equal(t, 0, arena.Len())
	require.Equal(t, peak3, arena.Peak())
}

func TestMonotonicArenaPeakEdgeCases(t *testing.T) {
	// Test with zero-sized arena
	arena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(0))
	require.Equal(t, 0, arena.Peak())

	ptr := arena.Alloc(1, 1)
	require.NotNil(t, ptr)
	require.Equal(t, 1, arena.Peak()) // Peak should reflect successful allocation

	// Test with zero buffers
	arena = NewMonotonicArena(WithInitialBufferCount(0), WithMinBufferSize(1024))
	require.Equal(t, 0, arena.Peak())

	ptr = arena.Alloc(1, 1)
	require.NotNil(t, ptr)
	require.Equal(t, 1, arena.Peak()) // Peak should reflect successful allocation
}

func BenchmarkMonotonicArenaPeak(b *testing.B) {
	arena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(1024*1024))

	// Pre-allocate some memory to set a peak
	for i := 0; i < 1000; i++ {
		arena.Alloc(100, 1)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = arena.Peak()
	}
}

func TestMonotonicArenaPeakOnAllocationFailure(t *testing.T) {
	// Create arena with small capacity to force new buffer creation
	arena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(100)) // Only 100 bytes initial capacity
	require.Equal(t, 0, arena.Peak())

	// Fill the arena completely
	ptr1 := arena.Alloc(100, 1)
	require.NotNil(t, ptr1)
	require.Equal(t, 100, arena.Peak())
	require.Equal(t, 100, arena.Len())

	// Try to allocate more - should succeed by creating a new buffer
	ptr2 := arena.Alloc(50, 1)
	require.NotNil(t, ptr2)             // Allocation should succeed
	require.Equal(t, 150, arena.Len())  // Current length should increase
	require.Equal(t, 150, arena.Peak()) // Peak should reflect what was allocated

	// Try another allocation
	ptr3 := arena.Alloc(200, 1)
	require.NotNil(t, ptr3)             // Allocation should succeed
	require.Equal(t, 350, arena.Len())  // Current length should increase
	require.Equal(t, 350, arena.Peak()) // Peak should reflect the larger allocation
}

func TestMonotonicArenaPeakOnAllocationFailureWithAlignment(t *testing.T) {
	// Create arena with small capacity
	arena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(100))
	require.Equal(t, 0, arena.Peak())

	// Allocate some memory with alignment
	ptr1 := arena.Alloc(50, 8)
	require.NotNil(t, ptr1)
	currentLen := arena.Len()
	require.True(t, currentLen >= 50) // Should be >= 50 due to alignment
	require.Equal(t, currentLen, arena.Peak())

	// Try to allocate more with alignment - should succeed by creating a new buffer
	ptr2 := arena.Alloc(60, 16)
	require.NotNil(t, ptr2) // Allocation should succeed
	newLen := arena.Len()
	require.True(t, newLen > currentLen)   // Length should increase
	require.Equal(t, newLen, arena.Peak()) // Peak should reflect new length
}

func TestMonotonicArenaPeakOnAllocationFailureMultipleBuffers(t *testing.T) {
	// Create arena with multiple small buffers
	arena := NewMonotonicArena(WithInitialBufferCount(2), WithMinBufferSize(50)) // 2 buffers of 50 bytes each
	require.Equal(t, 0, arena.Peak())

	// Fill first buffer
	ptr1 := arena.Alloc(50, 1)
	require.NotNil(t, ptr1)
	require.Equal(t, 50, arena.Peak())

	// Fill second buffer
	ptr2 := arena.Alloc(50, 1)
	require.NotNil(t, ptr2)
	require.Equal(t, 100, arena.Peak())

	// Try to allocate more - should succeed by creating a new buffer
	ptr3 := arena.Alloc(75, 1)
	require.NotNil(t, ptr3)             // Allocation should succeed
	require.Equal(t, 175, arena.Len())  // Current length should increase
	require.Equal(t, 175, arena.Peak()) // Peak should reflect what was allocated
}

func TestMonotonicArenaPeakOnAllocationFailureAfterReset(t *testing.T) {
	arena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(100))
	require.Equal(t, 0, arena.Peak())

	// Fill the arena
	ptr1 := arena.Alloc(100, 1)
	require.NotNil(t, ptr1)
	require.Equal(t, 100, arena.Peak())

	// Try allocation that creates a new buffer
	ptr2 := arena.Alloc(50, 1)
	require.NotNil(t, ptr2)
	require.Equal(t, 150, arena.Peak())

	// Reset without release - peak should be preserved
	arena.Reset()
	require.Equal(t, 0, arena.Len())
	require.Equal(t, 150, arena.Peak()) // Peak should not be reset

	// Try another allocation
	ptr3 := arena.Alloc(200, 1)
	require.NotNil(t, ptr3)
	require.Equal(t, 200, arena.Len())  // Current length should be 200
	require.Equal(t, 200, arena.Peak()) // Peak should be updated to the allocation size
}

func TestMonotonicArenaPeakOnAllocationFailureEdgeCases(t *testing.T) {
	// Test with zero-sized arena
	arena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(0))
	require.Equal(t, 0, arena.Peak())

	ptr := arena.Alloc(100, 1)
	require.NotNil(t, ptr)
	require.Equal(t, 100, arena.Len())
	require.Equal(t, 100, arena.Peak()) // Peak should reflect successful allocation

	// Test with zero buffers
	arena = NewMonotonicArena(WithInitialBufferCount(0), WithMinBufferSize(1024))
	require.Equal(t, 0, arena.Peak())

	ptr = arena.Alloc(100, 1)
	require.NotNil(t, ptr)
	require.Equal(t, 100, arena.Len())
	require.Equal(t, 100, arena.Peak()) // Peak should reflect successful allocation
}

func TestMonotonicArenaPeakOnAllocationFailureConcurrent(t *testing.T) {
	// Test concurrent arena with new buffer creation
	baseArena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(100)) // Small capacity
	arena := NewConcurrentArena(baseArena)
	require.Equal(t, 0, arena.Peak())

	// Fill the arena
	ptr1 := arena.Alloc(100, 1)
	require.NotNil(t, ptr1)
	require.Equal(t, 100, arena.Peak())

	// Try allocation that creates a new buffer
	ptr2 := arena.Alloc(50, 1)
	require.NotNil(t, ptr2)
	require.Equal(t, 150, arena.Len())
	require.Equal(t, 150, arena.Peak()) // Peak should reflect successful allocation
}

func TestMonotonicArenaNewBufferCreation(t *testing.T) {
	// Start with a buffer size that's too small for the objects we want to allocate
	arena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(50)) // Only 50 bytes initial capacity
	require.Equal(t, 1, len(arena.(*monotonicArena).buffers))
	require.Equal(t, 50, arena.Cap())
	require.Equal(t, 0, arena.Len())

	// Try to allocate an object larger than the initial buffer size
	ptr1 := arena.Alloc(100, 1) // 100 bytes > 50 bytes initial buffer
	require.NotNil(t, ptr1)
	require.Equal(t, 2, len(arena.(*monotonicArena).buffers)) // Should have created a new buffer
	require.Equal(t, 100, arena.Len())
	require.Equal(t, 100, arena.Peak())

	// The new buffer should be at least 100 bytes (the required size)
	// but could be larger if bufferSize (50) was used as minimum
	newBuffer := arena.(*monotonicArena).buffers[1]
	require.True(t, newBuffer.size >= 100) // New buffer should be large enough

	// Try to allocate another large object
	ptr2 := arena.Alloc(200, 1) // 200 bytes
	require.NotNil(t, ptr2)
	// The second buffer might be large enough for both allocations, or we might need a third buffer
	require.True(t, len(arena.(*monotonicArena).buffers) >= 2)
	require.Equal(t, 300, arena.Len())
	require.Equal(t, 300, arena.Peak())

	// The last buffer should be at least 200 bytes (and much larger due to minBufferSize)
	monoArena := arena.(*monotonicArena)
	lastBuffer := monoArena.buffers[len(monoArena.buffers)-1]
	require.True(t, lastBuffer.size >= 200)
}

func TestMonotonicArenaNewBufferCreationWithAlignment(t *testing.T) {
	// Start with a small buffer size
	arena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(30)) // Only 30 bytes initial capacity
	require.Equal(t, 1, len(arena.(*monotonicArena).buffers))

	// Allocate a small object first to create alignment requirements
	ptr1 := arena.Alloc(10, 1)
	require.NotNil(t, ptr1)
	require.Equal(t, 10, arena.Len())

	// Try to allocate a larger object with alignment that won't fit
	ptr2 := arena.Alloc(50, 8) // 50 bytes with 8-byte alignment
	require.NotNil(t, ptr2)
	require.Equal(t, 2, len(arena.(*monotonicArena).buffers)) // Should have created a new buffer

	// The new buffer should be large enough for the allocation plus alignment
	newBuffer := arena.(*monotonicArena).buffers[1]
	require.True(t, newBuffer.size >= 50) // Should be at least the required size
}

func TestMonotonicArenaNewBufferCreationMinimumSize(t *testing.T) {
	// Start with a larger buffer size
	arena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(1000)) // 1000 bytes initial capacity
	require.Equal(t, 1, len(arena.(*monotonicArena).buffers))

	// Fill the initial buffer
	ptr1 := arena.Alloc(1000, 1)
	require.NotNil(t, ptr1)
	require.Equal(t, 1000, arena.Len())

	// Try to allocate a small object that won't fit in the filled buffer
	ptr2 := arena.Alloc(10, 1) // Small allocation
	require.NotNil(t, ptr2)
	require.Equal(t, 2, len(arena.(*monotonicArena).buffers)) // Should have created a new buffer

	// The new buffer should be at least the configured buffer size (1000) even though we only need 10 bytes
	newBuffer := arena.(*monotonicArena).buffers[1]
	require.Equal(t, uintptr(1000), newBuffer.size) // Should use configured buffer size (1000)
	require.Equal(t, 1010, arena.Len())
	require.Equal(t, 1010, arena.Peak())
}

func TestMonotonicArenaNewBufferCreationMultipleAllocations(t *testing.T) {
	// Start with a very small buffer size
	arena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(10)) // Only 10 bytes initial capacity
	require.Equal(t, 1, len(arena.(*monotonicArena).buffers))

	// Make multiple allocations that require new buffers
	allocations := []uintptr{50, 100, 200, 150}

	for i, size := range allocations {
		ptr := arena.Alloc(size, 1)
		require.NotNil(t, ptr, "Allocation %d of size %d should succeed", i, size)
		// Should have at least 2 buffers (initial + additional buffers for allocations)
		require.True(t, len(arena.(*monotonicArena).buffers) >= 2,
			"Should have created additional buffers for allocations")
	}

	// Verify total length and peak
	totalAllocated := uintptr(0)
	for _, size := range allocations {
		totalAllocated += size
	}
	require.Equal(t, int(totalAllocated), arena.Len())
	require.Equal(t, int(totalAllocated), arena.Peak())
}

func TestMonotonicArenaOptionsPattern(t *testing.T) {
	// Test default behavior (should use minBufferSize)
	arena := NewMonotonicArena()
	require.Equal(t, int(minBufferSize), arena.Cap())

	// Test with custom buffer size
	customSize := 2048
	arena = NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(customSize))
	require.Equal(t, customSize, arena.Cap())

	// Test with multiple buffers and custom size
	arena = NewMonotonicArena(WithInitialBufferCount(3), WithMinBufferSize(512))
	require.Equal(t, 1536, arena.Cap()) // 512 * 3

	// Test that new buffers use the configured size
	// Fill the initial buffers
	for i := 0; i < 3; i++ {
		arena.Alloc(512, 1)
	}
	require.Equal(t, 1536, arena.Len())

	// Allocate more to trigger new buffer creation
	arena.Alloc(100, 1)
	require.Equal(t, 1636, arena.Len())

	// Check that the new buffer uses the configured size (512)
	monoArena := arena.(*monotonicArena)
	require.Equal(t, 4, len(monoArena.buffers))
	newBuffer := monoArena.buffers[3]
	require.Equal(t, uintptr(512), newBuffer.size)
}

func TestMonotonicArenaOptionsPatternDefault(t *testing.T) {
	// Test that default behavior uses minBufferSize
	arena := NewMonotonicArena()
	require.Equal(t, int(minBufferSize), arena.Cap())

	// Fill the buffer and trigger new buffer creation
	arena.Alloc(minBufferSize, 1)
	require.Equal(t, int(minBufferSize), arena.Len())

	// Allocate more to trigger new buffer creation
	arena.Alloc(100, 1)
	require.Equal(t, int(minBufferSize)+100, arena.Len())

	// Check that the new buffer uses minBufferSize
	monoArena := arena.(*monotonicArena)
	require.Equal(t, 2, len(monoArena.buffers))
	newBuffer := monoArena.buffers[1]
	require.Equal(t, uintptr(minBufferSize), newBuffer.size)
}

func TestMonotonicArenaOptionsPatternLargeAllocation(t *testing.T) {
	// Test that large allocations override the configured buffer size
	customSize := 100
	arena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(customSize))
	require.Equal(t, customSize, arena.Cap())

	// Allocate something larger than the configured size
	largeSize := uintptr(500)
	arena.Alloc(largeSize, 1)
	require.Equal(t, int(largeSize), arena.Len())

	// Check that the new buffer is large enough for the allocation
	monoArena := arena.(*monotonicArena)
	require.Equal(t, 2, len(monoArena.buffers))
	newBuffer := monoArena.buffers[1]
	require.True(t, newBuffer.size >= uintptr(largeSize))
}

func TestMonotonicArenaOptionsPatternZeroSize(t *testing.T) {
	// Test with zero buffer size
	arena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(0))
	require.Equal(t, 0, arena.Cap())

	// Should still be able to allocate
	ptr := arena.Alloc(100, 1)
	require.NotNil(t, ptr)
	require.Equal(t, 100, arena.Len())

	// Check that the new buffer is large enough for the allocation
	monoArena := arena.(*monotonicArena)
	require.Equal(t, 2, len(monoArena.buffers))
	newBuffer := monoArena.buffers[1]
	require.True(t, newBuffer.size >= 100)
}

func TestMonotonicArenaInitialBufferCountOption(t *testing.T) {
	// Test default behavior (should create 1 initial buffer)
	arena := NewMonotonicArena()
	require.Equal(t, 1, len(arena.(*monotonicArena).buffers))
	require.Equal(t, int(minBufferSize), arena.Cap())

	// Test with custom initial buffer count
	arena = NewMonotonicArena(WithInitialBufferCount(3))
	require.Equal(t, 3, len(arena.(*monotonicArena).buffers))
	require.Equal(t, int(minBufferSize)*3, arena.Cap())

	// Test with custom buffer size and count
	arena = NewMonotonicArena(WithInitialBufferCount(2), WithMinBufferSize(512))
	require.Equal(t, 2, len(arena.(*monotonicArena).buffers))
	require.Equal(t, 1024, arena.Cap()) // 512 * 2

	// Test with zero initial buffers
	arena = NewMonotonicArena(WithInitialBufferCount(0))
	require.Equal(t, 0, len(arena.(*monotonicArena).buffers))
	require.Equal(t, 0, arena.Cap())

	// Should still be able to allocate (will create buffers as needed)
	ptr := arena.Alloc(100, 1)
	require.NotNil(t, ptr)
	require.Equal(t, 100, arena.Len())
	require.Equal(t, 1, len(arena.(*monotonicArena).buffers)) // Should have created one buffer
}

func TestMonotonicArenaInitialBufferCountAllocation(t *testing.T) {
	// Test that allocations use existing buffers before creating new ones
	arena := NewMonotonicArena(WithInitialBufferCount(3), WithMinBufferSize(100))
	require.Equal(t, 3, len(arena.(*monotonicArena).buffers))
	require.Equal(t, 300, arena.Cap())

	// Allocate on first buffer
	ptr1 := arena.Alloc(50, 1)
	require.NotNil(t, ptr1)
	require.Equal(t, 50, arena.Len())
	require.Equal(t, 3, len(arena.(*monotonicArena).buffers)) // Should still have 3 buffers

	// Allocate on second buffer
	ptr2 := arena.Alloc(50, 1)
	require.NotNil(t, ptr2)
	require.Equal(t, 100, arena.Len())
	require.Equal(t, 3, len(arena.(*monotonicArena).buffers)) // Should still have 3 buffers

	// Allocate on third buffer
	ptr3 := arena.Alloc(50, 1)
	require.NotNil(t, ptr3)
	require.Equal(t, 150, arena.Len())
	require.Equal(t, 3, len(arena.(*monotonicArena).buffers)) // Should still have 3 buffers

	// Allocate more than fits in existing buffers - should create new buffer
	ptr4 := arena.Alloc(50, 1)
	require.NotNil(t, ptr4)
	require.Equal(t, 200, arena.Len())
	// The third buffer might have enough space, or we might need a fourth buffer
	require.True(t, len(arena.(*monotonicArena).buffers) >= 3)
}

func TestMonotonicArenaInitialBufferCountReset(t *testing.T) {
	// Test that reset works correctly with multiple initial buffers
	arena := NewMonotonicArena(WithInitialBufferCount(2), WithMinBufferSize(100))
	require.Equal(t, 2, len(arena.(*monotonicArena).buffers))

	// Allocate on both buffers
	arena.Alloc(50, 1)
	arena.Alloc(50, 1)
	require.Equal(t, 100, arena.Len())

	// Reset without release
	arena.Reset()
	require.Equal(t, 0, arena.Len())
	require.Equal(t, 2, len(arena.(*monotonicArena).buffers)) // Should still have 2 buffers

	// Reset with release
	arena.Release()
	require.Equal(t, 0, arena.Len())
	require.Equal(t, 2, len(arena.(*monotonicArena).buffers)) // Should still have 2 buffers (but memory released)
}
