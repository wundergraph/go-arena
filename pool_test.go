package arena

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewArenaPool(t *testing.T) {
	pool := NewArenaPool()
	otherPool := NewArenaPool()

	require.NotNil(t, pool, "NewArenaPool returned nil")
	assert.Same(t, sharedIdleArenaManager, pool.pool.manager, "all pools must share the process-wide idle bound")
	assert.Same(t, pool.pool.manager, otherPool.pool.manager)
	assert.NotSame(t, pool.pool, otherPool.pool, "different workloads must keep separate idle queues")
	assert.Equal(t, int64(maxRetainedArenaBytes), pool.pool.manager.maxBytes)
	assert.Len(t, pool.sizes, 0, "expected empty sizes map")
}

func newTestArenaPool(maxBytes int) *Pool {
	return newArenaPool(newIdleArenaPool(newIdleArenaManager(maxBytes)))
}

type poolTestArena struct {
	length   int
	capacity int
	reset    bool
	released bool
}

func (a *poolTestArena) Alloc(_, _ uintptr) unsafe.Pointer { return nil }
func (a *poolTestArena) Reset()                            { a.reset = true }
func (a *poolTestArena) Release()                          { a.released = true }
func (a *poolTestArena) Len() int                          { return a.length }
func (a *poolTestArena) Cap() int                          { return a.capacity }
func (a *poolTestArena) Peak() int                         { return a.length }

func TestArenaPool_Acquire_EmptyPool(t *testing.T) {
	pool := newTestArenaPool(maxRetainedArenaBytes)

	item := pool.Acquire(1)

	require.NotNil(t, item, "Acquire returned nil")
	assert.NotNil(t, item.Arena, "Arena is nil")

	// Verify we can use the arena
	buf := NewArenaBuffer(item.Arena)
	_, err := buf.WriteString("test")
	assert.NoError(t, err)
}

func TestArenaPool_ReleaseAndAcquire(t *testing.T) {
	pool := newTestArenaPool(maxRetainedArenaBytes)
	id := uint64(42)

	// Acquire first arena
	item1 := pool.Acquire(id)

	// Use the arena
	buf := NewArenaBuffer(item1.Arena)
	_, err := buf.WriteString("test data")
	assert.NoError(t, err)

	// Release it
	pool.Release(item1)
	require.Zero(t, item1.Arena.Len(), "released arena must be reset")
	require.Zero(t, item1.Key, "released item must not retain its use case key")

	// Acquire from pool
	item2 := pool.Acquire(id)

	require.NotNil(t, item2, "Acquire returned nil")

	// The acquired arena should be reset and usable
	buf2 := NewArenaBuffer(item2.Arena)
	_, err = buf2.WriteString("new data")
	assert.NoError(t, err)

	assert.Equal(t, "new data", buf2.String())
}

func TestArenaPool_Acquire_ReusesIdleItem(t *testing.T) {
	pool := newTestArenaPool(defaultArenaSize)
	const id uint64 = 800

	available := pool.Acquire(1)
	pool.Release(available)

	item := pool.Acquire(id)

	require.Same(t, available, item)
	require.Equal(t, id, item.Key)
	require.Zero(t, pool.pool.len())
	require.Zero(t, pool.pool.manager.retained())
	pool.Release(item)
}

func TestArenaPool_Acquire_ReusesIdleItemAfterGC(t *testing.T) {
	pool := newTestArenaPool(maxRetainedArenaBytes)
	const id uint64 = 801

	item := pool.Acquire(id)
	pool.Release(item)
	runtime.GC()
	runtime.GC()

	acquired := pool.Acquire(id)
	require.Same(t, item, acquired)
	pool.Release(acquired)
}

