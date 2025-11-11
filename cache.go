package cache

import (
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

	// Cache struct holds the cache data structure and configuration
	Cache[K hashable, V any] struct {
		cacheMap    *haxmap.Map[K, *element[V]] // Map to store key-value pairs
		RefreshTime time.Duration               // How often to refresh cache entries
		refreshFunc func(K) V                   // Function to generate new values
	}

	// element struct represents a single cache entry
	element[V any] struct {
		data     V             // The cached data
		lastUsed time.Time     // When the entry was last accessed
		created  time.Time     // When the entry was created
		ready    chan struct{} // Channel to signal when data is ready
	}
)

// New creates a new cache instance with specified refresh time and refresh function
func New[K hashable, V any](RefreshTime time.Duration, refreshFunc func(K) V) *Cache[K, V] {
	// Initialize new cache with provided parameters
	c := &Cache[K, V]{
		cacheMap:    haxmap.New[K, *element[V]](),
		RefreshTime: RefreshTime,
		refreshFunc: refreshFunc,
	}

	// Start background goroutine for cache maintenance
	go func() {
		for {
			// Sleep for 1/8th of refresh time between maintenance cycles
			time.Sleep(c.RefreshTime >> 3)

			// Track keys that need to be deleted
			var toDelete []K

			// Iterate through all cache entries
			c.cacheMap.ForEach(func(key K, value *element[V]) bool {
				sinceCreated := time.Since(value.created)

				if sinceCreated > c.RefreshTime { // Remove entries older than must-refresh-time
					toDelete = append(toDelete, key)

				} else if sinceCreated < c.RefreshTime>>1 { // If this is a fresh entry
					// No operation needed

				} else if value.created.After(value.lastUsed) { // If entry has not been used in a while
					// No operation needed
					// TODO: Consider staling out data early to save memory

				} else if time.Since(value.lastUsed) < c.RefreshTime>>1 {
					// Start a refresh for ensuring data is still fresh and relevant
					data := refreshFunc(key)
					value.data, value.created = data, time.Now()
				}
				return true
			})
			// Delete all expired entries
			c.cacheMap.Del(toDelete...)
		}
	}()
	return c
}

// Get retrieves a value from the cache by key
func (c *Cache[K, V]) Get(key K) (data V, fnCalled bool) {
	// Try to get value from cache
	value, loaded := c.cacheMap.GetOrCompute(key, func() *element[V] {
		// If not found, create a new entry
		return &element[V]{
			lastUsed: time.Now(),
			created:  time.Now(),
			ready:    make(chan struct{}, 1),
		}
	})

	if loaded {
		// Wait for data to be ready
		<-value.ready
		value.lastUsed = time.Now()
		return value.data, false
	}

	// Signal that data is ready on close
	defer close(value.ready)

	// Pull the data and set the data
	value.data, value.created = c.refreshFunc(key), time.Now()
	return value.data, true
}
