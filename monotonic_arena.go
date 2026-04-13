// SPDX-License-Identifier: Apache-2.0

package arena

import (
	"unsafe"
)

type monotonicArena struct {
	buffers            []*monotonicBuffer
	totalAlloc         uintptr // running sum of s.offset across all buffers; avoids O(buffers) scans on the hot path
	peak               uintptr // tracks peak allocated space
	minBufferSize      uintptr // minimum size for new buffers
	initialBufferCount int     // number of initial buffers to create
}

type monotonicBuffer struct {
	ptr    unsafe.Pointer
	offset uintptr
	size   uintptr
}

func newMonotonicBuffer(size int) *monotonicBuffer {
	return &monotonicBuffer{size: uintptr(size)}
}

// alloc reserves size bytes aligned to alignment and returns a pointer to the
// start of the region along with the total bytes consumed (size + alignment padding).
// The returned memory is guaranteed to be zeroed: freshly allocated buffers come
// from make([]byte, size) (zeroed by Go) and reset() zeroes the used prefix of
// the buffer in a single memclr, so the invariant "bytes at [offset, size) are
// zero" holds at the start of every alloc.
func (s *monotonicBuffer) alloc(size, alignment uintptr) (unsafe.Pointer, uintptr, bool) {
	if s.ptr == nil {
		if s.size > uintptr(maxInt) {
			return nil, 0, false
		}
		buf := make([]byte, s.size) // allocate monotonic buffer lazily
		s.ptr = unsafe.Pointer(unsafe.SliceData(buf))
	}
	// O(1) alignment: round offset up to the next multiple of alignment.
	// Works for any positive alignment (power-of-2 or not).
	offset := s.offset
	if alignment > 1 {
		if rem := (uintptr(s.ptr) + offset) % alignment; rem != 0 {
			offset += alignment - rem
		}
	}
	allocSize := (offset - s.offset) + size
	// Overflow guard: if size is close to MaxUintptr, the addition above
	// can wrap and produce a tiny allocSize, bypassing the bounds check.
	if allocSize < size {
		return nil, 0, false
	}

	if s.size-s.offset < allocSize {
		return nil, 0, false
	}
	ptr := unsafe.Pointer(uintptr(s.ptr) + offset)
	s.offset += allocSize
	// No per-alloc zeroing: the invariant is maintained by make() on lazy
	// buffer creation and by reset() doing a single bulk memclr.
	return ptr, allocSize, true
}

func (s *monotonicBuffer) reset() {
	if s.offset == 0 {
		return
	}
	// Zero the used prefix in one call so that the "bytes at [offset, size)
	// are zero" invariant holds for subsequent allocs without per-alloc
	// zeroing. This is the same total work as zeroing on every alloc, but
	// runtime.memclrNoHeapPointers is dramatically faster on large
	// contiguous ranges than on millions of tiny ones.
	clear(unsafe.Slice((*byte)(s.ptr), s.offset))
	s.offset = 0
}

func (s *monotonicBuffer) release() {
	s.offset = 0
	s.ptr = nil
}

// NewMonotonicArena creates a new monotonic arena with optional configuration.
// If no options are provided, it uses minBufferSize (32KB) as the default buffer size
// and creates 1 initial buffer.
func NewMonotonicArena(opts ...MonotonicArenaOption) Arena {
	a := &monotonicArena{
		minBufferSize:      minBufferSize, // Default to minBufferSize
		initialBufferCount: 1,             // Default to 1 initial buffer
	}

	// Apply options
	for _, opt := range opts {
		opt(a)
	}

	// Create initial buffers using the configured buffer size and count
	for i := 0; i < a.initialBufferCount; i++ {
		a.buffers = append(a.buffers, newMonotonicBuffer(int(a.minBufferSize)))
	}
	return a
}

const (
	minBufferSize = 1024 * 32 // 32KB
	maxInt        = int(^uint(0) >> 1)
)

// MonotonicArenaOption represents a configuration option for a monotonic arena.
type MonotonicArenaOption func(*monotonicArena)

// WithMinBufferSize sets the minimum buffer size for new buffers created by the arena.
func WithMinBufferSize(size int) MonotonicArenaOption {
	return func(a *monotonicArena) {
		a.minBufferSize = uintptr(size)
	}
}

// WithInitialBufferCount sets the number of initial buffers to create.
func WithInitialBufferCount(count int) MonotonicArenaOption {
	return func(a *monotonicArena) {
		a.initialBufferCount = count
	}
}

// Alloc satisfies the Arena interface.
func (a *monotonicArena) Alloc(size, alignment uintptr) unsafe.Pointer {
	// Zero-size allocations are a no-op. Returning nil tells the caller
	// (e.g. AllocateSlice, Allocate[T]) to fall back to the heap (make/new),
	// which keeps checkptr happy. For zero-sized types this means T is heap-
	// backed instead of arena-backed, but since the allocation holds no
	// memory, the lifetime difference is not observable.
	if size == 0 {
		return nil
	}
	for i := 0; i < len(a.buffers); i++ {
		ptr, consumed, ok := a.buffers[i].alloc(size, alignment)
		if ok {
			a.totalAlloc += consumed
			if a.totalAlloc > a.peak {
				a.peak = a.totalAlloc
			}
			return ptr
		}
	}

	// No existing buffer has enough space, create a new one. Reserve
	// (alignment - 1) bytes of margin so the first allocation on the
	// new buffer can always satisfy arbitrary alignment, even when the
	// backing []byte's base pointer is not aligned to `alignment`.
	// make([]byte, n) returns at least 8-byte-aligned memory on 64-bit
	// Go, so for alignment ≤ 8 the margin is wasted — but for callers
	// that pass alignment > 8 (e.g., 16 for SIMD-friendly structs)
	// this is what keeps the subsequent alloc from returning nil.
	newBufferSize := size
	if alignment > 1 {
		newBufferSize += alignment - 1
		// Overflow guard: if the caller passed a size near MaxUintptr,
		// adding the alignment margin wraps and we'd allocate a tiny
		// buffer that appears to satisfy a huge request.
		if newBufferSize < size {
			return nil
		}
	}
	if newBufferSize < a.minBufferSize {
		newBufferSize = a.minBufferSize
	}
	if newBufferSize > uintptr(maxInt) {
		return nil
	}

	newBuffer := newMonotonicBuffer(int(newBufferSize))
	a.buffers = append(a.buffers, newBuffer)

	ptr, consumed, _ := newBuffer.alloc(size, alignment)

	a.totalAlloc += consumed
	if a.totalAlloc > a.peak {
		a.peak = a.totalAlloc
	}

	return ptr
}

// Reset satisfies the Arena interface.
func (a *monotonicArena) Reset() {
	for _, s := range a.buffers {
		s.reset()
	}
	a.totalAlloc = 0
}

// Release satisfies the Arena interface.
func (a *monotonicArena) Release() {
	for _, s := range a.buffers {
		s.release()
	}
	a.totalAlloc = 0
}

// Len returns the total number of bytes currently allocated in the arena.
func (a *monotonicArena) Len() int {
	return int(a.totalAlloc)
}

// Cap returns the total capacity (maximum bytes) that can be allocated in the arena.
func (a *monotonicArena) Cap() int {
	var total uintptr
	for _, s := range a.buffers {
		total += s.size
	}
	return int(total)
}

// Peak returns the peak number of bytes that have been allocated in the arena.
// This value is not reset when Reset is called, allowing tracking of maximum usage.
func (a *monotonicArena) Peak() int {
	return int(a.peak)
}
