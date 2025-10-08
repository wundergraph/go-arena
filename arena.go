// SPDX-License-Identifier: Apache-2.0

package arena

import (
	"unsafe"
)

// Arena is an interface that describes a memory allocation arena.
type Arena interface {
	// Alloc allocates memory of the given size and returns a pointer to it.
	// The alignment parameter specifies the alignment of the allocated memory.
	Alloc(size, alignment uintptr) unsafe.Pointer

	// Reset resets the arena's state without releasing the underlying memory.
	// After invoking this method any pointer previously returned by Alloc becomes immediately invalid.
	// The arena can be reused for new allocations.
	Reset()

	// Release releases the arena's underlying memory back to the system.
	// After invoking this method, the arena should not be used for further allocations.
	Release()

	// Len returns the total number of bytes currently allocated in the arena.
	Len() int

	// Cap returns the total capacity (maximum bytes) that can be allocated in the arena.
	Cap() int

	// Peak returns the peak number of bytes that have been allocated in the arena.
	// This value is not reset when Reset is called, allowing tracking of maximum usage.
	// Peak is also different from Cap in that it reflects the high-water mark of allocations,
	// whereas Cap reflects the total capacity of the arena.
	// Cap can grow much higher than Peak when buffers have to grow.
	Peak() int
}

// Allocate allocates memory for a value of type T using the provided Arena.
// If the arena is non-nil, it returns a  *T pointer with memory allocated from the arena.
// If passed arena is nil, it allocates memory using Go's built-in new function.
func Allocate[T any](a Arena) *T {
	if a != nil {
		var x T
		if ptr := a.Alloc(unsafe.Sizeof(x), unsafe.Alignof(x)); ptr != nil {
			return (*T)(ptr)
		}
	}
	return new(T)
}
