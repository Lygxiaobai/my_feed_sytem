package video

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"my_feed_system/internal/cachex"
)

const defaultDetailCacheTTL = 5 * time.Minute
const detailCacheJitterRatio = 0.2

// DetailCache 封装 video detail 的 Redis L2 缓存。
type DetailCache struct {
	client redis.Cmdable
	ttl    time.Duration
}

func NewDetailCache(client redis.Cmdable) *DetailCache {
	return NewDetailCacheWithTTL(client, defaultDetailCacheTTL)
}

func NewDetailCacheWithTTL(client redis.Cmdable, ttl time.Duration) *DetailCache {
	if ttl <= 0 {
		ttl = defaultDetailCacheTTL
	}

	return &DetailCache{
		client: client,
		ttl:    ttl,
	}
}

func (c *DetailCache) Enabled() bool {
	return c != nil && c.client != nil
}

func (c *DetailCache) Get(ctx context.Context, videoID uint64) (*Video, bool, error) {
	payload, ok, err := c.GetRaw(ctx, videoID)
	if err != nil || !ok {
		return nil, ok, err
	}

	var item Video
	if err := json.Unmarshal(payload, &item); err != nil {
		return nil, false, err
	}

	return &item, true, nil
}

func (c *DetailCache) GetRaw(ctx context.Context, videoID uint64) ([]byte, bool, error) {
	if !c.Enabled() {
		return nil, false, nil
	}

	payload, err := c.client.Get(ctx, c.key(videoID)).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, false, nil
		}
		return nil, false, err
	}

	return payload, true, nil
}

func (c *DetailCache) Set(ctx context.Context, item *Video) error {
	if !c.Enabled() || item == nil {
		return nil
	}

	payload, err := json.Marshal(item)
	if err != nil {
		return err
	}

	return c.SetRaw(ctx, item.ID, payload)
}

func (c *DetailCache) SetRaw(ctx context.Context, videoID uint64, payload []byte) error {
	if !c.Enabled() || len(payload) == 0 {
		return nil
	}

	// detail key 加 jitter，避免大量热点视频同时过期。
	ttl := cachex.TTLWithJitterRatio(c.ttl, detailCacheJitterRatio)
	return c.client.Set(ctx, c.key(videoID), payload, ttl).Err()
}

func (c *DetailCache) SetNotFound(ctx context.Context, videoID uint64) error {
	if !c.Enabled() {
		return nil
	}

	// not found 做短负缓存，降低穿透到 DB 的频率。
	ttl := cachex.TTLWithJitterRatio(detailCacheNotFoundTTL, detailCacheJitterRatio)
	return c.client.Set(ctx, c.key(videoID), detailNotFoundMarker, ttl).Err()
}

func (c *DetailCache) Delete(ctx context.Context, videoID uint64) error {
	if !c.Enabled() {
		return nil
	}

	return c.client.Del(ctx, c.key(videoID)).Err()
}

func (c *DetailCache) key(videoID uint64) string {
	return fmt.Sprintf("video:detail:id=%d", videoID)
}
