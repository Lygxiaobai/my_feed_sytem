package cachex

import (
	"fmt"
	"time"

	"github.com/dgraph-io/ristretto"

	"my_feed_system/internal/observability"
)

const (
	defaultNumCounters = 1 << 16
	defaultMaxCost     = 32 << 20
	defaultBufferItems = 64
)

type BytesCache struct {
	name  string
	cache *ristretto.Cache
}

func NewBytesCache(name string, maxCost int64) (*BytesCache, error) {
	if maxCost <= 0 {
		maxCost = defaultMaxCost
	}

	cache, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: defaultNumCounters,
		MaxCost:     maxCost,
		BufferItems: defaultBufferItems,
		OnEvict: func(item *ristretto.Item) {
			observability.IncCacheL1Evict(name)
		},
	})
	if err != nil {
		return nil, fmt.Errorf("create ristretto cache: %w", err)
	}

	return &BytesCache{
		name:  name,
		cache: cache,
	}, nil
}

func (c *BytesCache) Enabled() bool {
	return c != nil && c.cache != nil
}

func (c *BytesCache) Get(key string) ([]byte, bool) {
	if !c.Enabled() {
		return nil, false
	}

	value, ok := c.cache.Get(key)
	if !ok {
		return nil, false
	}

	payload, ok := value.([]byte)
	if !ok {
		c.cache.Del(key)
		return nil, false
	}

	return cloneBytes(payload), true
}

func (c *BytesCache) Set(key string, payload []byte, ttl time.Duration) bool {
	if !c.Enabled() {
		return false
	}

	buf := cloneBytes(payload)
	cost := int64(len(key) + len(buf))
	if cost <= 0 {
		cost = 1
	}

	return c.cache.SetWithTTL(key, buf, cost, ttl)
}

func (c *BytesCache) Delete(key string) {
	if !c.Enabled() {
		return
	}
	c.cache.Del(key)
}

func (c *BytesCache) Clear() {
	if !c.Enabled() {
		return
	}
	c.cache.Clear()
}

func (c *BytesCache) Wait() {
	if !c.Enabled() {
		return
	}
	c.cache.Wait()
}

func (c *BytesCache) Close() {
	if !c.Enabled() {
		return
	}
	c.cache.Close()
}

func cloneBytes(src []byte) []byte {
	if len(src) == 0 {
		return nil
	}
	dst := make([]byte, len(src))
	copy(dst, src)
	return dst
}