func TestArenaPool_Release_UsageTracking(t *testing.T) {
	pool := newTestArenaPool(maxRetainedArenaBytes)
	id := uint64(200)

	// First arena
	item1 := pool.Acquire(id)
	buf1 := NewArenaBuffer(item1.Arena)
	_, err := buf1.WriteString("small")
	assert.NoError(t, err)

	usage1 := item1.Arena.Len()
	assert.Equal(t, 5, usage1)

	pool.Release(item1)

	// Check that size was tracked
	size, exists := pool.sizes[id]
	require.True(t, exists, "size tracking not created")
	assert.Equal(t, 1, size.count, "expected count 1")

	// Second arena
	item2 := pool.Acquire(id)
	buf2 := NewArenaBuffer(item2.Arena)
	_, err = buf2.WriteString("larger data")
	assert.NoError(t, err)

	pool.Release(item2)

	// Check updated tracking
	assert.Equal(t, 2, size.count, "expected count 2")
}

func TestArenaPool_Release_DoesNotCarryUsageAcrossKeys(t *testing.T) {
	pool := newTestArenaPool(defaultArenaSize)

	large := pool.Acquire(201)
	largeBuffer := NewArenaBuffer(large.Arena)
	_, err := largeBuffer.WriteString("large usage")
	require.NoError(t, err)
	require.Equal(t, 11, large.Arena.Len())
	pool.Release(large)

	small := pool.Acquire(202)
	require.Same(t, large, small, "test requires cross-key arena reuse")
	smallBuffer := NewArenaBuffer(small.Arena)
	_, err = smallBuffer.WriteString("x")
	require.NoError(t, err)
	pool.Release(small)

	require.Equal(t, 11, pool.sizes[201].totalBytes)
	require.Equal(t, 1, pool.sizes[202].totalBytes)
}

func TestArenaPool_Release_DropsOversizedArena(t *testing.T) {
	pool := newTestArenaPool(maxRetainedArenaBytes)
	item := pool.Acquire(203)

	ptr := item.Arena.Alloc(maxRetainedArenaCapacity+1, 1)
	require.NotNil(t, ptr)
	require.Greater(t, item.Arena.Cap(), maxRetainedArenaCapacity)

	pool.Release(item)

	require.Zero(t, pool.pool.len())
	require.Zero(t, pool.pool.manager.retained())
	require.Equal(t, maxRetainedArenaCapacity, pool.getArenaSize(203), "learned initial size must be capped")
	for _, buffer := range item.Arena.(*monotonicArena).buffers {
		require.Nil(t, buffer.ptr, "oversized arena buffers must be released")
	}
}

func TestArenaPool_Release_EvictsOldestArenaWhenIdleBudgetIsFull(t *testing.T) {
	pool := newTestArenaPool(defaultArenaSize)
	first := pool.Acquire(204)
	second := pool.Acquire(204)
	require.NotNil(t, first.Arena.Alloc(1, 1))
	require.NotNil(t, second.Arena.Alloc(1, 1))

	pool.Release(first)
	pool.Release(second)

	require.Equal(t, 1, pool.pool.len())
	require.Equal(t, int64(defaultArenaSize), pool.pool.manager.retained())
	require.Nil(t, first.Arena.(*monotonicArena).buffers[0].ptr, "oldest idle arena must be evicted")
	require.NotNil(t, second.Arena.(*monotonicArena).buffers[0].ptr, "newest arena must be retained")
	require.Same(t, second, pool.Acquire(204))
}

