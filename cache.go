package cache

import (
	"context"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"github.com/alphadose/haxmap"
	"golang.org/x/exp/constraints"
)

// hashable interface defines types that can be used as cache keys
type (
	hashable interface {
		constraints.Integer | constraints.Float | constraints.Complex | ~string | uintptr | ~unsafe.Pointer
	}

	// Cache holds the cache data structure and configuration
	Cache[K hashable, V any] struct {
		cacheMap    *haxmap.Map[K, *element[V]]                  // Map to store key-value pairs
		RefreshTime time.Duration                                // How often to refresh cache entries
		KeepTime    time.Duration                                // How long to keep cache entries before deleting
		refreshFunc func(context.Context, K) (val V, store bool) // Function to generate new values
		ctx         context.Context                              // Flag to indicate if cache is active
		cancel      context.CancelFunc
	}

	// element struct represents a single cache entry
	element[V any] struct {
		data     V             // The cached data
		lastUsed time.Time     // When the entry was last accessed
		created  time.Time     // When the entry was created
		ready    chan struct{} // Channel to signal when data is ready
	}

	// CacheMap holds the cache data structure and configuration
	CacheMap[K hashable, V any] struct {
		cacheMap    *haxmap.Map[K, *mapElement[V]]                 // Map to store key-value pairs
		RefreshTime time.Duration                                  // How often to refresh cache entries
		KeepTime    time.Duration                                  // How long to keep cache entries before deleting
		lastRefresh time.Time                                      // Time of the last refresh
		refreshFunc func(context.Context, func(K, V)) (store bool) // Function to generate all new values
		ctx         context.Context                                // Flag to indicate if cache is active
		cancel      context.CancelFunc

		ready chan struct{} // Channel to signal when data is ready
	}

	// element struct represents a single cache entry
	mapElement[V any] struct {
		data    V         // The cached data
		created time.Time // When the entry was created
	}
)

// New creates a new cache instance with specified refresh time and refresh function
func New[K hashable, V any](RefreshTime, KeepTime time.Duration,
	refreshFunc func(context.Context, K) (V, bool)) *Cache[K, V] {

	// Initialize new cache with provided parameters
	c := &Cache[K, V]{
		cacheMap:    haxmap.New[K, *element[V]](),
		RefreshTime: RefreshTime,
		KeepTime:    KeepTime,
		refreshFunc: refreshFunc,
	}
	c.ctx, c.cancel = context.WithCancel(context.Background())

	runtime.AddCleanup(c, func(int) {
		c.cancel()
		c.cacheMap.Clear()
	}, 0)

	// Start background goroutine for cache maintenance
	go func() {
		for c.ctx.Err() == nil {
			// Sleep for 1/4th of refresh time between maintenance cycles
			time.Sleep(c.RefreshTime >> 2)

			// Test if c.ctx is done
			if c.ctx.Err() != nil {
				break
			}

			// Track keys that need to be deleted
			var toDelete []K

			// Iterate through all cache entries
			c.cacheMap.ForEach(func(key K, value *element[V]) bool {
				// Test if c.ctx is done
				if c.ctx.Err() != nil {
					return false
				}

				sinceCreated := time.Since(value.created)

				if c.KeepTime > 0 && sinceCreated > c.KeepTime { // Remove entries older than must-refresh-time
					toDelete = append(toDelete, key)

				} else if sinceCreated < c.RefreshTime { // If this is a fresh entry
					// No operation needed

				} else if value.created.After(value.lastUsed) { // If entry has not been used in a while
					// No operation needed
					// TODO: Consider staling out data early to save memory

				} else if time.Since(value.lastUsed) < c.RefreshTime>>1 {
					withTimeout, _ := context.WithTimeout(c.ctx, c.RefreshTime>>1)

					// Start a refresh for ensuring data is still fresh and relevant
					data, ok := refreshFunc(withTimeout, key)
					if !ok {
						return true
					}
					value.data, value.created = data, time.Now()
				}
				return true
			})
			// Delete all expired entries
			c.cacheMap.Del(toDelete...)
		}

		c.cacheMap.Clear()
		runtime.GC()
	}()
	return c
}

