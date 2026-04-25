package feed

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const defaultLatestFeedCacheTTL = 5 * time.Second

// LatestCache 封装最新流缓存。
type LatestCache struct {
	client redis.Cmdable
	ttl    time.Duration
}

// NewLatestCache 使用默认 TTL 创建最新流缓存。
func NewLatestCache(client redis.Cmdable) *LatestCache {
	return NewLatestCacheWithTTL(client, defaultLatestFeedCacheTTL)
}

// NewLatestCacheWithTTL 使用指定 TTL 创建最新流缓存。
func NewLatestCacheWithTTL(client redis.Cmdable, ttl time.Duration) *LatestCache {
	if ttl <= 0 {
		ttl = defaultLatestFeedCacheTTL
	}

	return &LatestCache{
		client: client,
		ttl:    ttl,
	}
}

// Enabled 判断最新流缓存是否可用。
func (c *LatestCache) Enabled() bool {
	return c != nil && c.client != nil
}

// Get 读取最新流缓存；未命中时 ok=false。
func (c *LatestCache) Get(ctx context.Context, req ListLatestRequest) (*ListLatestResult, bool, error) {
	if !c.Enabled() {
		return nil, false, nil
	}

	payload, err := c.client.Get(ctx, c.key(req)).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, false, nil
		}
		return nil, false, err
	}

	var result ListLatestResult
	if err := json.Unmarshal([]byte(payload), &result); err != nil {
		return nil, false, err
	}

	return &result, true, nil
}

// Set 写入最新流缓存。
func (c *LatestCache) Set(ctx context.Context, req ListLatestRequest, result *ListLatestResult) error {
	if !c.Enabled() || result == nil {
		return nil
	}

	payload, err := json.Marshal(result)
	if err != nil {
		return err
	}

	return c.client.Set(ctx, c.key(req), payload, c.ttl).Err()
}

// DeleteAll 删除最新流的分页结果缓存，供时间线更新后统一失效。
func (c *LatestCache) DeleteAll(ctx context.Context) error {
	if !c.Enabled() {
		return nil
	}

	var cursor uint64
	for {
		keys, nextCursor, err := c.client.Scan(ctx, cursor, "feed:listLatest:*", 100).Result()
		if err != nil {
			return err
		}
		if len(keys) > 0 {
			if err := c.client.Del(ctx, keys...).Err(); err != nil {
				return err
			}
		}

		cursor = nextCursor
		if cursor == 0 {
			return nil
		}
	}
}

func (c *LatestCache) key(req ListLatestRequest) string {
	return fmt.Sprintf(
		"feed:listLatest:limit=%d:latest=%d:last_id=%d",
		req.Limit,
		req.LatestTime,
		req.LastID,
	)
}
