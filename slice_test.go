// SPDX-License-Identifier: Apache-2.0

package arena

import (
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
)

// mockArena is a simple implementation of the Arena interface for testing purposes.
// It simply allocates memory using Go's built-in make function.
type mockArena struct{}

func (m *mockArena) Alloc(size, _ uintptr) unsafe.Pointer {
	return unsafe.Pointer(&make([]byte, size)[0])
}

func (m *mockArena) Reset() {
	// Implementation can be empty for this test
}

func (m *mockArena) Release() {
	// Implementation can be empty for this test
}

func (m *mockArena) Len() int {
	// For testing purposes, return 0 as we don't track allocations
	return 0
}

func (m *mockArena) Cap() int {
	// For testing purposes, return a large value as we don't have a real limit
	return int(^uintptr(0) >> 1) // Maximum int value
}

func (m *mockArena) Peak() int {
	// For testing purposes, return 0 as we don't track peak allocations
	return 0
}

// TestSliceAppendWithArena tests the SliceAppend function using a mockArena.
func TestSliceAppendWithArena(t *testing.T) {
	a := &mockArena{}

	s := AllocateSlice[int](a, 3, 3)
	s[0] = 1
	s[1] = 2
	s[2] = 3

	data := []int{4, 5}

	// Append using the mockArena
	result := SliceAppend[int](a, s, data...)

	// Expected slice after appending
	expected := []int{1, 2, 3, 4, 5}

	// Compare the result with the expected slice
	require.Equal(t, expected, result)
}
