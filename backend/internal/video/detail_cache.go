package video

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const defaultDetailCacheTTL = 5 * time.Minute

// DetailCache 封装视频详情缓存。
type DetailCache struct {
	client redis.Cmdable
	ttl    time.Duration
}

// NewDetailCache 使用默认 TTL 创建视频详情缓存。
func NewDetailCache(client redis.Cmdable) *DetailCache {
	return NewDetailCacheWithTTL(client, defaultDetailCacheTTL)
}

// NewDetailCacheWithTTL 使用指定 TTL 创建视频详情缓存。
func NewDetailCacheWithTTL(client redis.Cmdable, ttl time.Duration) *DetailCache {
	if ttl <= 0 {
		ttl = defaultDetailCacheTTL
	}

	return &DetailCache{
		client: client,
		ttl:    ttl,
	}
}

// Enabled 判断详情缓存是否可用。
func (c *DetailCache) Enabled() bool {
	return c != nil && c.client != nil
}

// Get 读取缓存中的视频详情；未命中时 ok=false。
func (c *DetailCache) Get(ctx context.Context, videoID uint64) (*Video, bool, error) {
	if !c.Enabled() {
		return nil, false, nil
	}

	payload, err := c.client.Get(ctx, c.key(videoID)).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, false, nil
		}
		return nil, false, err
	}

	var item Video
	if err := json.Unmarshal([]byte(payload), &item); err != nil {
		return nil, false, err
	}

	return &item, true, nil
}

// Set 写入视频详情缓存。
func (c *DetailCache) Set(ctx context.Context, item *Video) error {
	if !c.Enabled() || item == nil {
		return nil
	}

	payload, err := json.Marshal(item)
	if err != nil {
		return err
	}

	return c.client.Set(ctx, c.key(item.ID), payload, c.ttl).Err()
}

// Delete 删除视频详情缓存。
func (c *DetailCache) Delete(ctx context.Context, videoID uint64) error {
	if !c.Enabled() {
		return nil
	}

	return c.client.Del(ctx, c.key(videoID)).Err()
}

func (c *DetailCache) key(videoID uint64) string {
	return fmt.Sprintf("video:detail:id=%d", videoID)
}