func TestArenaPool_IdleCapacityIsSharedAcrossPools(t *testing.T) {
	manager := newIdleArenaManager(maxRetainedArenaBytes)
	firstIdleArenas := newIdleArenaPool(manager)
	secondIdleArenas := newIdleArenaPool(manager)
	firstPool := newArenaPool(firstIdleArenas)
	secondPool := newArenaPool(secondIdleArenas)
	maxDefaultArenas := maxRetainedArenaBytes / defaultArenaSize
	items := make([]*PoolItem, maxDefaultArenas+1)

	for index := range items {
		if index%2 == 0 {
			items[index] = firstPool.Acquire(uint64(index))
		} else {
			items[index] = secondPool.Acquire(uint64(index))
		}
	}
	require.NotNil(t, items[0].Arena.Alloc(1, 1))
	require.NotNil(t, items[maxDefaultArenas].Arena.Alloc(1, 1))

	for index := range items {
		if index%2 == 0 {
			firstPool.Release(items[index])
		} else {
			secondPool.Release(items[index])
		}
	}

	require.Equal(t, maxDefaultArenas, firstIdleArenas.len()+secondIdleArenas.len())
	require.Equal(t, int64(maxRetainedArenaBytes), manager.retained())
	retainedCapacity := 0
	for _, idleArenas := range []*idleArenaPool{firstIdleArenas, secondIdleArenas} {
		for {
			item := idleArenas.get()
			if item == nil {
				break
			}
			retainedCapacity += item.Arena.Cap()
		}
	}
	require.LessOrEqual(t, retainedCapacity, maxRetainedArenaBytes)
	require.Zero(t, manager.retained())
	require.Nil(t, items[0].idlePool)
	require.Zero(t, items[0].retainedCapacity)
	for _, buffer := range items[0].Arena.(*monotonicArena).buffers {
		require.Nil(t, buffer.ptr, "the oldest item above the process-wide byte bound must be evicted")
	}
}

func TestIdleArenaManager_EvictsMultipleOldestItemsAcrossPools(t *testing.T) {
	manager := newIdleArenaManager(6 * defaultArenaSize)
	firstIdleArenas := newIdleArenaPool(manager)
	secondIdleArenas := newIdleArenaPool(manager)
	largeIdleArenas := newIdleArenaPool(manager)
	idleArenas := []*idleArenaPool{firstIdleArenas, secondIdleArenas, largeIdleArenas}

	smallItems := make([]*PoolItem, 6)
	for index := range smallItems {
		smallItems[index] = &PoolItem{Arena: &poolTestArena{capacity: defaultArenaSize}}
		pool := idleArenas[index%2]
		require.True(t, pool.put(smallItems[index], defaultArenaSize))
	}
	largeArena := &poolTestArena{capacity: maxRetainedArenaCapacity}
	largeItem := &PoolItem{Arena: largeArena}
	require.True(t, largeIdleArenas.put(largeItem, maxRetainedArenaCapacity))

	for index, item := range smallItems {
		arena := item.Arena.(*poolTestArena)
		if index < 4 {
			require.True(t, arena.released, "oldest item %d must be evicted", index)
			require.Nil(t, item.idlePool)
			require.Zero(t, item.retainedCapacity)
		} else {
			require.False(t, arena.released, "newer item %d must remain idle", index)
		}
	}
	require.False(t, largeArena.released)
	require.Equal(t, 1, firstIdleArenas.len())
	require.Equal(t, 1, secondIdleArenas.len())
	require.Equal(t, 1, largeIdleArenas.len())
	assertIdleArenaManagerState(t, manager, idleArenas...)
}

func TestArenaPool_DoesNotReuseIdleItemsAcrossPools(t *testing.T) {
	manager := newIdleArenaManager(2 * defaultArenaSize)
	firstPool := newArenaPool(newIdleArenaPool(manager))
	secondPool := newArenaPool(newIdleArenaPool(manager))

	firstItem := firstPool.Acquire(1)
	firstPool.Release(firstItem)
	secondItem := secondPool.Acquire(2)

	require.NotSame(t, firstItem, secondItem)
	require.Equal(t, 1, firstPool.pool.len())
	require.Zero(t, secondPool.pool.len())
	secondPool.Release(secondItem)
	require.Equal(t, int64(2*defaultArenaSize), manager.retained())
}

func TestArenaPool_Release_DoesNotShareForeignArena(t *testing.T) {
	pool := newTestArenaPool(maxRetainedArenaBytes)
	foreign := &poolTestArena{length: 1, capacity: 1}

	pool.Release(&PoolItem{Arena: foreign, Key: 205})

	require.False(t, foreign.released, "foreign arenas remain owned by their caller")
	require.Zero(t, pool.pool.len())
	require.Empty(t, pool.sizes)
}

