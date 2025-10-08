// SPDX-License-Identifier: Apache-2.0

package arena

import (
	"unsafe"
)

const growThreshold = 256

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
	newCap := cap(s)

	if newCap > 0 {
		for newLen > newCap {
			if newCap < growThreshold {
				newCap *= 2
			} else {
				newCap += newCap / 4
			}
		}
	} else {
		newCap = dataLen
	}
	if newCap == cap(s) {
		return s
	}
	s2 := AllocateSlice[T](a, len(s), newCap)
	copy(s2, s)
	return s2
}
