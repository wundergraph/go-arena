package arena

import (
	"sync"
	"weak"
)

// Pool provides a thread-safe pool of Arena instances for memory-efficient allocations.
// It uses weak pointers to allow garbage collection of unused arenas while maintaining
// a pool of reusable arenas for high-frequency allocation patterns.
//
// by storing PoolItem as weak pointers, the GC can collect them at any time
// before using an PoolItem, we try to get a strong pointer while removing it from the pool
// once we call Release, we turn the item back to the pool and make it a weak pointer again
// this means that at any time, GC can claim back the memory if required,
// allowing GC to automatically manage an appropriate pool size depending on available memory and GC pressure
type Pool struct {
	// pool is a slice of weak pointers to the struct holding the arena.Arena
	pool  []weak.Pointer[PoolItem]
	sizes map[uint64]*arenaPoolItemSize
	mu    sync.Mutex
}

// arenaPoolItemSize is used to track the required memory across the last 50 arenas in the pool
type arenaPoolItemSize struct {
	count      int
	totalBytes int
}

// PoolItem wraps an arena.Arena for use in the pool
type PoolItem struct {
	Arena Arena
	Key   uint64
}

// NewArenaPool creates a new Pool instance
func NewArenaPool() *Pool {
	return &Pool{
		sizes: make(map[uint64]*arenaPoolItemSize),
	}
}

// Acquire gets an arena from the pool or creates a new one if none are available.
// The id parameter is used to track arena sizes per use case for optimization.
func (p *Pool) Acquire(key uint64) *PoolItem {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Try to find an available arena in the pool
	for len(p.pool) > 0 {
		// Pop the last item
		lastIdx := len(p.pool) - 1
		wp := p.pool[lastIdx]
		p.pool = p.pool[:lastIdx]

		v := wp.Value()
		if v != nil {
			v.Key = key
			return v
		}
		// If weak pointer was nil (GC collected), continue to next item
	}

	// No arena available, create a new one
	size := WithMinBufferSize(p.getArenaSize(key))
	return &PoolItem{
		Arena: NewMonotonicArena(size),
		Key:   key,
	}
}

// Release returns an arena to the pool for reuse.
// The peak memory usage is recorded to optimize future arena sizes for this use case.
func (p *Pool) Release(item *PoolItem) {
	peak := item.Arena.Peak()
	item.Arena.Reset()

	p.mu.Lock()
	defer p.mu.Unlock()

	// Record the peak usage for this use case
	if size, ok := p.sizes[item.Key]; ok {
		if size.count == 50 {
			size.count = 1
			size.totalBytes = size.totalBytes / 50
		}
		size.count++
		size.totalBytes += peak
	} else {
		p.sizes[item.Key] = &arenaPoolItemSize{
			count:      1,
			totalBytes: peak,
		}
	}

	item.Key = 0

	// Add the arena back to the pool using a weak pointer
	w := weak.Make(item)
	p.pool = append(p.pool, w)
}

func (p *Pool) ReleaseMany(items []*PoolItem) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, item := range items {

		peak := item.Arena.Peak()
		item.Arena.Reset()

		// Record the peak usage for this use case
		if size, ok := p.sizes[item.Key]; ok {
			if size.count == 50 {
				size.count = 1
				size.totalBytes = size.totalBytes / 50
			}
			size.count++
			size.totalBytes += peak
		} else {
			p.sizes[item.Key] = &arenaPoolItemSize{
				count:      1,
				totalBytes: peak,
			}
		}

		item.Key = 0

		// Add the arena back to the pool using a weak pointer
		w := weak.Make(item)
		p.pool = append(p.pool, w)
	}
}

// getArenaSize returns the optimal arena size for a given use case ID.
// If no size is recorded, it defaults to 1MB.
func (p *Pool) getArenaSize(id uint64) int {
	if size, ok := p.sizes[id]; ok {
		return size.totalBytes / size.count
	}
	return 1024 * 1024 // Default 1MB
}