func TestArenaPool_Release_RejectsCopyWrongPoolAndImmediateDuplicate(t *testing.T) {
	manager := newIdleArenaManager(2 * defaultArenaSize)
	pool := newArenaPool(newIdleArenaPool(manager))
	otherPool := newArenaPool(newIdleArenaPool(manager))
	item := pool.Acquire(206)
	itemCopy := &PoolItem{Arena: item.Arena, Key: item.Key, owner: item.owner, leasePool: item.leasePool}

	pool.Release(itemCopy)
	require.Zero(t, pool.pool.len(), "a shallow copy must not be published")
	otherPool.Release(item)
	require.Zero(t, pool.pool.len(), "an item must be returned through the pool that acquired it")

	pool.Release(item)
	pool.Release(item)
	require.Equal(t, 1, pool.pool.len(), "an item must be published at most once")
}

func TestArenaPool_GetArenaSize(t *testing.T) {
	pool := newTestArenaPool(maxRetainedArenaBytes)

	// Test default size for unknown ID
	size1 := pool.getArenaSize(999)
	expectedDefault := defaultArenaSize
	assert.Equal(t, expectedDefault, size1, "expected default size")

	// Test calculated size after usage
	id := uint64(400)
	item := pool.Acquire(id)
	buf := NewArenaBuffer(item.Arena)
	_, err := buf.WriteString("some data")
	assert.NoError(t, err)
	pool.Release(item)

	size2 := pool.getArenaSize(id)
	assert.Equal(t, minBufferSize, size2, "learned size must preserve the arena minimum")
}

func TestArenaPool_MultipleItemsInPool(t *testing.T) {
	pool := newTestArenaPool(3 * defaultArenaSize)
	id := uint64(500)

	// Acquire multiple distinct items
	numItems := 3
	items := make([]*PoolItem, numItems)

	for i := 0; i < numItems; i++ {
		items[i] = pool.Acquire(id)
		buf := NewArenaBuffer(items[i].Arena)
		_, err := buf.WriteString("data")
		assert.NoError(t, err)
	}

	// Release all while keeping references
	for i := 0; i < numItems; i++ {
		pool.Release(items[i])
	}

	for _, item := range items {
		require.Zero(t, item.Arena.Len(), "released arena must be reset")
		require.Zero(t, item.Key, "released item must not retain its use case key")
	}

	acquired := make(map[*PoolItem]struct{}, numItems)
	for i := 0; i < numItems; i++ {
		item := pool.Acquire(id)
		require.NotNil(t, item)
		require.NotNil(t, item.Arena)
		acquired[item] = struct{}{}
	}
	require.Len(t, acquired, numItems, "idle items must have unique ownership")
}

func TestArenaPool_ReleaseMany(t *testing.T) {
	pool := newTestArenaPool(3 * defaultArenaSize)
	items := []*PoolItem{pool.Acquire(1), pool.Acquire(1), pool.Acquire(2)}
	for index, item := range items {
		buf := NewArenaBuffer(item.Arena)
		_, err := buf.WriteString([]string{"a", "bb", "ccc"}[index])
		require.NoError(t, err)
	}

	pool.ReleaseMany(items)

	require.Equal(t, 2, pool.sizes[1].count)
	require.Equal(t, 3, pool.sizes[1].totalBytes)
	require.Equal(t, 1, pool.sizes[2].count)
	require.Equal(t, 3, pool.sizes[2].totalBytes)
	require.Equal(t, len(items), pool.pool.len())
	require.Equal(t, int64(len(items)*defaultArenaSize), pool.pool.manager.retained())
	for _, item := range items {
		require.Zero(t, item.Arena.Len(), "released arena must be reset")
		require.Zero(t, item.Key, "released item must not retain its use case key")
	}
}

