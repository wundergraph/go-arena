package arena

import (
	"sync"
	"sync/atomic"
)

// Pool provides a thread-safe pool of Arena instances for memory-efficient allocations.
// Each Pool keeps a separate idle queue, while all queues share a bounded manager.
// Unlike weak pointers, taking an arena cannot wait for an in-progress GC.
type Pool struct {
	pool *idleArenaPool

	sizesMu sync.Mutex
	sizes   map[uint64]*arenaPoolItemSize
}

const (
	defaultArenaSize         = 1024 * 1024
	maxRetainedArenaCapacity = 4 * defaultArenaSize
	maxRetainedArenaBytes    = 64 * defaultArenaSize // Process-wide idle arena bound.
)

type idleArenaManager struct {
	mu sync.Mutex

	oldest *PoolItem
	newest *PoolItem

	retainedBytes int64
	maxBytes      int64
}

type idleArenaPool struct {
	manager *idleArenaManager

	oldest *PoolItem
	newest *PoolItem
	count  int
}

var sharedIdleArenaManager = newIdleArenaManager(maxRetainedArenaBytes)

// arenaPoolItemSize is used to track the required memory across the last 50 arenas in the pool
type arenaPoolItemSize struct {
	count      int
	totalBytes int
}

// PoolItem wraps an arena.Arena for use in the pool.
// It must not be copied and must be released exactly once to the Pool that acquired it.
type PoolItem struct {
	Arena Arena
	Key   uint64

	owner     *PoolItem
	leasePool *Pool
	inPool    atomic.Bool

	idlePool         *idleArenaPool
	poolOlder        *PoolItem
	poolNewer        *PoolItem
	globalOlder      *PoolItem
	globalNewer      *PoolItem
	retainedCapacity int
}

// NewArenaPool creates a new Pool instance
func NewArenaPool() *Pool {
	return newArenaPool(newIdleArenaPool(sharedIdleArenaManager))
}

func newArenaPool(idleArenas *idleArenaPool) *Pool {
	return &Pool{
		pool:  idleArenas,
		sizes: make(map[uint64]*arenaPoolItemSize),
	}
}

func newIdleArenaManager(maxBytes int) *idleArenaManager {
	return &idleArenaManager{maxBytes: int64(maxBytes)}
}

func newIdleArenaPool(manager *idleArenaManager) *idleArenaPool {
	return &idleArenaPool{manager: manager}
}

func (p *idleArenaPool) get() *PoolItem {
	p.manager.mu.Lock()
	item := p.newest
	if item != nil {
		p.remove(item)
		p.manager.remove(item)
		p.manager.retainedBytes -= int64(item.retainedCapacity)
		item.retainedCapacity = 0
	}
	p.manager.mu.Unlock()
	return item
}

func (p *idleArenaPool) put(item *PoolItem, capacity int) bool {
	if int64(capacity) > p.manager.maxBytes {
		return false
	}
	item.Arena.Reset()

	p.manager.mu.Lock()
	var evicted *PoolItem
	for int64(capacity) > p.manager.maxBytes-p.manager.retainedBytes {
		victim := p.manager.oldest
		if victim == nil {
			break
		}
		victim.idlePool.remove(victim)
		p.manager.remove(victim)
		p.manager.retainedBytes -= int64(victim.retainedCapacity)
		victim.retainedCapacity = 0
		victim.globalNewer = evicted
		evicted = victim
	}

	retained := int64(capacity) <= p.manager.maxBytes-p.manager.retainedBytes
	if retained {
		item.retainedCapacity = capacity
		item.idlePool = p
		p.append(item)
		p.manager.append(item)
		p.manager.retainedBytes += int64(capacity)
	}
	p.manager.mu.Unlock()

	for evicted != nil {
		next := evicted.globalNewer
		evicted.globalNewer = nil
		evicted.Arena.Release()
		evicted = next
	}
	return retained
}

func (p *idleArenaPool) append(item *PoolItem) {
	item.poolOlder = p.newest
	if p.newest == nil {
		p.oldest = item
	} else {
		p.newest.poolNewer = item
	}
	p.newest = item
	p.count++
}

