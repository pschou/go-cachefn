package cache_test

import (
	"context"
	"fmt"
	"log"
	"testing"
	"time"

	cache "github.com/pschou/go-cachefn"
)

func TestCache(t *testing.T) {
	// Create a map to serve as our cache
	cache := cache.New[string, int](3*time.Second, time.Hour, func(ctx context.Context, s string) (int, bool) {
		log.Println("  refreshing", s)
		return len(s), true
	})

	ctx := context.Background()

	one, ok := cache.Get(ctx, "one")
	log.Println("one:", one, ok)
	one, ok = cache.Get(ctx, "one")
	log.Println("one:", one, ok)

	log.Println("sleep 2")
	time.Sleep(2 * time.Second)

	one, ok = cache.Get(ctx, "one")
	log.Println("one:", one, ok)

	log.Println("sleep 5")
	time.Sleep(5 * time.Second)

	one, ok = cache.Get(ctx, "one")
	log.Println("one:", one, ok)
}

func TestCacheMap(t *testing.T) {
	// Create a map to serve as our cache
	cache := cache.NewMap[string, int](4*time.Second, time.Hour, func(ctx context.Context, set func(s string, v int)) bool {
		log.Println("  refreshing map")
		time.Sleep(1 * time.Second)
		for i := 0; i < 10 && ctx.Err() == nil; i++ {
			set(fmt.Sprintf("%d", i), i)
		}
		return true
	})

	ctx := context.Background()

	log.Println("looking up 1,2,3")
	one, ok := cache.Get(ctx, "1")
	log.Println("1:", one, ok)
	one, ok = cache.Get(ctx, "2")
	log.Println("2:", one, ok)

	log.Println("sleep 6")
	time.Sleep(6 * time.Second)

	one, ok = cache.Get(ctx, "3")
	log.Println("3:", one, ok)
}