func TestArenaPool_ConcurrentSharedPoolsWithGC(t *testing.T) {
	manager := newIdleArenaManager(2 * defaultArenaSize)
	pools := []*Pool{
		newArenaPool(newIdleArenaPool(manager)),
		newArenaPool(newIdleArenaPool(manager)),
		newArenaPool(newIdleArenaPool(manager)),
		newArenaPool(newIdleArenaPool(manager)),
	}
	const workers = 32
	const iterations = 50

	errors := make(chan error, workers)
	var inUse sync.Map
	var workersDone sync.WaitGroup
	workersDone.Add(workers)

	start := make(chan struct{})
	for worker := range workers {
		go func() {
			defer workersDone.Done()
			<-start
			pool := pools[worker%len(pools)]
			for range iterations {
				items := []*PoolItem{pool.Acquire(uint64(worker)), pool.Acquire(uint64(worker + workers))}
				for _, item := range items {
					if _, loaded := inUse.LoadOrStore(item, struct{}{}); loaded {
						errors <- fmt.Errorf("pool item %p acquired by multiple goroutines", item)
						return
					}
					if item.Arena.Alloc(64*1024, 8) == nil {
						errors <- fmt.Errorf("arena allocation failed")
						return
					}
				}
				for _, item := range items {
					inUse.Delete(item)
				}
				pool.ReleaseMany(items)
			}
		}()
	}

	gcDone := make(chan struct{})
	go func() {
		defer close(gcDone)
		<-start
		for range 20 {
			runtime.GC()
		}
	}()

	close(start)
	workersDone.Wait()
	<-gcDone
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}

	retainedBytes := manager.retained()
	require.GreaterOrEqual(t, retainedBytes, int64(0))
	require.LessOrEqual(t, retainedBytes, manager.maxBytes)
	idleArenas := make([]*idleArenaPool, len(pools))
	for index, pool := range pools {
		idleArenas[index] = pool.pool
	}
	assertIdleArenaManagerState(t, manager, idleArenas...)
	retainedCapacity := int64(0)
	for _, pool := range pools {
		for {
			item := pool.pool.get()
			if item == nil {
				break
			}
			retainedCapacity += int64(item.Arena.Cap())
			item.Arena.Release()
		}
	}
	require.Equal(t, retainedBytes, retainedCapacity)
	require.Zero(t, manager.retained())
}

func assertIdleArenaManagerState(t *testing.T, manager *idleArenaManager, pools ...*idleArenaPool) {
	t.Helper()
	manager.mu.Lock()
	defer manager.mu.Unlock()

	globalItems := make(map[*PoolItem]struct{})
	retainedCapacity := int64(0)
	var previous *PoolItem
	for item := manager.oldest; item != nil; item = item.globalNewer {
		_, exists := globalItems[item]
		require.False(t, exists, "global idle list must not contain a cycle or duplicate item")
		require.Same(t, previous, item.globalOlder)
		require.NotNil(t, item.idlePool)
		require.Positive(t, item.retainedCapacity)
		globalItems[item] = struct{}{}
		retainedCapacity += int64(item.retainedCapacity)
		previous = item
	}
	require.Same(t, previous, manager.newest)
	require.Equal(t, manager.retainedBytes, retainedCapacity)
	require.LessOrEqual(t, manager.retainedBytes, manager.maxBytes)

	localItems := make(map[*PoolItem]struct{})
	for _, pool := range pools {
		require.Same(t, manager, pool.manager)
		previous = nil
		count := 0
		for item := pool.oldest; item != nil; item = item.poolNewer {
			_, exists := localItems[item]
			require.False(t, exists, "per-pool idle lists must not contain duplicate items")
			require.Same(t, previous, item.poolOlder)
			require.Same(t, pool, item.idlePool)
			_, exists = globalItems[item]
			require.True(t, exists, "every per-pool item must be present in the global LRU")
			localItems[item] = struct{}{}
			previous = item
			count++
		}
		require.Same(t, previous, pool.newest)
		require.Equal(t, pool.count, count)
	}
	require.Len(t, localItems, len(globalItems), "every global LRU item must belong to one supplied pool")
}

