// SPDX-License-Identifier: Apache-2.0

package arena

import (
	"sync"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
)

func TestConcurrentArenaLen(t *testing.T) {
	// Create a concurrent arena wrapping a monotonic arena
	baseArena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(1024))
	arena := NewConcurrentArena(baseArena)

	require.Equal(t, 0, arena.Len())

	// Allocate some memory
	ptr1 := arena.Alloc(100, 1)
	require.NotNil(t, ptr1)
	require.Equal(t, 100, arena.Len())

	// Allocate more memory
	ptr2 := arena.Alloc(200, 1)
	require.NotNil(t, ptr2)
	require.Equal(t, 300, arena.Len())
}

func TestConcurrentArenaCap(t *testing.T) {
	// Create a concurrent arena wrapping a monotonic arena
	baseArena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(1024))
	arena := NewConcurrentArena(baseArena)

	require.Equal(t, 1024, arena.Cap())

	// Test with multiple buffers
	baseArena = NewMonotonicArena(WithInitialBufferCount(3), WithMinBufferSize(512))
	arena = NewConcurrentArena(baseArena)
	require.Equal(t, 1536, arena.Cap()) // 512 * 3
}

func TestConcurrentArenaLenCapAfterReset(t *testing.T) {
	baseArena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(1024))
	arena := NewConcurrentArena(baseArena)

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

func TestConcurrentArenaConcurrentAccess(t *testing.T) {
	baseArena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(1024*1024)) // Large arena for concurrent access
	arena := NewConcurrentArena(baseArena)

	const numGoroutines = 10
	const allocationsPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Start multiple goroutines that allocate memory concurrently
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < allocationsPerGoroutine; j++ {
				ptr := arena.Alloc(10, 1)
				require.NotNil(t, ptr)
			}
		}()
	}

	wg.Wait()

	// Verify total allocation
	expectedLen := numGoroutines * allocationsPerGoroutine * 10
	require.Equal(t, expectedLen, arena.Len())
	require.Equal(t, 1024*1024, arena.Cap())
}

func TestConcurrentArenaConcurrentLenCap(t *testing.T) {
	baseArena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(1024*1024))
	arena := NewConcurrentArena(baseArena)

	const numGoroutines = 10
	const operationsPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Start multiple goroutines that call Len() and Cap() concurrently
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < operationsPerGoroutine; j++ {
				// Allocate some memory
				ptr := arena.Alloc(10, 1)
				require.NotNil(t, ptr)

				// Check Len and Cap
				len := arena.Len()
				cap := arena.Cap()
				require.True(t, len > 0)
				require.Equal(t, 1024*1024, cap)
			}
		}()
	}

	wg.Wait()
}

func TestConcurrentArenaWithTypes(t *testing.T) {
	baseArena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(1024))
	arena := NewConcurrentArena(baseArena)

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

func TestConcurrentArenaWrappingNil(t *testing.T) {
	// Test wrapping a nil arena (should not panic)
	arena := NewConcurrentArena(nil)

	// These should not panic and should return safe default values
	ptr := arena.Alloc(10, 1)
	require.Nil(t, ptr)

	len := arena.Len()
	require.Equal(t, 0, len)

	cap := arena.Cap()
	require.Equal(t, 0, cap)

	// Reset should not panic
	arena.Release()
}

func TestConcurrentArenaWrappingMockArena(t *testing.T) {
	// Test wrapping a mock arena
	mockArena := &mockArena{}
	arena := NewConcurrentArena(mockArena)

	require.Equal(t, 0, arena.Len())
	require.Equal(t, int(^uintptr(0)>>1), arena.Cap()) // Maximum int value

	// Allocate some memory
	ptr := arena.Alloc(100, 1)
	require.NotNil(t, ptr)

	// Len should still be 0 for mock arena
	require.Equal(t, 0, arena.Len())
	require.Equal(t, int(^uintptr(0)>>1), arena.Cap())
}

func BenchmarkConcurrentArenaLen(b *testing.B) {
	baseArena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(1024*1024))
	arena := NewConcurrentArena(baseArena)

	// Pre-allocate some memory
	for i := 0; i < 1000; i++ {
		arena.Alloc(100, 1)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = arena.Len()
	}
}

func BenchmarkConcurrentArenaCap(b *testing.B) {
	baseArena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(1024*1024))
	arena := NewConcurrentArena(baseArena)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = arena.Cap()
	}
}

func BenchmarkConcurrentArenaAlloc(b *testing.B) {
	baseArena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(1024*1024))
	arena := NewConcurrentArena(baseArena)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ptr := arena.Alloc(100, 1)
		_ = ptr
	}
}

func TestConcurrentArenaPeak(t *testing.T) {
	// Create a concurrent arena wrapping a monotonic arena
	baseArena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(1024))
	arena := NewConcurrentArena(baseArena)

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

