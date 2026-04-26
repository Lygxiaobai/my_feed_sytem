package feed

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"my_feed_system/internal/cachex"
)

const (
	defaultLatestFeedCacheTTL      = 15 * time.Second
	defaultLatestFeedEmptyCacheTTL = 3 * time.Second
	defaultHotFeedCacheTTL         = 5 * time.Second
	defaultHotFeedEmptyCacheTTL    = 2 * time.Second
	feedCacheJitterRatio           = 0.2
	latestFeedVersionKey           = "feed:listLatest:version"
)

// LatestCache 封装 latest 流的 Redis 分页缓存，以及版本号读写。
type LatestCache struct {
	client   redis.Cmdable
	ttl      time.Duration
	emptyTTL time.Duration
}

func NewLatestCache(client redis.Cmdable) *LatestCache {
	return NewLatestCacheWithTTL(client, defaultLatestFeedCacheTTL)
}

func NewLatestCacheWithTTL(client redis.Cmdable, ttl time.Duration) *LatestCache {
	if ttl <= 0 {
		ttl = defaultLatestFeedCacheTTL
	}

	return &LatestCache{
		client:   client,
		ttl:      ttl,
		emptyTTL: defaultLatestFeedEmptyCacheTTL,
	}
}

func (c *LatestCache) Enabled() bool {
	return c != nil && c.client != nil
}

func (c *LatestCache) GetVersion(ctx context.Context) (int64, error) {
	if !c.Enabled() {
		return 1, nil
	}

	// 先兜底初始化版本号，避免首次读时因为 key 不存在而走异常分支。
	if err := c.client.SetNX(ctx, latestFeedVersionKey, 1, 0).Err(); err != nil {
		return 0, err
	}

	version, err := c.client.Get(ctx, latestFeedVersionKey).Int64()
	if err != nil {
		return 0, err
	}
	if version <= 0 {
		return 1, nil
	}

	return version, nil
}

func (c *LatestCache) BumpVersion(ctx context.Context) (int64, error) {
	if !c.Enabled() {
		return 1, nil
	}

	// 版本号只增不删，旧页 key 交给 TTL 自然淘汰。
	if err := c.client.SetNX(ctx, latestFeedVersionKey, 1, 0).Err(); err != nil {
		return 0, err
	}

	return c.client.Incr(ctx, latestFeedVersionKey).Result()
}

func (c *LatestCache) Get(ctx context.Context, version int64, req ListLatestRequest) (*ListLatestResult, bool, error) {
	if !c.Enabled() {
		return nil, false, nil
	}

	payload, err := c.client.Get(ctx, c.key(version, req)).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, false, nil
		}
		return nil, false, err
	}

	var result ListLatestResult
	if err := json.Unmarshal(payload, &result); err != nil {
		return nil, false, err
	}

	return &result, true, nil
}

func (c *LatestCache) Set(ctx context.Context, version int64, req ListLatestRequest, result *ListLatestResult) error {
	if !c.Enabled() || result == nil {
		return nil
	}

	payload, err := json.Marshal(result)
	if err != nil {
		return err
	}

	ttl := c.ttlForResult(result)
	return c.client.Set(ctx, c.key(version, req), payload, ttl).Err()
}

func (c *LatestCache) key(version int64, req ListLatestRequest) string {
	return fmt.Sprintf(
		"feed:listLatest:v=%d:limit=%d:latest=%d:last_id=%d",
		version,
		req.Limit,
		req.LatestTime,
		req.LastID,
	)
}

func (c *LatestCache) ttlForResult(result *ListLatestResult) time.Duration {
	base := c.ttl
	if result != nil && len(result.Videos) == 0 {
		// 空页更容易受新发布影响，使用更短 TTL 做负缓存。
		base = c.emptyTTL
	}
	return cachex.TTLWithJitterRatio(base, feedCacheJitterRatio)
}

// HotPageCache 封装 hot 榜最终组装结果页的 Redis 缓存。
type HotPageCache struct {
	client   redis.Cmdable
	ttl      time.Duration
	emptyTTL time.Duration
}

func NewHotPageCache(client redis.Cmdable) *HotPageCache {
	return NewHotPageCacheWithTTL(client, defaultHotFeedCacheTTL)
}

func NewHotPageCacheWithTTL(client redis.Cmdable, ttl time.Duration) *HotPageCache {
	if ttl <= 0 {
		ttl = defaultHotFeedCacheTTL
	}

	return &HotPageCache{
		client:   client,
		ttl:      ttl,
		emptyTTL: defaultHotFeedEmptyCacheTTL,
	}
}

func (c *HotPageCache) Enabled() bool {
	return c != nil && c.client != nil
}

func (c *HotPageCache) Get(ctx context.Context, req ListByPopularityRequest) (*ListByPopularityResult, bool, error) {
	if !c.Enabled() {
		return nil, false, nil
	}

	payload, err := c.client.Get(ctx, c.key(req)).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, false, nil
		}
		return nil, false, err
	}

	var result ListByPopularityResult
	if err := json.Unmarshal(payload, &result); err != nil {
		return nil, false, err
	}

	return &result, true, nil
}

func (c *HotPageCache) Set(ctx context.Context, req ListByPopularityRequest, result *ListByPopularityResult) error {
	if !c.Enabled() || result == nil {
		return nil
	}

	payload, err := json.Marshal(result)
	if err != nil {
		return err
	}

	ttl := c.ttlForResult(result)
	return c.client.Set(ctx, c.key(req), payload, ttl).Err()
}

func (c *HotPageCache) key(req ListByPopularityRequest) string {
	return fmt.Sprintf(
		"feed:listHot:asof=%d:limit=%d:offset=%d",
		req.AsOf,
		req.Limit,
		req.Offset,
	)
}

func (c *HotPageCache) ttlForResult(result *ListByPopularityResult) time.Duration {
	base := c.ttl
	if result != nil && len(result.Videos) == 0 {
		// 热榜空页一般是短暂状态，避免缓存过久。
		base = c.emptyTTL
	}
	return cachex.TTLWithJitterRatio(base, feedCacheJitterRatio)
}
