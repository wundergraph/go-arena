// SPDX-License-Identifier: Apache-2.0

package arena

import (
	"unsafe"
)

// AllocateSlice creates a slice of type T with a given length and capacity,
// using the provided Arena for memory allocation.
// If the arena is non-nil, it returns a slice with memory allocated from the arena.
// Otherwise, it returns a slice using Go's built-in make function.
func AllocateSlice[T any](a Arena, len, cap int) []T {
	if a != nil {
		var x T
		bufSize := int(unsafe.Sizeof(x)) * cap
		if ptr := (*T)(a.Alloc(uintptr(bufSize), unsafe.Alignof(x))); ptr != nil {
			s := unsafe.Slice(ptr, cap)
			return s[:len]
		}
	}
	return make([]T, len, cap)
}

// SliceAppend appends elements to a slice of type T using a provided Arena
// for memory allocation if needed.
func SliceAppend[T any](a Arena, s []T, data ...T) []T {
	if a == nil {
		return append(s, data...)
	}
	s = growSlice(a, s, len(data))
	s = append(s, data...)
	return s
}

func growSlice[T any](a Arena, s []T, dataLen int) []T {
	newLen := len(s) + dataLen
	if newLen <= cap(s) {
		return s
	}
	// Double the capacity (or fit newLen if that is larger, including when
	// 2*cap overflows). append grows large slices by only 1.25x because the
	// GC frees the old array, but the arena keeps every outgrown array until
	// Reset: 1.25x leaves about 4x the final capacity behind in dead copies,
	// and copies as many bytes; doubling leaves about 1x.
	s2 := AllocateSlice[T](a, len(s), max(2*cap(s), newLen))
	copy(s2, s)
	return s2
}