func (p *idleArenaPool) remove(item *PoolItem) {
	if item.poolOlder == nil {
		p.oldest = item.poolNewer
	} else {
		item.poolOlder.poolNewer = item.poolNewer
	}
	if item.poolNewer == nil {
		p.newest = item.poolOlder
	} else {
		item.poolNewer.poolOlder = item.poolOlder
	}
	item.idlePool = nil
	item.poolOlder = nil
	item.poolNewer = nil
	p.count--
}

func (p *idleArenaPool) len() int {
	p.manager.mu.Lock()
	defer p.manager.mu.Unlock()
	return p.count
}

func (m *idleArenaManager) append(item *PoolItem) {
	item.globalOlder = m.newest
	if m.newest == nil {
		m.oldest = item
	} else {
		m.newest.globalNewer = item
	}
	m.newest = item
}

func (m *idleArenaManager) remove(item *PoolItem) {
	if item.globalOlder == nil {
		m.oldest = item.globalNewer
	} else {
		item.globalOlder.globalNewer = item.globalNewer
	}
	if item.globalNewer == nil {
		m.newest = item.globalOlder
	} else {
		item.globalNewer.globalOlder = item.globalOlder
	}
	item.globalOlder = nil
	item.globalNewer = nil
}

func (m *idleArenaManager) retained() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.retainedBytes
}

// Acquire gets an arena from the pool or creates a new one if none are available.
// The id parameter is used to track arena sizes per use case for optimization.
func (p *Pool) Acquire(key uint64) *PoolItem {
	if item := p.pool.get(); item != nil {
		item.inPool.Store(false)
		item.Key = key
		item.leasePool = p
		return item
	}

	size := WithMinBufferSize(p.getArenaSize(key))
	item := &PoolItem{
		Arena:     NewMonotonicArena(size),
		Key:       key,
		leasePool: p,
	}
	item.owner = item
	return item
}

// Release returns an arena to the pool for reuse.
// The memory usage of this checkout is recorded to optimize future arena sizes.
func (p *Pool) Release(item *PoolItem) {
	// PoolItem is a lease: only the exact pointer returned by Acquire may be
	// released exactly once through the Pool that acquired it.
	if item.owner != item || item.leasePool != p || !item.inPool.CompareAndSwap(false, true) {
		return
	}
	if _, ok := item.Arena.(*monotonicArena); !ok {
		item.Arena.Release()
		return
	}

	key := item.Key
	usage := item.Arena.Len()
	capacity := item.Arena.Cap()
	item.Key = 0
	item.leasePool = nil

	p.sizesMu.Lock()
	p.recordArenaSize(key, usage)
	p.sizesMu.Unlock()

	if capacity > maxRetainedArenaCapacity {
		item.Arena.Release()
		return
	}

	if !p.pool.put(item, capacity) {
		item.Arena.Release()
	}
}

func (p *Pool) ReleaseMany(items []*PoolItem) {
	for _, item := range items {
		p.Release(item)
	}
}

// getArenaSize returns the optimal arena size for a given use case ID.
// If no size is recorded, it defaults to 1MB.
func (p *Pool) getArenaSize(id uint64) int {
	p.sizesMu.Lock()
	defer p.sizesMu.Unlock()

	if size, ok := p.sizes[id]; ok {
		average := size.totalBytes / size.count
		if average < minBufferSize {
			return minBufferSize
		}
		if average > maxRetainedArenaCapacity {
			return maxRetainedArenaCapacity
		}
		return average
	}
	return defaultArenaSize
}

// recordArenaSize updates the moving average for a use case.
// The caller must hold sizesMu.
func (p *Pool) recordArenaSize(key uint64, usage int) {
	if size, ok := p.sizes[key]; ok {
		if size.count == 50 {
			size.count = 1
			size.totalBytes = size.totalBytes / 50
		}
		size.count++
		size.totalBytes += usage
		return
	}
	p.sizes[key] = &arenaPoolItemSize{
		count:      1,
		totalBytes: usage,
	}
}
