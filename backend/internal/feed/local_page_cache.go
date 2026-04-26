package feed

import (
	"encoding/json"
	"fmt"
	"time"

	"my_feed_system/internal/cachex"
)

// 首页和热榜的L1缓存
const (
	localLatestFeedCacheTTL      = 2 * time.Second
	localLatestFeedEmptyCacheTTL = 1 * time.Second
	localHotFeedCacheTTL         = 5 * time.Second
	localHotFeedEmptyCacheTTL    = 2 * time.Second
)

type LocalLatestPageCache struct {
	store *cachex.BytesCache
}

func NewLocalLatestPageCache(store *cachex.BytesCache) *LocalLatestPageCache {
	if store == nil || !store.Enabled() {
		return nil
	}
	return &LocalLatestPageCache{store: store}
}

func (c *LocalLatestPageCache) Enabled() bool {
	return c != nil && c.store != nil && c.store.Enabled()
}

func (c *LocalLatestPageCache) Get(version int64, req ListLatestRequest) (*ListLatestResult, bool, error) {
	if !c.Enabled() {
		return nil, false, nil
	}

	payload, ok := c.store.Get(c.key(version, req))
	if !ok {
		return nil, false, nil
	}

	var result ListLatestResult
	if err := json.Unmarshal(payload, &result); err != nil {
		c.store.Delete(c.key(version, req))
		return nil, false, err
	}

	return &result, true, nil
}

func (c *LocalLatestPageCache) Set(version int64, req ListLatestRequest, result *ListLatestResult) {
	if !c.Enabled() || result == nil {
		return
	}

	payload, err := json.Marshal(result)
	if err != nil {
		return
	}

	base := localLatestFeedCacheTTL
	if len(result.Videos) == 0 {
		// latest 首屏变化快，空页只做秒级短缓存。
		base = localLatestFeedEmptyCacheTTL
	}
	c.store.Set(c.key(version, req), payload, cachex.TTLWithJitterRatio(base, feedCacheJitterRatio))
}

func (c *LocalLatestPageCache) Clear() {
	if !c.Enabled() {
		return
	}
	// latest 的 L1 key 带 version，收到版本 bump 事件后整表清空最简单可靠。
	c.store.Clear()
}

func (c *LocalLatestPageCache) key(version int64, req ListLatestRequest) string {
	return fmt.Sprintf(
		"feed:l1:listLatest:v=%d:limit=%d:latest=%d:last_id=%d",
		version,
		req.Limit,
		req.LatestTime,
		req.LastID,
	)
}

type LocalHotPageCache struct {
	store *cachex.BytesCache
}

func NewLocalHotPageCache(store *cachex.BytesCache) *LocalHotPageCache {
	if store == nil || !store.Enabled() {
		return nil
	}
	return &LocalHotPageCache{store: store}
}

func (c *LocalHotPageCache) Enabled() bool {
	return c != nil && c.store != nil && c.store.Enabled()
}

func (c *LocalHotPageCache) Get(req ListByPopularityRequest) (*ListByPopularityResult, bool, error) {
	if !c.Enabled() {
		return nil, false, nil
	}

	payload, ok := c.store.Get(c.key(req))
	if !ok {
		return nil, false, nil
	}

	var result ListByPopularityResult
	if err := json.Unmarshal(payload, &result); err != nil {
		c.store.Delete(c.key(req))
		return nil, false, err
	}

	return &result, true, nil
}

func (c *LocalHotPageCache) Set(req ListByPopularityRequest, result *ListByPopularityResult) {
	if !c.Enabled() || result == nil {
		return
	}

	payload, err := json.Marshal(result)
	if err != nil {
		return
	}

	base := localHotFeedCacheTTL
	if len(result.Videos) == 0 {
		// 空热榜通常是瞬时状态，负缓存不宜过长。
		base = localHotFeedEmptyCacheTTL
	}
	c.store.Set(c.key(req), payload, cachex.TTLWithJitterRatio(base, feedCacheJitterRatio))
}

func (c *LocalHotPageCache) key(req ListByPopularityRequest) string {
	return fmt.Sprintf(
		"feed:l1:listHot:asof=%d:limit=%d:offset=%d",
		req.AsOf,
		req.Limit,
		req.Offset,
	)
}
