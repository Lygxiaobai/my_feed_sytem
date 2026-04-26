package video

import (
	"encoding/json"
	"fmt"
	"time"

	"my_feed_system/internal/cachex"
)

const (
	localDetailCacheTTL         = 5 * time.Minute
	localDetailCacheJitterRatio = 0.2
)

type LocalDetailCache struct {
	store *cachex.BytesCache
}

func NewLocalDetailCache(store *cachex.BytesCache) *LocalDetailCache {
	if store == nil || !store.Enabled() {
		return nil
	}
	return &LocalDetailCache{store: store}
}

func (c *LocalDetailCache) Enabled() bool {
	return c != nil && c.store != nil && c.store.Enabled()
}

func (c *LocalDetailCache) Get(videoID uint64) (*Video, bool, bool, error) {
	if !c.Enabled() {
		return nil, false, false, nil
	}

	payload, ok := c.store.Get(c.key(videoID))
	if !ok {
		return nil, false, false, nil
	}

	if isDetailNotFoundPayload(payload) {
		return nil, true, true, nil
	}

	var item Video
	if err := json.Unmarshal(payload, &item); err != nil {
		c.Delete(videoID)
		return nil, false, false, err
	}

	return &item, false, true, nil
}

func (c *LocalDetailCache) SetVideo(videoID uint64, payload []byte) {
	if !c.Enabled() {
		return
	}

	ttl := cachex.TTLWithJitterRatio(localDetailCacheTTL, localDetailCacheJitterRatio)
	c.store.Set(c.key(videoID), payload, ttl)
}

func (c *LocalDetailCache) SetNotFound(videoID uint64) {
	if !c.Enabled() {
		return
	}

	ttl := cachex.TTLWithJitterRatio(detailCacheNotFoundTTL, localDetailCacheJitterRatio)
	c.store.Set(c.key(videoID), detailNotFoundMarker, ttl)
}

func (c *LocalDetailCache) Delete(videoID uint64) {
	if !c.Enabled() {
		return
	}
	c.store.Delete(c.key(videoID))
}

func (c *LocalDetailCache) key(videoID uint64) string {
	return fmt.Sprintf("video:detail:id=%d", videoID)
}