func TestArenaPool_Release_MovingWindow(t *testing.T) {
	pool := newTestArenaPool(maxRetainedArenaBytes)
	id := uint64(600)

	// Release exactly 50 items
	for i := 0; i < 50; i++ {
		item := pool.Acquire(id)
		buf := NewArenaBuffer(item.Arena)
		_, err := buf.WriteString("test data")
		assert.NoError(t, err)
		pool.Release(item)
	}

	// After 50 releases, verify count and total
	size := pool.sizes[id]
	require.NotNil(t, size, "size tracking should exist")
	assert.Equal(t, 50, size.count, "expected count to be 50")

	totalBytesAfter50 := size.totalBytes

	// Release one more item to trigger the window reset
	item51 := pool.Acquire(id)
	buf51 := NewArenaBuffer(item51.Arena)
	_, err := buf51.WriteString("test data")
	assert.NoError(t, err)
	usage51 := item51.Arena.Len()
	pool.Release(item51)

	// After 51st release, verify the window was reset
	// count should be 2 (reset to 1, then incremented)
	// totalBytes should be (totalBytesAfter50 / 50) + usage51
	assert.Equal(t, 2, size.count, "expected count to be 2 after window reset")

	expectedTotalBytes := (totalBytesAfter50 / 50) + usage51
	assert.Equal(t, expectedTotalBytes, size.totalBytes, "expected totalBytes to be divided by 50 and new peak added")

	// Verify we can continue releasing and counting works correctly
	for i := 0; i < 10; i++ {
		item := pool.Acquire(id)
		buf := NewArenaBuffer(item.Arena)
		_, err := buf.WriteString("more data")
		assert.NoError(t, err)
		pool.Release(item)
	}

	// After 10 more releases, count should be 12 (2 + 10)
	assert.Equal(t, 12, size.count, "expected count to continue incrementing after window reset")
}

func BenchmarkArenaPool_ParallelAcquireRelease(b *testing.B) {
	pool := newTestArenaPool(maxRetainedArenaBytes)
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			item := pool.Acquire(1000)
			pool.Release(item)
		}
	})
}

func BenchmarkArenaPool_ParallelSharedManager(b *testing.B) {
	manager := newIdleArenaManager(maxRetainedArenaBytes)
	pools := make([]*Pool, 8)
	for index := range pools {
		pools[index] = newArenaPool(newIdleArenaPool(manager))
	}
	var workers atomic.Uint64
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		pool := pools[(workers.Add(1)-1)%uint64(len(pools))]
		for pb.Next() {
			item := pool.Acquire(1000)
			pool.Release(item)
		}
	})
}

func BenchmarkArenaPool_WarmBurst16(b *testing.B) {
	benchmarkArenaPoolWarmBurst(b, 16)
}

func BenchmarkArenaPool_WarmBurst64(b *testing.B) {
	benchmarkArenaPoolWarmBurst(b, 64)
}

func BenchmarkArenaPool_WarmBurst256(b *testing.B) {
	benchmarkArenaPoolWarmBurst(b, 256)
}

func BenchmarkArenaPool_WarmBurst512(b *testing.B) {
	benchmarkArenaPoolWarmBurst(b, 512)
}

func benchmarkArenaPoolWarmBurst(b *testing.B, burstSize int) {
	pool := newTestArenaPool(maxRetainedArenaBytes)
	items := make([]*PoolItem, burstSize)
	runArenaPoolBurst(b, pool, items)
	b.Cleanup(func() {
		for {
			item := pool.pool.get()
			if item == nil {
				return
			}
			item.Arena.Release()
		}
	})
	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		runArenaPoolBurst(b, pool, items)
	}
}

func runArenaPoolBurst(b *testing.B, pool *Pool, items []*PoolItem) {
	b.Helper()
	for index := range items {
		item := pool.Acquire(1000)
		if item.Arena.Alloc(32*1024, 8) == nil {
			b.Fatal("arena allocation failed")
		}
		items[index] = item
	}
	for index, item := range items {
		pool.Release(item)
		items[index] = nil
	}
}