func TestConcurrentArenaPeakAfterReset(t *testing.T) {
	baseArena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(1024))
	arena := NewConcurrentArena(baseArena)

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

func TestConcurrentArenaPeakConcurrentAccess(t *testing.T) {
	baseArena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(1024*1024)) // Large arena for concurrent access
	arena := NewConcurrentArena(baseArena)

	const numGoroutines = 10
	const allocationsPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Start multiple goroutines that allocate memory concurrently
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < allocationsPerGoroutine; j++ {
				ptr := arena.Alloc(10, 1)
				require.NotNil(t, ptr)
			}
		}()
	}

	wg.Wait()

	// Verify peak allocation
	expectedPeak := numGoroutines * allocationsPerGoroutine * 10
	require.Equal(t, expectedPeak, arena.Peak())
	require.Equal(t, expectedPeak, arena.Len())
}

func TestConcurrentArenaPeakConcurrentPeakAccess(t *testing.T) {
	baseArena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(1024*1024))
	arena := NewConcurrentArena(baseArena)

	const numGoroutines = 10
	const operationsPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Start multiple goroutines that call Peak() concurrently
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < operationsPerGoroutine; j++ {
				// Allocate some memory
				ptr := arena.Alloc(10, 1)
				require.NotNil(t, ptr)

				// Check Peak
				peak := arena.Peak()
				require.True(t, peak > 0)
			}
		}()
	}

	wg.Wait()
}

func TestConcurrentArenaPeakWrappingNil(t *testing.T) {
	// Test wrapping a nil arena (should not panic)
	arena := NewConcurrentArena(nil)

	// Peak should not panic and should return safe default value
	peak := arena.Peak()
	require.Equal(t, 0, peak)
}

func TestConcurrentArenaPeakWrappingMockArena(t *testing.T) {
	// Test wrapping a mock arena
	mockArena := &mockArena{}
	arena := NewConcurrentArena(mockArena)

	require.Equal(t, 0, arena.Peak())

	// Allocate some memory
	ptr := arena.Alloc(100, 1)
	require.NotNil(t, ptr)

	// Peak should still be 0 for mock arena
	require.Equal(t, 0, arena.Peak())
}

func BenchmarkConcurrentArenaPeak(b *testing.B) {
	baseArena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(1024*1024))
	arena := NewConcurrentArena(baseArena)

	// Pre-allocate some memory to set a peak
	for i := 0; i < 1000; i++ {
		arena.Alloc(100, 1)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = arena.Peak()
	}
}

func TestConcurrentArenaPeakOnAllocationFailure(t *testing.T) {
	// Create a concurrent arena with small capacity to force allocation failures
	baseArena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(100)) // Only 100 bytes capacity
	arena := NewConcurrentArena(baseArena)
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

func TestConcurrentArenaPeakOnAllocationFailureWithAlignment(t *testing.T) {
	// Create a concurrent arena with small capacity
	baseArena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(100))
	arena := NewConcurrentArena(baseArena)
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

func TestConcurrentArenaPeakOnAllocationFailureConcurrentAccess(t *testing.T) {
	baseArena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(100)) // Small capacity
	arena := NewConcurrentArena(baseArena)

	const numGoroutines = 5
	const allocationsPerGoroutine = 20

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Start multiple goroutines that try to allocate memory concurrently
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < allocationsPerGoroutine; j++ {
				ptr := arena.Alloc(10, 1)
				// Some allocations will succeed, some will fail
				_ = ptr
			}
		}()
	}

	wg.Wait()

	// Verify that peak reflects the maximum allocation
	// All allocations should succeed by creating new buffers as needed
	require.True(t, arena.Peak() >= 100)
	// Length should reflect all successful allocations
	totalExpected := numGoroutines * allocationsPerGoroutine * 10
	require.Equal(t, totalExpected, arena.Len())
}

func TestConcurrentArenaPeakOnAllocationFailureAfterReset(t *testing.T) {
	baseArena := NewMonotonicArena(WithInitialBufferCount(1), WithMinBufferSize(100))
	arena := NewConcurrentArena(baseArena)
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

func TestConcurrentArenaPeakOnAllocationFailureWrappingNil(t *testing.T) {
	// Test wrapping a nil arena (should not panic)
	arena := NewConcurrentArena(nil)

	// Peak should not panic and should return safe default value
	peak := arena.Peak()
	require.Equal(t, 0, peak)

	// Failed allocation should not panic
	ptr := arena.Alloc(100, 1)
	require.Nil(t, ptr)
	require.Equal(t, 0, arena.Len())
	require.Equal(t, 0, arena.Peak()) // Peak should remain 0 for nil arena
}
