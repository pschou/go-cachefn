package cache_test

import (
	"log"
	"testing"
	"time"

	cache "github.com/pschou/go-cachefn"
)

func TestCacheMap(t *testing.T) {
	// Create a map to serve as our cache
	cache := cache.New[string, int](3*time.Second, func(s string) int {
		//log.Println("  refreshing", s)
		return len(s)
	})

	one, ok := cache.Get("one")
	log.Println("one:", one, ok)
	one, ok = cache.Get("one")
	log.Println("one:", one, ok)
	time.Sleep(5 * time.Second)
	one, ok = cache.Get("one")
	log.Println("one:", one, ok)
}