// Get retrieves a value from the cache by key
func (c *Cache[K, V]) Get(ctx context.Context, key K) (data V, ready bool) {
	// Try to get value from cache
	value, loaded := c.cacheMap.GetOrCompute(key, func() *element[V] {
		// If not found, create a new entry
		return &element[V]{
			created: time.Now(),
			ready:   make(chan struct{}, 1),
		}
	})

	if loaded {
		// Wait for data to be ready
		// If ctx is cancelled or c is not ready
		if value.ready != nil {
			select {
			case <-ctx.Done(): // return immediately
				return value.data, false
			case <-value.ready: // wait for the map to be populated
			}
		}

		if value.lastUsed.IsZero() {
			return value.data, false
		}
		value.lastUsed = time.Now()
		return value.data, true
	}

	// Signal that data is ready on close
	defer close(value.ready)

	// Pull the data and set the data
	data, ok := c.refreshFunc(ctx, key)
	if ok {
		value.data, value.lastUsed = data, time.Now()
	}
	return value.data, ok
}

// Set manually add a value to the cache for use
func (c *Cache[K, V]) Set(key K, value V) {
	now := time.Now()
	elm := &element[V]{
		data:     value,
		created:  now,
		lastUsed: now,
	}
	c.cacheMap.Set(key, elm)
}

// New creates a new cache instance with specified refresh time and refresh function
func NewMap[K hashable, V any](RefreshTime, KeepTime time.Duration,
	refreshFunc func(context.Context, func(K, V)) bool) *CacheMap[K, V] {
	// Initialize new cache with provided parameters
	c := &CacheMap[K, V]{
		cacheMap:    haxmap.New[K, *mapElement[V]](),
		RefreshTime: RefreshTime,
		KeepTime:    KeepTime,
		refreshFunc: refreshFunc,
		ready:       make(chan struct{}),
	}
	c.ctx, c.cancel = context.WithCancel(context.Background())
	ready := sync.OnceFunc(func() {
		close(c.ready)
	})

	runtime.AddCleanup(c, func(int) {
		c.cancel()
		c.cacheMap.Clear()
	}, 0)

	// Start background goroutine for cache maintenance
	go func() {
		defer ready() // If the service is cancelled, release any holds

		start := time.Now() // Mark the start of the refresh interval
		if c.refreshFunc(c.ctx, func(key K, val V) {
			c.cacheMap.Set(key, &mapElement[V]{
				data:    val,
				created: time.Now(),
			})
		}) && c.ctx.Err() == nil {
			c.lastRefresh = start
			ready()
		}

		for c.ctx.Err() == nil {
			// Sleep for 1/4th of refresh time between maintenance cycles
			time.Sleep(c.RefreshTime >> 2)

			if c.ctx.Err() != nil {
				return
			}

			// Sleep for 1/4th of refresh time between maintenance cycles
			time.Sleep(c.RefreshTime >> 4)

			// Track keys that need to be deleted
			var toDelete []K

			// Iterate through all cache entries
			c.cacheMap.ForEach(func(key K, value *mapElement[V]) bool {
				sinceCreated := time.Since(value.created)

				if sinceCreated > c.KeepTime { // Remove entries older than must-refresh-time
					toDelete = append(toDelete, key)
				}
				return true
			})
			// Delete all expired entries
			c.cacheMap.Del(toDelete...)

			if time.Since(c.lastRefresh) < c.RefreshTime {
				continue
			}

			start := time.Now() // Mark the start of the refresh interval
			if c.refreshFunc(c.ctx, func(key K, val V) {
				c.cacheMap.Set(key, &mapElement[V]{
					data:    val,
					created: time.Now(),
				})
			}) && c.ctx.Err() == nil {
				c.lastRefresh = start
				ready()
			}
		}
	}()
	return c
}

// Get retrieves a value from the cache by key
func (c *CacheMap[K, V]) Get(ctx context.Context, key K) (data V, found bool) {
	// If ctx is cancelled or c is not ready
	select {
	case <-ctx.Done(): // return immediately
		return
	case <-c.ready: // wait for the map to be populated
	}

	// Try to get value from cache
	value, loaded := c.cacheMap.Get(key)
	if loaded {
		return value.data, true
	}
	return
}
