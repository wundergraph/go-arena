// SPDX-License-Identifier: Apache-2.0

package arena

import (
	"sync"
	"unsafe"
)

type concurrentArena struct {
	mtx sync.Mutex
	a   Arena
}

// NewConcurrentArena returns an arena that is safe to be accessed concurrently
// from multiple goroutines.
func NewConcurrentArena(a Arena) Arena {
	return &concurrentArena{a: a}
}

// Alloc satisfies the Arena interface.
func (a *concurrentArena) Alloc(size, alignment uintptr) unsafe.Pointer {
	a.mtx.Lock()
	defer a.mtx.Unlock()
	if a.a == nil {
		return nil
	}
	return a.a.Alloc(size, alignment)
}

// Reset satisfies the Arena interface.
func (a *concurrentArena) Reset() {
	a.mtx.Lock()
	defer a.mtx.Unlock()
	if a.a == nil {
		return
	}
	a.a.Reset()
}

// Release satisfies the Arena interface.
func (a *concurrentArena) Release() {
	a.mtx.Lock()
	defer a.mtx.Unlock()
	if a.a == nil {
		return
	}
	a.a.Release()
}

// Len returns the total number of bytes currently allocated in the arena.
func (a *concurrentArena) Len() int {
	a.mtx.Lock()
	defer a.mtx.Unlock()
	if a.a == nil {
		return 0
	}
	return a.a.Len()
}

// Cap returns the total capacity (maximum bytes) that can be allocated in the arena.
func (a *concurrentArena) Cap() int {
	a.mtx.Lock()
	defer a.mtx.Unlock()
	if a.a == nil {
		return 0
	}
	return a.a.Cap()
}

// Peak returns the peak number of bytes that have been allocated in the arena.
// This value is not reset when Reset is called, allowing tracking of maximum usage.
func (a *concurrentArena) Peak() int {
	a.mtx.Lock()
	defer a.mtx.Unlock()
	if a.a == nil {
		return 0
	}
	return a.a.Peak()
}
