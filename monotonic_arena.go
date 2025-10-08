// SPDX-License-Identifier: Apache-2.0

package arena

import (
	"unsafe"
)

type monotonicArena struct {
	buffers            []*monotonicBuffer
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

func (s *monotonicBuffer) alloc(size, alignment uintptr) (unsafe.Pointer, bool) {
	if s.ptr == nil {
		buf := make([]byte, s.size) // allocate monotonic buffer lazily
		s.ptr = unsafe.Pointer(unsafe.SliceData(buf))
	}
	alignOffset := uintptr(0)
	for alignedPtr := uintptr(s.ptr) + s.offset; alignedPtr%alignment != 0; alignedPtr++ {
		alignOffset++
	}
	allocSize := size + alignOffset

	if s.availableBytes() < allocSize {
		return nil, false
	}
	ptr := unsafe.Pointer(uintptr(s.ptr) + s.offset + alignOffset)
	s.offset += allocSize

	// This piece of code will be translated into a runtime.memclrNoHeapPointers
	// invocation by the compiler, which is an assembler optimized implementation.
	// Architecture specific code can be found at src/runtime/memclr_$GOARCH.s
	// in Go source (since https://codereview.appspot.com/137880043).
	b := unsafe.Slice((*byte)(ptr), size)

	for i := range b {
		b[i] = 0
	}

	return ptr, true
}

func (s *monotonicBuffer) reset() {
	if s.offset == 0 {
		return
	}
	s.offset = 0
}

func (s *monotonicBuffer) release() {
	s.offset = 0
	s.ptr = nil
}

func (s *monotonicBuffer) availableBytes() uintptr {
	return s.size - s.offset
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
	for i := 0; i < len(a.buffers); i++ {
		ptr, ok := a.buffers[i].alloc(size, alignment)
		if ok {
			// Update peak if current allocation exceeds it
			currentLen := a.len()
			if currentLen > a.peak {
				a.peak = currentLen
			}
			return ptr
		}
	}

	// No existing buffer has enough space, create a new one
	// Calculate the required size including alignment
	currentLen := a.len()
	alignOffset := uintptr(0)
	if currentLen > 0 {
		for alignedPtr := currentLen; alignedPtr%alignment != 0; alignedPtr++ {
			alignOffset++
		}
	}
	requiredSize := size + alignOffset

	// New buffer should be at least minBuffer, but large enough for the allocation
	newBufferSize := requiredSize
	if newBufferSize < a.minBufferSize {
		newBufferSize = a.minBufferSize
	}

	// Create and add the new buffer
	newBuffer := newMonotonicBuffer(int(newBufferSize))
	a.buffers = append(a.buffers, newBuffer)

	// Allocate on the new buffer
	ptr, ok := newBuffer.alloc(size, alignment)
	if !ok {
		// This should never happen since we just created a buffer large enough
		panic("failed to allocate on newly created buffer")
	}

	// Update peak to account for the new buffer and allocation
	currentLen = a.len()
	if currentLen > a.peak {
		a.peak = currentLen
	}

	return ptr
}

// Reset satisfies the Arena interface.
func (a *monotonicArena) Reset() {
	for _, s := range a.buffers {
		s.reset()
	}
}

// Release satisfies the Arena interface.
func (a *monotonicArena) Release() {
	for _, s := range a.buffers {
		s.release()
	}
}

// len returns the total number of bytes currently allocated in the arena (internal helper).
func (a *monotonicArena) len() uintptr {
	var total uintptr
	for _, s := range a.buffers {
		total += s.offset
	}
	return total
}

// Len returns the total number of bytes currently allocated in the arena.
func (a *monotonicArena) Len() int {
	return int(a.len())
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
